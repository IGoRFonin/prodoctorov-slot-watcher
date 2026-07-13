package main

import (
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
	if len(info.Clinics) != 2 {
		t.Fatalf("клиник: %d, ожидалось 2 (чужой врач отфильтрован)", len(info.Clinics))
	}
	byID := map[int]Clinic{}
	for _, c := range info.Clinics {
		byID[c.LpuID] = c
	}
	if _, ok := byID[39025]; ok {
		t.Error("клиника другого врача (lpu 39025) не отфильтрована")
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
