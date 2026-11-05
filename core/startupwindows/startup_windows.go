package startupwindows

import (
	"errors"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const runPath = `Software\Microsoft\Windows\CurrentVersion\Run`
const preferencePath = `Software\Marshrut`
const valueName = "Marshrut"

func Command(executable string) string {
	return windows.ComposeCommandLine([]string{executable, "--startup"})
}
func ownerPath(owner, path string) (string, error) {
	if _, e := windows.StringToSid(owner); e != nil {
		return "", e
	}
	return owner + `\` + path, nil
}
func Preferred(owner string) (bool, error) {
	path, e := ownerPath(owner, preferencePath)
	if e != nil {
		return false, e
	}
	key, e := registry.OpenKey(registry.USERS, path, registry.QUERY_VALUE)
	if errors.Is(e, registry.ErrNotExist) {
		return true, nil
	}
	if e != nil {
		return false, e
	}
	defer key.Close()
	value, _, e := key.GetIntegerValue("AutoStart")
	if errors.Is(e, registry.ErrNotExist) {
		return true, nil
	}
	return value != 0, e
}
func Enabled(owner, executable string) (bool, error) {
	path, e := ownerPath(owner, runPath)
	if e != nil {
		return false, e
	}
	key, e := registry.OpenKey(registry.USERS, path, registry.QUERY_VALUE)
	if errors.Is(e, registry.ErrNotExist) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	defer key.Close()
	value, _, e := key.GetStringValue(valueName)
	if errors.Is(e, registry.ErrNotExist) {
		return false, nil
	}
	return value == Command(executable), e
}
func Set(owner, executable string, enabled bool) error {
	path, e := ownerPath(owner, runPath)
	if e != nil {
		return e
	}
	key, _, e := registry.CreateKey(registry.USERS, path, registry.SET_VALUE)
	if e != nil {
		return e
	}
	defer key.Close()
	if enabled {
		e = key.SetStringValue(valueName, Command(executable))
	} else {
		e = key.DeleteValue(valueName)
		if errors.Is(e, registry.ErrNotExist) {
			e = nil
		}
	}
	if e != nil {
		return e
	}
	path, _ = ownerPath(owner, preferencePath)
	pref, _, e := registry.CreateKey(registry.USERS, path, registry.SET_VALUE)
	if e != nil {
		return e
	}
	defer pref.Close()
	value := uint32(0)
	if enabled {
		value = 1
	}
	return pref.SetDWordValue("AutoStart", value)
}
func Remove(owner string) error {
	path, e := ownerPath(owner, runPath)
	if e != nil {
		return e
	}
	key, e := registry.OpenKey(registry.USERS, path, registry.SET_VALUE)
	if errors.Is(e, registry.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	defer key.Close()
	e = key.DeleteValue(valueName)
	if errors.Is(e, registry.ErrNotExist) {
		return nil
	}
	return e
}
