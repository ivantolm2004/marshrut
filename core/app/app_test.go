package app

import (
	"encoding/json"
	"marshrut/route"
	"marshrut/secure"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type testCodec struct{}

func (testCodec) Seal(b []byte) ([]byte, error) { return append([]byte{}, b...), nil }
func (testCodec) Open(b []byte) ([]byte, error) { return append([]byte{}, b...), nil }

type testRuntime struct{}

func (testRuntime) Start([]byte) error                    { return nil }
func (testRuntime) Stop() error                           { return nil }
func (testRuntime) Alive() bool                           { return true }
func (testRuntime) DPIAvailable() bool                    { return false }
func (testRuntime) ConfigureDPI([]string, []string) error { return nil }
func testManager(t *testing.T) *Manager {
	t.Helper()
	m, e := New("test", testRuntime{}, secure.Store{Path: filepath.Join(t.TempDir(), "settings"), Codec: testCodec{}})
	if e != nil {
		t.Fatal(e)
	}
	return m
}

const uri = "vless://a7c15317-2e8b-4fa8-9dc0-832b206e023b@proxy.example:443?security=tls&type=tcp#Personal"

func TestBadSubscriptionPreservesConfig(t *testing.T) {
	m := testManager(t)
	if r := m.Handle(Request{Action: "import", Text: uri}); !r.OK {
		t.Fatal(r.Error)
	}
	before, _ := json.Marshal(m.settings)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "upstream unavailable", 502) }))
	defer server.Close()
	m.subscriptionTransport = server.Client().Transport
	if r := m.Handle(Request{Action: "import", Text: server.URL}); r.OK || !strings.Contains(r.Error, "HTTP 502") {
		t.Fatal("unavailable subscription accepted")
	}
	after, _ := json.Marshal(m.settings)
	if string(before) != string(after) {
		t.Fatal("working configuration replaced")
	}
}
func TestStatusEncodingDoesNotRaceBackgroundProbes(t *testing.T) {
	m := testManager(t)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			m.mu.Lock()
			m.policy.Record("ai", route.Sample{Route: "vless-fixture", Reachable: i%2 == 0, CheckedAt: time.Now()})
			m.mu.Unlock()
		}
	}()
	for i := 0; i < 100; i++ {
		s := m.HandleJSON(`{"action":"status"}`)
		if !json.Valid([]byte(s)) {
			t.Fatal("invalid concurrent status snapshot")
		}
	}
	wg.Wait()
}
func TestDiagnosticsNeverContainCredentials(t *testing.T) {
	m := testManager(t)
	m.Handle(Request{Action: "import", Text: uri})
	response := m.HandleJSON(`{"action":"diagnostics"}`)
	for _, secret := range []string{"a7c15317-2e8b-4fa8-9dc0-832b206e023b", "proxy.example", "vless://", "private_key", "Personal"} {
		if strings.Contains(response, secret) {
			t.Fatal("diagnostic leaked secret")
		}
	}
}
func TestConsentRequired(t *testing.T) {
	m := testManager(t)
	r := m.Handle(Request{Action: "warp_register"})
	if r.OK || !strings.Contains(r.Error, "условия") {
		t.Fatal("registration should require terms acceptance")
	}
}
func TestFailedPersistenceIsAtomic(t *testing.T) {
	m := testManager(t)
	m.store.Codec = nil
	r := m.Handle(Request{Action: "import", Text: uri})
	if r.OK || len(m.settings.Servers) != 0 {
		t.Fatal("failed save changed in-memory configuration")
	}
}
