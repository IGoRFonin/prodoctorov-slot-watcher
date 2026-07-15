// Мониторинг свободных слотов записи к врачу на ProDoctorov.
// Опрашивает приватный API slots_bulk и шлёт уведомление в Telegram,
// когда появляются свободные слоты. Раз в день — сводка «я живой».
//
// Настройка — мастером при первом запуске, хранится в config.json.
// Только стандартная библиотека.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

type state struct {
	LastNotifSig  string      `json:"last_notif_sig"`
	LastDigestDay string      `json:"last_digest_day"`
	ConsecErrors  int         `json:"consec_errors"`
	LastOK        int64       `json:"last_ok"`
	ErrAlerted    bool        `json:"err_alerted"`
	CachedDoctor  *DoctorInfo `json:"cached_doctor,omitempty"`
}

const stateFileName = "state.json"

// errAlertThreshold — после скольких подряд неудачных опросов слать алерт.
const errAlertThreshold = 3

func loadState() state {
	var s state
	if b, err := os.ReadFile(filepath.Join(baseDir(), stateFileName)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func saveState(s state) {
	b, _ := json.MarshalIndent(s, "", "  ")
	if err := os.WriteFile(filepath.Join(baseDir(), stateFileName), b, 0644); err != nil {
		log.Printf("не удалось сохранить state.json: %v", err)
	}
}

// digestMessage — текст ежедневной сводки. ok=false, если последний опрос не удался.
func digestMessage(doc DoctorInfo, slots []freeSlot, ok bool, lastOK int64, clinics map[int]Clinic) string {
	lastOKLine := ""
	if lastOK > 0 {
		lastOKLine = time.Unix(lastOK, 0).Format("02.01.2006 15:04")
	}
	if !ok {
		msg := fmt.Sprintf("📋 Ежедневная сводка\n%s\nНе удалось получить данные с сайта — возможно, сайт недоступен. ", doc.Name)
		if lastOKLine != "" {
			msg += fmt.Sprintf("Последний успешный опрос: %s.", lastOKLine)
		} else {
			msg += "Успешных опросов ещё не было."
		}
		return msg
	}
	var msg string
	if len(slots) > 0 {
		msg = fmt.Sprintf("📋 Ежедневная сводка\n%s\nСвободные слоты:\n%s", doc.Name, formatSlots(slots, clinics))
	} else {
		msg = fmt.Sprintf("📋 Ежедневная сводка\n%s\nСвободных слотов нет. Бот работает, слежу дальше.", doc.Name)
	}
	if lastOKLine != "" {
		msg += fmt.Sprintf("\nПоследний успешный опрос: %s", lastOKLine)
	}
	return msg
}

// digestSender — то, что умеет слать сообщение (в тестах подменяется заглушкой).
type digestSender interface {
	sendMessage(chatID, text string) error
}

// sendDigest шлёт ежедневную сводку и ВСЕГДА отмечает день отправленным —
// даже если отправка не удалась. Сводка — некритичный сигнал «я живой» раз
// в сутки: при недоступном Telegram повторять её каждую минуту нельзя, иначе
// digestDue остаётся true и опрос API идёт каждую минуту → бан по частоте на
// prodoctorov. О реальных проблемах предупреждает отдельный алерт по ошибкам.
func sendDigest(bot digestSender, chatID string, doc DoctorInfo, slots []freeSlot, ok bool, st *state, clinics map[int]Clinic, now time.Time) {
	msg := digestMessage(doc, slots, ok, st.LastOK, clinics)
	if err := bot.sendMessage(chatID, msg); err != nil {
		log.Printf("не удалось отправить ежедневную сводку: %v", err)
	}
	st.LastDigestDay = now.Format("2006-01-02")
}

// digestDue — пора ли слать ежедневную сводку: сегодня ещё не слали
// и локальное время уже дошло до hhmm.
func digestDue(now time.Time, lastDay, hhmm string) bool {
	today := now.Format("2006-01-02")
	if lastDay == today {
		return false
	}
	at, err := time.ParseInLocation("2006-01-02 15:04", today+" "+hhmm, now.Location())
	if err != nil {
		return false
	}
	return !now.Before(at)
}

func main() {
	log.SetFlags(log.LstdFlags)

	cfg, err := loadConfig()
	if errors.Is(err, errNoConfig) {
		cfg, err = runWizard()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка:", err)
		os.Exit(1)
	}

	bot := tgBot{token: cfg.TelegramBotToken, client: newTelegramClient(cfg.ProxyURL)}
	client := newHTTPClient()
	st := loadState()

	// Автоопределение врача; при неудаче — кэш из state.json.
	doc, err := discoverDoctor(client, cfg.DoctorURL)
	if err != nil {
		if st.CachedDoctor == nil {
			fmt.Fprintln(os.Stderr, "Ошибка: не удалось получить данные врача:", err)
			fmt.Fprintln(os.Stderr, "Проверьте ссылку doctor_url в config.json и доступ к интернету.")
			os.Exit(1)
		}
		doc = *st.CachedDoctor
		log.Printf("автоопределение не удалось (%v), работаю на сохранённых данных", err)
		_ = bot.sendMessage(cfg.TelegramChatID, "⚠️ Не удалось обновить данные врача со страницы — работаю на сохранённых. Если это повторяется при каждом запуске, проверьте ссылку в config.json.")
	} else {
		st.CachedDoctor = &doc
		saveState(st)
	}

	clinics := map[int]Clinic{}
	for _, c := range doc.Clinics {
		clinics[c.LpuID] = c
	}
	log.Printf("watcher старт: %s (id %d), клиник: %d, опрос каждые %d мин, сводка в %s",
		doc.Name, doc.DoctorID, len(doc.Clinics), cfg.PollMinutes, cfg.DigestTime)

	// poll возвращает свободные слоты и ok=false при ошибке опроса.
	poll := func() ([]freeSlot, bool) {
		slots, err := fetchSlots(client, doc)
		if err != nil {
			st.ConsecErrors++
			log.Printf("ошибка опроса (#%d подряд): %v", st.ConsecErrors, err)
			if st.ConsecErrors >= errAlertThreshold && !st.ErrAlerted {
				if e := bot.sendMessage(cfg.TelegramChatID, fmt.Sprintf(
					"⚠️ Мониторинг не получает данные: %d ошибок подряд.\nПоследняя: %s\nМогу не увидеть появление записи.",
					st.ConsecErrors, truncate(err.Error(), 200))); e == nil {
					st.ErrAlerted = true
				}
			}
			saveState(st)
			return nil, false
		}
		if st.ErrAlerted {
			// Сбрасываем флаг, только если удалось сообщить о восстановлении —
			// иначе рискуем молча потерять единственный шанс предупредить.
			if e := bot.sendMessage(cfg.TelegramChatID, "✅ Мониторинг восстановлен, данные снова приходят."); e == nil {
				st.ErrAlerted = false
			}
		}
		st.ConsecErrors = 0
		st.LastOK = time.Now().Unix()
		log.Printf("опрос ок: свободных слотов=%d", len(slots))

		sig := signature(slots)
		if len(slots) > 0 {
			// Уведомляем, только если набор слотов изменился (без спама).
			if sig != st.LastNotifSig {
				if err := bot.sendMessage(cfg.TelegramChatID, notifyMessage(doc, slots, clinics)); err != nil {
					log.Printf("ошибка отправки уведомления: %v", err)
				} else {
					log.Printf("уведомление отправлено (%d слотов)", len(slots))
					st.LastNotifSig = sig
				}
			}
		} else {
			// Слотов нет — сбрасываем подпись, чтобы новое появление уведомило.
			st.LastNotifSig = ""
		}
		saveState(st)
		return slots, true
	}

	runDigest := func(slots []freeSlot, ok bool) {
		sendDigest(bot, cfg.TelegramChatID, doc, slots, ok, &st, clinics, time.Now())
		saveState(st)
	}

	slots, ok := poll() // сразу при старте
	if digestDue(time.Now(), st.LastDigestDay, cfg.DigestTime) {
		runDigest(slots, ok)
	}

	pollTicker := time.NewTicker(time.Duration(cfg.PollMinutes) * time.Minute)
	defer pollTicker.Stop()
	digestTicker := time.NewTicker(time.Minute)
	defer digestTicker.Stop()
	for {
		select {
		case <-pollTicker.C:
			poll()
		case <-digestTicker.C:
			if digestDue(time.Now(), st.LastDigestDay, cfg.DigestTime) {
				// Сводка должна показывать свежие данные — опрашиваем.
				runDigest(poll())
			}
		}
	}
}
