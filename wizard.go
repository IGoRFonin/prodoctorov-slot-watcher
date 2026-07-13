package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func ask(in *bufio.Reader, prompt string) string {
	fmt.Print(prompt)
	line, err := in.ReadString('\n')
	if err != nil && line == "" {
		// Ввод закрыт (Ctrl+D / обрыв) — не зацикливаемся на переспросах.
		fmt.Println("\nВвод прерван — настройка не завершена. Запустите программу ещё раз.")
		os.Exit(1)
	}
	return strings.TrimSpace(line)
}

// runWizard — мастер первого запуска: в диалоге собирает config.json.
func runWizard() (Config, error) {
	in := bufio.NewReader(os.Stdin)
	var cfg Config

	fmt.Println("Похоже, это первый запуск — давайте настроимся, это займёт пару минут.")

	// 1/5: врач.
	client := newHTTPClient()
	var doc DoctorInfo
	for {
		u := ask(in, "\n1/5. Вставьте ссылку на страницу врача с prodoctorov.ru\n(например, https://prodoctorov.ru/zelenograd/vrach/566096-adamyan/):\n> ")
		fmt.Println("Проверяю страницу...")
		d, err := discoverDoctor(client, u)
		if err != nil {
			fmt.Printf("Не получилось: %v\nПопробуйте ещё раз.\n", err)
			continue
		}
		fmt.Printf("Нашёл: %s\nКлиники:\n", d.Name)
		for _, c := range d.Clinics {
			if c.Phone != "" {
				fmt.Printf("  • %s — тел. %s\n", c.Name, c.Phone)
			} else {
				fmt.Printf("  • %s\n", c.Name)
			}
		}
		if strings.EqualFold(ask(in, "Верно? (Enter = да, n = ввести другую ссылку): "), "n") {
			continue
		}
		doc = d
		cfg.DoctorURL = u
		break
	}

	// 2/5: токен бота.
	var bot tgBot
	var botUsername string
	for {
		fmt.Println("\n2/5. Нужен Telegram-бот, который будет слать вам уведомления.")
		fmt.Println("Откройте в Telegram @BotFather → отправьте /newbot → следуйте подсказкам.")
		fmt.Println("В конце BotFather выдаст токен вида 1234567890:AA... — вставьте его сюда.")
		t := ask(in, "> ")
		b := tgBot{token: t}
		username, err := b.getMe()
		if err != nil {
			fmt.Printf("Токен не подошёл: %v\nПопробуйте ещё раз.\n", err)
			continue
		}
		fmt.Printf("Отлично, бот @%s на связи.\n", username)
		bot = b
		botUsername = username
		cfg.TelegramBotToken = t
		break
	}

	// 3/5: chat_id.
	fmt.Printf("\n3/5. Откройте @%s в Telegram и нажмите «Старт» (или отправьте любое сообщение).\n", botUsername)
	for {
		fmt.Println("Жду ваше сообщение боту (до 2 минут)...")
		chatID, err := bot.waitForChatID(2 * time.Minute)
		if err != nil {
			fmt.Printf("%v\nПроверьте, что написали именно боту @%s, и нажмите Enter для новой попытки.\n", err, botUsername)
			ask(in, "")
			continue
		}
		cfg.TelegramChatID = chatID
		break
	}
	if err := bot.sendMessage(cfg.TelegramChatID, "✅ Связь работает! Это тестовое сообщение."); err == nil {
		fmt.Println("Отправил тестовое сообщение — проверьте Telegram.")
	}

	// 4/5: интервал опроса.
	for {
		s := ask(in, "\n4/5. Как часто проверять расписание, в минутах? (Enter = 20, минимум 10): ")
		if s == "" {
			cfg.PollMinutes = 20
			break
		}
		n, err := strconv.Atoi(s)
		if err != nil || n < 10 {
			fmt.Println("Нужно целое число не меньше 10 (чаще опрашивать нельзя — бережём сайт).")
			continue
		}
		cfg.PollMinutes = n
		break
	}

	// 5/5: время сводки.
	for {
		s := ask(in, "\n5/5. Во сколько присылать ежедневную сводку? (Enter = 09:00, формат ЧЧ:ММ): ")
		if s == "" {
			s = "09:00"
		}
		if !validHHMM(s) {
			fmt.Println("Формат — ЧЧ:ММ, например 09:00 или 21:30.")
			continue
		}
		cfg.DigestTime = s
		break
	}

	if err := saveConfig(cfg); err != nil {
		return cfg, fmt.Errorf("не удалось сохранить config.json: %w", err)
	}
	fmt.Println("\nГотово! Настройки сохранены в config.json. Запускаю мониторинг.")
	_ = bot.sendMessage(cfg.TelegramChatID, fmt.Sprintf(
		"✅ Мониторинг запущен.\nВрач: %s\nПроверка каждые %d мин, сводка ежедневно в %s.",
		doc.Name, cfg.PollMinutes, cfg.DigestTime))
	return cfg, nil
}
