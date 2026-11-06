package app

import (
	"encoding/json"
	"errors"
	"marshrut/route"
	"marshrut/secure"
	"path/filepath"
	"strings"
	"testing"
)

func TestFailedApplyRestoresPreviousSnapshotButRetainsSavedEdits(t *testing.T) {
	m := windowsRulesManager(t)
	m.phase = "connected"
	m.appliedRules = append([]route.Rule{}, m.settings.RoutingRules...)
	oldHash := route.RulesFingerprint(m.appliedRules)
	rules := append([]route.Rule{}, m.settings.RoutingRules...)
	rules[0].Route = "vpn"
	if r := m.Handle(Request{Action: "routing_save", Rules: rules}); !r.OK {
		t.Fatal(r.Error)
	}
	calls := 0
	err := m.applyRoutingWith(func(rules []route.Rule, revision uint64, refresh bool) error {
		calls++
		if calls == 1 {
			if !refresh {
				t.Fatal("new configuration skips refresh")
			}
			m.phase = "error"
			return errors.New("invalid new configuration")
		}
		if refresh || revision != 0 || route.RulesFingerprint(rules) != oldHash {
			t.Fatal("wrong rollback snapshot")
		}
		m.appliedRules = rules
		m.phase = "connected"
		return nil
	})
	if err == nil || calls != 2 || m.settings.RoutingRules[0].Route != "vpn" || m.settings.RoutingRevision != 1 || m.phase != "connected" {
		t.Fatal("recovery lost saved edits", err)
	}
	if m.Handle(Request{Action: "status"}).Data.(map[string]any)["routing_pending"] != true {
		t.Fatal("unapplied edits not visible after recovery")
	}
}

func TestExplicitDisconnectPreventsApplyRecovery(t *testing.T) {
	m := windowsRulesManager(t)
	m.phase = "connected"
	calls := 0
	err := m.applyRoutingWith(func([]route.Rule, uint64, bool) error {
		calls++
		m.disconnectRequests++
		return errors.New("cancelled")
	})
	if err == nil || calls != 1 {
		t.Fatal("recovery ignored user disconnect")
	}
}

func windowsRulesManager(t *testing.T) *Manager {
	t.Helper()
	m, err := New("windows", testRuntime{}, secure.Store{Path: filepath.Join(t.TempDir(), "settings"), Codec: testCodec{}})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestRuleEditingPersistsOrderAndEmptyListAcrossRestart(t *testing.T) {
	m := windowsRulesManager(t)
	if len(m.settings.RoutingRules) != 31 {
		t.Fatal("defaults missing")
	}
	rules := []route.Rule{m.settings.RoutingRules[2], m.settings.RoutingRules[0]}
	rules[0].Route = "vpn"
	if r := m.Handle(Request{Action: "routing_save", Rules: rules}); !r.OK {
		t.Fatal(r.Error)
	}
	loaded, err := New("windows", testRuntime{}, m.store)
	if err != nil || len(loaded.settings.RoutingRules) != 2 || loaded.settings.RoutingRules[0].ID != "game-pubg" || loaded.settings.RoutingRules[0].Route != "vpn" {
		t.Fatal("rules not persisted", err)
	}
	if r := loaded.Handle(Request{Action: "routing_save", Rules: []route.Rule{}, RulesRevision: 1}); !r.OK {
		t.Fatal(r.Error)
	}
	loaded, err = New("windows", testRuntime{}, m.store)
	if err != nil || loaded.settings.RoutingRules == nil || len(loaded.settings.RoutingRules) != 0 {
		t.Fatal("empty list restored unwanted defaults", err)
	}
}

func TestInvalidAndStaleRuleWritesAreAtomic(t *testing.T) {
	m := windowsRulesManager(t)
	if r := m.Handle(Request{Action: "routing_save", Rules: m.settings.RoutingRules}); !r.OK {
		t.Fatal(r.Error)
	}
	before, _ := json.Marshal(m.settings)
	for _, req := range []Request{
		{Action: "routing_save", Rules: []route.Rule{}, RulesRevision: 0},
		{Action: "routing_save", Rules: []route.Rule{{ID: "bad", Name: "Bad", Enabled: true, Route: "direct"}}, RulesRevision: 1},
	} {
		if r := m.Handle(req); r.OK {
			t.Fatal("invalid update accepted")
		}
		after, _ := json.Marshal(m.settings)
		if string(before) != string(after) {
			t.Fatal("failed edit modified settings")
		}
	}
}

func TestRuleSaveWhileConnectedIsPendingAndDiagnosticsStayPrivate(t *testing.T) {
	m := windowsRulesManager(t)
	r := m.settings.RoutingRules[0]
	r.Name = "SENSITIVE_NAME"
	r.Domains = []string{"SENSITIVE.example"}
	r.ProcessPaths = []string{`C:\SENSITIVE\game.exe`}
	m.phase = "connected"
	if response := m.Handle(Request{Action: "routing_save", Rules: []route.Rule{r}}); !response.OK {
		t.Fatal(response.Error)
	}
	if m.phase != "connected" {
		t.Fatal("save unexpectedly disconnected the user")
	}
	status := m.Handle(Request{Action: "status"}).Data.(map[string]any)
	if status["routing_pending"] != true {
		t.Fatal("pending changes not surfaced")
	}
	diagnostics := m.HandleJSON(`{"action":"diagnostics"}`)
	if strings.Contains(strings.ToLower(diagnostics), "sensitive") {
		t.Fatal("routing conditions leaked to diagnostics")
	}
}

func TestNewRuleGetsIDAndResetRequiresCurrentRevision(t *testing.T) {
	m := windowsRulesManager(t)
	r := m.settings.RoutingRules[0]
	r.ID = ""
	if response := m.Handle(Request{Action: "routing_save", Rules: []route.Rule{r}}); !response.OK {
		t.Fatal(response.Error)
	}
	if !strings.HasPrefix(m.settings.RoutingRules[0].ID, "custom-") {
		t.Fatal("custom ID missing")
	}
	if response := m.Handle(Request{Action: "routing_reset", RulesRevision: 1}); !response.OK || len(m.settings.RoutingRules) != 31 {
		t.Fatal("reset failed", response.Error)
	}
}
