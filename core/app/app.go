package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"marshrut/catalogue"
	"marshrut/engine"
	"marshrut/probe"
	"marshrut/profile"
	"marshrut/route"
	"marshrut/secure"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type Runtime interface {
	Start(config []byte) error
	Stop() error
	DPIAvailable() bool
	ConfigureDPI(domains, excluded []string) error
	Alive() bool
}
type Subscription struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Updated   time.Time `json:"updated"`
	ServerIDs []string  `json:"server_ids"`
}
type Settings struct {
	Version           int                    `json:"version"`
	Servers           []profile.Server       `json:"servers"`
	Subscriptions     []Subscription         `json:"subscriptions"`
	Warp              *profile.Warp          `json:"warp,omitempty"`
	WarpAccount       map[string]string      `json:"warp_account,omitempty"`
	Pins              map[string]string      `json:"pins"`
	DirectFallback    bool                   `json:"direct_fallback"`
	DPIEnabled        bool                   `json:"dpi_enabled"`
	SelectedServer    string                 `json:"selected_server,omitempty"`
	LastServer        string                 `json:"last_server,omitempty"`
	ServerPings       map[string]ServerPing  `json:"server_pings,omitempty"`
	AutoWarp          bool                   `json:"auto_warp"`
	ConnectOnStartup  bool                   `json:"connect_on_startup"`
	RoutingSchema     int                    `json:"routing_schema"`
	RoutingRevision   uint64                 `json:"routing_revision"`
	RoutingRules      []route.Rule           `json:"routing_rules"`
	CatalogueEntries  []route.CatalogueEntry `json:"catalogue_entries,omitempty"`
	KnownApplications []route.Application    `json:"known_applications"`
}
type Request struct {
	Action        string              `json:"action"`
	Text          string              `json:"text,omitempty"`
	ID            string              `json:"id,omitempty"`
	Group         string              `json:"group,omitempty"`
	Route         string              `json:"route,omitempty"`
	Value         bool                `json:"value,omitempty"`
	AcceptTerms   bool                `json:"accept_terms,omitempty"`
	Rules         []route.Rule        `json:"rules,omitempty"`
	RulesRevision uint64              `json:"rules_revision,omitempty"`
	Name          string              `json:"name,omitempty"`
	Catalogue     *catalogue.Transfer `json:"catalogue,omitempty"`
}
type Response struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}
type Component struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}
type Manager struct {
	mu                      sync.Mutex
	operation               sync.Mutex
	runtime                 Runtime
	store                   secure.Store
	settings                Settings
	policy                  *route.Policy
	plan                    engine.Plan
	api                     engine.API
	platform                string
	phase                   string
	lastError               string
	warnings                []string
	user, password          string
	cancel                  context.CancelFunc
	runContext              context.Context
	lastSubscriptionAttempt time.Time
	dpiExcluded             []string
	dpiError                string
	warpRegistrationError   string
	warpTransport           http.RoundTripper
	subscriptionTransport   http.RoundTripper
	serverPings             map[string]ServerPing
	pingRun                 *serverPingRun
	backgroundCancel        context.CancelFunc
	warpAttemptCancel       context.CancelFunc
	warpWake                chan struct{}
	warpSetup               WarpSetupState
	appliedRules            []route.Rule
	appliedRulesRevision    uint64
	applications            []route.Application
	applicationChecked      time.Time
	applicationsSaved       time.Time
	applicationsDirty       bool
	applicationError        string
	catalogueEntries        []route.CatalogueEntry
	catalogueError          string
	disconnectRequests      uint64
}

func New(platform string, r Runtime, store secure.Store) (*Manager, error) {
	m := &Manager{runtime: r, store: store, platform: platform, policy: route.New(), phase: "disconnected", settings: Settings{Version: 1, DirectFallback: true, DPIEnabled: true, AutoWarp: true, ConnectOnStartup: platform == "windows", Pins: map[string]string{}}}
	if e := store.Load(&m.settings); e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	if m.settings.Version != 1 {
		return nil, errors.New("Неподдерживаемая версия настроек")
	}
	if m.settings.Pins == nil {
		m.settings.Pins = map[string]string{}
	}
	if platform == "windows" {
		if m.settings.RoutingSchema == 0 {
			m.settings.RoutingSchema = 1
			m.settings.RoutingRules = route.DefaultRules()
		} else if m.settings.RoutingSchema != 1 {
			return nil, errors.New("Неподдерживаемая версия правил маршрутизации")
		}
		if m.settings.RoutingRules == nil {
			m.settings.RoutingRules = []route.Rule{}
		}
		if err := m.loadCatalogue(); err != nil {
			return nil, err
		}
	} else if platform == "android" {
		if m.settings.RoutingSchema == 0 {
			m.settings.RoutingSchema = 1
			m.settings.CatalogueEntries = mobileCatalogueSeed()
		} else if m.settings.RoutingSchema != 1 {
			return nil, errors.New("Неподдерживаемая версия правил маршрутизации")
		}
		m.catalogueEntries = m.settings.CatalogueEntries
	}
	m.serverPings = copyServerPings(m.settings.ServerPings)
	for i := range m.settings.KnownApplications {
		m.settings.KnownApplications[i].Running = false
		m.settings.KnownApplications[i].ProcessCount = 0
		m.settings.KnownApplications[i].RuleID = ""
	}
	return m, nil
}
func token() string {
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		panic("secure random source unavailable")
	}
	return hex.EncodeToString(b)
}
func (m *Manager) HandleJSON(data string) string {
	var req Request
	if len(data) > profile.MaxBytes+4096 || json.Unmarshal([]byte(data), &req) != nil {
		b, _ := json.Marshal(Response{Error: "Некорректный запрос"})
		return string(b)
	}
	response := m.Handle(req)
	b, e := json.Marshal(response)
	if e != nil {
		return `{"ok":false,"error":"Ошибка формирования ответа"}`
	}
	return string(b)
}
func (m *Manager) Handle(req Request) Response {
	if req.Action == "status" || req.Action == "diagnostics" {
		m.mu.Lock()
		defer m.mu.Unlock()
		return Response{OK: true, Data: m.summary(req.Action == "diagnostics")}
	}
	if req.Action == "disconnect" {
		m.mu.Lock()
		m.disconnectRequests++
		if m.cancel != nil {
			m.cancel()
		}
		if m.warpAttemptCancel != nil {
			m.warpAttemptCancel()
		}
		m.mu.Unlock()
	}
	m.operation.Lock()
	defer m.operation.Unlock()
	var data any
	var err error
	switch req.Action {
	case "routing_preview":
		data, err = m.previewRouting(req.ID, req.Text)
	case "catalogue_save":
		err = m.saveCatalogueEntry(req)
	case "catalogue_delete":
		err = m.deleteCatalogueEntry(req.ID, req.RulesRevision)
	case "catalogue_export":
		data, err = m.exportCatalogue()
	case "catalogue_import":
		err = m.importCatalogue(req.Catalogue, req.RulesRevision)
	case "applications_refresh":
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var apps []route.Application
		apps, err = m.listApplications(ctx)
		cancel()
		if err == nil {
			m.mergeApplications(apps, time.Now().UTC())
		}
	case "routing_save":
		err = m.saveRoutingRules(req.Rules, req.RulesRevision)
		if err == nil {
			notice := "Правила сохранены"
			if m.isRunning() {
				notice = "Правила сохранены. Нажмите «Применить», чтобы переподключиться с новым списком"
			}
			data = map[string]any{"notice": notice}
		}
	case "routing_reset":
		err = m.saveRoutingRules(route.DefaultRules(), req.RulesRevision)
	case "routing_apply":
		if m.platform == "android" {
			data = map[string]any{"notice": "Правила применятся при следующем подключении"}
			break
		}
		if m.platform != "windows" {
			err = errors.New("Редактор правил доступен в Windows")
			break
		}
		if m.isRunning() {
			err = m.applyRoutingWith(m.connectWithRules)
		} else {
			data = map[string]any{"notice": "Правила применятся при следующем подключении"}
		}
	case "connect_on_startup":
		if m.platform != "windows" {
			err = errors.New("Эта настройка доступна только в Windows")
		} else {
			err = m.persist(func(s *Settings) { s.ConnectOnStartup = req.Value })
		}
	case "startup_connect":
		var attempted bool
		attempted, err = m.connectAtStartup(m.connect)
		data = map[string]any{"attempted": attempted}
	case "autostart":
		if startup, ok := m.runtime.(StartupRuntime); ok && startup.StartupSupported() {
			err = startup.SetStartupEnabled(req.Value)
		} else {
			err = errors.New("Автозапуск на этой платформе недоступен")
		}
	case "auto_warp":
		err = m.persist(func(s *Settings) { s.AutoWarp = req.Value })
		if !req.Value {
			m.mu.Lock()
			if m.warpAttemptCancel != nil {
				m.warpAttemptCancel()
			}
			m.mu.Unlock()
		} else {
			m.wakeWarpSetup()
		}
	case "import":
		data, err = m.importText(req.Text, true)
	case "refresh":
		data, err = m.refresh(req.ID)
	case "ping_servers":
		err = m.startServerPings()
	case "cancel_server_ping":
		m.stopServerPings()
	case "select_server":
		err = m.selectServer(req.ID)
	case "remove_server":
		err = m.removeServer(req.ID)
	case "remove_subscription":
		err = m.removeSubscription(req.ID)
	case "warp_import":
		if m.isRunning() {
			err = errors.New("Отключите соединение перед изменением WARP")
			break
		}
		var w profile.Warp
		w, err = profile.ParseWireGuard(req.Text)
		if err == nil {
			err = m.persist(func(s *Settings) { s.Warp = &w; s.WarpAccount = nil })
		}
	case "warp_register":
		if !req.AcceptTerms {
			err = errors.New("Нужно принять условия Cloudflare")
			break
		}
		wasRunning := m.isRunning()
		if wasRunning && m.settings.Warp != nil {
			err = errors.New("Отключите соединение для замены существующего WARP-профиля")
			break
		}
		m.mu.Lock()
		if m.warpSetup.Running {
			m.mu.Unlock()
			err = errors.New("Автонастройка WARP уже выполняется")
			break
		}
		m.warpSetup.Running = true
		m.mu.Unlock()
		var w profile.Warp
		var account map[string]string
		w, account, err = m.registerWarp()
		m.mu.Lock()
		m.warpRegistrationError = ""
		if err != nil {
			m.warpRegistrationError = err.Error()
		}
		m.mu.Unlock()
		if err == nil {
			err = m.persist(func(s *Settings) { s.Warp = &w; s.WarpAccount = account })
			if err == nil {
				notice := "Профиль WARP создан. Подключитесь, чтобы проверить туннель"
				if wasRunning {
					notice = "Профиль WARP создан. Переподключитесь, чтобы применить и проверить WARP"
				}
				data = map[string]string{"notice": notice}
			}
		}
		m.mu.Lock()
		if err != nil {
			m.warpSetup.Error = err.Error()
			m.warpSetup.NextAttempt = time.Now().Add(10 * time.Minute)
		} else {
			m.warpSetup.Error = ""
			m.warpSetup.NextAttempt = time.Time{}
		}
		m.warpSetup.Running = false
		m.mu.Unlock()
	case "warp_remove":
		if m.isRunning() {
			err = errors.New("Сначала отключите соединение")
		} else {
			err = m.persist(func(s *Settings) { s.Warp = nil; s.WarpAccount = nil; s.AutoWarp = false })
		}
	case "connect":
		err = m.connect()
	case "disconnect":
		err = m.disconnect()
	case "probe":
		err = m.check()
	case "speed":
		m.mu.Lock()
		d := m.policy.Current[req.Group]
		port := m.plan.Ports[d.Route]
		socks := probe.SOCKS{Address: fmt.Sprintf("127.0.0.1:%d", port), User: m.user, Password: m.password}
		m.mu.Unlock()
		if port == 0 {
			err = errors.New("Нет выбранного рабочего маршрута")
			break
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		data, err = probe.DownloadSpeed(ctx, socks)
		cancel()
	case "network_changed":
		m.mu.Lock()
		m.policy.Samples = map[string]map[string]route.Sample{}
		m.policy.Current = map[string]route.Decision{}
		m.mu.Unlock()
		err = m.check()
	case "pin":
		valid := false
		for _, g := range route.Groups() {
			if g.ID == req.Group {
				valid = true
			}
		}
		if !valid {
			err = errors.New("Неизвестная группа сервисов")
			break
		}
		if req.Route != "" && req.Route != "direct" && req.Route != "dpi" && req.Route != "warp" {
			found := false
			for _, s := range m.settings.Servers {
				if "vless-"+s.ID == req.Route {
					found = true
				}
			}
			if !found {
				err = errors.New("Неизвестный маршрут")
				break
			}
		}
		err = m.persist(func(s *Settings) {
			if req.Route == "" {
				delete(s.Pins, req.Group)
			} else {
				s.Pins[req.Group] = req.Route
			}
		})
		if err == nil && m.isRunning() {
			m.mu.Lock()
			m.policy.Manual = m.copyPins()
			m.policy.Current = map[string]route.Decision{}
			m.mu.Unlock()
			err = m.check()
		}
	case "direct_fallback":
		err = m.persist(func(s *Settings) { s.DirectFallback = req.Value })
		m.mu.Lock()
		m.policy.DirectFallback = req.Value
		m.policy.Current = map[string]route.Decision{}
		m.mu.Unlock()
		if err == nil && m.isRunning() {
			err = m.check()
		}
	case "dpi_enabled":
		if m.isRunning() {
			err = errors.New("Сначала отключите соединение")
		} else {
			err = m.persist(func(s *Settings) { s.DPIEnabled = req.Value })
		}
	default:
		err = errors.New("Неизвестная команда")
	}
	if err != nil {
		m.mu.Lock()
		m.lastError = err.Error()
		m.mu.Unlock()
		return Response{Error: err.Error()}
	}
	m.mu.Lock()
	m.lastError = ""
	if req.Action == "warp_import" || req.Action == "warp_remove" {
		m.warpRegistrationError = ""
	}
	m.mu.Unlock()
	return Response{OK: true, Data: data}
}
func (m *Manager) copyPins() map[string]string {
	r := map[string]string{}
	for k, v := range m.settings.Pins {
		r[k] = v
	}
	return r
}
func (m *Manager) persist(change func(*Settings)) error {
	// Save a detached copy; a failed write leaves both in-memory and on-disk settings intact.
	m.mu.Lock()
	defer m.mu.Unlock()
	b, _ := json.Marshal(m.settings)
	var next Settings
	json.Unmarshal(b, &next)
	change(&next)
	if e := m.store.Save(next); e != nil {
		return e
	}
	m.settings = next
	return nil
}
func (m *Manager) isRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.phase != "disconnected" && m.phase != "error"
}
func (m *Manager) importText(text string, allowURL bool) (any, error) {
	if m.isRunning() {
		return nil, errors.New("Отключите соединение перед изменением серверов")
	}
	m.stopServerPings()
	text = strings.TrimSpace(text)
	source := ""
	if strings.HasPrefix(text, "https://") {
		if !allowURL {
			return nil, errors.New("Подписка вернула ссылку вместо конфигурации")
		}
		source = text
		var e error
		text, e = m.fetch(source)
		if e != nil {
			return nil, e
		}
	}
	imported, e := m.decodeSubscription(text)
	if e != nil {
		return nil, e
	}
	e = m.persist(func(s *Settings) {
		seen := map[string]bool{}
		for _, srv := range s.Servers {
			seen[srv.ID] = true
		}
		for _, srv := range imported.Servers {
			if !seen[srv.ID] {
				s.Servers = append(s.Servers, srv)
				seen[srv.ID] = true
			}
		}
		if source != "" {
			ids := []string{}
			for _, srv := range imported.Servers {
				ids = append(ids, srv.ID)
			}
			sub := Subscription{ID: subscriptionID(source), URL: source, Updated: time.Now().UTC(), ServerIDs: ids}
			found := false
			for i := range s.Subscriptions {
				if s.Subscriptions[i].ID == sub.ID {
					s.Subscriptions[i] = sub
					found = true
				}
			}
			if !found {
				s.Subscriptions = append(s.Subscriptions, sub)
			}
		}
	})
	if e != nil {
		return nil, e
	}
	m.mu.Lock()
	m.warnings = imported.Warnings
	m.mu.Unlock()
	return map[string]any{"added": len(imported.Servers), "warnings": imported.Warnings}, nil
}
func subscriptionID(source string) string { // An irreversible ID is safe in diagnostics; the URL is never returned.
	return profileID(source)
}
func (m *Manager) fetch(source string) (string, error) {
	u, e := url.Parse(source)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return "", errors.New("Нужна HTTPS-ссылка подписки")
	}
	client := &http.Client{Timeout: 25 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 3 || r.URL.Scheme != "https" {
			return errors.New("Недопустимое перенаправление подписки")
		}
		return nil
	}}
	if m.subscriptionTransport != nil {
		client.Transport = m.subscriptionTransport
	}
	if m.isRunning() {
		m.mu.Lock()
		d := m.policy.Current["default"]
		port := m.plan.Ports[d.Route]
		socks := probe.SOCKS{Address: fmt.Sprintf("127.0.0.1:%d", port), User: m.user, Password: m.password}
		m.mu.Unlock()
		if port == 0 {
			return "", errors.New("Нет маршрута для обновления подписки")
		}
		client.Transport = &http.Transport{DialContext: func(c context.Context, n, a string) (net.Conn, error) { return socks.Dial(c, a) }}
	}
	// Subscription servers may select their output format by client identity.
	// Retry only format failures; never retry authorization or network failures.
	agents := []string{"Marshrut/0.1 sing-box"}
	negotiate := m.platform == "windows" || m.platform == "android"
	if negotiate {
		agents = append(agents, "v2rayN/7.0", "Clash.Meta")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var firstError error
	for _, agent := range agents {
		req, _ := http.NewRequestWithContext(ctx, "GET", source, nil)
		req.Header.Set("User-Agent", agent)
		req.Header.Set("Accept", "application/json, text/plain, application/yaml, */*")
		r, requestError := client.Do(req)
		if requestError != nil {
			if firstError != nil {
				break
			}
			return "", errors.New("Подписка недоступна; рабочие настройки сохранены")
		}
		if r.StatusCode != 200 {
			r.Body.Close()
			if firstError != nil {
				break
			}
			return "", fmt.Errorf("Подписка: HTTP %d; рабочие настройки сохранены", r.StatusCode)
		}
		b, readError := io.ReadAll(io.LimitReader(r.Body, profile.MaxBytes+1))
		r.Body.Close()
		if readError != nil || len(b) > profile.MaxBytes {
			return "", errors.New("Не удалось прочитать подписку; рабочие настройки сохранены")
		}
		if !negotiate {
			return string(b), nil
		}
		if _, decodeError := m.decodeSubscription(string(b)); decodeError == nil {
			return string(b), nil
		} else if firstError == nil {
			firstError = decodeError
			if strings.HasPrefix(strings.TrimSpace(strings.ToLower(string(b))), "<") {
				firstError = errors.New("Ссылка вернула веб-страницу; нужна ссылка подписки, а не личного кабинета")
			}
		}
	}
	return "", fmt.Errorf("%s; рабочие настройки сохранены", firstError)
}
func (m *Manager) decodeSubscription(text string) (profile.Import, error) {
	switch m.platform {
	case "windows":
		return profile.DecodeWindows(text)
	case "android":
		return profile.DecodeAndroid(text)
	default:
		return profile.Decode(text)
	}
}
func (m *Manager) refresh(id string) (any, error) {
	m.stopServerPings()
	var targets []Subscription
	for _, s := range m.settings.Subscriptions {
		if id == "" || s.ID == id {
			targets = append(targets, s)
		}
	}
	if len(targets) == 0 {
		return nil, errors.New("Подписка не найдена")
	}
	var warnings []string
	for _, sub := range targets {
		text, e := m.fetch(sub.URL)
		if e != nil {
			return nil, e
		}
		parsed, e := m.decodeSubscription(text)
		if e != nil {
			return nil, e
		}
		e = m.persist(func(s *Settings) {
			old := map[string]bool{}
			for _, x := range sub.ServerIDs {
				old[x] = true
			}
			for _, other := range s.Subscriptions {
				if other.ID != sub.ID {
					for _, x := range other.ServerIDs {
						delete(old, x)
					}
				}
			}
			var servers []profile.Server
			for _, x := range s.Servers {
				if !old[x.ID] {
					servers = append(servers, x)
				}
			}
			seen := map[string]bool{}
			for _, x := range servers {
				seen[x.ID] = true
			}
			var ids []string
			for _, x := range parsed.Servers {
				ids = append(ids, x.ID)
				if !seen[x.ID] {
					servers = append(servers, x)
					seen[x.ID] = true
				}
			}
			s.Servers = servers
			for i := range s.Subscriptions {
				if s.Subscriptions[i].ID == sub.ID {
					s.Subscriptions[i].ServerIDs = ids
					s.Subscriptions[i].Updated = time.Now().UTC()
				}
			}
		})
		if e != nil {
			return nil, e
		}
		warnings = append(warnings, parsed.Warnings...)
	}
	running := m.isRunning()
	m.mu.Lock()
	m.warnings = warnings
	if running {
		m.warnings = append(m.warnings, "Серверы обновлены. Новая подписка применится при следующем подключении")
	}
	m.mu.Unlock()
	return map[string]any{"updated": len(targets), "warnings": warnings}, nil
}
func (m *Manager) removeServer(id string) error {
	if m.isRunning() {
		return errors.New("Сначала отключите соединение")
	}
	m.stopServerPings()
	return m.persist(func(s *Settings) {
		var next []profile.Server
		for _, x := range s.Servers {
			if x.ID != id {
				next = append(next, x)
			}
		}
		s.Servers = next
		if s.SelectedServer == id {
			s.SelectedServer = ""
		}
		if s.LastServer == id {
			s.LastServer = ""
		}
		delete(s.ServerPings, id)
		for k, v := range s.Pins {
			if v == "vless-"+id {
				delete(s.Pins, k)
			}
		}
	})
}
func (m *Manager) removeSubscription(id string) error {
	if m.isRunning() {
		return errors.New("Сначала отключите соединение")
	}
	return m.persist(func(s *Settings) {
		var next []Subscription
		for _, x := range s.Subscriptions {
			if x.ID != id {
				next = append(next, x)
			}
		}
		s.Subscriptions = next
	})
}
func (m *Manager) connect() error {
	return m.connectWithRules(m.settings.RoutingRules, m.settings.RoutingRevision, true)
}

func (m *Manager) connectWithRules(rules []route.Rule, revision uint64, refresh bool) error {
	if m.isRunning() {
		return nil
	}
	m.stopServerPings()
	// Daily refresh is attempted before starting the engine; failures preserve the cache.
	for _, s := range m.settings.Subscriptions {
		if refresh && time.Since(s.Updated) >= 24*time.Hour {
			if _, e := m.refresh(s.ID); e != nil {
				m.mu.Lock()
				m.warnings = append(m.warnings, e.Error())
				m.mu.Unlock()
			}
		}
	}
	if len(m.settings.Servers) == 0 && m.settings.Warp == nil {
		return errors.New("Добавьте VLESS-подключение или WARP-профиль")
	}
	runCtx, stop := context.WithCancel(context.Background())
	m.mu.Lock()
	m.runContext = runCtx
	m.cancel = stop
	m.phase = "connecting"
	m.lastError = ""
	m.dpiError = ""
	m.policy = route.New()
	m.policy.Manual = m.copyPins()
	m.policy.DirectFallback = m.settings.DirectFallback
	m.mu.Unlock()
	failed := true
	defer func() {
		if failed {
			stop()
			m.runtime.Stop()
			m.mu.Lock()
			m.phase = "error"
			m.mu.Unlock()
		}
	}()
	secret, user, password := token(), "marshrut", token()
	apiPort, e := engine.FreePort()
	if e != nil {
		return errors.New("Нет свободного локального порта")
	}
	o := engine.Options{Platform: m.platform, Servers: m.settings.Servers, Warp: m.settings.Warp, DPI: m.settings.DPIEnabled && m.runtime.DPIAvailable(), TUN: true, APISecret: secret, ProbeUser: user, ProbePassword: password, APIPort: apiPort}
	if m.platform == "windows" || m.platform == "android" {
		o.Rules = rules
	}
	if preparer, ok := m.runtime.(interface {
		PrepareServers(context.Context, []profile.Server) ([]profile.Server, []string)
	}); ok {
		var warnings []string
		o.Servers, warnings = preparer.PrepareServers(runCtx, o.Servers)
		m.mu.Lock()
		m.warnings = append(m.warnings, warnings...)
		m.mu.Unlock()
		if runCtx.Err() != nil {
			return errors.New("Подключение отменено")
		}
		if len(o.Servers) == 0 && o.Warp == nil {
			if len(warnings) > 0 {
				return fmt.Errorf("Нет совместимых серверов. %s", warnings[0])
			}
			return errors.New("Нет совместимых серверов в подписке")
		}
		if selected := m.settings.SelectedServer; selected != "" {
			found := false
			for _, server := range o.Servers {
				found = found || server.ID == selected
			}
			if !found {
				return errors.New("Выбранный сервер содержит неподдерживаемые параметры; включите автоматический выбор или выберите другой сервер")
			}
		}
	}
	plan, e := engine.Build(o)
	if e != nil {
		return e
	}
	m.dpiExcluded = []string{"1.1.1.1", "1.0.0.1"}
	m.mu.Lock()
	initialCandidates := m.probeCandidatesLocked(plan.Candidates)
	m.mu.Unlock()
	activeServer := map[string]bool{}
	for _, candidate := range initialCandidates {
		activeServer[candidate] = true
	}
	for _, srv := range m.settings.Servers {
		if m.platform != "windows" {
			continue
		}
		if !activeServer["vless-"+srv.ID] {
			continue
		}
		h, _ := srv.Outbound["server"].(string)
		lookupCtx, lookupCancel := context.WithTimeout(runCtx, 2*time.Second)
		ips, lookupErr := net.DefaultResolver.LookupIPAddr(lookupCtx, h)
		lookupCancel()
		if lookupErr == nil {
			for _, ip := range ips {
				m.dpiExcluded = append(m.dpiExcluded, ip.IP.String())
			}
		}
	}
	if m.platform == "windows" && m.settings.Warp != nil {
		if ips, e := net.DefaultResolver.LookupIPAddr(runCtx, m.settings.Warp.Endpoint); e == nil {
			for _, ip := range ips {
				m.dpiExcluded = append(m.dpiExcluded, ip.IP.String())
			}
		}
	}
	if o.DPI {
		if e = m.runtime.ConfigureDPI(nil, m.dpiExcluded); e != nil {
			return e
		}
	}
	if e = m.startPreparedPlan(&plan, &o); e != nil {
		return e
	}
	m.mu.Lock()
	m.plan = plan
	m.appliedRules = o.Rules
	m.appliedRulesRevision = revision
	m.api = engine.API{Port: o.APIPort, Secret: secret}
	m.user = user
	m.password = password
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(runCtx, 10*time.Second)
	defer cancel()
	for {
		if m.api.Ready(ctx) == nil {
			break
		}
		select {
		case <-ctx.Done():
			return errors.New("Сетевое ядро не запустилось")
		case <-time.After(100 * time.Millisecond):
		}
	}
	if e = m.check(); e != nil {
		return e
	}
	failed = false
	go m.monitor(runCtx)
	return nil
}
func (m *Manager) disconnect() error {
	m.stopServerPings()
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.phase = "disconnecting"
	m.mu.Unlock()
	e := m.runtime.Stop()
	m.mu.Lock()
	m.policy = route.New()
	m.plan = engine.Plan{}
	m.appliedRules = nil
	m.appliedRulesRevision = 0
	m.phase = "disconnected"
	if e != nil {
		m.phase = "error"
	}
	m.mu.Unlock()
	return e
}

// Another process can acquire an ephemeral port after planning. Rebuild with
// new ports instead of requiring the user to find and close that application.
func (m *Manager) startPreparedPlan(plan *engine.Plan, options *engine.Options) error {
	for attempt := 0; attempt < 3; attempt++ {
		var err error
		if advanced, ok := m.runtime.(interface{ StartPlan(engine.Plan) error }); ok {
			err = advanced.StartPlan(*plan)
		} else if len(plan.XrayConfig) > 0 {
			return errors.New("Этот сервер требует Windows-версию с Xray")
		} else {
			err = m.runtime.Start(plan.Config)
		}
		if err == nil || !errors.Is(err, engine.ErrLocalPortBusy) || attempt == 2 {
			return err
		}
		options.APIPort, err = engine.FreePort()
		if err != nil {
			return engine.ErrLocalPortBusy
		}
		*plan, err = engine.Build(*options)
		if err != nil {
			return err
		}
	}
	return engine.ErrLocalPortBusy
}
func (m *Manager) Close() {
	m.stopServerPings()
	m.mu.Lock()
	if m.backgroundCancel != nil {
		m.backgroundCancel()
	}
	if m.warpAttemptCancel != nil {
		m.warpAttemptCancel()
	}
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()
	m.operation.Lock()
	defer m.operation.Unlock()
	m.disconnect()
}
func (m *Manager) monitor(ctx context.Context) {
	network := networkSignature()
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
			m.operation.Lock()
			if ctx.Err() != nil {
				m.operation.Unlock()
				return
			}
			if !m.runtime.Alive() {
				m.mu.Lock()
				m.phase = "error"
				m.lastError = "Сетевое ядро остановилось"
				m.mu.Unlock()
				m.runtime.Stop()
				m.operation.Unlock()
				return
			}
			next := networkSignature()
			if next != network {
				if len(m.plan.XrayConfig) > 0 {
					// Xray socket binding must follow the new physical interface.
					m.disconnect()
					if err := m.connect(); err != nil {
						m.mu.Lock()
						m.lastError = err.Error()
						m.mu.Unlock()
					}
					m.operation.Unlock()
					return
				}
				m.mu.Lock()
				m.policy.Samples = map[string]map[string]route.Sample{}
				m.policy.Current = map[string]route.Decision{}
				m.mu.Unlock()
				network = next
			}
			if e := m.check(); e != nil {
				m.mu.Lock()
				m.lastError = e.Error()
				m.mu.Unlock()
			}
			m.operation.Unlock()
		}
	}
}
func (m *Manager) check() error {
	m.mu.Lock()
	if m.plan.Ports == nil {
		m.mu.Unlock()
		return errors.New("Сначала подключите приложение")
	}
	ports := m.plan.Ports
	user, password := m.user, m.password
	candidates := m.probeCandidatesLocked(m.plan.Candidates)
	runCtx := m.runContext
	m.mu.Unlock()
	if runCtx == nil {
		runCtx = context.Background()
	}
	if runCtx.Err() != nil {
		return errors.New("Подключение отменено")
	}
	// DPI packets have their own socket marker on Windows; normal direct probes
	// do not require stopping DPI, which would disrupt new application connections.
	if m.platform == "windows" {
		usesDPI := false
		for _, r := range candidates {
			if r == "dpi" {
				usesDPI = true
			}
		}
		if usesDPI {
			var domains []string
			for _, g := range route.Groups() {
				domains = append(domains, g.Domains...)
			}
			if e := m.runtime.ConfigureDPI(domains, m.dpiExcluded); e != nil {
				var next []string
				for _, r := range candidates {
					if r != "dpi" {
						next = append(next, r)
					}
				}
				candidates = next
				m.mu.Lock()
				m.dpiError = e.Error()
				m.warnings = append(m.warnings, "DPI-обход недоступен; продолжается проверка других маршрутов")
				m.mu.Unlock()
			} else {
				m.mu.Lock()
				m.dpiError = ""
				m.mu.Unlock()
			}
		}
	}
	checkRoutes := func(routes []string) {
		var wg sync.WaitGroup
		limit := make(chan struct{}, 8)
		for _, r := range routes {
			if runCtx.Err() != nil {
				break
			}
			wg.Add(1)
			go func(r string) {
				defer wg.Done()
				select {
				case limit <- struct{}{}:
				case <-runCtx.Done():
					return
				}
				defer func() { <-limit }()
				socks := probe.SOCKS{Address: fmt.Sprintf("127.0.0.1:%d", ports[r]), User: user, Password: password}
				udp := false
				if r != "dpi" {
					ctx, cancel := context.WithTimeout(runCtx, 5*time.Second)
					udp = probe.CheckUDP(ctx, socks)
					cancel()
				}
				var groupChecks sync.WaitGroup
				for _, g := range route.Groups() {
					if runCtx.Err() != nil {
						break
					}
					groupChecks.Add(1)
					go func(g route.Group) {
						defer groupChecks.Done()
						ctx, cancel := context.WithTimeout(runCtx, 8*time.Second)
						defer cancel()
						sample := probe.Check(ctx, socks, g.Probe, r, false)
						sample.UDP = udp
						m.mu.Lock()
						m.policy.Record(g.ID, sample)
						m.mu.Unlock()
					}(g)
				}
				groupChecks.Wait()
			}(r)
		}
		wg.Wait()
	}
	checkRoutes(candidates)
	// Try further small batches immediately when a provider's first nodes are
	// unavailable. Bound startup work and retain explicit manual selections.
	if m.settings.SelectedServer == "" {
		seen := map[string]bool{}
		for _, candidate := range candidates {
			seen[candidate] = true
		}
		for round := 0; round < 2 && runCtx.Err() == nil; round++ {
			m.mu.Lock()
			workingTunnel := false
			for _, samples := range m.policy.Samples {
				for tag, sample := range samples {
					workingTunnel = workingTunnel || (strings.HasPrefix(tag, "vless-") && sample.Reachable && time.Since(sample.CheckedAt) < 2*time.Minute)
				}
			}
			next := m.probeCandidatesLocked(m.plan.Candidates)
			m.mu.Unlock()
			if workingTunnel {
				break
			}
			var batch []string
			for _, candidate := range next {
				if !seen[candidate] {
					seen[candidate] = true
					batch = append(batch, candidate)
					candidates = append(candidates, candidate)
				}
			}
			if len(batch) == 0 {
				break
			}
			checkRoutes(batch)
		}
	}
	if runCtx.Err() != nil {
		return errors.New("Подключение отменено")
	}
	hasWorking := false
	for _, g := range route.Groups() {
		for _, udp := range []bool{false, true} {
			m.mu.Lock()
			d := m.policy.Choose(g, candidates, udp, time.Now().UTC())
			m.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			e := m.api.Switch(ctx, d.Group, d.Route)
			cancel()
			if e != nil {
				return e
			}
			m.mu.Lock()
			m.policy.Commit(d)
			m.mu.Unlock()
			m.mu.Lock()
			if !udp && d.Route != "block" && m.policy.Samples[g.ID][d.Route].Reachable {
				hasWorking = true
			}
			m.mu.Unlock()
		}
	}
	for _, group := range route.Groups() {
		for _, udp := range []bool{false, true} {
			m.mu.Lock()
			decision := m.policy.ChooseTunnelFor(group.ID, candidates, udp, time.Now().UTC())
			m.mu.Unlock()
			ctx, cancel := context.WithTimeout(runCtx, 4*time.Second)
			err := m.api.Switch(ctx, decision.Group, decision.Route)
			cancel()
			if err != nil {
				return err
			}
			m.mu.Lock()
			m.policy.Commit(decision)
			m.mu.Unlock()
		}
	}
	if time.Since(m.lastRefresh()) >= 24*time.Hour && time.Since(m.lastSubscriptionAttempt) >= time.Hour && len(m.settings.Subscriptions) > 0 {
		m.lastSubscriptionAttempt = time.Now()
		if _, e := m.refresh(""); e != nil {
			m.mu.Lock()
			m.warnings = append(m.warnings, e.Error())
			m.mu.Unlock()
		}
	}
	m.mu.Lock()
	if hasWorking {
		m.phase = "connected"
	} else {
		m.phase = "degraded"
	}
	lastServer := ""
	for _, group := range []string{"default", "ai"} {
		d := m.policy.Current[group]
		if strings.HasPrefix(d.Route, "vless-") && m.policy.Samples[group][d.Route].Reachable {
			lastServer = strings.TrimPrefix(d.Route, "vless-")
			break
		}
	}
	remember := lastServer != "" && lastServer != m.settings.LastServer
	m.mu.Unlock()
	if remember {
		_ = m.persist(func(s *Settings) { s.LastServer = lastServer })
	}
	m.wakeWarpSetup()
	return nil
}
func (m *Manager) lastRefresh() time.Time {
	latest := time.Time{}
	for _, s := range m.settings.Subscriptions {
		if s.Updated.After(latest) {
			latest = s.Updated
		}
	}
	return latest
}
func networkSignature() string {
	ifs, _ := net.Interfaces()
	var values []string
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			values = append(values, i.Name+"="+a.String())
		}
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}
func (m *Manager) summary(diagnostics bool) map[string]any {
	servers := []map[string]string{}
	for _, s := range m.settings.Servers {
		servers = append(servers, map[string]string{"id": s.ID, "name": s.Name, "route": "vless-" + s.ID})
	}
	subscriptions := []map[string]any{}
	for _, s := range m.settings.Subscriptions {
		subscriptions = append(subscriptions, map[string]any{"id": s.ID, "updated": s.Updated, "servers": len(s.ServerIDs)})
	}
	vlessState := "unconfigured"
	if len(servers) > 0 {
		vlessState = "configured"
	}
	warpState := "unconfigured"
	if m.settings.Warp != nil {
		warpState = "configured"
	}
	dpiState := "unavailable"
	if m.runtime.DPIAvailable() {
		dpiState = "available"
	}
	if !m.settings.DPIEnabled {
		dpiState = "disabled"
	}
	attempted := map[string]bool{}
	working := map[string]bool{}
	for _, samples := range m.policy.Samples {
		for r, s := range samples {
			component := r
			if strings.HasPrefix(r, "vless-") {
				component = "vless"
			}
			attempted[component] = true
			working[component] = working[component] || s.Reachable
		}
	}
	for component, state := range map[string]*string{"vless": &vlessState, "warp": &warpState, "dpi": &dpiState} {
		if attempted[component] {
			*state = "error"
			if working[component] && m.phase != "error" {
				*state = "verified"
			}
		}
	}
	components := []Component{{"vless", vlessState, "Импортируемые пользовательские серверы"}, {"warp", warpState, "Встроенный WireGuard; проверка выхода Cloudflare"}, {"dpi", dpiState, ""}}
	if m.platform == "android" {
		components[2].Detail = "tpws: только TCP, без root"
	} else {
		components[2].Detail = "zapret2 / WinDivert"
	}
	if m.dpiError != "" {
		components[2].State = "error"
		components[2].Detail = m.dpiError
	}
	if m.settings.Warp == nil && m.warpRegistrationError != "" {
		components[1].State = "error"
		components[1].Detail = m.warpRegistrationError
	}
	if m.settings.Warp == nil && m.warpSetup.Running {
		components[1].State = "configuring"
		components[1].Detail = "Подготавливаем профиль автоматически"
	}
	result := map[string]any{"version": "0.1.7", "platform": m.platform, "phase": m.phase, "error": m.lastError, "components": components, "decisions": m.policy.Current, "samples": m.policy.Samples, "groups": route.Groups(), "pins": m.settings.Pins, "direct_fallback": m.settings.DirectFallback, "dpi_enabled": m.settings.DPIEnabled, "warnings": m.warnings, "servers": servers, "subscriptions": subscriptions,
		"selected_server": m.settings.SelectedServer, "server_pings": m.serverPings, "auto_warp": m.settings.AutoWarp, "warp_setup": m.warpSetup, "connect_on_startup": m.settings.ConnectOnStartup}
	if startup, ok := m.runtime.(StartupRuntime); ok && startup.StartupSupported() {
		result["autostart_supported"] = true
		enabled, e := startup.StartupEnabled()
		result["autostart"] = enabled
		if e != nil {
			result["autostart_error"] = "Не удалось прочитать настройку автозапуска"
		}
	}
	if m.pingRun != nil {
		result["server_ping"] = m.pingRun.Progress
	}
	if m.platform == "windows" {
		result["version"] = "0.1.11"
		result["compatibility_build"] = "windows-routing-dns-1"
	} else if m.platform == "android" {
		result["version"] = "0.1.11"
		result["compatibility_build"] = "android-provider-catalogue-1"
	}
	if m.platform == "windows" || m.platform == "android" {
		result["routing_supported"] = true
		result["catalogue_supported"] = true
		catalogueRows := m.catalogueSummary()
		result["catalogue"] = catalogueRows
		result["catalogue_record_count"] = len(catalogueRows)
		result["catalogue_schema"] = 1
		result["catalogue_transfer_supported"] = true
		result["catalogue_error"] = m.catalogueError
		if _, ok := m.runtime.(ApplicationRuntime); ok {
			result["application_discovery_supported"] = true
			result["applications"] = m.applicationSummary()
			result["applications_checked"] = m.applicationChecked
			result["applications_error"] = m.applicationError
		}
		result["routing_rules"] = m.settings.RoutingRules
		result["routing_revision"] = m.settings.RoutingRevision
		result["routing_pending"] = m.phase != "disconnected" && m.phase != "error" && route.RulesFingerprint(m.appliedRules) != route.RulesFingerprint(m.settings.RoutingRules)
		engines := map[string]int{"sing-box": 0, "xray": 0}
		for _, server := range m.settings.Servers {
			if server.Xray != nil {
				engines["xray"]++
			} else {
				engines["sing-box"]++
			}
		}
		result["configured_engines"] = engines
	}
	if diagnostics {
		delete(result, "servers")
		delete(result, "subscriptions")
		delete(result, "routing_rules")
		delete(result, "applications")
		delete(result, "catalogue")
		result["known_application_count"] = len(m.applications)
		result["routing_rule_count"] = len(m.settings.RoutingRules)
		result["notice"] = "Проверки TLS/STUN не подтверждают вход в аккаунт, воспроизведение видео или реальный звонок"
	}
	// Status is serialized after the mutex is released by HandleJSON. Detach all
	// nested maps so background probes cannot race JSON encoding.
	encoded, _ := json.Marshal(result)
	var snapshot map[string]any
	json.Unmarshal(encoded, &snapshot)
	return snapshot
}
