package windowsruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"marshrut/engine"
	"marshrut/probe"
	"marshrut/profile"
	"marshrut/route"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"
)

// Local HTTP CONNECT fixtures identify the chosen outbound. No public requests,
// real provider credentials, TUN adapters or system route changes are involved.
func routingProxy(t *testing.T, id string) profile.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" {
			http.Error(w, "CONNECT required", 400)
			return
		}
		conn, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		fmt.Fprint(buffer, "HTTP/1.1 200 Connection established\r\n\r\n")
		buffer.Flush()
		if _, err = http.ReadRequest(buffer.Reader); err != nil {
			return
		}
		fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(id), id)
	}))
	t.Cleanup(s.Close)
	addr := s.Listener.Addr().(*net.TCPAddr)
	return profile.Server{ID: id, Name: id, Outbound: map[string]any{"type": "http", "server": "127.0.0.1", "server_port": addr.Port}}
}

func TestRoutingRulesSelectActualTCPOutboundWithoutTUN(t *testing.T) {
	a, b := routingProxy(t, "a"), routingProxy(t, "b")
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	rule := func(id, target string) route.Rule {
		return route.Rule{ID: id, Name: id, Enabled: true, Route: target, Domains: []string{"route.example"}}
	}
	first, second := rule("first", "vless-a"), rule("second", "vless-b")
	disabled := first
	disabled.Enabled = false
	udp := first
	udp.Network = "udp"
	process := first
	process.Domains = nil
	process.ProcessPaths = []string{path}
	ip := first
	ip.Domains = nil
	ip.IPCIDRs = []string{"203.0.113.0/24"}
	for _, tt := range []struct {
		name, url, want string
		rules           []route.Rule
	}{
		{"first enabled wins", "http://route.example/test", "a", []route.Rule{first, second}},
		{"reordered", "http://route.example/test", "b", []route.Rule{second, first}},
		{"disabled skipped", "http://route.example/test", "b", []route.Rule{disabled, second}},
		{"UDP condition skips TCP", "http://route.example/test", "b", []route.Rule{udp, second}},
		{"full process path", "http://route.example/test", "a", []route.Rule{process, second}},
		{"IP subnet", "http://203.0.113.17/test", "a", []route.Rule{ip}},
		{"unavailable route blocks", "http://route.example/test", "", []route.Rule{rule("missing", "vless-missing"), second}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, err := New(t.TempDir(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			api, _ := engine.FreePort()
			port, _ := engine.FreePort()
			plan, err := engine.Build(engine.Options{Platform: "windows", Servers: []profile.Server{a, b}, Rules: tt.rules, APIPort: api, APISecret: "routing-fixture", ProbeUser: "fixture", ProbePassword: "fixture"})
			if err != nil {
				t.Fatal(err)
			}
			var config map[string]any
			if err = json.Unmarshal(plan.Config, &config); err != nil {
				t.Fatal(err)
			}
			config["inbounds"] = append(config["inbounds"].([]any), map[string]any{"type": "mixed", "tag": "routing-fixture", "listen": "127.0.0.1", "listen_port": port, "users": []any{map[string]any{"username": "fixture", "password": "fixture"}}})
			encoded, _ := json.Marshal(config)
			if err = r.Start(encoded); err != nil {
				t.Fatal(err)
			}
			socks := probe.SOCKS{Address: net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), User: "fixture", Password: "fixture"}
			client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) { return socks.Dial(ctx, address) }}}
			defer client.CloseIdleConnections()
			response, err := client.Get(tt.url)
			if tt.want == "" {
				if err == nil {
					response.Body.Close()
					t.Fatal("unavailable route leaked to another outbound")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if string(body) != tt.want {
				t.Fatal("wrong outbound", string(body))
			}
		})
	}
}
