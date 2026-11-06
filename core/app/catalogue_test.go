package app

import (
	"encoding/json"
	"marshrut/catalogue"
	"marshrut/route"
	"strings"
	"testing"
	"time"
)

func TestCatalogueTransferPreservesInactiveEntriesAndLocalDiscovery(t *testing.T) {
	source := windowsRulesManager(t)
	observed := route.NewApplication(`C:\Private\Observed.exe`, false)
	source.mergeApplications([]route.Application{observed}, time.Now())
	add := source.Handle(Request{Action: "catalogue_save", Text: "https://archive.example/page", Route: "inherit"})
	if !add.OK {
		t.Fatal(add.Error)
	}
	if response := source.Handle(Request{Action: "catalogue_save", ID: "game-dota2", Route: "vpn", RulesRevision: 1}); !response.OK {
		t.Fatal(response.Error)
	}
	exported := source.Handle(Request{Action: "catalogue_export"})
	if !exported.OK {
		t.Fatal(exported.Error)
	}
	document := exported.Data.(catalogue.Transfer)
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Observed.exe") {
		t.Fatal("untouched discovery history was exported")
	}
	destination := windowsRulesManager(t)
	local := route.NewApplication(`D:\Local\Local.exe`, false)
	destination.mergeApplications([]route.Application{local}, time.Now())
	apply := destination.Handle(Request{Action: "catalogue_import", Catalogue: &document})
	if !apply.OK {
		t.Fatal(apply.Error)
	}
	reopened, err := New("windows", testRuntime{}, destination.store)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.settings.KnownApplications) != 1 || reopened.settings.KnownApplications[0].ID != local.ID {
		t.Fatal("import changed discovery on the destination")
	}
	if route.RulesFingerprint(reopened.settings.RoutingRules) != route.RulesFingerprint(source.settings.RoutingRules) {
		t.Fatal("import changed rule behavior")
	}
	last := reopened.catalogueEntries[len(reopened.catalogueEntries)-1]
	if last.Definition.Name != "archive.example" || last.Definition.Enabled {
		t.Fatal("inactive catalogue definition was lost")
	}
	if response := reopened.Handle(Request{Action: "catalogue_import", Catalogue: &document}); response.OK {
		t.Fatal("stale import overwrote newer state")
	}
	document.Entries = append(document.Entries, document.Entries[0])
	if response := reopened.Handle(Request{Action: "catalogue_import", Catalogue: &document, RulesRevision: 1}); response.OK {
		t.Fatal("duplicate entries were accepted")
	}
	if reopened.settings.RoutingRevision != 1 {
		t.Fatal("rejected import changed the revision")
	}
}

func TestCatalogueEditRejectsDuplicateDestination(t *testing.T) {
	manager := windowsRulesManager(t)
	for i, name := range []string{"first.example", "second.example"} {
		result := manager.Handle(Request{Action: "catalogue_save", Text: name, Route: "inherit", RulesRevision: uint64(i)})
		if !result.OK {
			t.Fatal(result.Error)
		}
	}
	first := manager.catalogueEntries[len(manager.catalogueEntries)-2]
	result := manager.Handle(Request{Action: "catalogue_save", ID: first.ID, Text: "https://second.example/path", Route: "inherit", RulesRevision: 2})
	if result.OK || manager.settings.RoutingRevision != 2 {
		t.Fatal("duplicate destination accepted on edit")
	}
}

func TestCatalogueAddsLinksNetworksAndKeepsUnroutedEntries(t *testing.T) {
	m := windowsRulesManager(t)
	for i, input := range []string{"https://EXAMPLE.org/watch?v=test", "203.0.113.19/24"} {
		r := m.Handle(Request{Action: "catalogue_save", Text: input, Route: "inherit", RulesRevision: uint64(i)})
		if !r.OK {
			t.Fatal(r.Error)
		}
	}
	if len(m.settings.RoutingRules) != 31 {
		t.Fatal("catalogue membership changed routing")
	}
	loaded, err := New("windows", testRuntime{}, m.store)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.catalogueEntries) != 33 || loaded.catalogueEntries[31].Definition.Domains[0] != "example.org" || loaded.catalogueEntries[32].Definition.IPCIDRs[0] != "203.0.113.0/24" {
		t.Fatal("normalization or persistence failed")
	}
	for _, input := range []string{"https://example.org/another-path", "file://example.org", "https://user:password@private.example"} {
		before, _ := json.Marshal(loaded.settings)
		if r := loaded.Handle(Request{Action: "catalogue_save", Text: input, Route: "direct", RulesRevision: 2}); r.OK {
			t.Fatal("duplicate or unsafe address accepted")
		}
		after, _ := json.Marshal(loaded.settings)
		if string(before) != string(after) {
			t.Fatal("rejected edit changed rules")
		}
	}
}

func TestApplicationOverrideDoesNotChangeSharedRuleAndCanBeRemoved(t *testing.T) {
	m := windowsRulesManager(t)
	a := route.NewApplication(`C:\Games\Dota\dota2.exe`, false)
	m.mergeApplications([]route.Application{a}, time.Now())
	shared := m.settings.RoutingRules[0]
	id := "app-" + a.ID
	if r := m.Handle(Request{Action: "catalogue_save", ID: id, Route: "vpn"}); !r.OK {
		t.Fatal(r.Error)
	}
	if m.settings.RoutingRules[0].Route != "vpn" || m.settings.RoutingRules[0].ProcessPaths[0] != a.Path || m.settings.RoutingRules[1].Route != shared.Route {
		t.Fatal("override edited shared game rule or lost priority")
	}
	if r := m.Handle(Request{Action: "catalogue_save", ID: id, Route: "inherit", RulesRevision: 1}); !r.OK {
		t.Fatal(r.Error)
	}
	if len(m.settings.RoutingRules) != 31 || m.settings.RoutingRules[0].ID != shared.ID {
		t.Fatal("inherit removed shared rule")
	}
}

func TestCatalogueEditsAreCASAndRenamingDoesNotRequireReconnection(t *testing.T) {
	m := windowsRulesManager(t)
	m.phase = "connected"
	m.appliedRules = append([]route.Rule{}, m.settings.RoutingRules...)
	if r := m.Handle(Request{Action: "catalogue_save", ID: "game-dota2", Name: "My game", Route: "direct"}); !r.OK {
		t.Fatal(r.Error)
	}
	status := m.Handle(Request{Action: "status"}).Data.(map[string]any)
	if status["routing_pending"] != false {
		t.Fatal("cosmetic edit requires reconnect")
	}
	if r := m.Handle(Request{Action: "catalogue_save", ID: "game-dota2", Route: "vpn"}); r.OK {
		t.Fatal("stale editor overwrote data")
	}
	if r := m.Handle(Request{Action: "catalogue_save", ID: "game-dota2", Route: "vpn", RulesRevision: 1}); !r.OK {
		t.Fatal(r.Error)
	}
	status = m.Handle(Request{Action: "status"}).Data.(map[string]any)
	if status["routing_pending"] != true {
		t.Fatal("route change not pending")
	}
	if strings.Contains(m.HandleJSON(`{"action":"diagnostics"}`), "My game") {
		t.Fatal("catalogue leaked into diagnostics")
	}
}

func TestCatalogueDeletedRecordStaysDeletedAndDBWinsOverLegacyMirror(t *testing.T) {
	m := windowsRulesManager(t)
	if r := m.Handle(Request{Action: "catalogue_delete", ID: "game-dota2"}); !r.OK {
		t.Fatal(r.Error)
	}
	// Simulate an old application writing its legacy JSON after migration.
	legacy := m.settings
	legacy.RoutingRules = route.DefaultRules()
	legacy.RoutingRevision = 0
	if err := m.store.Save(legacy); err != nil {
		t.Fatal(err)
	}
	loaded, err := New("windows", testRuntime{}, m.store)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.settings.RoutingRules) != 30 || len(loaded.catalogueEntries) != 30 || loaded.settings.RoutingRevision != 1 {
		t.Fatal("migration repeated or DB edit lost")
	}
}

func TestIndividualAppKeepsServiceSpecificAutoGroup(t *testing.T) {
	m := windowsRulesManager(t)
	a := route.NewApplication(`C:\Apps\Discord\Discord.exe`, false)
	m.mergeApplications([]route.Application{a}, time.Now())
	if r := m.Handle(Request{Action: "catalogue_save", ID: "app-" + a.ID, Route: "vpn"}); !r.OK {
		t.Fatal(r.Error)
	}
	if m.settings.RoutingRules[0].AutoGroup != "discord" {
		t.Fatal("app override discarded service-specific probes")
	}
}
