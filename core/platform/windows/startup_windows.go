package windowsruntime

import (
	"errors"
	"marshrut/startupwindows"
	"path/filepath"
)

func (r *Runtime) StartupSupported() bool { return r.OwnerSID != "" && filepath.IsAbs(r.UIExecutable) }
func (r *Runtime) StartupEnabled() (bool, error) {
	if !r.StartupSupported() {
		return false, nil
	}
	return startupwindows.Enabled(r.OwnerSID, r.UIExecutable)
}
func (r *Runtime) SetStartupEnabled(value bool) error {
	if !r.StartupSupported() {
		return errors.New("Автозапуск недоступен в тестовом режиме")
	}
	if e := startupwindows.Set(r.OwnerSID, r.UIExecutable, value); e != nil {
		return errors.New("Не удалось изменить автозапуск Windows")
	}
	return nil
}
