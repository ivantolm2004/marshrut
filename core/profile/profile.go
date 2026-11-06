// Package profile imports credentials without ever including them in error messages.
package profile

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const MaxBytes = 4 << 20

type Server struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Outbound map[string]any `json:"outbound"`
	Xray     map[string]any `json:"xray,omitempty"`
}
type Import struct {
	Servers  []Server `json:"servers"`
	Warnings []string `json:"warnings"`
}

var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func identify(o map[string]any) string {
	b, _ := json.Marshal(o)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}
func Decode(text string) (Import, error) {
	return decode(text, false)
}

// DecodeWindows enables formats and transports supplied by the Windows bundle.
// Android keeps its existing capabilities until its own runtime supports Xray.
func DecodeWindows(text string) (Import, error) {
	return decode(text, true)
}

func decode(text string, windows bool) (Import, error) {
	var result Import
	if len(text) > MaxBytes {
		return result, errors.New("Подписка превышает 4 МБ")
	}
	text = strings.TrimSpace(strings.TrimPrefix(text, "\ufeff"))
	if text == "" {
		return result, errors.New("Пустая конфигурация")
	}
	if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
		if windows {
			return decodeWindowsJSON(text)
		}
		return decodeJSON(text)
	}
	if windows && (strings.HasPrefix(text, "proxies:") || strings.Contains(text, "\nproxies:")) {
		return decodeClash(text)
	}
	if !strings.Contains(text, "://") {
		compact := strings.Join(strings.Fields(text), "")
		decoded := false
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			b, err := enc.DecodeString(compact)
			if err == nil && (strings.Contains(string(b), "://") || (windows && (strings.HasPrefix(strings.TrimSpace(string(b)), "{") || strings.HasPrefix(strings.TrimSpace(string(b)), "[") || strings.Contains(string(b), "proxies:")))) {
				text = string(b)
				decoded = true
				break
			}
		}
		if !decoded {
			return result, errors.New("Ожидается VLESS-ссылка, Base64-подписка или JSON sing-box")
		}
		if windows && (strings.HasPrefix(strings.TrimSpace(text), "{") || strings.HasPrefix(strings.TrimSpace(text), "[") || strings.Contains(text, "proxies:")) {
			return decode(text, true)
		}
	}
	seen := map[string]bool{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		s, err := ParseVLESS(line)
		if windows {
			s, err = ParseVLESSWindows(line)
		}
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Строка %d: %s", i+1, err))
			continue
		}
		if !seen[s.ID] {
			seen[s.ID] = true
			result.Servers = append(result.Servers, s)
		}
	}
	if len(result.Servers) == 0 {
		if len(result.Warnings) > 0 {
			return result, fmt.Errorf("Нет поддерживаемых VLESS-подключений. %s", result.Warnings[0])
		}
		return result, errors.New("Нет поддерживаемых VLESS-подключений")
	}
	return result, nil
}

func ParseVLESS(text string) (Server, error) {
	fail := func(s string) (Server, error) { return Server{}, errors.New(s) }
	u, err := url.Parse(text)
	if err != nil || u.Scheme != "vless" {
		return fail("Поддерживается только vless://")
	}
	if u.User == nil || !uuid.MatchString(u.User.Username()) {
		return fail("Некорректный UUID VLESS")
	}
	if _, ok := u.User.Password(); ok {
		return fail("Пароль в VLESS URI не поддерживается")
	}
	host := u.Hostname()
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 || host == "" {
		return fail("Некорректный адрес или порт")
	}
	if u.Path != "" && u.Path != "/" {
		return fail("Путь должен задаваться параметром path")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return fail("Повреждённые параметры URI")
	}
	known := map[string]bool{}
	for _, k := range []string{"type", "security", "encryption", "flow", "sni", "serverName", "fp", "pbk", "sid", "spx", "alpn", "path", "host", "serviceName", "mode", "headerType", "allowInsecure", "insecure", "packetEncoding"} {
		known[k] = true
	}
	for k, v := range q {
		if !known[k] {
			return fail("Неизвестный параметр подключения")
		}
		if len(v) != 1 {
			return fail("Повторяющийся параметр подключения")
		}
	}
	if e := q.Get("encryption"); e != "" && e != "none" {
		return fail("Эта версия шифрования VLESS не поддерживается ядром")
	}
	if h := q.Get("headerType"); h != "" && h != "none" {
		return fail("Неподдерживаемая маскировка TCP headerType")
	}
	if q.Get("allowInsecure") == "1" || q.Get("insecure") == "1" || q.Get("allowInsecure") == "true" || q.Get("insecure") == "true" {
		return fail("Отключение проверки TLS не допускается")
	}
	o := map[string]any{"type": "vless", "server": host, "server_port": port, "uuid": strings.ToLower(u.User.Username())}
	flow := q.Get("flow")
	if flow != "" {
		if flow != "xtls-rprx-vision" {
			return fail("Неподдерживаемый flow")
		}
		o["flow"] = flow
	}
	if p := q.Get("packetEncoding"); p != "" {
		if p != "xudp" && p != "packetaddr" {
			return fail("Неподдерживаемый packetEncoding")
		}
		o["packet_encoding"] = p
	}
	security := q.Get("security")
	if security != "" && security != "none" && security != "tls" && security != "reality" {
		return fail("Неподдерживаемый security")
	}
	if security == "tls" || security == "reality" {
		name := q.Get("sni")
		if name == "" {
			name = q.Get("serverName")
		}
		if name == "" {
			name = host
		}
		tls := map[string]any{"enabled": true, "server_name": name}
		if alpn := q.Get("alpn"); alpn != "" {
			tls["alpn"] = strings.Split(alpn, ",")
		}
		fp := q.Get("fp")
		if fp != "" {
			allowed := map[string]bool{"chrome": true, "firefox": true, "edge": true, "safari": true, "ios": true, "android": true, "random": true, "randomized": true, "360": true, "qq": true}
			if !allowed[fp] {
				return fail("Неподдерживаемый TLS fingerprint")
			}
			tls["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
		}
		if security == "reality" {
			key, err := base64.RawURLEncoding.DecodeString(q.Get("pbk"))
			if err != nil || len(key) != 32 {
				return fail("Некорректный публичный ключ REALITY")
			}
			sid := q.Get("sid")
			b, err := hex.DecodeString(sid)
			if err != nil || len(b) > 8 {
				return fail("Некорректный short ID REALITY")
			}
			tls["reality"] = map[string]any{"enabled": true, "public_key": q.Get("pbk"), "short_id": sid}
			if fp == "" {
				tls["utls"] = map[string]any{"enabled": true, "fingerprint": "chrome"}
			}
			if q.Get("spx") != "" && q.Get("spx") != "/" {
				return fail("Параметр spiderX не поддерживается sing-box")
			}
		}
		o["tls"] = tls
	}
	typ := q.Get("type")
	if typ == "" {
		typ = "tcp"
	}
	switch typ {
	case "tcp":
		if q.Get("path") != "" || q.Get("host") != "" || q.Get("serviceName") != "" {
			return fail("Параметры транспорта не соответствуют TCP")
		}
	case "ws":
		t := map[string]any{"type": "ws", "path": q.Get("path")}
		if h := q.Get("host"); h != "" {
			t["headers"] = map[string]any{"Host": h}
		}
		o["transport"] = t
	case "grpc":
		if q.Get("mode") != "" && q.Get("mode") != "gun" {
			return fail("Поддерживается только gRPC mode=gun")
		}
		o["transport"] = map[string]any{"type": "grpc", "service_name": q.Get("serviceName")}
	case "httpupgrade":
		o["transport"] = map[string]any{"type": "httpupgrade", "path": q.Get("path"), "host": q.Get("host")}
	default:
		return fail("Неподдерживаемый транспорт; параметры не будут отброшены")
	}
	if flow != "" && (typ != "tcp" || security == "none" || security == "") {
		return fail("Vision требует TCP и TLS/REALITY")
	}
	name := strings.TrimSpace(u.Fragment)
	if name == "" {
		name = "VLESS " + host
	}
	if len(name) > 120 {
		name = name[:120]
	}
	return Server{ID: identify(o), Name: name, Outbound: o}, nil
}

func decodeJSON(text string) (Import, error) {
	var result Import
	var raw any
	if json.Unmarshal([]byte(text), &raw) != nil {
		return result, errors.New("Повреждённый JSON")
	}
	var candidates []any
	switch v := raw.(type) {
	case map[string]any:
		if os, ok := v["outbounds"].([]any); ok {
			candidates = os
		} else {
			candidates = []any{v}
		}
	case []any:
		candidates = v
	default:
		return result, errors.New("Ожидается объект или массив JSON")
	}
	allowed := map[string]bool{}
	for _, k := range []string{"type", "tag", "server", "server_port", "uuid", "flow", "tls", "transport", "packet_encoding", "network", "connect_timeout", "tcp_fast_open", "tcp_multi_path", "udp_fragment"} {
		allowed[k] = true
	}
	seen := map[string]bool{}
	for i, c := range candidates {
		o, ok := c.(map[string]any)
		if !ok || o["type"] != "vless" {
			continue
		}
		for k := range o {
			if !allowed[k] {
				return result, errors.New("JSON VLESS содержит неподдерживаемые поля")
			}
		}
		id, _ := o["uuid"].(string)
		host, _ := o["server"].(string)
		port, _ := o["server_port"].(float64)
		if !uuid.MatchString(id) || host == "" || port < 1 || port > 65535 || port != float64(int(port)) {
			return result, errors.New("Некорректный JSON VLESS")
		}
		if tls, ok := o["tls"].(map[string]any); ok {
			if tls["insecure"] == true {
				return result, errors.New("Отключение проверки TLS не допускается")
			}
			// Imported profiles run inside a privileged service. Permit inline TLS
			// options only: certificate/key/log paths must never reach the engine.
			if !safeTLS(tls) {
				return result, errors.New("JSON TLS содержит неподдерживаемые поля или пути к файлам")
			}
		}
		name, _ := o["tag"].(string)
		if name == "" {
			name = fmt.Sprintf("VLESS %d", i+1)
		}
		delete(o, "tag")
		s := Server{ID: identify(o), Name: name, Outbound: o}
		if !seen[s.ID] {
			seen[s.ID] = true
			result.Servers = append(result.Servers, s)
		}
	}
	if len(result.Servers) == 0 {
		return result, errors.New("JSON не содержит VLESS-подключений")
	}
	return result, nil
}

func safeTLS(tls map[string]any) bool {
	allowed := map[string]bool{}
	for _, key := range []string{"enabled", "server_name", "insecure", "alpn", "min_version", "max_version", "cipher_suites", "certificate", "certificate_public_key_sha256", "disable_sni", "utls", "reality"} {
		allowed[key] = true
	}
	for key := range tls {
		if !allowed[key] {
			return false
		}
	}
	for name, fields := range map[string][]string{"utls": {"enabled", "fingerprint"}, "reality": {"enabled", "public_key", "short_id"}} {
		if raw, exists := tls[name]; exists {
			object, ok := raw.(map[string]any)
			if !ok {
				return false
			}
			for key := range object {
				found := false
				for _, field := range fields {
					if key == field {
						found = true
					}
				}
				if !found {
					return false
				}
			}
		}
	}
	return true
}

type Warp struct {
	PrivateKey string   `json:"private_key"`
	PublicKey  string   `json:"public_key"`
	Addresses  []string `json:"addresses"`
	Endpoint   string   `json:"endpoint"`
	Port       int      `json:"port"`
	MTU        int      `json:"mtu"`
	Reserved   []int    `json:"reserved,omitempty"`
}

func ParseWireGuard(text string) (Warp, error) {
	var w Warp
	w.MTU = 1280
	fail := func() (Warp, error) {
		return Warp{}, errors.New("Некорректный WARP WireGuard-профиль")
	}
	section := ""
	peers := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = strings.ToLower(line)
			if section == "[peer]" {
				peers++
			}
			if section != "[peer]" && section != "[interface]" {
				return fail()
			}
			continue
		}
		p := strings.SplitN(line, "=", 2)
		if len(p) != 2 {
			return fail()
		}
		k, v := strings.TrimSpace(p[0]), strings.TrimSpace(p[1])
		switch section + "/" + k {
		case "[interface]/PrivateKey":
			w.PrivateKey = v
		case "[interface]/Address":
			for _, a := range strings.Split(v, ",") {
				a = strings.TrimSpace(a)
				if !strings.Contains(a, "/") {
					if strings.Contains(a, ":") {
						a += "/128"
					} else {
						a += "/32"
					}
				}
				if _, err := netip.ParsePrefix(a); err != nil {
					return fail()
				}
				w.Addresses = append(w.Addresses, a)
			}
		case "[interface]/MTU":
			n, e := strconv.Atoi(v)
			if e != nil || n < 1280 || n > 1500 {
				return fail()
			}
			w.MTU = n
		case "[interface]/DNS": // DNS is controlled by Marshrut, not a second system resolver.
		case "[peer]/PublicKey":
			w.PublicKey = v
		case "[peer]/Endpoint":
			h, p, e := net.SplitHostPort(v)
			n, e2 := strconv.Atoi(p)
			if e != nil || e2 != nil || n < 1 || n > 65535 || h == "" {
				return fail()
			}
			w.Endpoint = h
			w.Port = n
		case "[peer]/AllowedIPs":
			for _, a := range strings.Split(v, ",") {
				if _, e := netip.ParsePrefix(strings.TrimSpace(a)); e != nil {
					return fail()
				}
			}
		case "[peer]/PersistentKeepalive":
			n, e := strconv.Atoi(v)
			if e != nil || n < 0 || n > 65535 {
				return fail()
			}
		case "[peer]/Reserved":
			for _, r := range strings.Split(v, ",") {
				n, e := strconv.Atoi(strings.TrimSpace(r))
				if e != nil || n < 0 || n > 255 {
					return fail()
				}
				w.Reserved = append(w.Reserved, n)
			}
			if len(w.Reserved) != 3 {
				return fail()
			}
		default:
			return Warp{}, errors.New("Профиль содержит неподдерживаемые команды или параметры")
		}
	}
	for _, s := range []string{w.PrivateKey, w.PublicKey} {
		b, e := base64.StdEncoding.DecodeString(s)
		if e != nil || len(b) != 32 {
			return fail()
		}
	}
	if peers != 1 || len(w.Addresses) == 0 || w.Endpoint == "" {
		return fail()
	}
	return w, nil
}
