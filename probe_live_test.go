package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestProbeLive — ручной диагностический прогон против боевого prodoctorov.
// Запуск: PROBE_LIVE=https://prodoctorov.ru/.../vrach/.../ go test -run TestProbeLive -v
// Печатает сырые ответы GET страницы врача и POST slots_bulk, чтобы понять,
// что реально отдаёт антибот. В обычном `go test ./...` скипается — не ходит в сеть.
func TestProbeLive(t *testing.T) {
	pageURL := os.Getenv("PROBE_LIVE")
	if pageURL == "" {
		t.Skip("PROBE_LIVE не задан — пропускаю боевой прогон")
	}
	client := newHTTPClient()
	prof := browserProfiles[0]

	// 1) GET страницы врача (uTLS Chrome_133 + h2)
	csrf, err := getCSRF(client, pageURL, prof)
	if err != nil {
		t.Fatalf("getCSRF: %v", err)
	}
	t.Logf("csrf=%q", csrf)

	// 2) Автоопределение врача, чтобы собрать реальные doctor_id/lpu.
	doc, err := discoverDoctor(client, pageURL)
	if err != nil {
		t.Fatalf("discoverDoctor: %v", err)
	}
	t.Logf("doctor: id=%d name=%s clinics=%d", doc.DoctorID, doc.Name, len(doc.Clinics))

	// 3) Ровно тот же POST, что и в fetchSlots, но печатаем сырой ответ.
	type dl struct {
		DoctorID     int  `json:"doctor_id"`
		LpuID        int  `json:"lpu_id"`
		LpuTimedelta int  `json:"lpu_timedelta"`
		HasSlots     bool `json:"has_slots"`
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
	setBrowserHeaders(req, false, prof)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRFToken", csrf)
	req.Header.Set("X-Api-Scope", "browser")
	req.Header.Set("Referer", doc.URL)
	req.Header.Set("Origin", prodoctorovBase)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	fmt.Printf("=== POST status=%d proto=%s len=%d\n", resp.StatusCode, resp.Proto, len(raw))
	fmt.Printf("=== resp headers:\n")
	for k, v := range resp.Header {
		fmt.Printf("    %s: %v\n", k, v)
	}
	fmt.Printf("=== body (first 1500 bytes):\n%s\n", truncate(string(raw), 1500))
}
