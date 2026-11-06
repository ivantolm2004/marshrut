package windowsruntime

import (
	"encoding/json"
	"fmt"
	"marshrut/engine"
	"marshrut/route"
	"os"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestDNSFollowsEffectiveConnectionRules(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	site := func(id, target, network string) route.Rule {
		return route.Rule{ID: id, Name: id, Enabled: true, Route: target, Network: network, Domains: []string{"route.example"}}
	}
	app := func(id, target, network string) route.Rule {
		return route.Rule{ID: id, Name: id, Enabled: true, Route: target, Network: network, ProcessPaths: []string{path}}
	}
	for _, tc := range []struct {
		name  string
		rules []route.Rule
		want  string
	}{
		{"app overrides site", []route.Rule{app("app", "direct", ""), site("site", "vpn", "")}, "203.0.113.1"},
		{"site overrides app", []route.Rule{site("site", "vpn", ""), app("app", "direct", "")}, "203.0.113.2"},
		{"UDP tunnel beats TCP direct for shared DNS", []route.Rule{app("app", "direct", "tcp"), site("site", "vpn", "udp")}, "203.0.113.3"},
		{"shadowed UDP tunnel is ignored", []route.Rule{app("app", "direct", "udp"), site("site", "vpn", "udp")}, "203.0.113.1"},
		{"strict rule after a disabled direct rule", []route.Rule{{ID: "disabled", Name: "disabled", Route: "direct", Domains: []string{"route.example"}}, site("site", "vpn", "udp")}, "203.0.113.3"},
		{"UDP block leaves TCP usable", []route.Rule{site("block", "block", "udp"), site("allow", "direct", "tcp")}, "203.0.113.1"},
		{"missing UDP server leaves TCP usable", []route.Rule{site("missing", "vless-missing", "udp"), site("allow", "direct", "tcp")}, "203.0.113.1"},
		{"TCP block uses UDP default", []route.Rule{site("block", "block", "tcp")}, "203.0.113.5"},
		{"missing server for both transports refuses", []route.Rule{site("missing", "vless-missing", "")}, ""},
		{"both transport rules refuse", []route.Rule{site("tcp", "block", "tcp"), app("udp", "block", "udp")}, ""},
		{"IP-only condition leaves hostname lookup alone", []route.Rule{{ID: "ip", Name: "ip", Enabled: true, Route: "block", IPCIDRs: []string{"203.0.113.0/24"}}}, "203.0.113.4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := New(t.TempDir(), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			api, err := engine.FreePort()
			if err != nil {
				t.Fatal(err)
			}
			port, err := engine.FreePort()
			if err != nil {
				t.Fatal(err)
			}
			plan, err := engine.Build(engine.Options{Platform: "windows", Rules: tc.rules, APIPort: api, APISecret: "dns-fixture", ProbeUser: "fixture", ProbePassword: "fixture"})
			if err != nil {
				t.Fatal(err)
			}
			var config map[string]any
			if err := json.Unmarshal(plan.Config, &config); err != nil {
				t.Fatal(err)
			}
			config["inbounds"] = append(config["inbounds"].([]any), map[string]any{
				"type": "direct", "tag": "dns-fixture", "listen": "127.0.0.1", "listen_port": port,
				"override_address": "192.0.2.53", "override_port": 53,
			})
			router := config["route"].(map[string]any)
			router["rules"] = append([]any{map[string]any{"inbound": "dns-fixture", "action": "hijack-dns"}}, router["rules"].([]any)...)
			// Replace every resolver with local synthetic answers. A distinct
			// address proves which transport won without making upstream queries.
			responses := map[string]string{"dns-probe-direct": "203.0.113.1", "dns-vpn": "203.0.113.2", "dns-vpn-udp": "203.0.113.3", "dns-default": "203.0.113.4", "dns-default-udp": "203.0.113.5"}
			dnsConfig := config["dns"].(map[string]any)
			servers := dnsConfig["servers"].([]any)
			for i, server := range servers {
				tag := server.(map[string]any)["tag"].(string)
				ip := responses[tag]
				if ip == "" {
					ip = "203.0.113.99"
				}
				servers[i] = map[string]any{"type": "hosts", "tag": tag, "predefined": map[string]string{"route.example": ip}}
			}
			configJSON, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Start(configJSON); err != nil {
				t.Fatal(err)
			}
			query := new(dns.Msg).SetQuestion("route.example.", dns.TypeA)
			for _, transport := range []string{"tcp", "udp"} {
				client := dns.Client{Net: transport, Timeout: 3 * time.Second}
				response, _, err := client.Exchange(query, fmt.Sprintf("127.0.0.1:%d", port))
				if err != nil {
					t.Fatalf("%s query: %v", transport, err)
				}
				if tc.want == "" {
					if response.Rcode != dns.RcodeRefused || len(response.Answer) != 0 {
						t.Fatalf("%s query wasn't refused: %v", transport, response)
					}
					continue
				}
				if response.Rcode != dns.RcodeSuccess || len(response.Answer) != 1 {
					t.Fatalf("%s unexpected response: %v", transport, response)
				}
				address, ok := response.Answer[0].(*dns.A)
				if !ok || address.A.String() != tc.want {
					t.Fatalf("%s wrong DNS route: %v, want %s", transport, response.Answer, tc.want)
				}
			}
		})
	}
}
