package main

import (
	"math/rand/v2"
	"net/http"
)

// browserProfile — согласованный набор «отпечатков» одного браузера:
// User-Agent и связанные с ним Client Hints (sec-ch-ua, платформа).
// Важно, чтобы major-версия в UA совпадала с версией в sec-ch-ua, а
// платформа в sec-ch-ua-platform — с ОС в UA, иначе антибот-WAF
// prodoctorov сразу видит несостыковку и отправляет клиент на
// r.prodoctorov.ru/unblock.
type browserProfile struct {
	ua       string // User-Agent
	secCHUA  string // sec-ch-ua (бренды + версии)
	platform string // sec-ch-ua-platform, уже в кавычках: "macOS"
}

// browserProfiles — 10 актуальных десктопных профилей (Chrome/Edge на
// macOS/Windows/Linux, версии 148–150). Все mobile=?0. Версии сверены
// с реальным Chrome на машине (150.0.7871.124, июль 2026).
var browserProfiles = []browserProfile{
	{
		ua:       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36",
		secCHUA:  `"Not)A;Brand";v="8", "Chromium";v="150", "Google Chrome";v="150"`,
		platform: `"macOS"`,
	},
	{
		ua:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36",
		secCHUA:  `"Not)A;Brand";v="8", "Chromium";v="150", "Google Chrome";v="150"`,
		platform: `"Windows"`,
	},
	{
		ua:       "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36",
		secCHUA:  `"Not)A;Brand";v="8", "Chromium";v="150", "Google Chrome";v="150"`,
		platform: `"Linux"`,
	},
	{
		ua:       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36",
		secCHUA:  `"Google Chrome";v="149", "Chromium";v="149", "Not_A Brand";v="24"`,
		platform: `"macOS"`,
	},
	{
		ua:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36",
		secCHUA:  `"Google Chrome";v="149", "Chromium";v="149", "Not_A Brand";v="24"`,
		platform: `"Windows"`,
	},
	{
		ua:       "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36",
		secCHUA:  `"Google Chrome";v="149", "Chromium";v="149", "Not_A Brand";v="24"`,
		platform: `"Linux"`,
	},
	{
		ua:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36",
		secCHUA:  `"Chromium";v="148", "Google Chrome";v="148", "Not.A/Brand";v="99"`,
		platform: `"Windows"`,
	},
	{
		ua:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36 Edg/150.0.0.0",
		secCHUA:  `"Not)A;Brand";v="8", "Chromium";v="150", "Microsoft Edge";v="150"`,
		platform: `"Windows"`,
	},
	{
		ua:       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36 Edg/150.0.0.0",
		secCHUA:  `"Not)A;Brand";v="8", "Chromium";v="150", "Microsoft Edge";v="150"`,
		platform: `"macOS"`,
	},
	{
		ua:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36 Edg/149.0.0.0",
		secCHUA:  `"Microsoft Edge";v="149", "Chromium";v="149", "Not_A Brand";v="24"`,
		platform: `"Windows"`,
	},
}

// pickProfile выбирает случайный профиль из пула. Вызывается один раз
// на цикл опроса, чтобы GET-за-csrf и POST-slots_bulk шли под одной
// личностью — иначе внутри одной «сессии» браузер меняется на лету, что
// само по себе подозрительно.
func pickProfile() browserProfile {
	return browserProfiles[rand.IntN(len(browserProfiles))]
}

// setBrowserHeaders навешивает заголовки как у настоящего браузера, чтобы
// пройти антибот-WAF prodoctorov. Профиль (UA + Client Hints) передаётся
// снаружи, чтобы все запросы одного цикла шли согласованно.
// navigate=true — переход по ссылке (страница врача), false — XHR к API.
// Accept-Encoding НЕ трогаем: иначе Go перестанет сам распаковывать gzip.
func setBrowserHeaders(req *http.Request, navigate bool, p browserProfile) {
	req.Header.Set("User-Agent", p.ua)
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7")
	req.Header.Set("sec-ch-ua", p.secCHUA)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", p.platform)
	if navigate {
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
		req.Header.Set("Upgrade-Insecure-Requests", "1")
		req.Header.Set("Sec-Fetch-Dest", "document")
		req.Header.Set("Sec-Fetch-Mode", "navigate")
		req.Header.Set("Sec-Fetch-Site", "none")
		req.Header.Set("Sec-Fetch-User", "?1")
	} else {
		req.Header.Set("Accept", "application/json, text/plain, */*")
		req.Header.Set("Sec-Fetch-Dest", "empty")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
}
