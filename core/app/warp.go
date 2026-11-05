package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"marshrut/profile"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ViRb3/wgcf/v2/cloudflare"
	"github.com/ViRb3/wgcf/v2/openapi"
	"github.com/ViRb3/wgcf/v2/util"
	"github.com/ViRb3/wgcf/v2/wireguard"
)

func profileID(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:8]) }
func RegisterWarp() (profile.Warp, map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return registerWarpWithClient(ctx, cloudflare.MakeApiClient(nil))
}

func (m *Manager) registerWarp() (profile.Warp, map[string]string, error) {
	m.mu.Lock()
	parent := context.Background()
	if m.runContext != nil && (m.phase == "connected" || m.phase == "degraded") {
		parent = m.runContext
	}
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	m.mu.Lock()
	m.warpAttemptCancel = cancel
	m.mu.Unlock()
	defer func() { cancel(); m.mu.Lock(); m.warpAttemptCancel = nil; m.mu.Unlock() }()
	client := cloudflare.MakeApiClient(nil)
	if m.warpTransport != nil {
		client.GetConfig().HTTPClient.Transport = m.warpTransport
	}
	return registerWarpWithClient(ctx, client)
}

// A per-operation client avoids wgcf's global authenticated client retaining
// the token from a previous registration. Preserve its pinned TLS transport.
func registerWarpWithClient(ctx context.Context, client *openapi.APIClient) (profile.Warp, map[string]string, error) {
	client.GetConfig().HTTPClient.Timeout = 30 * time.Second
	client.GetConfig().HTTPClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	private, e := wireguard.NewPrivateKey()
	if e != nil {
		return profile.Warp{}, nil, errors.New("Не удалось создать ключ WARP")
	}
	model, osVersion, keyType, tunnelType, tos := "Marshrut", "16.0.0", "curve25519", "wireguard", util.GetTimestamp()
	request := openapi.RegisterRequest{FcmToken: "", InstallId: "", Key: private.Public().String(), Locale: "en_US", Model: &model, Tos: &tos, SerialNumber: "", OsVersion: &osVersion, KeyType: &keyType, TunnelType: &tunnelType}
	device, response, e := client.DefaultAPI.Register(ctx, cloudflare.ApiVersion).RegisterRequest(request).Execute()
	if e != nil || device == nil || device.Token == "" || device.Id == "" {
		return profile.Warp{}, nil, warpRegistrationError("регистрация", response, e)
	}
	c := device.Config
	if c == nil {
		client.GetConfig().DefaultHeader["Authorization"] = "Bearer " + device.Token
		source, response, e := client.DefaultAPI.GetSourceDevice(ctx, cloudflare.ApiVersion, device.Id).Execute()
		if e != nil || source == nil {
			return profile.Warp{}, nil, warpRegistrationError("получение профиля", response, e)
		}
		c = source.Config
	}
	if c == nil || len(c.Peers) == 0 {
		return profile.Warp{}, nil, errors.New("Cloudflare зарегистрировал устройство, но не вернул WireGuard-профиль")
	}
	peer := c.Peers[0]
	endpoint, e := cloudflare.WireGuardEndpoint(peer.Endpoint)
	if e != nil {
		return profile.Warp{}, nil, errors.New("В ответе Cloudflare отсутствует адрес WireGuard")
	}
	host, port, e := net.SplitHostPort(endpoint)
	if e != nil {
		return profile.Warp{}, nil, errors.New("Cloudflare вернул некорректный адрес WireGuard")
	}
	p, e := strconv.Atoi(port)
	if e != nil || p < 1 || p > 65535 {
		return profile.Warp{}, nil, errors.New("Cloudflare вернул некорректный порт WireGuard")
	}
	w := profile.Warp{PrivateKey: private.String(), PublicKey: peer.PublicKey, Endpoint: host, Port: p, MTU: 1280}
	for _, a := range []string{c.Interface.Addresses.V4, c.Interface.Addresses.V6} {
		if a != "" {
			if !strings.Contains(a, "/") {
				if strings.Contains(a, ":") {
					a += "/128"
				} else {
					a += "/32"
				}
			}
			w.Addresses = append(w.Addresses, a)
		}
	}
	if id, e := base64.StdEncoding.DecodeString(c.ClientId); e == nil && len(id) == 3 {
		for _, b := range id {
			w.Reserved = append(w.Reserved, int(b))
		}
	}
	if len(w.Addresses) == 0 {
		return profile.Warp{}, nil, errors.New("Cloudflare не вернул внутренний адрес WireGuard")
	}
	return w, map[string]string{"device_id": device.Id, "token": device.Token}, nil
}

func warpRegistrationError(stage string, response *http.Response, cause error) error {
	// Never include URL errors, response bodies, tokens or device identifiers.
	if response != nil && response.StatusCode >= 300 {
		switch response.StatusCode {
		case 403:
			return fmt.Errorf("WARP: Cloudflare отказал в доступе (HTTP 403, %s). Попробуйте автонастройку при подключённом VLESS", stage)
		case 429:
			return fmt.Errorf("WARP: Cloudflare ограничил запросы (HTTP 429, %s). Повторите позже", stage)
		default:
			return fmt.Errorf("WARP: Cloudflare вернул HTTP %d на этапе «%s»", response.StatusCode, stage)
		}
	}
	var timeout interface{ Timeout() bool }
	if errors.Is(cause, context.DeadlineExceeded) || (errors.As(cause, &timeout) && timeout.Timeout()) {
		return fmt.Errorf("WARP: время ожидания истекло (%s). Попробуйте при подключённом VLESS", stage)
	}
	var dns *net.DNSError
	if errors.As(cause, &dns) {
		return errors.New("WARP: не удалось разрешить адрес API Cloudflare")
	}
	if response != nil && response.StatusCode >= 200 && response.StatusCode < 300 {
		return fmt.Errorf("WARP: неподдерживаемый ответ Cloudflare (%s, HTTP %d)", stage, response.StatusCode)
	}
	return fmt.Errorf("WARP: не удалось связаться с API Cloudflare (%s). Попробуйте при подключённом VLESS", stage)
}
