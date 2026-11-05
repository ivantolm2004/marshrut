package app

// Called only by the Windows UI launched with --startup. The operation mutex
// held by Handle serializes this with manual connect/disconnect and settings.
func (m *Manager) connectAtStartup(connect func() error) (bool, error) {
	m.mu.Lock()
	eligible := m.platform == "windows" && m.settings.ConnectOnStartup &&
		(len(m.settings.Servers) > 0 || m.settings.Warp != nil) &&
		(m.phase == "disconnected" || m.phase == "error")
	m.mu.Unlock()
	if !eligible {
		return false, nil
	}
	startup, ok := m.runtime.(StartupRuntime)
	if !ok || !startup.StartupSupported() {
		return false, nil
	}
	enabled, err := startup.StartupEnabled()
	if err != nil {
		return false, err
	}
	if !enabled {
		return false, nil
	}
	return true, connect()
}
