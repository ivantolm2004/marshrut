package ipc

import (
	"bufio"
	"context"
	"errors"
	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
	"io"
	"marshrut/app"
	"marshrut/profile"
	"net"
	"strings"
	"time"
)

const Pipe = `\\.\pipe\Marshrut.Core.v1`

func Listen(owner string) (net.Listener, error) {
	if _, e := windows.StringToSid(owner); e != nil {
		return nil, errors.New("Некорректный SID владельца")
	}
	return winio.ListenPipe(Pipe, &winio.PipeConfig{SecurityDescriptor: "D:P(A;;GA;;;SY)(A;;GA;;;" + owner + ")", MessageMode: false, InputBufferSize: 65536, OutputBufferSize: 65536})
}
func Serve(ctx context.Context, l net.Listener, m *app.Manager) error {
	go func() { <-ctx.Done(); l.Close() }()
	for {
		c, e := l.Accept()
		if e != nil {
			if ctx.Err() != nil {
				return nil
			}
			return e
		}
		go func() {
			defer c.Close()
			c.SetDeadline(time.Now().Add(5 * time.Minute))
			reader := bufio.NewReader(io.LimitReader(c, profile.MaxBytes+4097))
			b, e := reader.ReadString('\n')
			if e != nil {
				return
			}
			if len(b) > profile.MaxBytes+4096 {
				return
			}
			io.WriteString(c, m.HandleJSON(strings.TrimSpace(b))+"\n")
		}()
	}
}
func Call(ctx context.Context, request string) (string, error) {
	c, e := winio.DialPipeContext(ctx, Pipe)
	if e != nil {
		return "", errors.New("Служба Маршрут недоступна. Установите и запустите службу")
	}
	defer c.Close()
	if deadline, ok := ctx.Deadline(); ok {
		c.SetDeadline(deadline)
	}
	if len(request) > profile.MaxBytes+4096 {
		return "", errors.New("Запрос слишком большой")
	}
	if _, e = io.WriteString(c, request+"\n"); e != nil {
		return "", e
	}
	b, e := bufio.NewReader(io.LimitReader(c, 8<<20)).ReadString('\n')
	if e != nil {
		return "", errors.New("Служба не ответила")
	}
	return strings.TrimSpace(b), nil
}
