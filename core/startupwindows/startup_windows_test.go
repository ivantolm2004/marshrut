package startupwindows

import (
	"golang.org/x/sys/windows"
	"reflect"
	"testing"
)

func TestStartupCommandPreservesExecutableWithSpaces(t *testing.T) {
	exe := `C:\Program Files\Маршрут\marshrut.exe`
	args, e := windows.DecomposeCommandLine(Command(exe))
	if e != nil || !reflect.DeepEqual(args, []string{exe, "--startup"}) {
		t.Fatal("unsafe startup command")
	}
	if _, e := ownerPath(`INVALID\SID`, runPath); e == nil {
		t.Fatal("invalid owner accepted")
	}
}
