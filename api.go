package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// prodoctorovBase подменяется в тестах.
var prodoctorovBase = "https://prodoctorov.ru"

// errAntibot — антибот prodoctorov отдал HTML-заглушку (unblock/капча) со
// статусом 200 вместо JSON. Обёрнутая через errors.Is: по ней main.go решает,
// что в Telegram нужен алерт с инструкцией пройти капчу в браузере.
var errAntibot = errors.New("prodoctorov отдал проверку браузера вместо данных (нужна капча)")

// Параметры ретрая POST slots_bulk при попадании на антибот-заглушку.
// Заглушка часто «моргает» на минуты — несколько попыток с новым профилем
// её обычно пересиживают, не поднимая ложного алерта.
// Вынесены в переменные, чтобы тесты не ждали реальных пауз.
var (
	antibotMaxAttempts = 3
	antibotRetryDelay  = 3 * time.Second
)

// swapRetryDelay временно выставляет паузу ретрая (для тестов) и возвращает
// функцию восстановления прежнего значения.
func swapRetryDelay(d time.Duration) func() {
	old := antibotRetryDelay
	antibotRetryDelay = d
	return func() { antibotRetryDelay = old }
}

// looksLikeAntibot распознаёт HTML-заглушку антибота в ответе JSON-API: тело
// начинается с '<' (HTML-документ) или Content-Type — html. Настоящий ответ
// API — application/json. Годится ТОЛЬКО для ответов API: страница врача сама
// по себе HTML, для неё есть looksLikeChallenge.
func looksLikeAntibot(contentType string, body []byte) bool {
	if strings.Contains(strings.ToLower(contentType), "html") {
		return true
	}
	return bytes.HasPrefix(bytes.TrimSpace(body), []byte("<"))
}

// looksLikeChallenge распознаёт JS-заглушку антибота на месте HTML-страницы.
// Отличить по «это HTML» нельзя — настоящая страница врача тоже HTML, поэтому
// ищем загрузчик ServicePipe, который есть только в заглушке.
func looksLikeChallenge(body []byte) bool {
	return bytes.Contains(bytes.ToLower(body), []byte("servicepipe"))
}

type slotsResponse struct {
	Result []struct {
		DoctorID int `json:"doctor_id"`
		LpuID    int `json:"lpu_id"`
		Slots    map[string][]struct {
			Time     string `json:"time"`
			Free     bool   `json:"free"`
			Duration int    `json:"duration"`
		} `json:"slots"`
	} `json:"result"`
}

type freeSlot struct {
	Lpu  int
	Date string
	Time string
}

// fetchSlots опрашивает API и возвращает все свободные слоты врача.
// При попадании на антибот-заглушку делает до antibotMaxAttempts попыток с
// новой «личностью» (профилем): короткая заглушка так пересиживается без
// ложного алерта. Если заглушка держится все попытки — возвращает errAntibot.
//
// csrf-токен и cookie здесь не нужны: slots_bulk отдаёт данные по одним только
// браузерным заголовкам (проверено 13.09.2026 — 8 из 8 запросов с пустым cookie
// jar и без X-CSRFToken вернули настоящий JSON). Раньше перед каждым POST шёл
// GET страницы врача ради токена — именно этот GET упирался в антибот-заглушку
// и ронял весь опрос, хотя охраняемый им POST в токене не нуждался.
func fetchSlots(c *http.Client, doc DoctorInfo) ([]freeSlot, error) {
	if len(doc.Clinics) == 0 {
		return nil, fmt.Errorf("у врача не найдено ни одной клиники — нечего опрашивать")
	}

	type dl struct {
		DoctorID          int         `json:"doctor_id"`
		LpuID             int         `json:"lpu_id"`
		LpuTimedelta      int         `json:"lpu_timedelta"`
		HasSlots          bool        `json:"has_slots"`
		TelemedPrice      interface{} `json:"telemed_price"`
		TelemedMedtochkaP interface{} `json:"telemed_medtochka_price"`
	}
	var docs []dl
	for _, cl := range doc.Clinics {
		docs = append(docs, dl{DoctorID: doc.DoctorID, LpuID: cl.LpuID, LpuTimedelta: cl.Timedelta, HasSlots: true})
	}
	body := map[string]interface{}{
		"days":           30,
		"dt_start":       time.Now().Format("2006-01-02"),
		"town_timedelta": doc.Clinics[0].Timedelta,
		"doctors_lpus":   docs,
	}
	bb, _ := json.Marshal(body)

	var raw []byte
	for attempt := 0; attempt < antibotMaxAttempts; attempt++ {
		if attempt > 0 && antibotRetryDelay > 0 {
			time.Sleep(antibotRetryDelay)
		}
		// Новая «личность» на каждую попытку — иначе повторяем тот же
		// подозрительный для антибота запрос.
		profile := pickProfile()

		req, _ := http.NewRequest("POST", prodoctorovBase+"/ajax/schedule/slots_bulk/", bytes.NewReader(bb))
		setBrowserHeaders(req, false, profile)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Api-Scope", "browser")
		req.Header.Set("Referer", doc.URL)
		req.Header.Set("Origin", prodoctorovBase)

		resp, err := c.Do(req)
		if err != nil {
			return nil, err
		}
		raw, _ = io.ReadAll(resp.Body)
		ct := resp.Header.Get("Content-Type")
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
		}
		if looksLikeAntibot(ct, raw) {
			continue // заглушка — пробуем ещё раз новой личностью
		}

		var sr slotsResponse
		if err := json.Unmarshal(raw, &sr); err != nil {
			return nil, fmt.Errorf("не удалось разобрать ответ API: %w; тело=%s", err, truncate(string(raw), 200))
		}
		return collectFreeSlots(sr), nil
	}
	// Все попытки упёрлись в заглушку — это капча/затяжная блокировка.
	return nil, fmt.Errorf("%w (после %d попыток)", errAntibot, antibotMaxAttempts)
}

// collectFreeSlots вытаскивает из ответа API только свободные слоты,
// отсортированные по дате и времени.
func collectFreeSlots(sr slotsResponse) []freeSlot {
	var free []freeSlot
	for _, d := range sr.Result {
		for date, slots := range d.Slots {
			for _, s := range slots {
				if s.Free {
					free = append(free, freeSlot{Lpu: d.LpuID, Date: date, Time: s.Time})
				}
			}
		}
	}
	sort.Slice(free, func(i, j int) bool {
		if free[i].Date != free[j].Date {
			return free[i].Date < free[j].Date
		}
		return free[i].Time < free[j].Time
	})
	return free
}

// truncate обрезает s до не более n байт, сохраняя валидность UTF-8
// (обрезка до границы руны) — иначе Telegram отклонит сообщение
// с разорванной посреди руны кириллицей.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// signature — стабильная подпись набора слотов для дедупликации уведомлений.
func signature(slots []freeSlot) string {
	parts := make([]string, 0, len(slots))
	for _, s := range slots {
		parts = append(parts, fmt.Sprintf("%d|%s|%s", s.Lpu, s.Date, s.Time))
	}
	return strings.Join(parts, ",")
}

func clinicLabel(lpu int, clinics map[int]Clinic) string {
	if c, ok := clinics[lpu]; ok && c.Name != "" {
		return c.Name
	}
	return fmt.Sprintf("клиника %d", lpu)
}

var ruMonths = [...]string{"", "января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря"}

var ruWeekdays = map[time.Weekday]string{
	time.Monday: "пн", time.Tuesday: "вт", time.Wednesday: "ср",
	time.Thursday: "чт", time.Friday: "пт", time.Saturday: "сб", time.Sunday: "вс",
}

// humanDate — «2026-07-28» → «28 июля, вт». При ошибке разбора возвращает исходную строку.
func humanDate(d string) string {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return d
	}
	return fmt.Sprintf("%d %s, %s", t.Day(), ruMonths[t.Month()], ruWeekdays[t.Weekday()])
}

// formatSlots — слоты, сгруппированные сначала по клинике, внутри — по дате.
// Название клиники выносится в заголовок блока один раз, а не повторяется у
// каждого дня: при одной клинике сообщение читается как компактный список
// дней, при нескольких — как отдельные блоки. Каждый день — одна строка
// «дата — времена через запятую».
func formatSlots(slots []freeSlot, clinics map[int]Clinic) string {
	// Группируем по клинике (в порядке появления), внутри — по дате.
	byClinic := map[int]map[string][]string{}
	var clinicOrder []int
	for _, s := range slots {
		if _, ok := byClinic[s.Lpu]; !ok {
			byClinic[s.Lpu] = map[string][]string{}
			clinicOrder = append(clinicOrder, s.Lpu)
		}
		byClinic[s.Lpu][s.Date] = append(byClinic[s.Lpu][s.Date], s.Time)
	}
	var b strings.Builder
	for i, lpu := range clinicOrder {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "🏥 %s\n", clinicLabel(lpu, clinics))
		byDate := byClinic[lpu]
		dates := make([]string, 0, len(byDate))
		for d := range byDate {
			dates = append(dates, d)
		}
		sort.Strings(dates)
		for _, d := range dates {
			fmt.Fprintf(&b, "%s — %s\n", humanDate(d), strings.Join(byDate[d], ", "))
		}
	}
	return b.String()
}

// notifyMessage — текст уведомления о появившихся слотах: слоты,
// телефоны клиник (запись чаще всего по телефону) и ссылка на врача.
func notifyMessage(doc DoctorInfo, slots []freeSlot, clinics map[int]Clinic) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🎉 ПОЯВИЛАСЬ ЗАПИСЬ!\n%s\n\n%s", doc.Name, formatSlots(slots, clinics))
	seen := map[int]bool{}
	var phones []string
	for _, s := range slots {
		c, ok := clinics[s.Lpu]
		if ok && !seen[s.Lpu] && c.Phone != "" {
			phones = append(phones, fmt.Sprintf("%s: %s", c.Name, c.Phone))
			seen[s.Lpu] = true
		}
	}
	if len(phones) > 0 {
		fmt.Fprintf(&b, "\n📞 Запись по телефону:\n%s\n", strings.Join(phones, "\n"))
	}
	fmt.Fprintf(&b, "\nСтраница врача: %s", doc.URL)
	return b.String()
}
