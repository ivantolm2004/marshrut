package engine

import (
	"bytes"
	"encoding/json"
	"marshrut/profile"
	"os"
	"os/exec"
	"testing"
)

func TestConfigAndOptionalSingBoxValidation(t *testing.T) {
	imported, e := profile.Decode("vless://a7c15317-2e8b-4fa8-9dc0-832b206e023b@proxy.example:443?security=tls&type=tcp#Test")
	if e != nil {
		t.Fatal(e)
	}
	for _, platform := range []string{"windows", "android"} {
		p, e := Build(Options{Platform: platform, Servers: imported.Servers, TUN: true, DPI: true, APISecret: "test-local-secret", ProbeUser: "test", ProbePassword: "test-password", Warp: &profile.Warp{PrivateKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", Addresses: []string{"172.16.0.2/32"}, Endpoint: "engage.cloudflareclient.com", Port: 2408, MTU: 1280}})
		if e != nil {
			t.Fatal(e)
		}
		var c map[string]any
		if json.Unmarshal(p.Config, &c) != nil {
			t.Fatal("invalid JSON")
		}
		if bytes.Contains(p.Config, []byte(`"interrupt_exist_connections": true`)) {
			t.Fatal("existing flows would be interrupted")
		}
		if binary := os.Getenv("MARSHRUT_SING_BOX"); binary != "" {
			if platform == "windows" {
				continue
			} // Windows registers its private marked DPI outbound in-process.
			cmd := exec.Command(binary, "check", "-c", "stdin")
			cmd.Stdin = bytes.NewReader(p.Config)
			output, e := cmd.CombinedOutput()
			if e != nil {
				t.Fatalf("%s config rejected: %s", platform, string(output))
			}
		}
	}
}
