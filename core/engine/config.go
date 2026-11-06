package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"marshrut/profile"
	"marshrut/route"
	"net"
	"net/netip"
	"sort"
	"strings"
)

type Options struct {
	Platform      string
	Servers       []profile.Server
	Warp          *profile.Warp
	DPI           bool
	TUN           bool
	APISecret     string
	ProbeUser     string
	ProbePassword string
	APIPort       int
	BasePort      int
	DPIPort       int
	Pins          map[string]string
	Rules         []route.Rule
}
type Plan struct {
	Config     []byte
	TUN        bool
	XrayConfig []byte
	XrayPorts  []int
	Candidates []string
	Ports      map[string]int
	Tags       map[string]string
}

func Build(o Options) (Plan, error) {
	var plan Plan
	plan.TUN = o.TUN
	plan.Ports = map[string]int{}
	plan.Tags = map[string]string{}
	if o.APISecret == "" || o.ProbePassword == "" {
		return plan, errors.New("Не заданы локальные ключи доступа")
	}
	if o.Rules != nil {
		var err error
		o.Rules, err = route.NormalizeRules(o.Rules)
		if err != nil {
			return plan, err
		}
	}
	if o.APIPort == 0 {
		o.APIPort = 2090
	}
	if o.DPIPort == 0 {
		o.DPIPort = 2083
	}
	out := []any{map[string]any{"type": "direct", "tag": "direct"}, map[string]any{"type": "block", "tag": "block"}}
	var xrayInbounds, xrayOutbounds, xrayRules []any
	usedPorts := map[int]bool{o.APIPort: true}
	var reservations []net.Listener
	defer func() {
		for _, listener := range reservations {
			listener.Close()
		}
	}()
	reserve := func(preferred int) (int, error) {
		if usedPorts[preferred] {
			preferred = 0
		}
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferred))
		if err != nil && preferred != 0 {
			listener, err = net.Listen("tcp", "127.0.0.1:0")
		}
		if err != nil {
			return 0, errors.New("Нет свободного локального порта")
		}
		port := listener.Addr().(*net.TCPAddr).Port
		reservations = append(reservations, listener)
		usedPorts[port] = true
		return port, nil
	}
	plan.Candidates = []string{"direct"}
	plan.Tags["direct"] = "direct"
	for _, s := range o.Servers {
		b, _ := json.Marshal(s.Outbound)
		var copy map[string]any
		json.Unmarshal(b, &copy)
		tag := "vless-" + s.ID
		if s.Xray != nil {
			if o.Platform != "windows" {
				return plan, errors.New("Этот сервер требует Xray, доступный в Windows-версии")
			}
			port, err := reserve(0)
			if err != nil {
				return plan, errors.New("Нет свободного порта для подключения")
			}
			plan.XrayPorts = append(plan.XrayPorts, port)
			b, _ := json.Marshal(s.Xray)
			var xray map[string]any
			json.Unmarshal(b, &xray)
			xray["tag"] = tag
			xrayOutbounds = append(xrayOutbounds, xray)
			xrayInbounds = append(xrayInbounds, map[string]any{"tag": tag, "listen": "127.0.0.1", "port": port, "protocol": "socks", "settings": map[string]any{"auth": "password", "accounts": []any{map[string]any{"user": o.ProbeUser, "pass": o.ProbePassword}}, "udp": true, "ip": "127.0.0.1"}})
			xrayRules = append(xrayRules, map[string]any{"type": "field", "inboundTag": []string{tag}, "outboundTag": tag})
			copy = map[string]any{"type": "socks", "server": "127.0.0.1", "server_port": port, "version": "5", "username": o.ProbeUser, "password": o.ProbePassword}
		}
		copy["tag"] = tag
		copy["domain_resolver"] = "bootstrap"
		out = append(out, copy)
		plan.Candidates = append(plan.Candidates, tag)
		plan.Tags[tag] = tag
	}
	if len(xrayOutbounds) > 0 {
		// Routing only by local inbound keeps each provider isolated. No public
		// listener, arbitrary inbound, DNS setting or route is imported from users.
		plan.XrayConfig, _ = json.Marshal(map[string]any{"log": map[string]any{"loglevel": "none"}, "inbounds": xrayInbounds, "outbounds": xrayOutbounds, "routing": map[string]any{"rules": xrayRules}})
	}
	var endpoints []any
	if w := o.Warp; w != nil {
		peer := map[string]any{"address": w.Endpoint, "port": w.Port, "public_key": w.PublicKey, "allowed_ips": []string{"0.0.0.0/0", "::/0"}, "persistent_keepalive_interval": 25}
		if len(w.Reserved) == 3 {
			peer["reserved"] = w.Reserved
		}
		endpoints = append(endpoints, map[string]any{"type": "wireguard", "tag": "warp", "system": false, "mtu": w.MTU, "address": w.Addresses, "private_key": w.PrivateKey, "peers": []any{peer}, "domain_resolver": "bootstrap"})
		plan.Candidates = append(plan.Candidates, "warp")
		plan.Tags["warp"] = "warp"
	}
	if o.DPI {
		if o.Platform == "android" {
			out = append(out, map[string]any{"type": "socks", "tag": "dpi", "server": "127.0.0.1", "server_port": o.DPIPort, "version": "5", "network": "tcp"})
		} else {
			out = append(out, map[string]any{"type": "marshrut-dpi", "tag": "dpi"})
		}
		plan.Candidates = append(plan.Candidates, "dpi")
		plan.Tags["dpi"] = "dpi"
	}
	selectorChoices := append([]string{"block"}, plan.Candidates...)
	var inbounds, rules, dnsRules, dnsServers, dnsRuleSets []any
	rules = append(rules, map[string]any{"action": "sniff", "timeout": "300ms"}, map[string]any{"protocol": "dns", "action": "hijack-dns"})
	dnsServers = append(dnsServers, map[string]any{"type": "https", "tag": "bootstrap", "server": "1.1.1.1", "server_port": 443, "path": "/dns-query", "tls": map[string]any{"enabled": true, "server_name": "cloudflare-dns.com"}, "detour": "direct"})
	for i, r := range plan.Candidates {
		preferred := 0
		if o.BasePort != 0 {
			preferred = o.BasePort + i
		}
		port, err := reserve(preferred)
		if err != nil {
			return plan, err
		}
		plan.Ports[r] = port
		tag := "probe-" + r
		inbounds = append(inbounds, map[string]any{"type": "mixed", "tag": tag, "listen": "127.0.0.1", "listen_port": port, "users": []any{map[string]any{"username": o.ProbeUser, "password": o.ProbePassword}}})
		rules = append(rules, map[string]any{"inbound": []string{tag}, "action": "resolve", "server": "dns-probe-" + r, "strategy": "ipv4_only"}, map[string]any{"inbound": []string{tag}, "action": "route", "outbound": r})
		dnsTag := "dns-probe-" + r
		detour := r
		if r == "dpi" {
			detour = "direct"
		}
		dnsServers = append(dnsServers, map[string]any{"type": "https", "tag": dnsTag, "server": "1.1.1.1", "tls": map[string]any{"enabled": true, "server_name": "cloudflare-dns.com"}, "detour": detour})
		dnsRules = append(dnsRules, map[string]any{"inbound": []string{tag}, "action": "route", "server": dnsTag})
	}
	// Private ranges stay local even when the default selector is blocked.
	rules = append(rules, map[string]any{"ip_is_private": true, "action": "route", "outbound": "direct"})
	// Telegram uses literal IPs as well as domain names. Route its published ranges.
	if o.Rules == nil {
		rules = append(rules, map[string]any{"ip_cidr": []string{"149.154.160.0/20", "91.108.4.0/22", "91.108.8.0/22", "91.108.12.0/22", "91.108.16.0/22", "91.108.56.0/22"}, "network": "udp", "action": "route", "outbound": "telegram-udp"}, map[string]any{"ip_cidr": []string{"149.154.160.0/20", "91.108.4.0/22", "91.108.8.0/22", "91.108.12.0/22", "91.108.16.0/22", "91.108.56.0/22"}, "action": "route", "outbound": "telegram"})
	}
	if o.Rules != nil {
		rules = append(rules, compileRoutingRules(o.Rules, plan.Tags, o.Platform)...)
		dnsCustom, sets := compileDNSRules(o.Rules, plan.Tags, o.Platform)
		dnsRules = append(dnsRules, dnsCustom...)
		dnsRuleSets = sets
	}
	tunnelChoices := []string{"block"}
	for _, candidate := range plan.Candidates {
		if candidate == "warp" || strings.HasPrefix(candidate, "vless-") {
			tunnelChoices = append(tunnelChoices, candidate)
		}
	}
	for _, group := range route.Groups() {
		tag := "vpn"
		if group.ID != "default" {
			tag += "-" + group.ID
		}
		for _, choice := range []string{tag, tag + "-udp"} {
			out = append(out, map[string]any{"type": "selector", "tag": choice, "outbounds": tunnelChoices, "default": "block", "interrupt_exist_connections": false})
			if choice == tag || o.Rules != nil {
				dnsServers = append(dnsServers, dnsSelector(choice))
			}
		}
	}
	for _, g := range route.Groups() {
		udpChoices := []string{"block"}
		for _, r := range plan.Candidates {
			if r != "dpi" {
				udpChoices = append(udpChoices, r)
			}
		}
		out = append(out, map[string]any{"type": "selector", "tag": g.ID, "outbounds": selectorChoices, "default": "block", "interrupt_exist_connections": false}, map[string]any{"type": "selector", "tag": g.ID + "-udp", "outbounds": udpChoices, "default": "block", "interrupt_exist_connections": false})
		if len(g.Domains) > 0 && o.Rules == nil {
			rules = append(rules, map[string]any{"domain_suffix": g.Domains, "network": "udp", "action": "route", "outbound": g.ID + "-udp"}, map[string]any{"domain_suffix": g.Domains, "action": "route", "outbound": g.ID})
			dnsRules = append(dnsRules, map[string]any{"domain_suffix": g.Domains, "action": "route", "server": "dns-" + g.ID})
		}
		dnsServers = append(dnsServers, dnsSelector(g.ID))
		if o.Rules != nil {
			dnsServers = append(dnsServers, dnsSelector(g.ID+"-udp"))
		}
	}
	rules = append(rules, map[string]any{"network": "udp", "action": "route", "outbound": "default-udp"})
	if o.TUN {
		tun := map[string]any{"type": "tun", "tag": "tun-in", "interface_name": "Marshrut", "address": []string{"172.29.127.1/30", "fd27:6d61:7273::1/126"}, "mtu": 1280, "auto_route": true, "stack": "mixed", "dns_mode": "hijack", "dns_address": []string{"172.29.127.2"}, "strict_route": true}
		if o.Platform == "android" {
			delete(tun, "interface_name")
			delete(tun, "strict_route")
		}
		inbounds = append(inbounds, tun)
	}
	router := map[string]any{"rules": rules, "final": "default", "auto_detect_interface": o.Platform != "android", "default_domain_resolver": map[string]any{"server": "bootstrap", "strategy": "ipv4_only"}}
	if len(dnsRuleSets) > 0 {
		router["rule_set"] = dnsRuleSets
	}
	config := map[string]any{
		"log":      map[string]any{"disabled": true}, // Raw engine messages can contain subscription credentials.
		"dns":      map[string]any{"servers": dnsServers, "rules": dnsRules, "final": "dns-default", "strategy": "ipv4_only", "independent_cache": true, "reverse_mapping": true},
		"inbounds": inbounds, "outbounds": out,
		"route":        router,
		"experimental": map[string]any{"clash_api": map[string]any{"external_controller": fmt.Sprintf("127.0.0.1:%d", o.APIPort), "secret": o.APISecret}},
	}
	for _, s := range dnsServers {
		server := s.(map[string]any)
		if server["detour"] == "direct" {
			delete(server, "detour")
		}
	}
	if len(endpoints) > 0 {
		config["endpoints"] = endpoints
	}
	plan.Config, _ = json.MarshalIndent(config, "", "  ")
	return plan, nil
}

func dnsSelector(tag string) map[string]any {
	return map[string]any{"type": "https", "tag": "dns-" + tag, "server": "1.1.1.1", "server_port": 443, "path": "/dns-query", "tls": map[string]any{"enabled": true, "server_name": "cloudflare-dns.com"}, "detour": tag}
}
func EndpointIPs(servers []profile.Server, w *profile.Warp) []string {
	var ips []string
	for _, s := range servers {
		if h, ok := s.Outbound["server"].(string); ok {
			if a, e := netip.ParseAddr(h); e == nil {
				ips = append(ips, a.String())
			}
		}
	}
	if w != nil {
		if a, e := netip.ParseAddr(w.Endpoint); e == nil {
			ips = append(ips, a.String())
		}
	}
	sort.Strings(ips)
	return ips
}
