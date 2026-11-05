package app

import (
	"errors"
	"marshrut/secure"
	"path/filepath"
	"testing"
)

func windowsStartupManager(t *testing.T) *Manager {
	t.Helper()
	m, err := New("windows", &startupFixture{enabled: true}, secure.Store{Path: filepath.Join(t.TempDir(), "settings"), Codec: testCodec{}})
	if err != nil {
		t.Fatal(err)
	}
	if r := m.Handle(Request{Action: "import", Text: uri}); !r.OK {
		t.Fatal(r.Error)
	}
	return m
}

func TestStartupConnectionHonorsSettingsAndExistingConnection(t *testing.T) {
	for _, scenario := range []string{"enabled", "toggle_off", "autorun_off", "already_connected", "connecting", "no_profile", "android"} {
		t.Run(scenario, func(t *testing.T) {
			m := windowsStartupManager(t)
			switch scenario {
			case "toggle_off":
				m.settings.ConnectOnStartup = false
			case "autorun_off":
				m.runtime.(*startupFixture).enabled = false
			case "already_connected":
				m.phase = "connected"
			case "connecting":
				m.phase = "connecting"
			case "no_profile":
				m.settings.Servers = nil
			case "android":
				m.platform = "android"
			}
			calls := 0
			attempted, err := m.connectAtStartup(func() error { calls++; m.phase = "connected"; return nil })
			expected := scenario == "enabled"
			if err != nil || attempted != expected || (calls == 1) != expected {
				t.Fatalf("attempted=%v calls=%d err=%v", attempted, calls, err)
			}
			if attempted {
				again, err := m.connectAtStartup(func() error { calls++; return nil })
				if again || err != nil || calls != 1 {
					t.Fatal("an active connection was started twice")
				}
			}
		})
	}
}

func TestStartupPreferenceDefaultsAndPersists(t *testing.T) {
	m := windowsStartupManager(t)
	if !m.settings.ConnectOnStartup {
		t.Fatal("Windows default must connect on startup")
	}
	if r := m.Handle(Request{Action: "connect_on_startup", Value: false}); !r.OK {
		t.Fatal(r.Error)
	}
	restored, err := New("windows", m.runtime, m.store)
	if err != nil || restored.settings.ConnectOnStartup {
		t.Fatal("disabled preference was lost", err)
	}
	// Settings created before the new field keep the Windows default on upgrade.
	legacy := map[string]any{"version": 1, "direct_fallback": true, "dpi_enabled": true}
	if err = m.store.Save(legacy); err != nil {
		t.Fatal(err)
	}
	restored, err = New("windows", m.runtime, m.store)
	if err != nil || !restored.settings.ConnectOnStartup {
		t.Fatal("legacy settings did not migrate", err)
	}
	android, err := New("android", m.runtime, m.store)
	if err != nil || android.settings.ConnectOnStartup {
		t.Fatal("Windows behavior enabled on Android", err)
	}
}

type failingStartupRuntime struct {
	startupFixture
	starts int
}

func (r *failingStartupRuntime) Start([]byte) error {
	r.starts++
	return errors.New("test runtime unavailable")
}
func TestStartupCommandCallsConnectionAndReportsFailure(t *testing.T) {
	m := windowsStartupManager(t)
	runtime := &failingStartupRuntime{startupFixture: startupFixture{enabled: true}}
	m.runtime = runtime
	response := m.Handle(Request{Action: "startup_connect"})
	if response.OK || runtime.starts != 1 || m.phase != "error" {
		t.Fatal("startup failure was not handled", response, runtime.starts)
	}
	if response.Error != "test runtime unavailable" {
		t.Fatal("connection error was lost")
	}
}
