package app

import (
	"encoding/json"
	"errors"
	"marshrut/catalogue"
	"marshrut/profile"
	"marshrut/route"
	"strings"
)

func (m *Manager) exportCatalogue() (catalogue.Transfer, error) {
	if m.platform != "windows" {
		return catalogue.Transfer{}, errors.New("Каталог доступен в Windows")
	}
	document := catalogue.Transfer{
		Format: catalogue.TransferFormat, Schema: 1,
		Entries: append([]route.CatalogueEntry{}, m.catalogueEntries...),
		Rules:   append([]route.Rule{}, m.settings.RoutingRules...),
	}
	encoded, err := json.Marshal(document)
	if err != nil || len(encoded) > profile.MaxBytes {
		return catalogue.Transfer{}, errors.New("Каталог слишком большой для переноса одним JSON")
	}
	return document, nil
}

func (m *Manager) importCatalogue(document *catalogue.Transfer, revision uint64) error {
	if document == nil || document.Format != catalogue.TransferFormat || document.Schema != 1 || document.Entries == nil || document.Rules == nil {
		return errors.New("Нужен JSON, скопированный через «Перенос каталога»")
	}
	entries, err := route.NormalizeCatalogueEntries(document.Entries)
	if err != nil {
		return err
	}
	rules, err := route.NormalizeRules(document.Rules)
	if err != nil {
		return err
	}
	return m.updateCatalogue(func(s *catalogue.Snapshot) error {
		if err := checkCatalogueRevision(s, revision); err != nil {
			return err
		}
		s.Entries = entries
		s.Rules = rules
		syncCatalogueRules(s)
		s.Revision++
		return nil
	})
}

func (m *Manager) catalogueSeed() catalogue.Snapshot {
	return catalogue.Snapshot{Schema: 1, Revision: m.settings.RoutingRevision, Rules: m.settings.RoutingRules,
		Entries: route.SeedCatalogue(m.settings.RoutingRules), Applications: m.settings.KnownApplications}
}

func (m *Manager) loadCatalogue() error {
	s, err := catalogue.Update(m.store.Path+".routing.sqlite", m.store.Codec, m.catalogueSeed(), nil)
	if err != nil {
		return err
	}
	m.settings.RoutingRules = s.Rules
	m.settings.RoutingRevision = s.Revision
	m.settings.KnownApplications = s.Applications
	m.catalogueEntries = s.Entries
	return nil
}

// The database is authoritative. A failed compatibility mirror is surfaced
// separately; a committed database operation must not be reported as rejected.
func (m *Manager) updateCatalogue(change func(*catalogue.Snapshot) error, metadataOnly ...bool) error {
	if m.platform != "windows" {
		return errors.New("Каталог доступен в Windows")
	}
	s, err := catalogue.Update(m.store.Path+".routing.sqlite", m.store.Codec, m.catalogueSeed(), change)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settings.RoutingRules = s.Rules
	m.settings.RoutingRevision = s.Revision
	m.settings.KnownApplications = s.Applications
	m.catalogueEntries = s.Entries
	if len(metadataOnly) > 0 && metadataOnly[0] {
		return nil
	}
	m.catalogueError = ""
	if err = m.store.Save(m.settings); err != nil {
		m.catalogueError = "Каталог сохранён. Не удалось обновить копию для предыдущей версии приложения."
	}
	return nil
}

func checkCatalogueRevision(s *catalogue.Snapshot, revision uint64) error {
	if s.Revision != revision {
		return errors.New("Список уже изменился. Обновите его перед сохранением")
	}
	return nil
}

func syncCatalogueRules(s *catalogue.Snapshot) {
	for _, r := range s.Rules {
		found := false
		for i, e := range s.Entries {
			if e.ID == r.ID {
				s.Entries[i] = route.EntryFromRule(r, e.Origin)
				found = true
				break
			}
		}
		if !found {
			s.Entries = append(s.Entries, route.EntryFromRule(r, "custom"))
		}
	}
}

// Called with m.mu held, or by the operation-serialized editor. Discovery rows
// use a dedicated full path; changing one never edits a shared launcher rule.
func (m *Manager) catalogueSummary() []map[string]any {
	rows := []map[string]any{}
	apps := m.applicationSummary()
	appPaths := map[string]bool{}
	for _, a := range apps {
		appPaths[strings.ToLower(a.Path)] = true
	}
	appendRow := func(e route.CatalogueEntry, a *route.Application) {
		mode := "inherit"
		var owned *route.Rule
		for _, r := range m.settings.RoutingRules {
			if r.ID == e.Definition.ID {
				copy := r
				owned = &copy
				if r.Enabled {
					mode = r.Route
				}
				break
			}
		}
		row := map[string]any{"id": e.ID, "origin": e.Origin, "kind": e.Kind, "definition": e.Definition, "route": mode, "rule": owned}
		if a != nil {
			row["application"] = a
			row["preview_tcp"] = route.Explain(m.settings.RoutingRules, route.MatchTarget{Application: a}, "tcp")
			row["preview_udp"] = route.Explain(m.settings.RoutingRules, route.MatchTarget{Application: a}, "udp")
		}
		rows = append(rows, row)
	}
	for _, e := range m.catalogueEntries {
		r := e.Definition
		if len(r.ProcessPaths) == 1 && len(r.Domains)+len(r.Processes)+len(r.IPCIDRs) == 0 && appPaths[strings.ToLower(r.ProcessPaths[0])] {
			continue
		}
		appendRow(e, nil)
	}
	for _, a := range apps {
		r := route.Rule{ID: "app-" + a.ID, Name: a.Name, Category: "apps", Route: "auto", AutoGroup: "default", ProcessPaths: []string{a.Path}}
		for _, shared := range m.settings.RoutingRules {
			if shared.ID == a.RuleID && shared.AutoGroup != "" {
				r.AutoGroup = shared.AutoGroup
				break
			}
		}
		for _, e := range m.catalogueEntries {
			if e.ID == "app-"+a.ID {
				r = e.Definition
				break
			}
		}
		for _, existing := range m.settings.RoutingRules {
			if len(existing.ProcessPaths) == 1 && len(existing.Domains)+len(existing.Processes)+len(existing.IPCIDRs) == 0 && existing.MatchesApplication(a) {
				r = existing
				break
			}
		}
		e := route.EntryFromRule(r, "discovered")
		e.ID = "app-" + a.ID
		copy := a
		appendRow(e, &copy)
	}
	return rows
}

func (m *Manager) saveCatalogueEntry(req Request) error {
	if req.Route == "" {
		return errors.New("Выберите маршрут")
	}
	var entry route.CatalogueEntry
	if req.ID != "" {
		for _, row := range m.catalogueSummary() {
			if row["id"] == req.ID {
				entry = route.CatalogueEntry{ID: req.ID, Origin: row["origin"].(string), Kind: row["kind"].(string), Definition: row["definition"].(route.Rule)}
				break
			}
		}
		if entry.ID == "" {
			return errors.New("Запись больше не существует. Обновите список")
		}
	} else {
		entry = route.CatalogueEntry{ID: "entry-" + token()[:16], Origin: "custom"}
		entry.Definition = route.Rule{ID: entry.ID, Category: "other", AutoGroup: "default", Route: "auto"}
	}
	if req.Text != "" {
		if entry.Origin != "custom" {
			return errors.New("Условия встроенной записи меняются в редакторе правил")
		}
		domain, prefix, err := route.ParseDestination(req.Text)
		if err != nil {
			return err
		}
		entry.Definition.Domains = nil
		entry.Definition.IPCIDRs = nil
		if domain != "" {
			entry.Definition.Domains = []string{domain}
		} else {
			entry.Definition.IPCIDRs = []string{prefix}
		}
		if entry.Definition.Name == "" {
			if domain != "" {
				entry.Definition.Name = domain
			} else {
				entry.Definition.Name = prefix
			}
		}
	}
	if req.Name != "" {
		entry.Definition.Name = req.Name
	}
	entry.Definition.Route = req.Route
	entry.Definition.Enabled = true
	if req.Route == "inherit" {
		entry.Definition.Route = "auto"
		entry.Definition.Enabled = false
	}
	if req.Route == "dpi" {
		entry.Definition.Network = "tcp"
	}
	normalized, err := route.NormalizeRules([]route.Rule{entry.Definition})
	if err != nil {
		return err
	}
	entry.Definition = normalized[0]
	kind := route.EntryFromRule(entry.Definition, entry.Origin).Kind
	entry.Kind = kind
	return m.updateCatalogue(func(s *catalogue.Snapshot) error {
		if err := checkCatalogueRevision(s, req.RulesRevision); err != nil {
			return err
		}
		if len(s.Entries) >= 2048 && req.ID == "" {
			return errors.New("В каталоге не больше 2048 записей")
		}
		if req.Text != "" {
			for _, e := range s.Entries {
				if e.ID != entry.ID && sameDestination(e.Definition, entry.Definition) {
					return errors.New("Такой адрес уже есть в каталоге. Найдите его через поиск")
				}
			}
		}
		found := false
		for i, e := range s.Entries {
			if e.ID == entry.ID {
				s.Entries[i] = entry
				found = true
				break
			}
		}
		if !found {
			s.Entries = append(s.Entries, entry)
		}
		index := -1
		rules := []route.Rule{}
		for _, r := range s.Rules {
			if r.ID == entry.Definition.ID {
				index = len(rules)
				continue
			}
			rules = append(rules, r)
		}
		if req.Route != "inherit" {
			// New explicit overrides take precedence over shared defaults.
			if index < 0 {
				index = 0
			}
			rules = append(rules, route.Rule{})
			copy(rules[index+1:], rules[index:])
			rules[index] = entry.Definition
		}
		var err error
		s.Rules, err = route.NormalizeRules(rules)
		if err != nil {
			return err
		}
		s.Revision++
		return nil
	})
}

func sameDestination(a, b route.Rule) bool {
	if len(a.Processes)+len(a.ProcessPaths)+len(b.Processes)+len(b.ProcessPaths) != 0 {
		return false
	}
	return (len(a.Domains) == 1 && len(b.Domains) == 1 && a.Domains[0] == b.Domains[0]) || (len(a.IPCIDRs) == 1 && len(b.IPCIDRs) == 1 && a.IPCIDRs[0] == b.IPCIDRs[0])
}

func (m *Manager) deleteCatalogueEntry(id string, revision uint64) error {
	return m.updateCatalogue(func(s *catalogue.Snapshot) error {
		if err := checkCatalogueRevision(s, revision); err != nil {
			return err
		}
		index := -1
		for i, e := range s.Entries {
			if e.ID == id {
				index = i
				break
			}
		}
		if index < 0 {
			return errors.New("Запись не найдена. Для обнаруженного приложения выберите общий маршрут")
		}
		ruleID := s.Entries[index].Definition.ID
		s.Entries = append(s.Entries[:index], s.Entries[index+1:]...)
		rules := []route.Rule{}
		for _, r := range s.Rules {
			if r.ID != ruleID {
				rules = append(rules, r)
			}
		}
		s.Rules = rules
		s.Revision++
		return nil
	})
}
