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
	reDoctorID    = regexp.MustCompile(`/vrach/(\d+)-`)
	reDoctorsLpus = regexp.MustCompile(`'doctor_id':\s*(\d+),\s*'lpu_id':\s*(\d+),\s*'lpu_timedelta':\s*(-?\d+)`)
	reTitle       = regexp.MustCompile(`<title>([^<]+)</title>`)
	reAddrList    = regexp.MustCompile(`:lpu-address-list="([^"]*)"`)
)

func newHTTPClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 30 * time.Second}
}

// extractDoctorID достаёт ID врача из ссылки вида
// https://prodoctorov.ru/zelenograd/vrach/304702-tonyan/
func extractDoctorID(doctorURL string) (int, error) {
	m := reDoctorID.FindStringSubmatch(doctorURL)
	if m == nil {
		return 0, fmt.Errorf("это не похоже на ссылку на страницу врача (нужен адрес вида https://prodoctorov.ru/<город>/vrach/<номер>-<фамилия>/): %s", doctorURL)
	}
	id, _ := strconv.Atoi(m[1])
	return id, nil
}

// parseDoctorPage разбирает HTML страницы врача: имя, клиники (lpu),
// часовой пояс, названия и телефоны клиник.
func parseDoctorPage(pageHTML string, doctorID int) (DoctorInfo, error) {
	info := DoctorInfo{DoctorID: doctorID}

	// Имя врача — из <title>: "Тонян Иосиф Павлович, стоматолог - ...".
	if m := reTitle.FindStringSubmatch(pageHTML); m != nil {
		t := html.UnescapeString(m[1])
		if i := strings.IndexAny(t, ",|"); i > 0 {
			t = t[:i]
		}
		info.Name = strings.TrimSpace(t)
	}

	// Блок doctorsLpus: тройки doctor_id/lpu_id/lpu_timedelta.
	// На странице бывают блоки других врачей — фильтруем по doctorID.
	for _, m := range reDoctorsLpus.FindAllStringSubmatch(pageHTML, -1) {
		did, _ := strconv.Atoi(m[1])
		if did != doctorID {
			continue
		}
		lpu, _ := strconv.Atoi(m[2])
		td, _ := strconv.Atoi(m[3])
		info.Clinics = append(info.Clinics, Clinic{
			LpuID:     lpu,
			Name:      fmt.Sprintf("клиника %d", lpu),
			Timedelta: td,
		})
	}
	if len(info.Clinics) == 0 {
		return info, fmt.Errorf("на странице не нашлось расписание врача %d — возможно, сайт изменил вёрстку", doctorID)
	}

	// Названия и телефоны клиник — из атрибута :lpu-address-list
	// (HTML-экранированный JSON). Ошибки не фатальны: останутся
	// названия-заглушки «клиника <id>».
	if m := reAddrList.FindStringSubmatch(pageHTML); m != nil {
		var entries []struct {
			DoctorID int    `json:"doctor_id"`
			LpuID    int    `json:"lpu_id"`
			Phone    string `json:"phone"`
			Lpu      struct {
				Name string `json:"name"`
			} `json:"lpu"`
		}
		if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &entries); err == nil {
			for i := range info.Clinics {
				for _, e := range entries {
					if e.LpuID == info.Clinics[i].LpuID && e.DoctorID == doctorID {
						if e.Lpu.Name != "" {
							info.Clinics[i].Name = e.Lpu.Name
						}
						info.Clinics[i].Phone = e.Phone
						break
					}
				}
			}
		}
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
