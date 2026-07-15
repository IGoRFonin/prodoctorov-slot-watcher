package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// stubSender — отправитель-заглушка: возвращает заданную ошибку и помнит,
// что попытка отправки была.
type stubSender struct {
	err    error
	called bool
}

func (s *stubSender) sendMessage(chatID, text string) error {
	s.called = true
	return s.err
}

// Регрессия на «опрос каждую минуту»: если Telegram недоступен и сводка не
// уходит, день всё равно должен отметиться отправленным — иначе digestDue
// остаётся true и раскручивает опрос API до бана по частоте.
func TestSendDigestMarksDayEvenOnSendFailure(t *testing.T) {
	st := state{LastDigestDay: "2026-07-13"}
	now := time.Date(2026, 7, 15, 9, 0, 0, 0, time.Local)
	s := &stubSender{err: fmt.Errorf("telegram недоступен")}

	sendDigest(s, "42", DoctorInfo{Name: "Врач"}, nil, true, &st, nil, now)

	if !s.called {
		t.Fatal("должны были попытаться отправить сводку")
	}
	if st.LastDigestDay != "2026-07-15" {
		t.Fatalf("день сводки не отмечен при сбое отправки: %q", st.LastDigestDay)
	}
}

func TestSendDigestMarksDayOnSuccess(t *testing.T) {
	st := state{LastDigestDay: "2026-07-13"}
	now := time.Date(2026, 7, 15, 9, 0, 0, 0, time.Local)
	s := &stubSender{}

	sendDigest(s, "42", DoctorInfo{Name: "Врач"}, nil, true, &st, nil, now)

	if st.LastDigestDay != "2026-07-15" {
		t.Fatalf("день сводки не отмечен при успешной отправке: %q", st.LastDigestDay)
	}
}

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
