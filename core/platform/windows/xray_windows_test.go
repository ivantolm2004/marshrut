package windowsruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"marshrut/engine"
	"marshrut/probe"
	"marshrut/profile"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func xrayRuntime(t *testing.T) *Runtime {
	t.Helper()
	binary := os.Getenv("MARSHRUT_XRAY")
	if binary == "" {
		t.Skip("set MARSHRUT_XRAY to the verified bundled binary")
	}
	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "xray"), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(directory, "xray", "xray.exe"), data, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	manifest, _ := json.Marshal(map[string]string{"xray/xray.exe": hex.EncodeToString(hash[:])})
	if err = os.WriteFile(filepath.Join(directory, "manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	r, err := New(directory, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return r
}

func TestXHTTPBridgeForwardsHTTPWithoutChangingSystemRoutes(t *testing.T) {
	r := xrayRuntime(t)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "local-xhttp-fixture") }))
	defer httpServer.Close()
	serverPort, _ := engine.FreePort()
	serverConfig := fmt.Sprintf(`{"log":{"loglevel":"none"},"inbounds":[{"listen":"127.0.0.1","port":%d,"protocol":"vless","settings":{"clients":[{"id":"a7c15317-2e8b-4fa8-9dc0-832b206e023b"}],"decryption":"none"},"streamSettings":{"network":"xhttp","xhttpSettings":{"path":"/test"}}}],"outbounds":[{"protocol":"freedom","settings":{"finalRules":[{"action":"allow","ip":["127.0.0.1/32"]}]}}]}`, serverPort)
	server, err := r.spawn(filepath.Join(r.Directory, "xray", "xray.exe"), []string{"run", "-format", "json", "-config", "stdin:"}, []byte(serverConfig))
	if err != nil {
		t.Fatal(err)
	}
	defer stop(server)
	// Validate both an XHTTP client/server exchange and the native SOCKS bridge.
	imported, err := profile.DecodeWindows(fmt.Sprintf("vless://a7c15317-2e8b-4fa8-9dc0-832b206e023b@127.0.0.1:%d?type=xhttp&path=%%2Ftest&mode=packet-up", serverPort))
	if err != nil {
		t.Fatal(err)
	}
	api, _ := engine.FreePort()
	base, _ := engine.FreePort()
	plan, err := engine.Build(engine.Options{Platform: "windows", Servers: imported.Servers, APIPort: api, BasePort: base, APISecret: "local-test", ProbeUser: "fixture", ProbePassword: "fixture-private-password"})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.StartPlan(plan); err != nil {
		t.Fatal(err)
	}
	port := plan.Ports["vless-"+imported.Servers[0].ID]
	socks := probe.SOCKS{Address: fmt.Sprintf("127.0.0.1:%d", port), User: "fixture", Password: "fixture-private-password"}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (conn net.Conn, err error) {
		return socks.Dial(ctx, address)
	}}}
	defer client.CloseIdleConnections()
	var body []byte
	for i := 0; i < 1; i++ {
		response, requestError := client.Get(httpServer.URL)
		if requestError == nil {
			body, err = io.ReadAll(response.Body)
			response.Body.Close()
			break
		}
		err = requestError
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil || string(body) != "local-xhttp-fixture" {
		t.Fatal("XHTTP forwarding failed", err)
	}
	child := r.xray
	if err = r.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-child.done:
	default:
		t.Fatal("Xray survived disconnect")
	}
	if r.Alive() {
		t.Fatal("runtime survived disconnect")
	}
}

func TestXrayBindsPhysicalInterfaceAndKeepsTLSAndHTTPNames(t *testing.T) {
	imported, err := profile.DecodeWindows("vless://a7c15317-2e8b-4fa8-9dc0-832b206e023b@127.0.0.1:443?type=xhttp&security=tls&sni=example.org")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := engine.Build(engine.Options{Platform: "windows", Servers: imported.Servers, APISecret: "test", ProbePassword: "test"})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := bindXrayConfig(plan.XrayConfig, "test-adapter")
	if err != nil || !strings.Contains(string(bound), `"interface":"test-adapter"`) || !strings.Contains(string(bound), `"serverName":"example.org"`) || !strings.Contains(string(bound), `"host":"127.0.0.1"`) {
		t.Fatal("endpoint binding changed TLS or HTTP identity", err)
	}
}

func TestPreflightIsolatesInvalidProviderNode(t *testing.T) {
	r := xrayRuntime(t)
	valid, err := profile.DecodeWindows("vless://a7c15317-2e8b-4fa8-9dc0-832b206e023b@127.0.0.1:443?type=tcp&security=tls")
	if err != nil {
		t.Fatal(err)
	}
	invalid, err := profile.DecodeWindows("vless://a7c15317-2e8b-4fa8-9dc0-832b206e023b@127.0.0.1:443?type=xhttp&encryption=SENSITIVE_INVALID_ENCRYPTION")
	if err != nil {
		t.Fatal(err)
	}
	servers := append(valid.Servers, invalid.Servers...)
	accepted, warnings := r.PrepareServers(context.Background(), servers)
	if len(accepted) != 1 || accepted[0].ID != valid.Servers[0].ID || len(warnings) != 1 || strings.Contains(strings.Join(warnings, ""), "SENSITIVE") {
		t.Fatal("invalid node blocked a working profile or leaked credentials")
	}
}
