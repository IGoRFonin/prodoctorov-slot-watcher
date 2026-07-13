package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const configFileName = "config.json"

// baseDir — папка, где лежит бинарник; config.json и state.json живут рядом
// с программой, а не в текущей директории (важно при автозапуске).
func baseDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

// Config — настройки пользователя, лежат в config.json рядом с программой.
type Config struct {
	TelegramBotToken string `json:"telegram_bot_token"`
	TelegramChatID   string `json:"telegram_chat_id"`
	DoctorURL        string `json:"doctor_url"`
	PollMinutes      int    `json:"poll_minutes"`
	DigestTime       string `json:"digest_time"`
}

var errNoConfig = errors.New("config.json не найден")

func loadConfig() (Config, error) {
	var c Config
	b, err := os.ReadFile(filepath.Join(baseDir(), configFileName))
	if os.IsNotExist(err) {
		return c, errNoConfig
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("config.json повреждён (%w) — исправьте его или удалите, чтобы настроить заново", err)
	}
	if err := c.validate(); err != nil {
		return c, err
	}
	return c, nil
}

func (c Config) validate() error {
	if c.TelegramBotToken == "" {
		return errors.New("в config.json пустой telegram_bot_token")
	}
	if c.TelegramChatID == "" {
		return errors.New("в config.json пустой telegram_chat_id")
	}
	if _, err := extractDoctorID(c.DoctorURL); err != nil {
		return fmt.Errorf("в config.json некорректный doctor_url: %w", err)
	}
	if c.PollMinutes < 10 {
		return errors.New("poll_minutes в config.json должен быть не меньше 10")
	}
	if !validHHMM(c.DigestTime) {
		return errors.New("digest_time в config.json должен быть в формате ЧЧ:ММ, например 09:00")
	}
	return nil
}

func saveConfig(c Config) error {
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(filepath.Join(baseDir(), configFileName), b, 0600)
}

// validHHMM — строгая проверка времени "ЧЧ:ММ" (ровно две цифры и там, и там).
func validHHMM(s string) bool {
	t, err := time.Parse("15:04", s)
	return err == nil && t.Format("15:04") == s
}
