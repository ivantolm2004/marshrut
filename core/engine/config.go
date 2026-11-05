package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"marshrut/profile"
	"marshrut/route"
	"net/netip"
	"sort"
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
}
type Plan struct {
	Config     []byte
	Candidates []string
	Ports      map[string]int
	Tags       map[string]string
}

func Build(o Options) (Plan, error) {
	var plan Plan
	plan.Ports = map[string]int{}
	plan.Tags = map[string]string{}
	if o.APISecret == "" || o.ProbePassword == "" {
		return plan, errors.New("Не заданы локальные ключи доступа")
	}
	if o.APIPort == 0 {
		o.APIPort = 2090
	}
	if o.BasePort == 0 {
		o.BasePort = 2100
	}
	if o.DPIPort == 0 {
		o.DPIPort = 2083
	}
	out := []any{map[string]any{"type": "direct", "tag": "direct"}, map[string]any{"type": "block", "tag": "block"}}
	plan.Candidates = []string{"direct"}
	plan.Tags["direct"] = "direct"
	for _, s := range o.Servers {
		b, _ := json.Marshal(s.Outbound)
		var copy map[string]any
		json.Unmarshal(b, &copy)
		tag := "vless-" + s.ID
		copy["tag"] = tag
		copy["domain_resolver"] = "bootstrap"
		out = append(out, copy)
		plan.Candidates = append(plan.Candidates, tag)
		plan.Tags[tag] = tag
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
	var inbounds, rules, dnsRules, dnsServers []any
	rules = append(rules, map[string]any{"action": "sniff", "timeout": "300ms"}, map[string]any{"protocol": "dns", "action": "hijack-dns"})
	dnsServers = append(dnsServers, map[string]any{"type": "https", "tag": "bootstrap", "server": "1.1.1.1", "server_port": 443, "path": "/dns-query", "tls": map[string]any{"enabled": true, "server_name": "cloudflare-dns.com"}, "detour": "direct"})
	for i, r := range plan.Candidates {
		port := o.BasePort + i
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
	rules = append(rules, map[string]any{"ip_cidr": []string{"149.154.160.0/20", "91.108.4.0/22", "91.108.8.0/22", "91.108.12.0/22", "91.108.16.0/22", "91.108.56.0/22"}, "network": "udp", "action": "route", "outbound": "telegram-udp"}, map[string]any{"ip_cidr": []string{"149.154.160.0/20", "91.108.4.0/22", "91.108.8.0/22", "91.108.12.0/22", "91.108.16.0/22", "91.108.56.0/22"}, "action": "route", "outbound": "telegram"})
	for _, g := range route.Groups() {
		udpChoices := []string{"block"}
		for _, r := range plan.Candidates {
			if r != "dpi" {
				udpChoices = append(udpChoices, r)
			}
		}
		out = append(out, map[string]any{"type": "selector", "tag": g.ID, "outbounds": selectorChoices, "default": "block", "interrupt_exist_connections": false}, map[string]any{"type": "selector", "tag": g.ID + "-udp", "outbounds": udpChoices, "default": "block", "interrupt_exist_connections": false})
		if len(g.Domains) > 0 {
			rules = append(rules, map[string]any{"domain_suffix": g.Domains, "network": "udp", "action": "route", "outbound": g.ID + "-udp"}, map[string]any{"domain_suffix": g.Domains, "action": "route", "outbound": g.ID})
			dnsRules = append(dnsRules, map[string]any{"domain_suffix": g.Domains, "action": "route", "server": "dns-" + g.ID})
		}
		dnsServers = append(dnsServers, map[string]any{"type": "https", "tag": "dns-" + g.ID, "server": "1.1.1.1", "server_port": 443, "path": "/dns-query", "tls": map[string]any{"enabled": true, "server_name": "cloudflare-dns.com"}, "detour": g.ID})
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
	config := map[string]any{
		"log":      map[string]any{"disabled": true}, // Raw engine messages can contain subscription credentials.
		"dns":      map[string]any{"servers": dnsServers, "rules": dnsRules, "final": "dns-default", "strategy": "ipv4_only", "independent_cache": true, "reverse_mapping": true},
		"inbounds": inbounds, "outbounds": out,
		"route":        map[string]any{"rules": rules, "final": "default", "auto_detect_interface": o.Platform != "android", "default_domain_resolver": map[string]any{"server": "bootstrap", "strategy": "ipv4_only"}},
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
