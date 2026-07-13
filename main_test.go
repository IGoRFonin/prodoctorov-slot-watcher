package main

import (
	"strings"
	"testing"
	"time"
)

func TestDigestMessage(t *testing.T) {
	doc := DoctorInfo{Name: "Тестовый Врач"}
	clinics := map[int]Clinic{}
	lastOK := time.Date(2026, 7, 12, 15, 4, 0, 0, time.Local).Unix()
	wantTime := time.Unix(lastOK, 0).Format("02.01.2006 15:04")

	t.Run("ok со слотами", func(t *testing.T) {
		slots := []freeSlot{{Lpu: 1, Date: "2026-07-13", Time: "10:00"}}
		msg := digestMessage(doc, slots, true, lastOK, clinics)
		if !strings.Contains(msg, wantTime) {
			t.Errorf("нет времени последнего опроса: %q", msg)
		}
		if !strings.Contains(msg, "10:00") {
			t.Errorf("нет слотов в сводке: %q", msg)
		}
	})

	t.Run("ok без слотов", func(t *testing.T) {
		msg := digestMessage(doc, nil, true, lastOK, clinics)
		if !strings.Contains(msg, "нет") {
			t.Errorf("нет упоминания об отсутствии слотов: %q", msg)
		}
		if !strings.Contains(msg, wantTime) {
			t.Errorf("нет времени последнего опроса: %q", msg)
		}
	})

	t.Run("опрос не удался", func(t *testing.T) {
		msg := digestMessage(doc, nil, false, lastOK, clinics)
		if !strings.Contains(msg, "Не удалось получить данные") {
			t.Errorf("нет сообщения об ошибке: %q", msg)
		}
		if !strings.Contains(msg, wantTime) {
			t.Errorf("нет времени последнего опроса: %q", msg)
		}
	})

	t.Run("опрос не удался, успешных опросов ещё не было", func(t *testing.T) {
		msg := digestMessage(doc, nil, false, 0, clinics)
		if !strings.Contains(msg, "Успешных опросов ещё не было") {
			t.Errorf("нет фразы про отсутствие успешных опросов: %q", msg)
		}
	})
}

func TestDigestDue(t *testing.T) {
	loc := time.FixedZone("MSK", 3*3600)
	at := func(h, m int) time.Time { return time.Date(2026, 7, 13, h, m, 0, 0, loc) }

	if digestDue(at(8, 59), "", "09:00") {
		t.Error("до времени сводки — рано")
	}
	if !digestDue(at(9, 0), "", "09:00") {
		t.Error("ровно в срок — пора")
	}
	if !digestDue(at(15, 30), "2026-07-12", "09:00") {
		t.Error("вчера слали, сегодня после срока — пора")
	}
	if digestDue(at(15, 30), "2026-07-13", "09:00") {
		t.Error("сегодня уже слали — не пора")
	}
	if digestDue(at(15, 30), "", "мусор") {
		t.Error("кривое время — не слать")
	}
}
