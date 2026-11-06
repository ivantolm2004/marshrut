package engine

import (
	"bytes"
	"encoding/json"
	"marshrut/profile"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestXrayPlansValidateAgainstBundledCore(t *testing.T) {
	base := "vless://a7c15317-2e8b-4fa8-9dc0-832b206e023b@proxy.example:443?"
	for _, query := range []string{
		"security=tls&type=xhttp&path=%2Ftunnel&mode=packet-up",
		"security=tls&type=xhttp&mode=stream-one",
		"security=reality&type=xhttp&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sni=example.org&sid=abcd",
		"security=tls&type=grpc&mode=multi&serviceName=service",
		"security=reality&type=tcp&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sni=example.org&sid=abcd&spx=%2Fcrawl&flow=xtls-rprx-vision",
		"security=tls&type=xhttp&extra=" + url.QueryEscape(`{"noGRPCHeader":true,"xmux":{"maxConcurrency":"16-32"}}`),
	} {
		t.Run(query[:min(35, len(query))], func(t *testing.T) {
			imported, err := profile.DecodeWindows(base + query)
			if err != nil {
				t.Fatal(err)
			}
			p, err := Build(Options{Platform: "windows", Servers: imported.Servers, APISecret: "test-secret", ProbeUser: "test", ProbePassword: "private-test-password"})
			if err != nil || len(p.XrayPorts) != 1 || !json.Valid(p.XrayConfig) {
				t.Fatal("Xray plan not generated", err)
			}
			if strings.Contains(string(p.Config), "a7c15317") || bytes.Contains(p.Config, []byte(`"xhttp"`)) {
				t.Fatal("Xray profile leaked into native sing-box config")
			}
			if _, err := Build(Options{Platform: "android", Servers: imported.Servers, APISecret: "test", ProbePassword: "test"}); err == nil {
				t.Fatal("unsupported Android transport accepted")
			}
			if binary := os.Getenv("MARSHRUT_XRAY"); binary != "" {
				command := exec.Command(binary, "run", "-test", "-format", "json", "-config", "stdin:")
				command.Stdin = bytes.NewReader(p.XrayConfig)
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatalf("synthetic Xray config rejected: %s", output)
				}
			}
		})
	}
}
