package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

type API struct {
	Port   int
	Secret string
}

func (a API) Request(ctx context.Context, method, path string, body any) error {
	data, _ := json.Marshal(body)
	req, e := http.NewRequestWithContext(ctx, method, fmt.Sprintf("http://127.0.0.1:%d%s", a.Port, path), bytes.NewReader(data))
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+a.Secret)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil}}
	res, e := client.Do(req)
	if e != nil {
		return errors.New("Сетевое ядро недоступно")
	}
	defer res.Body.Close()
	io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return errors.New("Ядро отклонило изменение маршрута")
	}
	return nil
}
func (a API) Switch(ctx context.Context, group, route string) error {
	return a.Request(ctx, "PUT", "/proxies/"+url.PathEscape(group), map[string]string{"name": route})
}
func (a API) Ready(ctx context.Context) error { return a.Request(ctx, "GET", "/version", nil) }
func FreePort() (int, error) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return 0, e
	}
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return p, nil
}
