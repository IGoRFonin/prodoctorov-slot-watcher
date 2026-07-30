package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/cookiejar"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

// tlsFingerprints — пул TLS-отпечатков (ClientHello) свежих Chrome. На каждое
// новое соединение выбирается случайный: у антибота prodoctorov не оказывается
// одного стабильного JA3/JA4, по которому нас можно выделить и заблокировать.
// Все три — недавние Chrome (133/131/120), их ClientHello у настоящего браузера
// почти не менялся, поэтому любой из них согласуется с UA Chrome 148–150.
var tlsFingerprints = []utls.ClientHelloID{
	utls.HelloChrome_133,
	utls.HelloChrome_131,
	utls.HelloChrome_120,
}

// pickFingerprint выбирает случайный TLS-отпечаток из пула.
func pickFingerprint() utls.ClientHelloID {
	return tlsFingerprints[rand.IntN(len(tlsFingerprints))]
}

// newHTTPClient — клиент для запросов к prodoctorov с TLS-отпечатком
// Chrome (uTLS) и HTTP/2. Обычный Go-клиент режет антибот-WAF по JA3:
// рукопожатие стандартной библиотеки не похоже на браузерное. uTLS
// шлёт ClientHello как настоящий Chrome (актуальный порядок шифров,
// расширения, GREASE и пост-квантовый обмен ключами X25519MLKEM768),
// а http2.Transport говорит по h2 — как браузер.
// Прокси тут не используется: он только для Telegram (newTelegramClient).
func newHTTPClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	tr := &http2.Transport{DialTLSContext: dialUTLS}
	return &http.Client{Jar: jar, Timeout: 30 * time.Second, Transport: tr}
}

// dialUTLS открывает TLS-соединение, притворяясь свежим Chrome на уровне
// рукопожатия (совпадают и JA3, и JA4). Конкретный отпечаток берётся случайно
// из пула (pickFingerprint) — чтобы у антибота не было одного стабильного JA3.
// Версии отпечатков (120–133) отстают от UA (Chrome 148–150) намеренно: TLS
// ClientHello у современных Chrome почти не менялся, а 133 — самый свежий из
// зашитых в uTLS v1.8.2.
func dialUTLS(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
	raw, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		raw.Close()
		return nil, err
	}
	uconn := utls.UClient(raw, &utls.Config{ServerName: host}, pickFingerprint())
	if err := uconn.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, fmt.Errorf("uTLS handshake: %w", err)
	}
	if p := uconn.ConnectionState().NegotiatedProtocol; p != http2.NextProtoTLS {
		uconn.Close()
		return nil, fmt.Errorf("сервер не согласовал HTTP/2 (ALPN=%q)", p)
	}
	return uconn, nil
}
