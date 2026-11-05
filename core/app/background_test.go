package app

import (
	"context"
	"encoding/json"
	"github.com/ViRb3/wgcf/v2/openapi"
	"io"
	"marshrut/route"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func warpSuccessResponse(r *http.Request) *http.Response {
	b, _ := json.Marshal(openapi.Register200Response{Id: "fixture", Token: "PRIVATE_AUTO_TOKEN", KeyType: "curve25519", TunnelType: "wireguard", Config: warpFixture()})
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(b))), Request: r}
}
func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("background action timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
func TestAutomaticWarpDoesNotRequireButtonAndPersists(t *testing.T) {
	m := testManager(t)
	defer m.Close()
	var calls atomic.Int32
	m.warpTransport = warpRoundTripper(func(r *http.Request) (*http.Response, error) { calls.Add(1); return warpSuccessResponse(r), nil })
	m.StartBackground()
	m.StartBackground()
	waitFor(t, func() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.settings.Warp != nil && !m.warpSetup.Running })
	loaded, e := New("test", testRuntime{}, m.store)
	if e != nil || loaded.settings.Warp == nil {
		t.Fatal("automatic profile was not persisted", e)
	}
	m.wakeWarpSetup()
	if calls.Load() != 1 {
		t.Fatal("duplicate registration")
	}
	if strings.Contains(m.HandleJSON(`{"action":"diagnostics"}`), "PRIVATE_AUTO_TOKEN") {
		t.Fatal("auto token leaked")
	}
}
func TestAutomaticWarpCanBeDisabledWithoutWaitingForHTTP(t *testing.T) {
	m := testManager(t)
	defer m.Close()
	started := make(chan struct{})
	m.warpTransport = warpRoundTripper(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	m.StartBackground()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("setup did not start")
	}
	done := make(chan Response, 1)
	go func() { done <- m.Handle(Request{Action: "auto_warp", Value: false}) }()
	select {
	case r := <-done:
		if !r.OK {
			t.Fatal(r.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP blocked settings operation")
	}
	waitFor(t, func() bool { m.mu.Lock(); defer m.mu.Unlock(); return !m.warpSetup.Running })
	if m.settings.Warp != nil {
		t.Fatal("cancelled provisioning persisted a profile")
	}
}
func TestAutomaticWarpRetriesAfterVLESSAppears(t *testing.T) {
	m := testManager(t)
	defer m.Close()
	var calls atomic.Int32
	m.warpTransport = warpRoundTripper(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return &http.Response{StatusCode: 403, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("SENSITIVE_BODY")), Request: r}, nil
		}
		return warpSuccessResponse(r), nil
	})
	m.StartBackground()
	waitFor(t, func() bool { m.mu.Lock(); defer m.mu.Unlock(); return !m.warpSetup.Running && m.warpSetup.Error != "" })
	m.mu.Lock()
	m.phase = "connected"
	m.policy.Current["default"] = route.Decision{Route: "vless-fixture"}
	m.mu.Unlock()
	m.wakeWarpSetup()
	waitFor(t, func() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.settings.Warp != nil && !m.warpSetup.Running })
	if calls.Load() != 2 {
		t.Fatal("unexpected retries", calls.Load())
	}
}
func TestAutoWarpHonorsRateLimitEvenWhenRouteChanges(t *testing.T) {
	m := testManager(t)
	defer m.Close()
	var calls atomic.Int32
	m.warpTransport = warpRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 429, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("private")), Request: r}, nil
	})
	m.provisionWarp(context.Background())
	m.mu.Lock()
	m.policy.Current["default"] = route.Decision{Route: "vless-fixture"}
	m.mu.Unlock()
	m.provisionWarp(context.Background())
	if calls.Load() != 1 {
		t.Fatal("rate limit was ignored")
	}
}

type startupFixture struct {
	testRuntime
	enabled bool
}

func (s *startupFixture) StartupSupported() bool         { return true }
func (s *startupFixture) StartupEnabled() (bool, error)  { return s.enabled, nil }
func (s *startupFixture) SetStartupEnabled(v bool) error { s.enabled = v; return nil }
func TestStartupToggleUsesPlatformController(t *testing.T) {
	m := testManager(t)
	native := &startupFixture{enabled: true}
	m.runtime = native
	if r := m.Handle(Request{Action: "autostart", Value: false}); !r.OK || native.enabled {
		t.Fatal("startup was not disabled")
	}
	s := m.Handle(Request{Action: "status"}).Data.(map[string]any)
	if s["autostart"] != false || s["autostart_supported"] != true {
		t.Fatal("incorrect startup status")
	}
}
