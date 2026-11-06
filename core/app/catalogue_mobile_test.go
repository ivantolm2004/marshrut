package app

import (
	"marshrut/catalogue"
	"marshrut/route"
	"strings"
	"testing"
)

func TestAndroidCatalogueUpgradesWithoutChangingExistingRoutes(t *testing.T) {
	old := testManager(t)
	if r := old.Handle(Request{Action: "import", Text: uri}); !r.OK {
		t.Fatal(r.Error)
	}
	open := func() *Manager {
		t.Helper()
		m, err := New("android", testRuntime{}, old.store)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	m := open()
	if len(m.settings.Servers) != 1 || len(m.settings.RoutingRules) != 0 || len(m.catalogueEntries) == 0 {
		t.Fatal("migration changed the saved subscription or activated presets")
	}
	for _, entry := range m.catalogueEntries {
		if entry.Definition.Enabled || len(entry.Definition.Processes)+len(entry.Definition.ProcessPaths) != 0 {
			t.Fatal("migration installed a Windows application rule")
		}
	}
	if r := m.Handle(Request{Action: "catalogue_save", Text: "203.0.113.0/24", Route: "vpn"}); !r.OK {
		t.Fatal(r.Error)
	}
	m = open()
	doc, err := m.exportCatalogue()
	if err != nil || m.settings.RoutingRevision != 1 || len(doc.Rules) != 1 || doc.Rules[0].Route != "vpn" ||
		len(doc.Rules[0].IPCIDRs) != 1 || doc.Rules[0].IPCIDRs[0] != "203.0.113.0/24" {
		t.Fatal("Android rule was not persisted", err)
	}
	doc.Rules = append(doc.Rules, route.DefaultRules()[0])
	r := m.Handle(Request{Action: "catalogue_import", Catalogue: &catalogue.Transfer{
		Format: doc.Format, Schema: doc.Schema, Rules: doc.Rules, Entries: doc.Entries,
	}, RulesRevision: 1})
	if r.OK || !strings.Contains(r.Error, "Windows") || open().settings.RoutingRevision != 1 {
		t.Fatal("incompatible import changed Android rules", r.Error)
	}
}
