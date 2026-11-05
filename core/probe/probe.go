package probe

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"marshrut/route"
	"net"
	"net/http"
	"strings"
	"time"
)

func Check(ctx context.Context, s SOCKS, target, tag string, udp bool) route.Sample {
	result := route.Sample{Route: tag, CheckedAt: time.Now().UTC()}
	start := time.Now()
	c, e := s.Dial(ctx, target)
	if e != nil {
		result.Error = "TCP-соединение не установлено"
		return result
	}
	defer c.Close()
	host, _, _ := net.SplitHostPort(target)
	tc := tls.Client(c, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	if e = tc.HandshakeContext(ctx); e != nil {
		result.Error = "TLS-проверка не пройдена"
		return result
	}
	result.Reachable = true
	result.LatencyMS = time.Since(start).Milliseconds()
	if tag == "warp" {
		result.WarpVerified = VerifyWarp(ctx, s)
		if !result.WarpVerified {
			result.Reachable = false
			result.Error = "Выход через WARP не подтверждён"
		}
	}
	if udp && tag != "dpi" {
		result.UDP = CheckUDP(ctx, s)
	}
	return result
}

// A successful authenticated HTTPS trace through WireGuard proves bidirectional
// tunnel traffic, including an actual WireGuard handshake, rather than process liveness.
func VerifyWarp(ctx context.Context, s SOCKS) bool {
	c, e := s.Dial(ctx, "www.cloudflare.com:443")
	if e != nil {
		return false
	}
	defer c.Close()
	t := tls.Client(c, &tls.Config{ServerName: "www.cloudflare.com", MinVersion: tls.VersionTLS12})
	if t.HandshakeContext(ctx) != nil {
		return false
	}
	if _, e = fmt.Fprint(t, "GET /cdn-cgi/trace HTTP/1.1\r\nHost: www.cloudflare.com\r\nConnection: close\r\n\r\n"); e != nil {
		return false
	}
	r, e := http.ReadResponse(bufio.NewReader(t), nil)
	if e != nil {
		return false
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return false
	}
	body, e := io.ReadAll(io.LimitReader(r.Body, 4096))
	if e != nil {
		return false
	}
	for _, line := range strings.Split(string(body), "\n") {
		if line == "warp=on" || line == "warp=plus" {
			return true
		}
	}
	return false
}

// CheckUDP sends a STUN binding request over SOCKS UDP ASSOCIATE. TCP reachability
// is deliberately not reused as proof that voice/UDP works.
func CheckUDP(ctx context.Context, s SOCKS) bool {
	control, relay, e := s.open(ctx, 3, "0.0.0.0:0")
	if e != nil {
		return false
	}
	defer control.Close()
	host, p, e := net.SplitHostPort(relay)
	if e != nil {
		return false
	}
	if host == "0.0.0.0" || host == "::" {
		proxyHost, _, _ := net.SplitHostPort(s.Address)
		relay = net.JoinHostPort(proxyHost, p)
	}
	a, e := net.ResolveUDPAddr("udp", relay)
	if e != nil {
		return false
	}
	c, e := net.DialUDP("udp", nil, a)
	if e != nil {
		return false
	}
	defer c.Close()
	deadline := time.Now().Add(4 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	c.SetDeadline(deadline)
	stun := make([]byte, 20)
	binary.BigEndian.PutUint16(stun, 1)
	binary.BigEndian.PutUint32(stun[4:], 0x2112a442)
	rand.Read(stun[8:])
	addr, _ := address("stun.l.google.com:19302")
	packet := append([]byte{0, 0, 0}, addr...)
	packet = append(packet, stun...)
	if _, e = c.Write(packet); e != nil {
		return false
	}
	b := make([]byte, 1500)
	n, e := c.Read(b)
	if e != nil || n < 4 || b[0] != 0 || b[1] != 0 || b[2] != 0 {
		return false
	}
	reader := bytes.NewReader(b[3:n])
	if _, e = readAddress(reader); e != nil {
		return false
	}
	reply, e := io.ReadAll(reader)
	return e == nil && len(reply) >= 20 && binary.BigEndian.Uint16(reply) == 0x0101 && binary.BigEndian.Uint32(reply[4:]) == 0x2112a442 && bytes.Equal(reply[8:20], stun[8:20])
}
