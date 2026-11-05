package profile

import (
	"encoding/base64"
	"strings"
	"testing"
)

const testURI = "vless://a7c15317-2e8b-4fa8-9dc0-832b206e023b@proxy.example:443?type=tcp&security=reality&sni=www.example.org&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=abcd&flow=xtls-rprx-vision#Test"

func TestImportPreservesReality(t *testing.T) {
	r, e := Decode(base64.StdEncoding.EncodeToString([]byte(testURI + "\n" + testURI)))
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Servers) != 1 {
		t.Fatalf("deduplication: %d", len(r.Servers))
	}
	o := r.Servers[0].Outbound
	tls := o["tls"].(map[string]any)
	reality := tls["reality"].(map[string]any)
	if o["flow"] != "xtls-rprx-vision" || tls["server_name"] != "www.example.org" || reality["short_id"] != "abcd" {
		t.Fatal("critical parameters changed")
	}
}
func TestRejectWithoutCredentialLeak(t *testing.T) {
	for _, u := range []string{strings.Replace(testURI, "type=tcp", "type=xhttp", 1), strings.Replace(testURI, "#Test", "&unknown=secret#Test", 1), strings.Replace(testURI, "sid=abcd", "sid=xyz", 1), strings.Replace(testURI, "a7c15317-2e8b-4fa8-9dc0-832b206e023b", "SENSITIVE_TOKEN", 1), strings.Replace(testURI, "security=reality", "security=reality&allowInsecure=1", 1)} {
		_, e := ParseVLESS(u)
		if e == nil {
			t.Errorf("invalid profile accepted")
		}
		if e != nil && (strings.Contains(e.Error(), "SENSITIVE_TOKEN") || strings.Contains(e.Error(), "vless://")) {
			t.Fatal("credential leaked")
		}
	}
}
func TestJSONKeepsParameters(t *testing.T) {
	s, e := ParseVLESS(testURI)
	if e != nil {
		t.Fatal(e)
	}
	r, e := Decode(`{"outbounds":[{"type":"direct","tag":"direct"},{"type":"vless","tag":"my server","server":"proxy.example","server_port":443,"uuid":"a7c15317-2e8b-4fa8-9dc0-832b206e023b","flow":"xtls-rprx-vision","tls":{"enabled":true,"server_name":"example.org"}}]}`)
	if e != nil || len(r.Servers) != 1 || r.Servers[0].Name != "my server" {
		t.Fatalf("JSON import failed %v", e)
	}
	if s.ID == r.Servers[0].ID {
		t.Fatal("different profiles should have different IDs")
	}
}
func TestWireGuard(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	text := "[Interface]\nPrivateKey = " + key + "\nAddress = 172.16.0.2/32, fd00::2/128\nDNS = 1.1.1.1\nMTU = 1280\n[Peer]\nPublicKey = " + key + "\nEndpoint = engage.cloudflareclient.com:2408\nAllowedIPs = 0.0.0.0/0, ::/0\nPersistentKeepalive = 25\nReserved = 1, 2, 3"
	w, e := ParseWireGuard(text)
	if e != nil {
		t.Fatal(e)
	}
	if w.Port != 2408 || len(w.Addresses) != 2 || w.Reserved[2] != 3 {
		t.Fatal("profile changed")
	}
	if _, e = ParseWireGuard(text + "\nPostUp = dangerous command"); e == nil {
		t.Fatal("shell hook accepted")
	}
	if _, e = ParseWireGuard(text + "\n[Peer]\nPublicKey = " + key); e == nil {
		t.Fatal("multiple peers accepted")
	}
}

func TestJSONCannotReadOrWriteServiceFiles(t *testing.T) {
	for _, field := range []string{"certificate_path", "key_log_path", "key_path"} {
		_, err := Decode(`{"type":"vless","server":"proxy.example","server_port":443,"uuid":"a7c15317-2e8b-4fa8-9dc0-832b206e023b","tls":{"enabled":true,"` + field + `":"SENSITIVE_PATH"}}`)
		if err == nil {
			t.Fatal("filesystem TLS option accepted")
		}
		if strings.Contains(err.Error(), "SENSITIVE_PATH") {
			t.Fatal("path leaked in error")
		}
	}
}
