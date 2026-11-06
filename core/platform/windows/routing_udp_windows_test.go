package windowsruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"marshrut/engine"
	"marshrut/profile"
	"marshrut/route"
	"net"
	"os"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/protocol/socks"
)

// Route a SOCKS UDP relay to a loopback responder. The engine under test still
// sees the original domain or IP and must choose the correct relay.
func udpRoutingRelay(t *testing.T, id string) (profile.Server, string) {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buffer := make([]byte, 1024)
		for {
			n, sender, err := conn.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			conn.WriteToUDP(append([]byte(id+":"), buffer[:n]...), sender)
		}
	}()
	runtime, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)
	port, err := engine.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`{
		"log":{"disabled":true},
		"inbounds":[{"type":"mixed","tag":"relay","listen":"127.0.0.1","listen_port":%d}],
		"outbounds":[{"type":"direct","tag":"direct"}],
		"route":{"rules":[{"inbound":"relay","action":"route","outbound":"direct","override_address":"127.0.0.1","override_port":%d}]}
	}`, port, conn.LocalAddr().(*net.UDPAddr).Port)
	if err := runtime.Start([]byte(config)); err != nil {
		t.Fatal(err)
	}
	return profile.Server{ID: id, Name: id, Outbound: map[string]any{"type": "socks", "server": "127.0.0.1", "server_port": port, "version": "5"}}, conn.LocalAddr().String()
}

func TestRoutingRulesSelectActualUDPOutboundWithoutTUN(t *testing.T) {
	a, lan := udpRoutingRelay(t, "a")
	b, _ := udpRoutingRelay(t, "b")
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	site := func(id, target, network string) route.Rule {
		return route.Rule{ID: id, Name: id, Enabled: true, Route: target, Network: network, Domains: []string{"route.example"}}
	}
	app := route.Rule{ID: "app", Name: "app", Enabled: true, Route: "vless-a", ProcessPaths: []string{path}}
	subnet := route.Rule{ID: "subnet", Name: "subnet", Enabled: true, Route: "vless-a", IPCIDRs: []string{"203.0.113.0/24"}}
	for _, tc := range []struct {
		name, address, want string
		rules               []route.Rule
	}{
		{"TCP rule skips UDP packets", "route.example:25000", "b", []route.Rule{site("tcp", "vless-a", "tcp"), site("udp", "vless-b", "udp")}},
		{"process path wins", "route.example:25000", "a", []route.Rule{app, site("site", "vless-b", "")}},
		{"domain wins", "route.example:25000", "b", []route.Rule{site("site", "vless-b", ""), app}},
		{"subnet wins", "203.0.113.17:25000", "a", []route.Rule{subnet}},
		{"private IP stays local", lan, "a", []route.Rule{{ID: "all", Name: "all", Enabled: true, Route: "vless-b", IPCIDRs: []string{"0.0.0.0/0"}}}},
		{"unavailable fixed route blocks packets", "route.example:25000", "", []route.Rule{site("missing", "vless-missing", "udp"), site("site", "vless-b", "")}},
		{"strict VPN selector has no direct fallback", "route.example:25000", "", []route.Rule{site("strict", "vpn", "udp"), site("site", "vless-b", "")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime, err := New(t.TempDir(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()
			api, err := engine.FreePort()
			if err != nil {
				t.Fatal(err)
			}
			port, err := engine.FreePort()
			if err != nil {
				t.Fatal(err)
			}
			plan, err := engine.Build(engine.Options{Platform: "windows", Servers: []profile.Server{a, b}, Rules: tc.rules, APIPort: api, APISecret: "udp-fixture", ProbeUser: "fixture", ProbePassword: "fixture"})
			if err != nil {
				t.Fatal(err)
			}
			var config map[string]any
			if err := json.Unmarshal(plan.Config, &config); err != nil {
				t.Fatal(err)
			}
			config["inbounds"] = append(config["inbounds"].([]any), map[string]any{
				"type": "mixed", "tag": "udp-fixture", "listen": "127.0.0.1", "listen_port": port,
				"users": []any{map[string]any{"username": "fixture", "password": "fixture"}},
			})
			encoded, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			if err := runtime.Start(encoded); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			client := socks.NewClient(N.SystemDialer, M.ParseSocksaddr(fmt.Sprintf("127.0.0.1:%d", port)), socks.Version5, "fixture", "fixture")
			connection, err := client.DialContext(ctx, "udp", M.ParseSocksaddr(tc.address))
			if err != nil {
				t.Fatal(err)
			}
			defer connection.Close()
			connection.SetDeadline(time.Now().Add(time.Second))
			if _, err := connection.Write([]byte("game-packet")); err != nil {
				t.Fatal(err)
			}
			var reply [64]byte
			n, err := connection.Read(reply[:])
			if tc.want == "" {
				var netErr net.Error
				if !errors.As(err, &netErr) || !netErr.Timeout() {
					t.Fatalf("blocked datagram: reply %q, error %v", reply[:n], err)
				}
				return
			}
			if err != nil || string(reply[:n]) != tc.want+":game-packet" {
				t.Fatalf("wrong UDP outbound: reply %q, error %v", reply[:n], err)
			}
		})
	}
}
