package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func fakeTelegram(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/botTOKEN/getMe", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":true,"result":{"username":"my_test_bot"}}`)
	})
	mux.HandleFunc("/botTOKEN/sendMessage", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("chat_id") != "42" {
			t.Errorf("chat_id = %q", r.Form.Get("chat_id"))
		}
		fmt.Fprint(w, `{"ok":true,"result":{}}`)
	})
	mux.HandleFunc("/botTOKEN/getUpdates", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":true,"result":[{"update_id":7,"message":{"chat":{"id":42}}}]}`)
	})
	mux.HandleFunc("/botBAD/getMe", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":false,"description":"Unauthorized"}`)
	})
	return httptest.NewServer(mux)
}

func TestTelegram(t *testing.T) {
	srv := fakeTelegram(t)
	defer srv.Close()
	old := telegramAPIBase
	telegramAPIBase = srv.URL
	defer func() { telegramAPIBase = old }()

	bot := tgBot{token: "TOKEN"}

	username, err := bot.getMe()
	if err != nil || username != "my_test_bot" {
		t.Fatalf("getMe: %q, %v", username, err)
	}
	if err := bot.sendMessage("42", "привет"); err != nil {
		t.Fatalf("sendMessage: %v", err)
	}
	chatID, err := bot.waitForChatID(5 * time.Second)
	if err != nil || chatID != "42" {
		t.Fatalf("waitForChatID: %q, %v", chatID, err)
	}
	if _, err := (tgBot{token: "BAD"}).getMe(); err == nil {
		t.Fatal("плохой токен должен давать ошибку")
	}
}
