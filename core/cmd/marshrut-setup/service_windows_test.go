//go:build installer

package main

import (
	"reflect"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/mgr"
)

func TestServiceConfigPreservesRequiredWindowsTypeAndArguments(t *testing.T) {
	exe := `C:\Program Files\Маршрут\marshrut-core.exe`
	args := serviceArgs("S-1-5-21-1000", `C:\Program Files\Маршрут\runtime`, `C:\ProgramData\Маршрут`)
	c := serviceConfig(exe, args)
	if c.ServiceType != windows.SERVICE_WIN32_OWN_PROCESS {
		t.Fatal("ChangeServiceConfig requires an explicit supported type, not zero")
	}
	if c.StartType != mgr.StartAutomatic {
		t.Fatal("service must start automatically")
	}
	decoded, err := windows.DecomposeCommandLine(c.BinaryPathName)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, append([]string{exe}, args...)) {
		t.Fatal("service command line lost arguments or quoting")
	}
}

func TestServiceErrorIncludesNativeCode(t *testing.T) {
	err := serviceError("Не удалось настроить службу", syscall.Errno(87))
	if !strings.Contains(err.Error(), "87") {
		t.Fatal("native error code was discarded")
	}
}
