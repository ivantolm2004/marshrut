package app

import (
	"context"
	"errors"
	"marshrut/catalogue"
	"marshrut/route"
	"sort"
	"time"
)

type ApplicationRuntime interface {
	ListApplications(context.Context) ([]route.Application, error)
}

func (m *Manager) listApplications(ctx context.Context) ([]route.Application, error) {
	provider, ok := m.runtime.(ApplicationRuntime)
	if !ok || m.platform != "windows" {
		return nil, errors.New("Обнаружение приложений доступно в Windows")
	}
	return provider.ListApplications(ctx)
}

func (m *Manager) applicationDiscoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		attempt, cancel := context.WithTimeout(ctx, 5*time.Second)
		apps, err := m.listApplications(attempt)
		cancel()
		if ctx.Err() != nil {
			return
		}
		m.operation.Lock()
		if ctx.Err() == nil {
			if err == nil {
				m.mergeApplications(apps, time.Now().UTC())
			} else {
				m.mu.Lock()
				m.applicationError = "Не удалось обновить список приложений"
				m.mu.Unlock()
			}
		}
		m.operation.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Discovery updates metadata only: no routing rule is created or enabled and
// the routing revision is unchanged. Previously seen apps remain configurable.
func (m *Manager) mergeApplications(found []route.Application, now time.Time) {
	m.mu.Lock()
	base := m.applications
	if base == nil {
		base = m.settings.KnownApplications
	}
	byID := map[string]route.Application{}
	for _, app := range base {
		app.Running = false
		app.ProcessCount = 0
		app.RuleID = ""
		byID[app.ID] = app
	}
	newApps := false
	for _, app := range found {
		if _, exists := byID[app.ID]; !exists {
			newApps = true
		}
		app.LastSeen = now
		app.Running = true
		app.RuleID = ""
		byID[app.ID] = app
	}
	values := make([]route.Application, 0, len(byID))
	for _, app := range byID {
		values = append(values, app)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Running != values[j].Running {
			return values[i].Running
		}
		return values[i].LastSeen.After(values[j].LastSeen)
	})
	if len(values) > 512 {
		values = values[:512]
	}
	m.applications = values
	m.applicationChecked = now
	m.applicationError = ""
	save := newApps || m.applicationsDirty || now.Sub(m.applicationsSaved) >= time.Minute
	m.mu.Unlock()
	if !save {
		return
	}
	stored := append([]route.Application(nil), values...)
	for i := range stored {
		stored[i].Running = false
		stored[i].ProcessCount = 0
		stored[i].RuleID = ""
	}
	err := m.updateCatalogue(func(s *catalogue.Snapshot) error { s.Applications = stored; return nil }, true)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.applicationError = "Не удалось сохранить список приложений"
		m.applicationsDirty = true
	} else {
		m.applicationsSaved = now
		m.applicationsDirty = false
	}
}

func (m *Manager) applicationSummary() []route.Application {
	values := m.applications
	if values == nil {
		values = m.settings.KnownApplications
	}
	result := append([]route.Application{}, values...)
	for i := range result {
		result[i].RuleID = route.ApplicationRuleID(result[i], m.settings.RoutingRules)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Running != result[j].Running {
			return result[i].Running
		}
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].Path < result[j].Path
	})
	return result
}
