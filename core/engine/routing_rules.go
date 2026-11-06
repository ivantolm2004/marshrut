package engine

import "marshrut/route"

func compileRoutingRules(entries []route.Rule, available map[string]string, platform string) []any {
	var rules []any
	for _, entry := range entries {
		if !entry.Enabled {
			continue
		}
		matchers := nameMatchers(entry, platform)
		if len(entry.IPCIDRs) > 0 {
			matchers = append(matchers, map[string]any{"ip_cidr": entry.IPCIDRs})
		}
		for _, matcher := range matchers {
			for _, network := range []string{"udp", "tcp"} {
				if entry.Network != "" && entry.Network != network {
					continue
				}
				rule := map[string]any{"network": network, "action": "route", "outbound": routingTarget(entry, network, available)}
				for key, value := range matcher {
					rule[key] = value
				}
				rules = append(rules, rule)
			}
		}
	}
	return rules
}

func nameMatchers(entry route.Rule, platform string) []map[string]any {
	var matchers []map[string]any
	if len(entry.Processes)+len(entry.ProcessPaths) > 0 && platform == "windows" {
		matchers = append(matchers, map[string]any{"process_path_regex": entry.ProcessPatterns()})
	}
	if len(entry.Domains) > 0 {
		matchers = append(matchers, map[string]any{"domain_suffix": entry.Domains})
	}
	return matchers
}

func routingTarget(r route.Rule, network string, available map[string]string) string {
	if r.Route == "auto" {
		tag := r.AutoGroup
		if tag == "" {
			tag = "default"
		}
		if network == "udp" {
			tag += "-udp"
		}
		return tag
	}
	if r.Route == "vpn" {
		tag := "vpn"
		if r.AutoGroup != "" && r.AutoGroup != "default" {
			tag += "-" + r.AutoGroup
		}
		if network == "udp" {
			return tag + "-udp"
		}
		return tag
	}
	if r.Route == "block" {
		return "block"
	}
	if _, ok := available[r.Route]; !ok {
		return "block"
	}
	if network == "udp" && r.Route == "dpi" {
		return "block"
	}
	return r.Route
}
