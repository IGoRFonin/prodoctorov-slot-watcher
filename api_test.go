package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSignatureAndFormat(t *testing.T) {
	slots := []freeSlot{
		{Lpu: 70382, Date: "2026-07-30", Time: "08:00"},
		{Lpu: 99990, Date: "2026-07-30", Time: "09:30"},
	}
	clinics := map[int]Clinic{
		70382: {LpuID: 70382, Name: "Корпус 1501", Phone: "(926) 929-03-11"},
	}
	if signature(slots) == signature(slots[:1]) {
		t.Error("подписи разных наборов совпали")
	}
	out := formatSlots(slots, clinics)
	if !strings.Contains(out, "Корпус 1501") {
		t.Errorf("нет названия клиники: %q", out)
	}
	if !strings.Contains(out, "клиника 99990") {
		t.Errorf("нет заглушки для неизвестной клиники: %q", out)
	}
	msg := notifyMessage(DoctorInfo{Name: "Тестовый Врач", URL: "https://x/"}, slots, clinics)
	if !strings.Contains(msg, "(926) 929-03-11") {
		t.Errorf("в уведомлении нет телефона клиники: %q", msg)
	}
	if !strings.Contains(msg, "https://x/") {
		t.Errorf("в уведомлении нет ссылки на врача: %q", msg)
	}
}

func TestFormatSlotsGrouping(t *testing.T) {
	// Одна клиника, несколько дней: название клиники — один раз в шапке,
	// каждый день одной строкой «дата — времена».
	slots := []freeSlot{
		{Lpu: 70382, Date: "2026-07-16", Time: "16:00"},
		{Lpu: 70382, Date: "2026-07-16", Time: "18:00"},
		{Lpu: 70382, Date: "2026-07-17", Time: "15:00"},
	}
	clinics := map[int]Clinic{
		70382: {LpuID: 70382, Name: "Стоматология «ПрезиДент»"},
	}
	out := formatSlots(slots, clinics)
	if n := strings.Count(out, "Стоматология «ПрезиДент»"); n != 1 {
		t.Errorf("название клиники должно встречаться один раз, встретилось %d раз: %q", n, out)
	}
	if !strings.Contains(out, "🏥 Стоматология «ПрезиДент»") {
		t.Errorf("нет заголовка клиники: %q", out)
	}
	if !strings.Contains(out, "16 июля, чт — 16:00, 18:00") {
		t.Errorf("день не в одну строку с временами: %q", out)
	}
	if !strings.Contains(out, "17 июля, пт — 15:00") {
		t.Errorf("нет второго дня: %q", out)
	}

	// Несколько клиник: каждая своим блоком, название не дублируется.
	multi := []freeSlot{
		{Lpu: 70382, Date: "2026-07-16", Time: "16:00"},
		{Lpu: 99990, Date: "2026-07-16", Time: "09:30"},
	}
	out = formatSlots(multi, clinics)
	if strings.Count(out, "🏥") != 2 {
		t.Errorf("ожидались два блока клиник: %q", out)
	}
}

func TestTruncateUTF8Boundary(t *testing.T) {
	s := "Хафез Йамен Мухаммадович"
	got := truncate(s, 5) // режет ровно посреди кириллической руны
	if !utf8.ValidString(got) {
		t.Fatalf("truncate вернул невалидный UTF-8: %q", got)
	}
	if !strings.HasPrefix(s, got) {
		t.Fatalf("truncate не является префиксом исходной строки: %q", got)
	}
}

func TestTruncateShortASCIIUnchanged(t *testing.T) {
	s := "hello"
	if got := truncate(s, 100); got != s {
		t.Fatalf("truncate изменил строку короче n: %q", got)
	}
}

func TestFetchSlots(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/vrach/", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "tok123", Path: "/"})
	})
	mux.HandleFunc("/ajax/schedule/slots_bulk/", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-CSRFToken"); got != "tok123" {
			t.Errorf("X-CSRFToken = %q, ожидалось tok123", got)
		}
		w.Write([]byte(`{"result":[{"doctor_id":975987,"lpu_id":70382,"slots":{"2026-07-30":[{"time":"08:00","free":true,"duration":30},{"time":"09:00","free":false,"duration":30}]}}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	oldBase := prodoctorovBase
	prodoctorovBase = srv.URL
	defer func() { prodoctorovBase = oldBase }()

	doc := DoctorInfo{
		DoctorID: 975987,
		URL:      srv.URL + "/vrach/975987-test/",
		Clinics:  []Clinic{{LpuID: 70382, Timedelta: 3}},
	}
	slots, err := fetchSlots(newHTTPClient(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 1 || slots[0].Time != "08:00" || slots[0].Lpu != 70382 {
		t.Fatalf("слоты: %+v, ожидался один свободный 08:00", slots)
	}
}
