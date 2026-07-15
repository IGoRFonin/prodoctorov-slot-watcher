package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

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

// dialUTLS открывает TLS-соединение, притворяясь Chrome 133+ на уровне
// рукопожатия (совпадают и JA3, и JA4). Версия отпечатка отстаёт от UA
// (Chrome 148–150) намеренно: TLS ClientHello у современных Chrome почти
// не менялся, а 133 — самый свежий из зашитых в uTLS v1.8.2.
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
	uconn := utls.UClient(raw, &utls.Config{ServerName: host}, utls.HelloChrome_133)
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
