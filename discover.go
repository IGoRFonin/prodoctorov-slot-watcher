package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"

// Clinic — одна клиника врача.
type Clinic struct {
	LpuID     int    `json:"lpu_id"`
	Name      string `json:"name"`
	Phone     string `json:"phone"`
	Timedelta int    `json:"timedelta"`
}

// DoctorInfo — всё, что нужно для опроса slots_bulk, автоопределяется
// со страницы врача.
type DoctorInfo struct {
	DoctorID int      `json:"doctor_id"`
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	Clinics  []Clinic `json:"clinics"`
}

var (
	reDoctorID = regexp.MustCompile(`/vrach/(\d+)-`)
	reTitle    = regexp.MustCompile(`<title>([^<]+)</title>`)
	// Стабильные простые атрибуты компонента расписания.
	reApptIDs = regexp.MustCompile(`lpu-with-appointment-ids="\[([^\]]*)\]"`)
	reTownTD  = regexp.MustCompile(`town-timedelta="(-?\d+)"`)
	reInt     = regexp.MustCompile(`-?\d+`)
	// Полный JSON по клиникам — для названий/телефонов и как запасной список.
	reAddrList = regexp.MustCompile(`:lpu-address-list="([^"]*)"`)
)

// addrEntry — одна запись из :lpu-address-list. Берём только то, что нужно
// для уведомлений; вложенность минимальна, чтобы меньше зависеть от вёрстки.
type addrEntry struct {
	DoctorID int    `json:"doctor_id"`
	LpuID    int    `json:"lpu_id"`
	Phone    string `json:"phone"`
	Lpu      struct {
		Name string `json:"name"`
		Town struct {
			Timedelta int `json:"timedelta"`
		} `json:"town"`
	} `json:"lpu"`
}

func newHTTPClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 30 * time.Second}
}

// extractDoctorID достаёт ID врача из ссылки вида
// https://prodoctorov.ru/moskva/vrach/975987-hafez/
func extractDoctorID(doctorURL string) (int, error) {
	m := reDoctorID.FindStringSubmatch(doctorURL)
	if m == nil {
		return 0, fmt.Errorf("это не похоже на ссылку на страницу врача (нужен адрес вида https://prodoctorov.ru/<город>/vrach/<номер>-<фамилия>/): %s", doctorURL)
	}
	id, _ := strconv.Atoi(m[1])
	return id, nil
}

// parseDoctorPage разбирает HTML страницы врача: имя врача и список клиник
// (id, название, телефон, часовой пояс). Список клиник берём из стабильного
// атрибута lpu-with-appointment-ids, а названия/телефоны — из lpu-address-list.
func parseDoctorPage(pageHTML string, doctorID int) (DoctorInfo, error) {
	info := DoctorInfo{DoctorID: doctorID}

	// Имя врача — из <title>: "Хафез Йамен Мухаммадович, стоматолог - ...".
	if m := reTitle.FindStringSubmatch(pageHTML); m != nil {
		t := html.UnescapeString(m[1])
		if i := strings.IndexAny(t, ",|"); i > 0 {
			t = t[:i]
		}
		info.Name = strings.TrimSpace(t)
	}
	if info.Name == "" {
		info.Name = fmt.Sprintf("врач %d", doctorID)
	}

	// Часовой пояс города — простой стабильный атрибут town-timedelta.
	townTD := 0
	if m := reTownTD.FindStringSubmatch(pageHTML); m != nil {
		townTD, _ = strconv.Atoi(m[1])
	}

	// lpu-address-list — полный JSON по клиникам врача. Используем его для
	// названий и телефонов, а также как запасной список id. Разбор
	// best-effort: если сайт перекроит структуру, останутся заглушки, но
	// мониторинг продолжит работать по стабильным атрибутам ниже.
	byLpu := map[int]addrEntry{}
	var addrOrder []int
	if m := reAddrList.FindStringSubmatch(pageHTML); m != nil {
		var entries []addrEntry
		if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &entries); err == nil {
			for _, e := range entries {
				// На странице бывают клиники других врачей — фильтруем.
				if e.DoctorID != doctorID {
					continue
				}
				if _, ok := byLpu[e.LpuID]; !ok {
					addrOrder = append(addrOrder, e.LpuID)
				}
				byLpu[e.LpuID] = e
			}
		}
	}

	// Основной, стабильный источник списка клиник: массив id этого врача,
	// где включена онлайн-запись.
	var lpuIDs []int
	if m := reApptIDs.FindStringSubmatch(pageHTML); m != nil {
		for _, s := range reInt.FindAllString(m[1], -1) {
			id, _ := strconv.Atoi(s)
			lpuIDs = append(lpuIDs, id)
		}
	}
	// Запасной источник: все клиники врача из lpu-address-list.
	if len(lpuIDs) == 0 {
		lpuIDs = addrOrder
	}

	for _, id := range lpuIDs {
		c := Clinic{LpuID: id, Name: fmt.Sprintf("клиника %d", id), Timedelta: townTD}
		if e, ok := byLpu[id]; ok {
			if e.Lpu.Name != "" {
				c.Name = e.Lpu.Name
			}
			c.Phone = e.Phone
			if c.Timedelta == 0 {
				c.Timedelta = e.Lpu.Town.Timedelta
			}
		}
		info.Clinics = append(info.Clinics, c)
	}
	if len(info.Clinics) == 0 {
		return info, fmt.Errorf("на странице не нашлось расписание врача %d — возможно, сайт изменил вёрстку", doctorID)
	}
	return info, nil
}

// discoverDoctor скачивает страницу врача и разбирает её. Заодно в cookie
// jar клиента попадает csrftoken — он нужен api.go.
func discoverDoctor(c *http.Client, doctorURL string) (DoctorInfo, error) {
	id, err := extractDoctorID(doctorURL)
	if err != nil {
		return DoctorInfo{}, err
	}
	req, err := http.NewRequest("GET", doctorURL, nil)
	if err != nil {
		return DoctorInfo{}, fmt.Errorf("некорректная ссылка: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9")
	resp, err := c.Do(req)
	if err != nil {
		return DoctorInfo{}, fmt.Errorf("не удалось открыть страницу врача: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return DoctorInfo{}, fmt.Errorf("страница врача вернула HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return DoctorInfo{}, err
	}
	info, err := parseDoctorPage(string(b), id)
	if err != nil {
		return DoctorInfo{}, err
	}
	info.URL = doctorURL
	return info, nil
}
