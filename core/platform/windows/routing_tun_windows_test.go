package windowsruntime

import (
	"encoding/json"
	"fmt"
	"io"
	"marshrut/engine"
	"marshrut/profile"
	"marshrut/route"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/miekg/dns"
	"golang.org/x/sys/windows"
)

// Opt-in system test. The temporary adapter routes one documentation address,
// does not configure DNS, and uses only loopback upstream fixtures.
func TestIsolatedWindowsTUNRouting(t *testing.T) {
	if os.Getenv("MARSHRUT_TUN_TEST") != "1" {
		t.Skip("set MARSHRUT_TUN_TEST=1 in an elevated Windows shell")
	}
	if !windows.GetCurrentProcessToken().IsElevated() {
		t.Fatal("TUN test requires an elevated Windows shell")
	}
	const target = "203.0.113.174"
	const hostname = "tun-route.example"
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tcp := routingProxy(t, "tcp")
	udp, _ := udpRoutingRelay(t, "udp")
	api, err := engine.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := engine.Build(engine.Options{
		Platform: "windows", TUN: true, APIPort: api, APISecret: "tun-fixture",
		ProbeUser: "fixture", ProbePassword: "fixture", Servers: []profile.Server{tcp, udp},
		Rules: []route.Rule{
			{ID: "app", Name: "Application UDP", Enabled: true, Route: "vless-udp", Network: "udp", ProcessPaths: []string{path}},
			{ID: "site", Name: "Domain TCP", Enabled: true, Route: "vless-tcp", Network: "tcp", Domains: []string{hostname}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(plan.Config, &config); err != nil {
		t.Fatal(err)
	}
	for _, raw := range config["inbounds"].([]any) {
		inbound := raw.(map[string]any)
		if inbound["tag"] == "tun-in" {
			inbound["interface_name"] = fmt.Sprintf("MarshrutLab-%d", os.Getpid())
			inbound["address"] = []string{"172.31.241.1/30"}
			inbound["route_address"] = []string{target + "/32"}
			inbound["strict_route"] = false
			inbound["dns_mode"] = "disabled"
			delete(inbound, "dns_address")
		}
	}
	router := config["route"].(map[string]any)
	router["rules"] = append([]any{
		map[string]any{"inbound": "tun-in", "port": 10553, "action": "hijack-dns"},
	}, router["rules"].([]any)...)
	dnsConfig := config["dns"].(map[string]any)
	servers := dnsConfig["servers"].([]any)
	for i, raw := range servers {
		tag := raw.(map[string]any)["tag"].(string)
		answer := "203.0.113.175"
		if tag == "dns-probe-vless-udp" {
			answer = target
		}
		servers[i] = map[string]any{"type": "hosts", "tag": tag, "predefined": map[string]string{hostname: answer}}
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := New(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if err := runtime.Start(encoded); err != nil {
		t.Fatal(err)
	}
	query := new(dns.Msg).SetQuestion(hostname+".", dns.TypeA)
	dnsClient := dns.Client{Net: "udp", Timeout: 4 * time.Second}
	response, _, err := dnsClient.Exchange(query, target+":10553")
	if err != nil || len(response.Answer) != 1 {
		t.Fatalf("DNS through TUN failed: %v, %v", response, err)
	}
	answer, ok := response.Answer[0].(*dns.A)
	if !ok || answer.A.String() != target {
		t.Fatalf("TUN DNS did not match the app: %v", response.Answer)
	}
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 4 * time.Second}
	request, err := http.NewRequest(http.MethodGet, "http://"+target+":25000/", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Send the site's real HTTP authority, as a browser would after DNS.
	// An explicit IP Host is a different destination and does not match it.
	request.Host = hostname
	httpResponse, err := client.Do(request)
	if err != nil {
		t.Fatalf("TCP through TUN failed: %v", err)
	}
	body, err := io.ReadAll(io.LimitReader(httpResponse.Body, 32))
	httpResponse.Body.Close()
	if err != nil || string(body) != "tcp" {
		t.Fatalf("wrong TCP route through TUN: %q, %v", body, err)
	}
	conn, err := net.DialTimeout("udp4", target+":25000", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(4 * time.Second))
	if _, err := conn.Write([]byte("game-packet")); err != nil {
		t.Fatal(err)
	}
	var packet [64]byte
	n, err := conn.Read(packet[:])
	if err != nil || string(packet[:n]) != "udp:game-packet" {
		t.Fatalf("wrong UDP route through TUN: %q, %v", packet[:n], err)
	}
	t.Log("Verified process-aware DNS, domain TCP, and process-path UDP through the Windows TUN adapter")
}
