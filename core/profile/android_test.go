package profile

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

func TestAndroidProviderFormatsPreserveNativeVLESS(t *testing.T) {
	const id = "a7c15317-2e8b-4fa8-9dc0-832b206e023b"
	const ws = "vless://" + id + "@proxy.example:443?security=tls&type=websocket&sni=example.org&path="
	xray := `{"outbounds":[{"protocol":"vless","tag":"Xray WS","settings":{"vnext":[{"address":"proxy.example","port":443,"users":[{"id":"` + id + `","encryption":"none"}]}]},"streamSettings":{"network":"ws","security":"tls","tlsSettings":{"serverName":"example.org"},"wsSettings":{"path":"/socket?ed=2048"}}}]}`
	clash := "proxies:\n  - name: Clash WS\n    type: vless\n    server: proxy.example\n    port: 443\n    uuid: " + id + "\n    network: ws\n    tls: true\n    servername: example.org\n    ws-opts:\n      path: /socket\n      max-early-data: 2048\n"
	singbox := `{"outbounds":[{"type":"vless","tag":"sing-box","server":"proxy.example","server_port":443,"uuid":"` + id + `","transport":{"type":"ws","path":"/socket","max_early_data":2048,"early_data_header_name":"Sec-WebSocket-Protocol"}}]}`
	for name, source := range map[string]string{
		"WebSocket URI": ws + url.QueryEscape("/socket?ed=2048"),
		"Xray JSON":     xray,
		"Clash YAML":    clash,
		"base64 YAML":   base64.StdEncoding.EncodeToString([]byte(clash)),
		"base64 Xray":   base64.RawURLEncoding.EncodeToString([]byte(xray)),
		"sing-box JSON": singbox,
	} {
		t.Run(name, func(t *testing.T) {
			result, err := DecodeAndroid(source)
			if err != nil || len(result.Servers) != 1 || result.Servers[0].Xray != nil {
				t.Fatal("expected a native VLESS server", err)
			}
			transport := result.Servers[0].Outbound["transport"].(map[string]any)
			if transport["path"] != "/socket" || stringValue(transport["max_early_data"]) != "2048" {
				t.Fatal("WebSocket settings lost")
			}
		})
	}
}

func TestAndroidMixedSubscriptionSkipsOnlyXrayNodes(t *testing.T) {
	xhttp := strings.Replace(strings.Replace(testURI, "type=tcp", "type=xhttp", 1), "&flow=xtls-rprx-vision", "", 1)
	result, err := DecodeAndroid(testURI + "\n" + xhttp)
	legacy, legacyErr := ParseVLESS(testURI)
	if err != nil || legacyErr != nil || len(result.Servers) != 1 || result.Servers[0].ID != legacy.ID ||
		len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "XHTTP") {
		t.Fatal("mixed subscription did not preserve the original compatible node", err)
	}
	if result, err := DecodeAndroid(xhttp); err == nil || len(result.Servers) != 0 || !strings.Contains(err.Error(), "XHTTP") {
		t.Fatal("XHTTP-only subscription did not explain the incompatibility")
	}
}
