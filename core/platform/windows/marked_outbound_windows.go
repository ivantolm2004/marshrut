package windowsruntime

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/direct"
	"github.com/sagernet/sing/common"
	M "github.com/sagernet/sing/common/metadata"
	"golang.org/x/sys/windows"
)

const dpiMarkerTTL = 203

// The marker is set after TCP connect and BEFORE returning the connection to
// sing-box. Thus only the DPI outbound's application packets are manipulated.
// Direct and VLESS/WARP data remain unmarked, even for the same destination.
// WinDivert additionally observes SYN/FIN/RST unchanged for conntrack.
type markedOutbound struct{ adapter.Outbound }

func registerMarkedOutbound(registry *outbound.Registry) {
	outbound.Register[option.DirectOutboundOptions](registry, "marshrut-dpi", func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.DirectOutboundOptions) (adapter.Outbound, error) {
		inner, e := direct.NewOutbound(ctx, router, logger, tag, options)
		if e != nil {
			return nil, e
		}
		return &markedOutbound{inner}, nil
	})
}
func (o *markedOutbound) Type() string      { return "marshrut-dpi" }
func (o *markedOutbound) Network() []string { return []string{"tcp"} }
func (o *markedOutbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	if !strings.HasPrefix(network, "tcp") {
		return nil, errors.New("DPI route only supports TCP")
	}
	conn, e := o.Outbound.DialContext(ctx, network, destination)
	if e != nil {
		return nil, e
	}
	socket, ok := common.Cast[syscall.Conn](conn)
	if !ok {
		conn.Close()
		return nil, errors.New("DPI socket control unavailable")
	}
	raw, e := socket.SyscallConn()
	if e != nil {
		conn.Close()
		return nil, e
	}
	var socketError error
	controlError := raw.Control(func(fd uintptr) {
		if destination.Addr.Is6() {
			socketError = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IPV6, windows.IPV6_UNICAST_HOPS, dpiMarkerTTL)
		} else {
			socketError = windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IP, windows.IP_TTL, dpiMarkerTTL)
		}
	})
	if controlError != nil || socketError != nil {
		conn.Close()
		return nil, errors.New("DPI socket marking failed")
	}
	return conn, nil
}
func (o *markedOutbound) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, errors.New("DPI route only supports TCP")
}
func (o *markedOutbound) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if lifecycle, ok := o.Outbound.(interface {
		Start(adapter.StartStage, *adapter.Scope) error
	}); ok {
		return lifecycle.Start(stage, scope)
	}
	return nil
}
func (o *markedOutbound) InterfaceUpdated(ctx context.Context) {
	if listener, ok := o.Outbound.(adapter.InterfaceUpdateListener); ok {
		listener.InterfaceUpdated(ctx)
	}
}
