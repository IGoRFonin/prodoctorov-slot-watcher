package main

import (
	"testing"
	"time"
)

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
