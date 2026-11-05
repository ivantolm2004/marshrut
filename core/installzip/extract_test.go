package installzip

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func payload(name string) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create(name)
	w.Write([]byte("test-data"))
	z.Close()
	return b.Bytes()
}

func TestUnchangedInstalledFileIsNotRewritten(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "driver.sys")
	if e := os.WriteFile(path, []byte("test-data"), 0444); e != nil {
		t.Fatal(e)
	}
	defer os.Chmod(path, 0644)
	stamp := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if e := os.Chtimes(path, stamp, stamp); e != nil {
		t.Fatal(e)
	}
	if e := Extract(payload("driver.sys"), root); e != nil {
		t.Fatal(e)
	}
	st, _ := os.Stat(path)
	if !st.ModTime().Equal(stamp) {
		t.Fatal("unchanged file was rewritten")
	}
}

func TestChangedInstalledFileIsReplaced(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "app.bin")
	if e := os.WriteFile(path, []byte("old-data"), 0644); e != nil {
		t.Fatal(e)
	}
	if e := Extract(payload("app.bin"), root); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(path)
	if e != nil || string(b) != "test-data" {
		t.Fatal("changed file was skipped")
	}
}
func TestPayloadCannotEscapeInstallDirectory(t *testing.T) {
	for _, name := range []string{"../outside.txt", "C:/outside.txt", "/outside.txt", "sub/../../outside.txt", "file:stream", "sub\\..\\outside.txt"} {
		if e := Extract(payload(name), t.TempDir()); e == nil {
			t.Fatalf("unsafe entry accepted: %s", name)
		}
	}
}
func TestPayloadExtractsExactContents(t *testing.T) {
	root := t.TempDir()
	if e := Extract(payload("data/file.txt"), root); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Join(root, "data", "file.txt"))
	if e != nil || string(b) != "test-data" {
		t.Fatal("payload was not preserved")
	}
}
