package profile

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// Normalize only documented aliases and explicitly harmless display metadata.
// Unknown connection parameters still fail instead of silently changing a tunnel.
func ParseVLESSWindows(text string) (Server, error) {
	u, err := url.Parse(text)
	if err != nil || u.Scheme != "vless" {
		return Server{}, errors.New("Ожидается vless://; другой протокол пока не поддерживается")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return Server{}, errors.New("Повреждённые параметры VLESS")
	}
	for _, values := range q {
		if len(values) != 1 {
			return Server{}, errors.New("Повторяющийся параметр подключения")
		}
	}
	for _, key := range []string{"allowInsecure", "insecure"} {
		if value := strings.ToLower(q.Get(key)); value != "" && value != "0" && value != "false" {
			return Server{}, errors.New("Отключение проверки TLS не допускается")
		}
	}
	for alias, key := range map[string]string{"service_name": "serviceName", "packet_encoding": "packetEncoding"} {
		if values, ok := q[alias]; ok {
			if other := q.Get(key); other != "" && other != values[0] {
				return Server{}, errors.New("Противоречивые параметры подключения")
			}
			q.Set(key, values[0])
			q.Del(alias)
		}
	}
	if q.Get("sni") != "" && q.Get("serverName") != "" && q.Get("sni") != q.Get("serverName") {
		return Server{}, errors.New("Противоречивые имена TLS-сервера")
	}
	for _, key := range []string{"remarks", "remark", "group"} {
		q.Del(key)
	}
	switch q.Get("type") {
	case "", "raw":
		q.Set("type", "tcp")
	case "websocket":
		q.Set("type", "ws")
	case "splithttp":
		q.Set("type", "xhttp")
	}
	if q.Get("type") == "ws" {
		if path, err := url.Parse(q.Get("path")); err == nil && path.Query().Has("ed") {
			args := path.Query()
			if q.Has("ed") && q.Get("ed") != args.Get("ed") {
				return Server{}, errors.New("Противоречивые параметры WebSocket Early Data")
			}
			q.Set("ed", args.Get("ed"))
			args.Del("ed")
			path.RawQuery = args.Encode()
			q.Set("path", path.String())
		}
	}
	// Validate every ordinary parameter with the existing strict importer first.
	// Substitute only fields handled explicitly below by Xray/early-data support.
	base := cloneQuery(q)
	for _, key := range []string{"ed", "eh", "extra"} {
		base.Del(key)
	}
	transport := q.Get("type")
	for key, transports := range map[string]string{"path": "ws,httpupgrade,xhttp", "host": "ws,httpupgrade,xhttp", "serviceName": "grpc", "mode": "grpc,xhttp"} {
		if q.Get(key) != "" && !strings.Contains(","+transports+",", ","+transport+",") {
			return Server{}, errors.New("Параметры не соответствуют выбранному транспорту VLESS")
		}
	}
	if q.Get("security") != "reality" && (q.Get("pbk") != "" || q.Get("sid") != "" || q.Get("spx") != "") {
		return Server{}, errors.New("Параметры REALITY требуют security=reality")
	}
	needsXray := transport == "xhttp" || (transport == "grpc" && q.Get("mode") == "multi") || (q.Get("encryption") != "" && q.Get("encryption") != "none") || (q.Get("spx") != "" && q.Get("spx") != "/")
	if needsXray {
		base.Set("type", "tcp")
		base.Set("encryption", "none")
		base.Del("spx")
		for _, key := range []string{"path", "host", "serviceName", "mode"} {
			base.Del(key)
		}
	}
	u.RawQuery = base.Encode()
	s, err := ParseVLESS(u.String())
	if err != nil {
		return Server{}, err
	}
	if transport != "tcp" && transport != "ws" && transport != "grpc" && transport != "httpupgrade" && transport != "xhttp" {
		return Server{}, errors.New("Этот транспорт VLESS пока не поддерживается")
	}
	if q.Get("extra") != "" && transport != "xhttp" {
		return Server{}, errors.New("Параметр extra поддерживается только для XHTTP")
	}
	if (q.Get("ed") != "" || q.Get("eh") != "") && transport != "ws" {
		return Server{}, errors.New("Early Data поддерживается только для WebSocket")
	}
	if transport != "tcp" && q.Get("flow") != "" {
		return Server{}, errors.New("Vision требует транспорт TCP/RAW")
	}
	if !needsXray {
		if transport == "ws" && (q.Get("ed") != "" || q.Get("eh") != "") {
			n, err := strconv.Atoi(q.Get("ed"))
			if err != nil || n < 0 || n > 65535 {
				return Server{}, errors.New("Некорректный размер WebSocket Early Data")
			}
			t := s.Outbound["transport"].(map[string]any)
			t["max_early_data"] = n
			header := q.Get("eh")
			if header == "" {
				header = "Sec-WebSocket-Protocol"
			}
			t["early_data_header_name"] = header
			s.ID = identify(s.Outbound)
		}
		return s, nil
	}
	if q.Get("ed") != "" || q.Get("eh") != "" {
		return Server{}, errors.New("Сочетание Early Data с этими параметрами пока не поддерживается")
	}
	stream := map[string]any{"network": transport, "security": "none"}
	if q.Get("packetEncoding") == "packetaddr" {
		return Server{}, errors.New("packetaddr для Xray пока не поддерживается")
	}
	security := q.Get("security")
	if security == "tls" || security == "reality" {
		stream["security"] = security
		tls := s.Outbound["tls"].(map[string]any)
		settings := map[string]any{"serverName": tls["server_name"]}
		if fp := q.Get("fp"); fp != "" {
			settings["fingerprint"] = fp
		}
		if security == "reality" {
			settings["publicKey"] = q.Get("pbk")
			settings["shortId"] = q.Get("sid")
			settings["spiderX"] = q.Get("spx")
			if q.Get("fp") == "" {
				settings["fingerprint"] = "chrome"
			}
		} else if alpn := q.Get("alpn"); alpn != "" {
			settings["alpn"] = strings.Split(alpn, ",")
		}
		stream[security+"Settings"] = settings
	}
	switch transport {
	case "tcp":
	case "ws":
		ws := map[string]any{"path": q.Get("path")}
		if q.Get("host") != "" {
			ws["headers"] = map[string]any{"Host": q.Get("host")}
		}
		stream["wsSettings"] = ws
	case "grpc":
		if mode := q.Get("mode"); mode != "" && mode != "gun" && mode != "multi" {
			return Server{}, errors.New("Неподдерживаемый режим gRPC")
		}
		stream["grpcSettings"] = map[string]any{"serviceName": q.Get("serviceName"), "multiMode": q.Get("mode") == "multi"}
	case "httpupgrade":
		stream["httpupgradeSettings"] = map[string]any{"host": q.Get("host"), "path": q.Get("path")}
	case "xhttp":
		mode := q.Get("mode")
		if mode == "" {
			mode = "auto"
		}
		if mode != "auto" && mode != "packet-up" && mode != "stream-up" && mode != "stream-one" {
			return Server{}, errors.New("Неподдерживаемый режим XHTTP")
		}
		xhttp := map[string]any{"host": q.Get("host"), "path": q.Get("path"), "mode": mode}
		if extra := q.Get("extra"); extra != "" {
			var v map[string]any
			if len(extra) > 64<<10 || json.Unmarshal([]byte(extra), &v) != nil || !safeXHTTP(v) {
				return Server{}, errors.New("XHTTP extra содержит неподдерживаемые параметры")
			}
			xhttp["extra"] = v
		}
		stream["xhttpSettings"] = xhttp
	}
	encryption := q.Get("encryption")
	if encryption == "" {
		encryption = "none"
	}
	user := map[string]any{"id": s.Outbound["uuid"], "encryption": encryption}
	if q.Get("flow") != "" {
		user["flow"] = q.Get("flow")
	}
	s.Xray = map[string]any{"protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": s.Outbound["server"], "port": s.Outbound["server_port"], "users": []any{user}}}}, "streamSettings": stream}
	// This outbound retains endpoint metadata for pings/exclusions, but must never
	// be passed to sing-box as a native VLESS configuration.
	s.ID = identify(s.Xray)
	return s, nil
}

func cloneQuery(q url.Values) url.Values {
	r := url.Values{}
	for k, v := range q {
		r[k] = append([]string(nil), v...)
	}
	return r
}

func safeXHTTP(v map[string]any) bool {
	for k, value := range v {
		switch k {
		case "noGRPCHeader", "noSSEHeader":
			if _, ok := value.(bool); !ok {
				return false
			}
		case "scMaxEachPostBytes", "scMinPostsIntervalMs", "scMaxBufferedPosts", "scStreamUpServerSecs", "xPaddingBytes":
			switch value.(type) {
			case string, float64:
			default:
				return false
			}
		case "headers":
			headers, ok := value.(map[string]any)
			if !ok {
				return false
			}
			for _, header := range headers {
				if _, ok := header.(string); !ok {
					return false
				}
			}
		case "xmux":
			xmux, ok := value.(map[string]any)
			if !ok {
				return false
			}
			for field, item := range xmux {
				switch field {
				case "maxConcurrency", "maxConnections", "cMaxReuseTimes", "hMaxRequestTimes", "hMaxReusableSecs", "hKeepAlivePeriod":
				default:
					return false
				}
				switch item.(type) {
				case string, float64:
				default:
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}
