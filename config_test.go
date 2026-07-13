package main

import "testing"

func validTestConfig() Config {
	return Config{
		TelegramBotToken: "123:abc",
		TelegramChatID:   "42",
		DoctorURL:        "https://prodoctorov.ru/zelenograd/vrach/304702-tonyan/",
		PollMinutes:      20,
		DigestTime:       "09:00",
	}
}

func TestConfigValidate(t *testing.T) {
	if err := validTestConfig().validate(); err != nil {
		t.Fatalf("валидный конфиг не прошёл: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"пустой токен", func(c *Config) { c.TelegramBotToken = "" }},
		{"пустой chat_id", func(c *Config) { c.TelegramChatID = "" }},
		{"плохая ссылка", func(c *Config) { c.DoctorURL = "https://example.com/" }},
		{"слишком частый опрос", func(c *Config) { c.PollMinutes = 5 }},
		{"кривое время сводки", func(c *Config) { c.DigestTime = "25:99" }},
	}
	for _, tc := range cases {
		c := validTestConfig()
		tc.mutate(&c)
		if err := c.validate(); err == nil {
			t.Errorf("%s: ожидалась ошибка", tc.name)
		}
	}
}

func TestValidHHMM(t *testing.T) {
	for _, ok := range []string{"09:00", "21:30", "00:00", "23:59"} {
		if !validHHMM(ok) {
			t.Errorf("%s должно быть валидным", ok)
		}
	}
	for _, bad := range []string{"9:00", "24:00", "09:60", "0900", "утро", ""} {
		if validHHMM(bad) {
			t.Errorf("%s должно быть невалидным", bad)
		}
	}
}
