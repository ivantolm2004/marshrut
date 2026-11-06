package app

import (
	"context"
	"encoding/json"
	"marshrut/route"
	"strings"
	"testing"
	"time"
)

type applicationFixtureRuntime struct {
	testRuntime
	values []route.Application
}

func (r applicationFixtureRuntime) ListApplications(context.Context) ([]route.Application, error) {
	return r.values, nil
}

func TestDiscoveryPersistsMetadataWithoutChangingRouting(t *testing.T) {
	m := windowsRulesManager(t)
	before, _ := json.Marshal(m.settings.RoutingRules)
	a := route.NewApplication(`C:\Apps\Chrome\chrome.exe`, false)
	m.runtime = applicationFixtureRuntime{values: []route.Application{a}}
	response := m.Handle(Request{Action: "applications_refresh"})
	if !response.OK {
		t.Fatal(response.Error)
	}
	after, _ := json.Marshal(m.settings.RoutingRules)
	if string(before) != string(after) || m.settings.RoutingRevision != 0 || len(m.applications) != 1 || !m.applications[0].Running {
		t.Fatal("discovery changed routing or did not add an app")
	}
	loaded, err := New("windows", testRuntime{}, m.store)
	if err != nil || len(loaded.settings.KnownApplications) != 1 || loaded.settings.KnownApplications[0].Running {
		t.Fatal("remembered apps were lost or falsely shown as running", err)
	}
	m.mergeApplications(nil, time.Now().Add(10*time.Second))
	if len(m.applications) != 1 || m.applications[0].Running {
		t.Fatal("closed app was deleted instead of remembered")
	}
}

func TestDiscoveryIsPrivateAndFindsExistingRule(t *testing.T) {
	m := windowsRulesManager(t)
	a := route.NewApplication(`C:\SENSITIVE\dota2.exe`, false)
	m.runtime = applicationFixtureRuntime{values: []route.Application{a}}
	m.mergeApplications([]route.Application{a}, time.Now())
	response := m.Handle(Request{Action: "status"}).Data.(map[string]any)
	apps := response["applications"].([]any)
	if apps[0].(map[string]any)["rule_id"] != "game-dota2" {
		t.Fatal("existing game rule not linked")
	}
	text := m.HandleJSON(`{"action":"diagnostics"}`)
	if strings.Contains(text, "SENSITIVE") || strings.Contains(text, "dota2.exe") {
		t.Fatal("application metadata leaked into diagnostics")
	}
}

func TestDiscoveryRetriesMetadataSaveAfterFailure(t *testing.T) {
	m := windowsRulesManager(t)
	m.store.Codec = nil
	a := route.NewApplication(`C:\Apps\example.exe`, false)
	m.mergeApplications([]route.Application{a}, time.Now())
	if !m.applicationsDirty || m.applicationError == "" || len(m.applications) != 1 {
		t.Fatal("save failure lost observed app")
	}
	m.store.Codec = testCodec{}
	m.mergeApplications(nil, time.Now().Add(time.Second))
	if m.applicationsDirty || len(m.settings.KnownApplications) != 1 {
		t.Fatal("discovery did not retry metadata save")
	}
}
