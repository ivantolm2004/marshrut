package app

import (
	"context"
	"github.com/ViRb3/wgcf/v2/cloudflare"
	"strings"
	"time"
)

type StartupRuntime interface {
	StartupSupported() bool
	StartupEnabled() (bool, error)
	SetStartupEnabled(bool) error
}

type WarpSetupState struct {
	Running     bool      `json:"running"`
	NextAttempt time.Time `json:"next_attempt,omitempty"`
	LastPath    string    `json:"last_path,omitempty"`
	Error       string    `json:"error,omitempty"`
	Notice      string    `json:"notice,omitempty"`
}

// Production entry points explicitly start provisioning after initialization.
// New() alone never performs external requests, including in unit tests.
func (m *Manager) StartBackground() {
	m.mu.Lock()
	if m.backgroundCancel != nil {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.backgroundCancel = cancel
	m.warpWake = make(chan struct{}, 1)
	m.mu.Unlock()
	go m.backgroundLoop(ctx)
	if _, ok := m.runtime.(ApplicationRuntime); ok && m.platform == "windows" {
		go m.applicationDiscoveryLoop(ctx)
	}
	m.wakeWarpSetup()
}
func (m *Manager) wakeWarpSetup() {
	m.mu.Lock()
	wake := m.warpWake
	m.mu.Unlock()
	if wake != nil {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}
func (m *Manager) backgroundLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-m.warpWake:
		}
		if ctx.Err() != nil {
			return
		}
		m.provisionWarp(ctx)
	}
}
func (m *Manager) provisionWarp(parent context.Context) {
	m.mu.Lock()
	path := "direct"
	if strings.HasPrefix(m.policy.Current["default"].Route, "vless-") {
		path = "vless"
	}
	if !m.settings.AutoWarp || m.settings.Warp != nil || m.warpSetup.Running || m.phase == "connecting" || m.phase == "disconnecting" {
		m.mu.Unlock()
		return
	}
	limited := strings.Contains(m.warpSetup.Error, "HTTP 429")
	if time.Now().Before(m.warpSetup.NextAttempt) && (path == m.warpSetup.LastPath || limited) {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	m.warpAttemptCancel = cancel
	m.warpSetup.Running = true
	m.warpSetup.LastPath = path
	m.mu.Unlock()
	defer cancel()
	client := cloudflare.MakeApiClient(nil)
	if m.warpTransport != nil {
		client.GetConfig().HTTPClient.Transport = m.warpTransport
	}
	profile, account, err := registerWarpWithClient(ctx, client)
	// Provisioning never holds the connection-operation mutex during HTTP I/O.
	// Persist only after checking that a manual import did not supersede it.
	m.operation.Lock()
	m.mu.Lock()
	save := ctx.Err() == nil && m.settings.AutoWarp && m.settings.Warp == nil
	m.mu.Unlock()
	if err == nil && save {
		err = m.persist(func(s *Settings) { s.Warp = &profile; s.WarpAccount = account })
	}
	m.mu.Lock()
	m.warpSetup.Running = false
	m.warpAttemptCancel = nil
	if err != nil && parent.Err() == nil && m.settings.AutoWarp {
		m.warpRegistrationError = err.Error()
		m.warpSetup.Error = err.Error()
		m.warpSetup.NextAttempt = time.Now().Add(10 * time.Minute)
	} else if save && err == nil {
		m.warpRegistrationError = ""
		m.warpSetup.Error = ""
		m.warpSetup.NextAttempt = time.Time{}
		m.warpSetup.Notice = "Профиль WARP подготовлен автоматически; применяется при следующем подключении"
	}
	m.mu.Unlock()
	m.operation.Unlock()
}
