package app

import (
	"context"
	"errors"
	"fmt"
	"marshrut/probe"
	"marshrut/profile"
	"marshrut/route"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const autoServerLimit = 3
const serverPingWorkers = 16

type ServerPing struct {
	Reachable bool      `json:"reachable"`
	LatencyMS int64     `json:"latency_ms"`
	CheckedAt time.Time `json:"checked_at"`
	Error     string    `json:"error,omitempty"`
}
type ServerPingProgress struct {
	Running   bool      `json:"running"`
	Completed int       `json:"completed"`
	Total     int       `json:"total"`
	StartedAt time.Time `json:"started_at"`
	Error     string    `json:"error,omitempty"`
}
type serverPingRun struct {
	Progress ServerPingProgress
	Cancel   context.CancelFunc
}
type serverDial func(context.Context, string) (net.Conn, error)

func copyServerPings(source map[string]ServerPing) map[string]ServerPing {
	copy := make(map[string]ServerPing, len(source))
	for id, value := range source {
		copy[id] = value
	}
	return copy
}

func (m *Manager) selectServer(id string) error {
	if m.settings.SelectedServer == id {
		return nil
	}
	if id != "" {
		found := false
		for _, server := range m.settings.Servers {
			if server.ID == id {
				found = true
				break
			}
		}
		if !found {
			return errors.New("Сервер не найден в подписке")
		}
		if m.isRunning() && m.plan.Ports["vless-"+id] == 0 {
			return errors.New("Переподключитесь, чтобы применить обновлённый список серверов")
		}
	}
	if err := m.persist(func(s *Settings) { s.SelectedServer = id }); err != nil {
		return err
	}
	if m.isRunning() {
		m.mu.Lock()
		m.policy.Current = map[string]route.Decision{}
		m.mu.Unlock()
		return m.check()
	}
	return nil
}

// probeCandidatesLocked bounds the expensive service probes independently of
// subscription size. Explicit service pins override the global VLESS choice.
// Keep the previous good server; rotate failed batches rather than probing 100
// servers on every connection or monitor cycle.
func (m *Manager) probeCandidatesLocked(all []string) []string {
	var result, vless []string
	available := map[string]bool{}
	for _, candidate := range all {
		available[candidate] = true
		if strings.HasPrefix(candidate, "vless-") {
			vless = append(vless, candidate)
		} else {
			result = append(result, candidate)
		}
	}
	chosen := map[string]bool{}
	add := func(candidate string) {
		if available[candidate] && !chosen[candidate] {
			chosen[candidate] = true
			result = append(result, candidate)
		}
	}
	if m.settings.SelectedServer != "" {
		add("vless-" + m.settings.SelectedServer)
	} else {
		now := time.Now()
		type health struct {
			good, failed bool
			checkedAt    time.Time
		}
		healths := map[string]health{}
		for _, candidate := range vless {
			h := health{}
			observed, allFailed := false, true
			for _, samples := range m.policy.Samples {
				if sample, exists := samples[candidate]; exists {
					observed = true
					h.good = h.good || (sample.Reachable && now.Sub(sample.CheckedAt) < 2*time.Minute)
					allFailed = allFailed && sample.Failures >= 3
					if sample.CheckedAt.After(h.checkedAt) {
						h.checkedAt = sample.CheckedAt
					}
				}
			}
			// Failed first attempts yield to untested servers immediately. Existing
			// working routes retain the policy's three-failure hysteresis.
			h.failed = observed && (allFailed || !h.good)
			healths[candidate] = h
		}
		sort.SliceStable(vless, func(i, j int) bool {
			a, b := vless[i], vless[j]
			ah, bh := healths[a], healths[b]
			if ah.failed != bh.failed {
				return !ah.failed
			}
			if ah.good != bh.good {
				return ah.good
			}
			if ah.failed && bh.failed && !ah.checkedAt.Equal(bh.checkedAt) {
				return ah.checkedAt.Before(bh.checkedAt)
			}
			al, bl := a == "vless-"+m.settings.LastServer, b == "vless-"+m.settings.LastServer
			if al != bl {
				return al
			}
			ap, bp := m.serverPings[strings.TrimPrefix(a, "vless-")], m.serverPings[strings.TrimPrefix(b, "vless-")]
			av, bv := ap.Reachable && now.Sub(ap.CheckedAt) < 24*time.Hour, bp.Reachable && now.Sub(bp.CheckedAt) < 24*time.Hour
			if av != bv {
				return av
			}
			if av && ap.LatencyMS != bp.LatencyMS {
				return ap.LatencyMS < bp.LatencyMS
			}
			return false
		})
		for i, candidate := range vless {
			if i == autoServerLimit {
				break
			}
			add(candidate)
		}
	}
	for _, group := range []string{"youtube", "discord", "telegram", "ai", "default"} {
		if pin := m.settings.Pins[group]; strings.HasPrefix(pin, "vless-") {
			add(pin)
		}
	}
	rules := m.settings.RoutingRules
	if m.appliedRules != nil {
		rules = m.appliedRules
	}
	for _, rule := range rules {
		if rule.Enabled && strings.HasPrefix(rule.Route, "vless-") {
			add(rule.Route)
		}
	}
	return result
}

func (m *Manager) stopServerPings() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pingRun != nil && m.pingRun.Progress.Running {
		m.pingRun.Cancel()
		m.pingRun.Progress.Running = false
	}
}

func (m *Manager) startServerPings() error {
	m.mu.Lock()
	if m.pingRun != nil && m.pingRun.Progress.Running {
		m.mu.Unlock()
		return errors.New("Проверка серверов уже идёт")
	}
	if m.phase == "connecting" || m.phase == "disconnecting" {
		m.mu.Unlock()
		return errors.New("Дождитесь завершения подключения")
	}
	servers := append([]profile.Server{}, m.settings.Servers...)
	if len(servers) == 0 {
		m.mu.Unlock()
		return errors.New("Сначала добавьте серверы")
	}
	var dial serverDial = func(ctx context.Context, target string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", target)
	}
	if m.phase == "connected" || m.phase == "degraded" {
		port := m.plan.Ports["direct"]
		if port == 0 {
			m.mu.Unlock()
			return errors.New("Прямой тестовый маршрут недоступен")
		}
		// The core's direct outbound binds/protects physical sockets. A normal
		// Go dial while VPN is active could instead measure the selected tunnel.
		socks := probe.SOCKS{Address: fmt.Sprintf("127.0.0.1:%d", port), User: m.user, Password: m.password}
		dial = socks.Dial
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &serverPingRun{Progress: ServerPingProgress{Running: true, Total: len(servers), StartedAt: time.Now().UTC()}, Cancel: cancel}
	m.pingRun = run
	m.mu.Unlock()
	go func() {
		defer cancel()
		measureServerPings(ctx, servers, dial, func(id string, result ServerPing) {
			m.mu.Lock()
			defer m.mu.Unlock()
			if m.pingRun != run || !run.Progress.Running || ctx.Err() != nil {
				return
			}
			m.serverPings[id] = result
			run.Progress.Completed++
		})
		// Serialize the final cache write with imports, disconnect and selection.
		m.operation.Lock()
		defer m.operation.Unlock()
		m.mu.Lock()
		if m.pingRun != run || !run.Progress.Running || ctx.Err() != nil {
			m.mu.Unlock()
			return
		}
		cache := copyServerPings(m.serverPings)
		m.mu.Unlock()
		err := m.persist(func(s *Settings) {
			s.ServerPings = map[string]ServerPing{}
			for _, server := range s.Servers {
				if ping, ok := cache[server.ID]; ok {
					s.ServerPings[server.ID] = ping
				}
			}
		})
		m.mu.Lock()
		defer m.mu.Unlock()
		run.Progress.Running = false
		if err != nil {
			run.Progress.Error = "Не удалось сохранить результаты проверки"
		}
	}()
	return nil
}

// TCP connect latency is intentionally not reported as a verified VLESS
// handshake. Sixteen workers and a two-second deadline bound large scans.
func measureServerPings(ctx context.Context, servers []profile.Server, dial serverDial, result func(string, ServerPing)) {
	jobs := make(chan profile.Server)
	var workers sync.WaitGroup
	for i := 0; i < min(serverPingWorkers, len(servers)); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for server := range jobs {
				if ctx.Err() != nil {
					return
				}
				value := ServerPing{CheckedAt: time.Now().UTC()}
				host, _ := server.Outbound["server"].(string)
				port := fmt.Sprint(server.Outbound["server_port"])
				if n, e := strconv.Atoi(port); host == "" || e != nil || n < 1 || n > 65535 {
					value.Error = "Некорректный адрес сервера"
					result(server.ID, value)
					continue
				}
				attempt, cancel := context.WithTimeout(ctx, 2*time.Second)
				start := time.Now()
				connection, err := dial(attempt, net.JoinHostPort(host, port))
				cancel()
				if err == nil {
					connection.Close()
					value.Reachable = true
					value.LatencyMS = max(1, time.Since(start).Milliseconds())
				} else {
					value.Error = "TCP-соединение недоступно"
				}
				if ctx.Err() == nil {
					result(server.ID, value)
				}
			}
		}()
	}
send:
	for _, server := range servers {
		select {
		case jobs <- server:
		case <-ctx.Done():
			break send
		}
	}
	close(jobs)
	workers.Wait()
}
