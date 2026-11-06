package windowsruntime

import (
	"context"
	"fmt"
	"io"
	"marshrut/engine"
	"marshrut/probe"
	"marshrut/profile"
	"marshrut/route"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"
)

func TestEmbeddedEngineLifecycleWithoutSystemRoutes(t *testing.T) {
	r, e := New(t.TempDir(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	apiPort, _ := engine.FreePort()
	p, e := engine.Build(engine.Options{Platform: "windows", TUN: false, DPI: true, APIPort: apiPort, APISecret: "lifecycle-test", ProbeUser: "test", ProbePassword: "test", Rules: route.DefaultRules()})
	if e != nil {
		t.Fatal(e)
	}
	if e = r.Start(p.Config); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e = (engine.API{Port: apiPort, Secret: "lifecycle-test"}).Ready(ctx); e != nil {
		t.Fatal(e)
	}
	if os.Getenv("MARSHRUT_NETWORK_TEST") == "1" {
		s := probe.Check(ctx, probe.SOCKS{Address: fmt.Sprintf("127.0.0.1:%d", p.Ports["direct"]), User: "test", Password: "test"}, "www.cloudflare.com:443", "direct", false)
		if !s.Reachable {
			t.Fatalf("direct network integration: %s", s.Error)
		}
		t.Logf("Direct TLS verified: %dms", s.LatencyMS)
	}
	if e = r.Stop(); e != nil {
		t.Fatal(e)
	}
	if r.Alive() {
		t.Fatal("engine survived stop")
	}
	c, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", apiPort), 300*time.Millisecond)
	if e == nil {
		c.Close()
		t.Fatal("control listener survived stop")
	}
}
func TestPersonalVLESSConnectionsWithoutTUN(t *testing.T) {
	source := os.Getenv("MARSHRUT_TEST_SUBSCRIPTION")
	if source == "" {
		t.Skip("personal credentials only via environment")
	}
	req, _ := http.NewRequest("GET", source, nil)
	req.Header.Set("User-Agent", "Marshrut/0.1 sing-box")
	client := &http.Client{Timeout: 25 * time.Second, Transport: &http.Transport{Proxy: nil}}
	res, e := client.Do(req)
	if e != nil {
		t.Fatal("subscription unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("subscription HTTP %d", res.StatusCode)
	}
	body, e := io.ReadAll(io.LimitReader(res.Body, profile.MaxBytes+1))
	if e != nil {
		t.Fatal("subscription read failed")
	}
	imported, e := profile.Decode(string(body))
	clear(body)
	if e != nil {
		t.Fatal("subscription import failed")
	}
	apiPort, _ := engine.FreePort()
	r, e := New(t.TempDir(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	p, e := engine.Build(engine.Options{Platform: "windows", Servers: imported.Servers, TUN: false, APIPort: apiPort, BasePort: 24000, APISecret: "private-test-control", ProbeUser: "test", ProbePassword: "private-test-probes"})
	if e != nil {
		t.Fatal("configuration generation failed")
	}
	if e = r.Start(p.Config); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	success, udpSuccess := 0, 0
	limit := make(chan struct{}, 4)
	for _, s := range imported.Servers {
		tag := "vless-" + s.ID
		wg.Add(1)
		go func() {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			result := probe.Check(ctx, probe.SOCKS{Address: fmt.Sprintf("127.0.0.1:%d", p.Ports[tag]), User: "test", Password: "private-test-probes"}, "www.cloudflare.com:443", tag, true)
			mu.Lock()
			if result.Reachable {
				success++
			}
			if result.UDP {
				udpSuccess++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	t.Logf("Live subscription: %d profiles, %d verified TLS routes, %d verified UDP routes; no system routes modified", len(imported.Servers), success, udpSuccess)
	if success == 0 {
		t.Fatal("No live VLESS route passed the TLS probe on this network")
	}
}
