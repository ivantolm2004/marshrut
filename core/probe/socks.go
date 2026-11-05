package probe

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"time"
)

type SOCKS struct{ Address, User, Password string }

func (s SOCKS) open(ctx context.Context, command byte, target string) (net.Conn, string, error) {
	c, e := (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, "tcp", s.Address)
	if e != nil {
		return nil, "", errors.New("Локальный прокси недоступен")
	}
	ok := false
	defer func() {
		if !ok {
			c.Close()
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	if d, yes := ctx.Deadline(); yes && d.Before(deadline) {
		deadline = d
	}
	c.SetDeadline(deadline)
	if len(s.User) > 255 || len(s.Password) > 255 {
		return nil, "", errors.New("Некорректные локальные реквизиты")
	}
	if _, e = c.Write([]byte{5, 1, 2}); e != nil {
		return nil, "", e
	}
	var b [2]byte
	if _, e = io.ReadFull(c, b[:]); e != nil || b != [2]byte{5, 2} {
		return nil, "", errors.New("SOCKS аутентификация недоступна")
	}
	auth := append([]byte{1, byte(len(s.User))}, []byte(s.User)...)
	auth = append(auth, byte(len(s.Password)))
	auth = append(auth, []byte(s.Password)...)
	if _, e = c.Write(auth); e != nil {
		return nil, "", e
	}
	if _, e = io.ReadFull(c, b[:]); e != nil || b[1] != 0 {
		return nil, "", errors.New("SOCKS аутентификация отклонена")
	}
	addr, e := address(target)
	if e != nil {
		return nil, "", e
	}
	request := append([]byte{5, command, 0}, addr...)
	if _, e = c.Write(request); e != nil {
		return nil, "", e
	}
	var header [3]byte
	if _, e = io.ReadFull(c, header[:]); e != nil || header[0] != 5 || header[1] != 0 {
		return nil, "", errors.New("Маршрут отклонил соединение")
	}
	bound, e := readAddress(c)
	if e != nil {
		return nil, "", e
	}
	ok = true
	return c, bound, nil
}
func (s SOCKS) Dial(ctx context.Context, target string) (net.Conn, error) {
	c, _, e := s.open(ctx, 1, target)
	return c, e
}
func address(target string) ([]byte, error) {
	host, port, e := net.SplitHostPort(target)
	if e != nil {
		return nil, e
	}
	p, e := strconv.Atoi(port)
	if e != nil || p < 0 || p > 65535 {
		return nil, errors.New("Некорректный порт")
	}
	var b []byte
	if ip := net.ParseIP(host); ip != nil {
		if v := ip.To4(); v != nil {
			b = append([]byte{1}, v...)
		} else {
			b = append([]byte{4}, ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			return nil, errors.New("Слишком длинный домен")
		}
		b = append([]byte{3, byte(len(host))}, []byte(host)...)
	}
	return append(b, byte(p>>8), byte(p)), nil
}
func readAddress(r io.Reader) (string, error) {
	var typ [1]byte
	if _, e := io.ReadFull(r, typ[:]); e != nil {
		return "", e
	}
	n := 0
	switch typ[0] {
	case 1:
		n = 4
	case 4:
		n = 16
	case 3:
		var l [1]byte
		if _, e := io.ReadFull(r, l[:]); e != nil {
			return "", e
		}
		n = int(l[0])
	default:
		return "", errors.New("Некорректный SOCKS ответ")
	}
	b := make([]byte, n+2)
	if _, e := io.ReadFull(r, b); e != nil {
		return "", e
	}
	host := string(b[:n])
	if typ[0] != 3 {
		host = net.IP(b[:n]).String()
	}
	return net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(b[n:])))), nil
}
