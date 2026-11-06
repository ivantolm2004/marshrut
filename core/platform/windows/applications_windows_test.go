package windowsruntime

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWindowsApplicationEnumerationStaysWithinOwner(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	owner := user.User.Sid.String()
	path, ok := ownedProcessPath(uint32(os.Getpid()), owner)
	if !ok || !strings.HasSuffix(strings.ToLower(path), ".exe") {
		t.Fatal("could not query current process image")
	}
	if _, ok = ownedProcessPath(uint32(os.Getpid()), "not-the-owner"); ok {
		t.Fatal("foreign-owner process exposed")
	}
	r := &Runtime{OwnerSID: owner}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	apps, err := r.ListApplications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, app := range apps {
		if seen[app.ID] || app.ProcessCount < 1 || app.Path == "" || !app.Running {
			t.Fatal("invalid or duplicate application")
		}
		seen[app.ID] = true
	}
}
