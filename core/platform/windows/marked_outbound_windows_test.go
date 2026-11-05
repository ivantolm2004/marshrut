package windowsruntime

import (
	"context"
	"errors"
	"net"
	"os"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"github.com/sagernet/sing-box/adapter/outbound"
	M "github.com/sagernet/sing/common/metadata"
	"golang.org/x/sys/windows"
)

type plainTestOutbound struct{ outbound.Adapter }

func (p plainTestOutbound) DialContext(c context.Context, n string, d M.Socksaddr) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(c, n, d.String())
}
func (p plainTestOutbound) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, errors.New("unused")
}
func TestOnlyDPIOutboundMarksSocket(t *testing.T) {
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	go func() {
		for {
			c, e := listener.Accept()
			if e != nil {
				return
			}
			defer c.Close()
		}
	}()
	plain := plainTestOutbound{outbound.NewAdapter("direct", "test", []string{"tcp"}, nil)}
	destination := M.ParseSocksaddr(listener.Addr().String())
	marked := &markedOutbound{&plain}
	c, e := marked.DialContext(context.Background(), "tcp", destination)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	readTTL := func(conn net.Conn) int {
		t.Helper()
		raw, e := conn.(syscall.Conn).SyscallConn()
		if e != nil {
			t.Fatal(e)
		}
		ttl := 0
		var err error
		raw.Control(func(fd uintptr) {
			ttl, err = windows.GetsockoptInt(windows.Handle(fd), windows.IPPROTO_IP, windows.IP_TTL)
		})
		if err != nil {
			t.Fatal(err)
		}
		return ttl
	}
	if readTTL(c) != dpiMarkerTTL {
		t.Fatal("DPI connection was not marked before application data")
	}
	normal, e := plain.DialContext(context.Background(), "tcp", destination)
	if e != nil {
		t.Fatal(e)
	}
	defer normal.Close()
	if readTTL(normal) == dpiMarkerTTL {
		t.Fatal("ordinary direct connection unexpectedly marked")
	}
}
func TestWinDivertFilterCompilesWithoutLoadingDriver(t *testing.T) {
	path := os.Getenv("MARSHRUT_WINDIVERT")
	if path == "" {
		t.Skip("set a verified WinDivert DLL path")
	}
	source, e := os.ReadFile("runtime_windows.go")
	if e != nil {
		t.Fatal(e)
	}
	match := regexp.MustCompile(`filter := "([^"]+)"`).FindSubmatch(source)
	if len(match) != 2 {
		t.Fatal("filter source missing")
	}
	filter := string(match[1]) + " and (not ip or ip.DstAddr != 203.0.113.1) and (not ipv6 or ipv6.DstAddr != 2001:db8::1)"
	if !strings.Contains(filter, "ip.TTL == 203") {
		t.Fatal("filter marker does not match socket marker")
	}
	dll := windows.NewLazyDLL(path)
	compile := dll.NewProc("WinDivertHelperCompileFilter")
	text, _ := windows.BytePtrFromString(filter)
	var errorText *byte
	var position uint32
	result, _, _ := compile.Call(uintptr(unsafe.Pointer(text)), 0, 0, 0, uintptr(unsafe.Pointer(&errorText)), uintptr(unsafe.Pointer(&position)))
	if result == 0 {
		t.Fatalf("WinDivert filter rejected at %d: %s", position, windows.BytePtrToString(errorText))
	}
}
