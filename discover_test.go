package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestExtractDoctorID(t *testing.T) {
	id, err := extractDoctorID("https://prodoctorov.ru/moskva/vrach/975987-hafez/")
	if err != nil || id != 975987 {
		t.Fatalf("got id=%d err=%v, ожидалось 975987", id, err)
	}
	if _, err := extractDoctorID("https://prodoctorov.ru/moskva/"); err == nil {
		t.Fatal("ожидалась ошибка для ссылки без /vrach/<id>-")
	}
}

func TestParseDoctorPage(t *testing.T) {
	b, err := os.ReadFile("testdata/doctor_page.html")
	if err != nil {
		t.Fatal(err)
	}
	info, err := parseDoctorPage(string(b), 975987)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "Хафез Йамен Мухаммадович" {
		t.Errorf("имя врача: %q", info.Name)
	}
	// lpu-with-appointment-ids в тестовых данных = [70382]: мониторим только
	// клиники этого врача с включённой онлайн-записью (99990 с выключенной —
	// не в списке), чужой врач (39025) тоже отфильтрован.
	if len(info.Clinics) != 1 {
		t.Fatalf("клиник: %d, ожидалось 1 (только с включённой записью)", len(info.Clinics))
	}
	byID := map[int]Clinic{}
	for _, c := range info.Clinics {
		byID[c.LpuID] = c
	}
	if _, ok := byID[39025]; ok {
		t.Error("клиника другого врача (lpu 39025) не отфильтрована")
	}
	if _, ok := byID[99990]; ok {
		t.Error("клиника с выключенной онлайн-записью (lpu 99990) не должна мониториться")
	}
	c1, ok := byID[70382]
	if !ok {
		t.Fatal("нет клиники 70382")
	}
	if c1.Timedelta != 3 {
		t.Errorf("timedelta клиники 70382: %d, ожидалось 3", c1.Timedelta)
	}
	if !strings.Contains(c1.Name, "1501") {
		t.Errorf("название клиники 70382: %q, ожидалось «...корпус 1501»", c1.Name)
	}
	if c1.Phone == "" {
		t.Error("телефон клиники 70382 пуст")
	}
}

// Антибот-заглушка приходит как HTML со статусом 200 — парсер принял бы её за
// перекроенную вёрстку и выдал бы «сайт изменил вёрстку». discoverDoctor должен
// назвать настоящую причину (errAntibot), иначе алерт уводит в ложную сторону.
func TestDiscoverDoctorDetectsChallenge(t *testing.T) {
	const challenge = `<!DOCTYPE html><html><head>
<script src="https://servicepipe.tech/loaders/abc.js" async></script>
</head><body><div id="id_spinner"></div></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(challenge))
	}))
	defer srv.Close()

	_, err := discoverDoctor(srv.Client(), srv.URL+"/moskva/vrach/975987-test/")
	if err == nil {
		t.Fatal("ожидалась ошибка на антибот-заглушке")
	}
	if !errors.Is(err, errAntibot) {
		t.Fatalf("ожидалась errAntibot, получили: %v", err)
	}
	if strings.Contains(err.Error(), "вёрстк") {
		t.Errorf("ошибка винит вёрстку вместо антибота: %v", err)
	}
}

// Настоящая страница врача — тоже HTML, и её проверка на заглушку пропускать
// не должна: иначе фикс сломает рабочий путь.
func TestDiscoverDoctorAcceptsRealPage(t *testing.T) {
	page := `<html><head><title>Тестовый Врач, стоматолог - отзывы</title></head><body>
<schedule-widget town-timedelta="3" lpu-with-appointment-ids="[70382]"></schedule-widget>
</body></html>`

	var gotXHR, gotReferer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotXHR, gotReferer = r.Header.Get("X-Requested-With"), r.Header.Get("Referer")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(page))
	}))
	defer srv.Close()

	info, err := discoverDoctor(srv.Client(), srv.URL+"/moskva/vrach/975987-test/")
	if err != nil {
		t.Fatalf("настоящая страница отвергнута: %v", err)
	}
	if info.Name != "Тестовый Врач" || len(info.Clinics) != 1 || info.Clinics[0].LpuID != 70382 {
		t.Fatalf("разбор страницы: %+v", info)
	}
	// Эти два заголовка — единственное, что уводит запрос из-под антибот-заглушки
	// ServicePipe. Потеряем любой из них — мониторинг снова ослепнет, причём молча.
	if gotXHR != "XMLHttpRequest" {
		t.Errorf("X-Requested-With = %q, ожидалось XMLHttpRequest — без него придёт заглушка", gotXHR)
	}
	if gotReferer == "" {
		t.Error("Referer пуст — без него ServicePipe отдаёт заглушку даже на XHR")
	}
}
