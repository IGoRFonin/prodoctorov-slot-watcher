package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// telegramAPIBase подменяется в тестах.
var telegramAPIBase = "https://api.telegram.org"

// tgHTTPClient — клиент с таймаутом, чтобы вызовы Telegram не зависали.
// 40с покрывает long-poll getUpdates (timeout=10) с запасом.
var tgHTTPClient = &http.Client{Timeout: 40 * time.Second}

type tgBot struct{ token string }

type tgResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

func (b tgBot) call(method string, form url.Values) (json.RawMessage, error) {
	resp, err := tgHTTPClient.PostForm(fmt.Sprintf("%s/bot%s/%s", telegramAPIBase, b.token, method), form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var tr tgResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		return nil, fmt.Errorf("telegram: HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	if !tr.OK {
		return nil, fmt.Errorf("telegram: %s", tr.Description)
	}
	return tr.Result, nil
}

func (b tgBot) sendMessage(chatID, text string) error {
	form := url.Values{}
	form.Set("chat_id", chatID)
	form.Set("text", text)
	form.Set("disable_web_page_preview", "true")
	_, err := b.call("sendMessage", form)
	return err
}

// getMe проверяет токен и возвращает username бота.
func (b tgBot) getMe() (string, error) {
	raw, err := b.call("getMe", nil)
	if err != nil {
		return "", err
	}
	var me struct {
		Username string `json:"username"`
	}
	if err := json.Unmarshal(raw, &me); err != nil {
		return "", err
	}
	return me.Username, nil
}

// waitForChatID ждёт, пока пользователь напишет боту, и возвращает chat_id
// первого пришедшего сообщения.
func (b tgBot) waitForChatID(timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	offset := 0
	for time.Now().Before(deadline) {
		form := url.Values{}
		form.Set("timeout", "10")
		form.Set("offset", strconv.Itoa(offset))
		raw, err := b.call("getUpdates", form)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		var updates []struct {
			UpdateID int `json:"update_id"`
			Message  struct {
				Chat struct {
					ID int64 `json:"id"`
				} `json:"chat"`
			} `json:"message"`
		}
		if err := json.Unmarshal(raw, &updates); err != nil {
			return "", err
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			if u.Message.Chat.ID != 0 {
				return strconv.FormatInt(u.Message.Chat.ID, 10), nil
			}
		}
	}
	return "", fmt.Errorf("за отведённое время сообщение боту не пришло")
}
