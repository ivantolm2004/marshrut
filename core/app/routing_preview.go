package app

import (
	"errors"
	"marshrut/route"
	"net/netip"
)

func (m *Manager) previewRouting(id, input string) (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var definition route.Rule
	var app *route.Application
	for _, row := range m.catalogueSummary() {
		if row["id"] == id {
			definition = row["definition"].(route.Rule)
			if a, ok := row["application"].(*route.Application); ok {
				app = a
			}
			break
		}
	}
	if definition.ID == "" {
		return nil, errors.New("Запись не найдена. Обновите список")
	}
	targets := []route.MatchTarget{}
	if app != nil {
		targets = append(targets, route.MatchTarget{Label: app.Path, Application: app})
	} else {
		for _, name := range definition.Processes {
			a := route.Application{Executable: name}
			targets = append(targets, route.MatchTarget{Label: name, Application: &a})
		}
		for _, path := range definition.ProcessPaths {
			a := route.NewApplication(path, false)
			targets = append(targets, route.MatchTarget{Label: path, Application: &a})
		}
	}
	for _, domain := range definition.Domains {
		targets = append(targets, route.MatchTarget{Label: domain, Domain: domain})
	}
	for _, raw := range definition.IPCIDRs {
		if p, err := netip.ParsePrefix(raw); err == nil {
			targets = append(targets, route.MatchTarget{Label: raw + " (пример: " + p.Addr().String() + ")", IP: p.Addr().String()})
		}
	}
	if input != "" {
		domain, prefix, err := route.ParseDestination(input)
		if err != nil {
			return nil, err
		}
		target := route.MatchTarget{Label: input, Domain: domain, Application: app}
		if prefix != "" {
			p, _ := netip.ParsePrefix(prefix)
			target.IP = p.Addr().String()
		}
		targets = []route.MatchTarget{target}
	}
	results := []map[string]any{}
	for i, target := range targets {
		if i >= 16 {
			break
		}
		row := map[string]any{"target": target.Label}
		for _, network := range []string{"tcp", "udp"} {
			row[network] = route.Explain(m.settings.RoutingRules, target, network)
			if m.phase != "disconnected" && m.phase != "error" {
				row["active_"+network] = route.Explain(m.appliedRules, target, network)
			}
		}
		results = append(results, row)
	}
	return map[string]any{"results": results, "total": len(targets), "notice": "Предпросмотр порядка правил для отдельных условий. Это не проверка реального трафика: IP сайта, DNS, порт и сочетание приложения с сайтом могут изменить совпадение."}, nil
}
