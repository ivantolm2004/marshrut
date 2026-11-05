package probe

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

type Speed struct {
	Bytes      int64   `json:"bytes"`
	DurationMS int64   `json:"duration_ms"`
	Mbps       float64 `json:"mbps"`
}

// DownloadSpeed measures only when requested, with an explicit 2 MiB / 20s cap.
func DownloadSpeed(ctx context.Context, s SOCKS) (Speed, error) {
	var result Speed
	transport := &http.Transport{Proxy: nil, DialContext: func(c context.Context, network, address string) (net.Conn, error) { return s.Dial(c, address) }}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("speed test redirect rejected") }}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://speed.cloudflare.com/__down?bytes=2097152", nil)
	start := time.Now()
	r, e := client.Do(req)
	if e != nil {
		return result, errors.New("Не удалось измерить скорость выбранного маршрута")
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return result, errors.New("Сервер измерения скорости недоступен")
	}
	n, e := io.Copy(io.Discard, io.LimitReader(r.Body, 2097153))
	if e != nil || n != 2097152 {
		return result, errors.New("Тестовый файл получен не полностью")
	}
	elapsed := time.Since(start)
	result = Speed{n, elapsed.Milliseconds(), float64(n) * 8 / elapsed.Seconds() / 1e6}
	return result, nil
}
