package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func TestFetchSlots(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/vrach/", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "csrftoken", Value: "tok123", Path: "/"})
	})
	mux.HandleFunc("/ajax/schedule/slots_bulk/", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-CSRFToken"); got != "tok123" {
			t.Errorf("X-CSRFToken = %q, ожидалось tok123", got)
		}
		w.Write([]byte(`{"result":[{"doctor_id":304702,"lpu_id":70382,"slots":{"2026-07-30":[{"time":"08:00","free":true,"duration":30},{"time":"09:00","free":false,"duration":30}]}}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	oldBase := prodoctorovBase
	prodoctorovBase = srv.URL
	defer func() { prodoctorovBase = oldBase }()

	doc := DoctorInfo{
		DoctorID: 304702,
		URL:      srv.URL + "/vrach/304702-test/",
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
