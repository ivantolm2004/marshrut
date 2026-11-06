package profile

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestWindowsAliasesPreserveExistingProfileIDs(t *testing.T) {
	before, err := ParseVLESS(testURI)
	if err != nil {
		t.Fatal(err)
	}
	after, err := ParseVLESSWindows(strings.Replace(testURI, "type=tcp", "type=raw&remarks=display-only", 1))
	if err != nil || after.ID != before.ID || after.Xray != nil {
		t.Fatal("existing profile changed", err)
	}
}

func TestWindowsSelectsEngineFromRequiredCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name, uri string
		xray      bool
	}{
		{"native Reality", testURI, false},
		{"XHTTP Reality", strings.Replace(strings.Replace(testURI, "type=tcp", "type=xhttp&path=%2Ftunnel&mode=packet-up", 1), "&flow=xtls-rprx-vision", "", 1), true},
		{"gRPC multi", strings.Replace(strings.Replace(testURI, "type=tcp", "type=grpc&serviceName=service&mode=multi", 1), "&flow=xtls-rprx-vision", "", 1), true},
		{"Reality spiderX", strings.Replace(testURI, "#Test", "&spx=%2Fcrawl#Test", 1), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := ParseVLESSWindows(tc.uri)
			if err != nil || (s.Xray != nil) != tc.xray {
				t.Fatal("incorrect capability selection", err)
			}
			if tc.xray && s.Xray["protocol"] != "vless" {
				t.Fatal("protocol changed")
			}
		})
	}
}

func TestWindowsWebSocketEarlyData(t *testing.T) {
	u := "vless://a7c15317-2e8b-4fa8-9dc0-832b206e023b@proxy.example:443?security=tls&type=ws&path=" + url.QueryEscape("/socket?ed=2048&foo=bar")
	s, err := ParseVLESSWindows(u)
	if err != nil {
		t.Fatal(err)
	}
	transport := s.Outbound["transport"].(map[string]any)
	if transport["path"] != "/socket?foo=bar" || transport["max_early_data"] != 2048 || transport["early_data_header_name"] != "Sec-WebSocket-Protocol" {
		t.Fatal("early data settings lost")
	}
}

func TestWindowsImportsProviderFormats(t *testing.T) {
	xray := `{"outbounds":[{"protocol":"vless","tag":"Sample","settings":{"vnext":[{"address":"proxy.example","port":443,"users":[{"id":"a7c15317-2e8b-4fa8-9dc0-832b206e023b","encryption":"none"}]}]},"streamSettings":{"network":"xhttp","security":"tls","tlsSettings":{"serverName":"example.org"},"xhttpSettings":{"path":"/tunnel","mode":"packet-up"}}}]}`
	clash := "proxies:\n  - name: Sample\n    type: vless\n    server: proxy.example\n    port: 443\n    uuid: a7c15317-2e8b-4fa8-9dc0-832b206e023b\n    network: ws\n    tls: true\n    servername: example.org\n    ws-opts:\n      path: /socket\n      headers:\n        Host: example.org\n"
	for _, source := range []string{xray, clash, base64.RawURLEncoding.EncodeToString([]byte(xray)), base64.StdEncoding.EncodeToString([]byte(clash))} {
		result, err := DecodeWindows(source)
		if err != nil || len(result.Servers) != 1 || result.Servers[0].Name != "Sample" {
			t.Fatal("format import failed", err)
		}
	}
}

func TestWindowsSkipsBadJSONServersWithoutDroppingGoodOnes(t *testing.T) {
	text := `[{"type":"vless","server":"proxy.example","server_port":443,"uuid":"bad-secret"},{"type":"vless","server":"proxy.example","server_port":443,"uuid":"a7c15317-2e8b-4fa8-9dc0-832b206e023b"}]`
	r, err := DecodeWindows(text)
	if err != nil || len(r.Servers) != 1 || len(r.Warnings) != 1 || strings.Contains(strings.Join(r.Warnings, ""), "bad-secret") {
		t.Fatal("invalid server not isolated", err)
	}
}

func TestWindowsDoesNotAcceptUnsafeOrUnknownOptions(t *testing.T) {
	xhttp := strings.Replace(strings.Replace(testURI, "type=tcp", "type=xhttp", 1), "&flow=xtls-rprx-vision", "", 1)
	for _, text := range []string{
		strings.Replace(xhttp, "#Test", "&secret=DO_NOT_LOG#Test", 1),
		strings.Replace(xhttp, "#Test", "&allowInsecure=1#Test", 1),
		strings.Replace(xhttp, "#Test", "&extra="+url.QueryEscape(`{"downloadSettings":{"tlsSettings":{"keyLog":"DO_NOT_LOG"}}}`)+"#Test", 1),
		strings.Replace(testURI, "type=tcp", "type=xhttp", 1),
		strings.Replace(xhttp, "#Test", "&serviceName=DO_NOT_LOG#Test", 1),
		strings.Replace(xhttp, "#Test", "&packetEncoding=packetaddr#Test", 1),
	} {
		_, err := ParseVLESSWindows(text)
		if err == nil || strings.Contains(err.Error(), "DO_NOT_LOG") || strings.Contains(err.Error(), "vless://") {
			t.Fatal("unsafe profile or credential leak", err)
		}
	}
}

func TestWindowsXHTTPExtraPreserved(t *testing.T) {
	q := url.Values{"type": {"xhttp"}, "security": {"tls"}, "path": {"/tunnel"}, "extra": {`{"noGRPCHeader":true,"xmux":{"maxConcurrency":"16-32"}}`}}
	s, err := ParseVLESSWindows("vless://a7c15317-2e8b-4fa8-9dc0-832b206e023b@proxy.example:443?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(s.Xray)
	if !strings.Contains(string(b), `"maxConcurrency":"16-32"`) {
		t.Fatal("XHTTP extra changed")
	}
}
