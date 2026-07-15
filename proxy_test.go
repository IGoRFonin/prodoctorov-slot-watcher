package main

import (
	"net/http"
	"testing"
)

func TestValidateProxyURL(t *testing.T) {
	for _, ok := range []string{
		"", // пусто = прокси не используется
		"socks5://127.0.0.1:1080",
		"http://host:3128",
		"https://user:pass@host:8080",
	} {
		if err := validateProxyURL(ok); err != nil {
			t.Errorf("%q должно быть валидным: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"ftp://host:21",   // неподдерживаемая схема
		"127.0.0.1:1080",  // без схемы
		"socks5://",       // без хоста
		"просто строка",   // не URL
	} {
		if err := validateProxyURL(bad); err == nil {
			t.Errorf("%q должно быть невалидным", bad)
		}
	}
}

func TestTelegramClientUsesProxy(t *testing.T) {
	// Прокси задаётся только для Telegram.
	c := newTelegramClient("socks5://127.0.0.1:1080")
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.Proxy == nil {
		t.Fatal("прокси не настроен")
	}
	req, _ := http.NewRequest("GET", "https://api.telegram.org/", nil)
	u, err := tr.Proxy(req)
	if err != nil || u == nil || u.Scheme != "socks5" || u.Host != "127.0.0.1:1080" {
		t.Fatalf("прокси неверный: %v (err %v)", u, err)
	}

	// Без прокси — прямое соединение (Transport по умолчанию).
	if newTelegramClient("").Transport != nil {
		t.Error("без прокси у telegram-клиента транспорт должен быть nil")
	}
}
