package secure

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDPAPIRoundTripAndTamper(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "settings.dpapi"), Codec: DPAPI{}}
	input := map[string]string{"secret": "sensitive-test-credential"}
	if e := s.Save(input); e != nil {
		t.Fatal(e)
	}
	disk, _ := os.ReadFile(s.Path)
	if bytes.Contains(disk, []byte(input["secret"])) {
		t.Fatal("plaintext written")
	}
	var output map[string]string
	if e := s.Load(&output); e != nil || output["secret"] != input["secret"] {
		t.Fatal("roundtrip failed", e)
	}
	disk[len(disk)/2] ^= 1
	os.WriteFile(s.Path, disk, 0600)
	if e := s.Load(&output); e == nil {
		t.Fatal("tampering accepted")
	}
}
