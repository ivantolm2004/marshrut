//go:build windows

package catalogue

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"errors"
	"marshrut/route"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type encryptedFixture struct {
	cipher.AEAD
	fail bool
}

func (c encryptedFixture) Seal(p []byte) ([]byte, error) {
	if c.fail {
		return nil, errors.New("disk/codec failure")
	}
	n := make([]byte, c.NonceSize())
	if _, err := rand.Read(n); err != nil {
		return nil, err
	}
	return c.AEAD.Seal(n, n, p, nil), nil
}
func (c encryptedFixture) Open(p []byte) ([]byte, error) {
	if len(p) < c.NonceSize() {
		return nil, errors.New("truncated")
	}
	return c.AEAD.Open(nil, p[:c.NonceSize()], p[c.NonceSize():], nil)
}
func fixture(t *testing.T) (string, encryptedFixture, Snapshot) {
	t.Helper()
	block, err := aes.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	a, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	r := route.DefaultRules()
	r[0].Name = "PRIVATE_NAME"
	r[0].ProcessPaths = []string{`C:\PRIVATE_PATH\game.exe`}
	return filepath.Join(t.TempDir(), "spaces # & база", "routing.sqlite"), encryptedFixture{AEAD: a}, Snapshot{Schema: 1, Rules: r, Entries: route.SeedCatalogue(r)}
}

func TestMigrationOnceEncryptedAndRollback(t *testing.T) {
	path, c, seed := fixture(t)
	if _, err := Update(path, c, seed, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("PRIVATE")) || bytes.Contains(raw, []byte("dota2.exe")) {
		t.Fatal("plaintext catalogue leaked")
	}
	seed.Rules = nil
	loaded, err := Update(path, c, seed, nil)
	if err != nil || len(loaded.Rules) != 31 {
		t.Fatal("migration repeated", err)
	}
	c.fail = true
	if _, err = Update(path, c, seed, func(s *Snapshot) error { s.Rules = []route.Rule{}; s.Revision = 8; return nil }); err == nil {
		t.Fatal("seal failure accepted")
	}
	c.fail = false
	loaded, err = Update(path, c, seed, nil)
	if err != nil || loaded.Revision != 0 || len(loaded.Rules) != 31 {
		t.Fatal("failed transaction damaged data", err)
	}
	if _, err = Update(path, c, seed, func(s *Snapshot) error { s.Rules = []route.Rule{}; s.Revision++; return nil }); err != nil {
		t.Fatal(err)
	}
	loaded, err = Update(path, c, seed, nil)
	if err != nil || loaded.Rules == nil || len(loaded.Rules) != 0 {
		t.Fatal("intentional empty state lost", err)
	}
}

func TestConcurrentEditorsCannotOverwriteEachOther(t *testing.T) {
	path, c, seed := fixture(t)
	if _, err := Update(path, c, seed, nil); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Update(path, c, seed, func(s *Snapshot) error {
				if s.Revision != 0 {
					return errors.New("stale")
				}
				s.Revision++
				return nil
			})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatal("lost update protection failed", accepted)
	}
}

func TestFutureSchemaAndCorruptionArePreserved(t *testing.T) {
	path, c, seed := fixture(t)
	if _, err := Update(path, c, seed, nil); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("PRAGMA user_version=99"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	before, _ := os.ReadFile(path)
	if _, err = Update(path, c, seed, nil); err == nil {
		t.Fatal("future schema reset")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("unsupported db changed")
	}
	if err = os.WriteFile(path, []byte("corrupt sensitive data"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Update(path, c, seed, nil); err == nil {
		t.Fatal("corrupt database reset")
	}
	after, _ = os.ReadFile(path)
	if string(after) != "corrupt sensitive data" {
		t.Fatal("corrupt file overwritten")
	}
}
