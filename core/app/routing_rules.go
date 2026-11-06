package app

import (
	"errors"
	"fmt"
	"marshrut/catalogue"
	"marshrut/route"
)

func (m *Manager) saveRoutingRules(rules []route.Rule, revision uint64) error {
	if m.platform != "windows" {
		return errors.New("Редактор правил доступен в Windows")
	}
	if revision != m.settings.RoutingRevision {
		return errors.New("Список уже изменился. Обновите его перед сохранением")
	}
	if rules == nil {
		return errors.New("Не передан список правил")
	}
	copy := append([]route.Rule{}, rules...)
	for i := range copy {
		if copy[i].ID == "" {
			copy[i].ID = "custom-" + token()[:16]
		}
	}
	normalized, err := route.NormalizeRules(copy)
	if err != nil {
		return err
	}
	return m.updateCatalogue(func(s *catalogue.Snapshot) error {
		if err := checkCatalogueRevision(s, revision); err != nil {
			return err
		}
		s.Rules = normalized
		s.Revision++
		syncCatalogueRules(s)
		return nil
	})
}

// Keep saved edits after a failed apply, but bring the previous active rule
// snapshot back online. An explicit user disconnect cancels that recovery.
func (m *Manager) applyRoutingWith(start func([]route.Rule, uint64, bool) error) error {
	m.mu.Lock()
	previous := append([]route.Rule{}, m.appliedRules...)
	previousRevision := m.appliedRulesRevision
	requests := m.disconnectRequests
	m.mu.Unlock()
	if err := m.disconnect(); err != nil {
		return err
	}
	err := start(m.settings.RoutingRules, m.settings.RoutingRevision, true)
	if err == nil {
		return nil
	}
	m.mu.Lock()
	cancelled := m.disconnectRequests != requests
	m.mu.Unlock()
	if cancelled {
		return errors.New("Применение отменено: запрошено отключение")
	}
	if recovery := start(previous, previousRevision, false); recovery != nil {
		return fmt.Errorf("Новые правила не запустились: %s. Восстановить прежнее подключение тоже не удалось: %s", err, recovery)
	}
	return fmt.Errorf("Новые правила не применены: %s. Прежние маршруты восстановлены; изменения сохранены в редакторе", err)
}
