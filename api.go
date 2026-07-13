package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// prodoctorovBase подменяется в тестах.
var prodoctorovBase = "https://prodoctorov.ru"

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

// lastGoodToken — кэш последнего рабочего csrftoken на случай, если
// очередной GET за токеном не удастся.
var lastGoodToken string

// getCSRF делает GET страницы врача, чтобы получить свежий csrftoken
// в cookie jar. До 2 попыток; при неудаче — кэшированный токен.
func getCSRF(c *http.Client, pageURL string) (string, error) {
	u, err := url.Parse(pageURL)
	if err != nil {
		return "", fmt.Errorf("некорректный адрес страницы врача: %w", err)
	}
	base := &url.URL{Scheme: u.Scheme, Host: u.Host}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, _ := http.NewRequest("GET", pageURL, nil)
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9")
		resp, err := c.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		for _, ck := range c.Jar.Cookies(base) {
			if ck.Name == "csrftoken" && ck.Value != "" {
				lastGoodToken = ck.Value
				return ck.Value, nil
			}
		}
		lastErr = fmt.Errorf("csrftoken не найден в ответе")
	}
	if lastGoodToken != "" {
		return lastGoodToken, nil
	}
	return "", lastErr
}

// fetchSlots опрашивает API и возвращает все свободные слоты врача.
func fetchSlots(c *http.Client, doc DoctorInfo) ([]freeSlot, error) {
	if len(doc.Clinics) == 0 {
		return nil, fmt.Errorf("у врача не найдено ни одной клиники — нечего опрашивать")
	}
	csrf, err := getCSRF(c, doc.URL)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить csrf-токен: %w", err)
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

	req, _ := http.NewRequest("POST", prodoctorovBase+"/ajax/schedule/slots_bulk/", bytes.NewReader(bb))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRFToken", csrf)
	req.Header.Set("X-Api-Scope", "browser")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9")
	req.Header.Set("Referer", doc.URL)
	req.Header.Set("Origin", prodoctorovBase)

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	var sr slotsResponse
	if err := json.Unmarshal(raw, &sr); err != nil {
		return nil, fmt.Errorf("не удалось разобрать ответ API: %w; тело=%s", err, truncate(string(raw), 200))
	}

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
	return free, nil
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

// formatSlots — слоты, сгруппированные сначала по дате, внутри — по клинике.
// Внутри клиники все времена в одну строку через запятую, чтобы название
// клиники не повторялось у каждого слота.
func formatSlots(slots []freeSlot, clinics map[int]Clinic) string {
	byDate := map[string][]freeSlot{}
	var dates []string
	for _, s := range slots {
		if _, ok := byDate[s.Date]; !ok {
			dates = append(dates, s.Date)
		}
		byDate[s.Date] = append(byDate[s.Date], s)
	}
	sort.Strings(dates)
	var b strings.Builder
	for _, d := range dates {
		fmt.Fprintf(&b, "📅 %s\n", humanDate(d))
		byClinic := map[int][]string{}
		var order []int
		for _, s := range byDate[d] {
			if _, ok := byClinic[s.Lpu]; !ok {
				order = append(order, s.Lpu)
			}
			byClinic[s.Lpu] = append(byClinic[s.Lpu], s.Time)
		}
		for _, lpu := range order {
			fmt.Fprintf(&b, "   🕐 %s — %s\n", strings.Join(byClinic[lpu], ", "), clinicLabel(lpu, clinics))
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
