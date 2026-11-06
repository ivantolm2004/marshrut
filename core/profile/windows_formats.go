package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func decodeWindowsJSON(text string) (Import, error) {
	var raw any
	if json.Unmarshal([]byte(text), &raw) != nil {
		return Import{}, errors.New("Повреждённый JSON")
	}
	if document, ok := raw.(map[string]any); ok && document["proxies"] != nil {
		return decodeClash(text)
	}
	var candidates []any
	var collect func(any, int)
	collect = func(value any, depth int) {
		if depth > 8 || len(candidates) >= 4096 {
			return
		}
		switch v := value.(type) {
		case []any:
			for _, item := range v {
				collect(item, depth+1)
			}
		case map[string]any:
			if outbounds, ok := v["outbounds"]; ok {
				collect(outbounds, depth+1)
			} else {
				candidates = append(candidates, v)
			}
		}
	}
	collect(raw, 0)
	return importCandidates(candidates, func(o map[string]any) (Server, error) {
		if o["protocol"] == "vless" {
			return xrayToServer(o)
		}
		if o["type"] != "vless" {
			return Server{}, errors.New("Пропущен сервер с другим протоколом")
		}
		b, _ := json.Marshal(o)
		result, err := decodeJSON(string(b))
		if err != nil {
			return Server{}, err
		}
		return result.Servers[0], nil
	})
}

func decodeClash(text string) (Import, error) {
	var document struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if yaml.Unmarshal([]byte(text), &document) != nil {
		return Import{}, errors.New("Повреждённая подписка Clash YAML")
	}
	candidates := make([]any, len(document.Proxies))
	for i, item := range document.Proxies {
		candidates[i] = item
	}
	return importCandidates(candidates, clashToServer)
}

func importCandidates(candidates []any, parse func(map[string]any) (Server, error)) (Import, error) {
	var result Import
	seen := map[string]bool{}
	for i, candidate := range candidates {
		o, ok := candidate.(map[string]any)
		if !ok {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Сервер %d: некорректный объект", i+1))
			continue
		}
		// Local direct/block/selectors are not remote servers in full configs.
		if typ, _ := o["type"].(string); typ == "direct" || typ == "block" || typ == "selector" || typ == "urltest" || typ == "dns" {
			continue
		}
		if protocol, _ := o["protocol"].(string); protocol == "freedom" || protocol == "blackhole" || protocol == "dns" {
			continue
		}
		s, err := parse(o)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Сервер %d: %s", i+1, err))
			continue
		}
		if !seen[s.ID] {
			result.Servers = append(result.Servers, s)
			seen[s.ID] = true
		}
	}
	if len(result.Servers) == 0 {
		if len(result.Warnings) > 0 {
			return result, fmt.Errorf("Нет поддерживаемых VLESS-подключений. %s", result.Warnings[0])
		}
		return result, errors.New("Подписка не содержит VLESS-подключений")
	}
	return result, nil
}

func allowedFields(o map[string]any, fields ...string) bool {
	allowed := map[string]bool{}
	for _, field := range fields {
		allowed[field] = true
	}
	for field := range o {
		if !allowed[field] {
			return false
		}
	}
	return true
}
func object(value any) map[string]any { v, _ := value.(map[string]any); return v }
func objectFields(o map[string]any, fields ...string) bool {
	for _, field := range fields {
		if value, exists := o[field]; exists {
			if _, ok := value.(map[string]any); !ok {
				return false
			}
		}
	}
	return true
}
func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}
func setQuery(q url.Values, key string, value any) {
	if v := stringValue(value); v != "" {
		q.Set(key, v)
	}
}
func arrayStrings(value any) string {
	items, _ := value.([]any)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if v, ok := item.(string); ok {
			result = append(result, v)
		}
	}
	return strings.Join(result, ",")
}
func serverFromFields(host, port, id, name any, q url.Values) (Server, error) {
	u := url.URL{Scheme: "vless", User: url.User(stringValue(id)), Host: net.JoinHostPort(stringValue(host), stringValue(port)), RawQuery: q.Encode(), Fragment: stringValue(name)}
	return ParseVLESSWindows(u.String())
}

func xrayToServer(o map[string]any) (Server, error) {
	bad := errors.New("JSON Xray содержит неподдерживаемые параметры VLESS")
	if !objectFields(o, "settings", "streamSettings", "mux") {
		return Server{}, bad
	}
	if !allowedFields(o, "protocol", "tag", "settings", "streamSettings", "mux") {
		return Server{}, bad
	}
	if mux := object(o["mux"]); len(mux) > 0 && mux["enabled"] != false {
		return Server{}, errors.New("Xray Mux пока не поддерживается при импорте")
	}
	settings := object(o["settings"])
	if !allowedFields(settings, "vnext") {
		return Server{}, bad
	}
	servers, _ := settings["vnext"].([]any)
	if len(servers) != 1 {
		return Server{}, bad
	}
	server := object(servers[0])
	if !allowedFields(server, "address", "port", "users") {
		return Server{}, bad
	}
	users, _ := server["users"].([]any)
	if len(users) != 1 {
		return Server{}, bad
	}
	user := object(users[0])
	if !allowedFields(user, "id", "encryption", "flow", "email", "level") {
		return Server{}, bad
	}
	stream := object(o["streamSettings"])
	if !objectFields(stream, "tlsSettings", "realitySettings", "tcpSettings", "rawSettings", "wsSettings", "grpcSettings", "httpupgradeSettings", "xhttpSettings", "splithttpSettings") {
		return Server{}, bad
	}
	if !allowedFields(stream, "network", "security", "tlsSettings", "realitySettings", "tcpSettings", "rawSettings", "wsSettings", "grpcSettings", "httpupgradeSettings", "xhttpSettings", "splithttpSettings") {
		return Server{}, bad
	}
	q := url.Values{}
	setQuery(q, "type", stream["network"])
	setQuery(q, "security", stream["security"])
	setQuery(q, "flow", user["flow"])
	setQuery(q, "encryption", user["encryption"])
	for _, name := range []string{"tls", "reality"} {
		tls := object(stream[name+"Settings"])
		if !allowedFields(tls, "serverName", "fingerprint", "allowInsecure", "alpn", "publicKey", "password", "shortId", "spiderX", "show") {
			return Server{}, bad
		}
		if value, exists := tls["allowInsecure"]; exists && value != false {
			return Server{}, errors.New("Отключение проверки TLS не допускается")
		}
		setQuery(q, "sni", tls["serverName"])
		setQuery(q, "fp", tls["fingerprint"])
		setQuery(q, "pbk", tls["publicKey"])
		if q.Get("pbk") == "" {
			setQuery(q, "pbk", tls["password"])
		}
		setQuery(q, "sid", tls["shortId"])
		setQuery(q, "spx", tls["spiderX"])
		if alpn := arrayStrings(tls["alpn"]); alpn != "" {
			q.Set("alpn", alpn)
		}
	}
	for _, name := range []string{"tcpSettings", "rawSettings"} {
		if tcp := object(stream[name]); len(tcp) > 0 {
			if !allowedFields(tcp, "header") || !allowedFields(object(tcp["header"]), "type") {
				return Server{}, bad
			}
			setQuery(q, "headerType", object(tcp["header"])["type"])
		}
	}
	if ws := object(stream["wsSettings"]); len(ws) > 0 {
		if !allowedFields(ws, "path", "headers", "host") || !allowedFields(object(ws["headers"]), "Host") {
			return Server{}, bad
		}
		setQuery(q, "path", ws["path"])
		setQuery(q, "host", ws["host"])
		if h := object(ws["headers"])["Host"]; h != nil {
			setQuery(q, "host", h)
		}
	}
	if grpc := object(stream["grpcSettings"]); len(grpc) > 0 {
		if !allowedFields(grpc, "serviceName", "multiMode") {
			return Server{}, bad
		}
		setQuery(q, "serviceName", grpc["serviceName"])
		if grpc["multiMode"] == true {
			q.Set("mode", "multi")
		}
	}
	if upgrade := object(stream["httpupgradeSettings"]); len(upgrade) > 0 {
		if !allowedFields(upgrade, "host", "path") {
			return Server{}, bad
		}
		setQuery(q, "host", upgrade["host"])
		setQuery(q, "path", upgrade["path"])
	}
	for _, key := range []string{"xhttpSettings", "splithttpSettings"} {
		if xhttp := object(stream[key]); len(xhttp) > 0 {
			if !allowedFields(xhttp, "host", "path", "mode", "extra") {
				return Server{}, bad
			}
			for _, field := range []string{"host", "path", "mode"} {
				setQuery(q, field, xhttp[field])
			}
			if extra, ok := xhttp["extra"]; ok {
				b, _ := json.Marshal(extra)
				q.Set("extra", string(b))
			}
		}
	}
	return serverFromFields(server["address"], server["port"], user["id"], o["tag"], q)
}

func clashToServer(o map[string]any) (Server, error) {
	bad := errors.New("Clash VLESS содержит неподдерживаемые параметры")
	if !objectFields(o, "reality-opts", "ws-opts", "grpc-opts", "http-upgrade-opts", "xhttp-opts") {
		return Server{}, bad
	}
	for _, field := range []string{"tls", "udp", "tfo", "mptcp"} {
		if value, exists := o[field]; exists {
			if _, ok := value.(bool); !ok {
				return Server{}, bad
			}
		}
	}
	if o["type"] != "vless" {
		return Server{}, errors.New("Пропущен сервер с другим протоколом")
	}
	if !allowedFields(o, "type", "name", "server", "port", "uuid", "network", "tls", "servername", "server-name", "sni", "client-fingerprint", "reality-opts", "ws-opts", "grpc-opts", "http-upgrade-opts", "xhttp-opts", "flow", "packet-encoding", "udp", "skip-cert-verify", "alpn", "tfo", "mptcp") {
		return Server{}, bad
	}
	if value, exists := o["skip-cert-verify"]; exists && value != false {
		return Server{}, errors.New("Отключение проверки TLS не допускается")
	}
	q := url.Values{}
	setQuery(q, "type", o["network"])
	setQuery(q, "flow", o["flow"])
	setQuery(q, "packetEncoding", o["packet-encoding"])
	for _, field := range []string{"servername", "server-name", "sni"} {
		setQuery(q, "sni", o[field])
	}
	setQuery(q, "fp", o["client-fingerprint"])
	if o["tls"] == true {
		q.Set("security", "tls")
	}
	if alpn := arrayStrings(o["alpn"]); alpn != "" {
		q.Set("alpn", alpn)
	}
	if reality := object(o["reality-opts"]); len(reality) > 0 {
		if !allowedFields(reality, "public-key", "short-id") {
			return Server{}, bad
		}
		q.Set("security", "reality")
		setQuery(q, "pbk", reality["public-key"])
		setQuery(q, "sid", reality["short-id"])
	}
	if ws := object(o["ws-opts"]); len(ws) > 0 {
		if !allowedFields(ws, "path", "headers", "max-early-data", "early-data-header-name") || !allowedFields(object(ws["headers"]), "Host") {
			return Server{}, bad
		}
		setQuery(q, "path", ws["path"])
		setQuery(q, "host", object(ws["headers"])["Host"])
		setQuery(q, "ed", ws["max-early-data"])
		setQuery(q, "eh", ws["early-data-header-name"])
	}
	if grpc := object(o["grpc-opts"]); len(grpc) > 0 {
		if !allowedFields(grpc, "grpc-service-name") {
			return Server{}, bad
		}
		setQuery(q, "serviceName", grpc["grpc-service-name"])
	}
	for key, transport := range map[string]string{"http-upgrade-opts": "httpupgrade", "xhttp-opts": "xhttp"} {
		if options := object(o[key]); len(options) > 0 {
			if !allowedFields(options, "host", "path", "mode", "extra") {
				return Server{}, bad
			}
			q.Set("type", transport)
			for _, field := range []string{"host", "path", "mode"} {
				setQuery(q, field, options[field])
			}
			if extra, ok := options["extra"]; ok {
				b, _ := json.Marshal(extra)
				q.Set("extra", string(b))
			}
		}
	}
	s, err := serverFromFields(o["server"], o["port"], o["uuid"], o["name"], q)
	if err != nil {
		return s, err
	}
	if s.Xray == nil {
		if o["tfo"] == true {
			s.Outbound["tcp_fast_open"] = true
		}
		if o["mptcp"] == true {
			s.Outbound["tcp_multi_path"] = true
		}
		s.ID = identify(s.Outbound)
	} else if o["tfo"] == true || o["mptcp"] == true {
		return Server{}, bad
	}
	return s, nil
}
