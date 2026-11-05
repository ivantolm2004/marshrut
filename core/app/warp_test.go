package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/ViRb3/wgcf/v2/cloudflare"
	"github.com/ViRb3/wgcf/v2/openapi"
)

func warpFixture() *openapi.Config {
	host := "engage.example:2408"
	return &openapi.Config{ClientId: "AQID", Interface: openapi.ConfigInterface{Addresses: openapi.NetworkAddress{V4: "172.16.0.2", V6: "fd00::2"}}, Peers: []openapi.Peer{{PublicKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), Endpoint: openapi.Endpoint{Host: &host, V4: "192.0.2.1", V6: "2001:db8::1"}}}}
}

func TestWarpErrorsIncludeStatusAndNeverBodyCredentials(t *testing.T) {
	for _, code := range []int{403, 429, 500} {
		err := warpRegistrationError("регистрация", &http.Response{StatusCode: code}, errors.New("SENSITIVE_TOKEN_AND_DEVICE_ID"))
		if strings.Contains(err.Error(), "SENSITIVE") || !strings.Contains(err.Error(), strconv.Itoa(code)) {
			t.Fatal("unsafe or missing HTTP status")
		}
	}
	err := warpRegistrationError("регистрация", nil, context.DeadlineExceeded)
	if !strings.Contains(err.Error(), "ожидания") {
		t.Fatal(err)
	}
}

func TestWarpFallbackUsesOwnTokenForEveryRegistration(t *testing.T) {
	for _, token := range []string{"fixture-token-a", "fixture-token-b"} {
		registration := openapi.Register200Response{Id: "fixture-id", Token: token, KeyType: "curve25519", TunnelType: "wireguard"}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodPost {
				var request openapi.RegisterRequest
				if json.NewDecoder(r.Body).Decode(&request) != nil || request.Tos == nil || request.Key == "" || request.TunnelType == nil || *request.TunnelType != "wireguard" {
					t.Error("registration request lost required fields")
				}
				_ = json.NewEncoder(w).Encode(registration)
			} else {
				if r.Header.Get("Authorization") != "Bearer "+token {
					t.Error("wrong token reused")
				}
				_ = json.NewEncoder(w).Encode(openapi.GetSourceDevice200Response{Id: "fixture-id", Config: warpFixture()})
			}
		}))
		client := cloudflare.MakeApiClient(nil)
		client.GetConfig().Servers = []openapi.ServerConfiguration{{URL: server.URL}}
		client.GetConfig().HTTPClient = server.Client()
		profile, account, err := registerWarpWithClient(context.Background(), client)
		server.Close()
		if err != nil || account["token"] != token || profile.Port != 2408 || len(profile.Reserved) != 3 || len(profile.Addresses) != 2 {
			t.Fatal("profile conversion failed", err)
		}
	}
}

type warpRoundTripper func(*http.Request) (*http.Response, error)

func (f warpRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWarpCanRegisterWhileConnectedWithoutDisconnecting(t *testing.T) {
	m := testManager(t)
	m.phase = "connected"
	called := false
	m.warpTransport = warpRoundTripper(func(r *http.Request) (*http.Response, error) {
		called = true
		b, _ := json.Marshal(openapi.Register200Response{Id: "fixture", Token: "PRIVATE_FIXTURE_TOKEN", KeyType: "curve25519", TunnelType: "wireguard", Config: warpFixture()})
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(b))), Request: r}, nil
	})
	if r := m.Handle(Request{Action: "warp_register"}); r.OK || called {
		t.Fatal("registration bypassed consent")
	}
	if r := m.Handle(Request{Action: "warp_register", AcceptTerms: true}); !r.OK {
		t.Fatal(r.Error)
	}
	if m.phase != "connected" || m.settings.Warp == nil {
		t.Fatal("connection interrupted or profile not saved")
	}
	if strings.Contains(m.HandleJSON(`{"action":"diagnostics"}`), "PRIVATE_FIXTURE_TOKEN") {
		t.Fatal("token leaked")
	}
	m.phase = "disconnected"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.runContext = ctx
	m.settings.Warp = nil
	if r := m.Handle(Request{Action: "warp_register", AcceptTerms: true}); !r.OK {
		t.Fatal("cancelled VPN context broke disconnected registration", r.Error)
	}
}
