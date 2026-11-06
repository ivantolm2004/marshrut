package engine

import (
	"marshrut/route"
	"strconv"
	"strings"
)

// DNS queries can be shared between TCP and UDP connections. Resolve through
// an explicit tunnel if either protocol's winning rule requests one. Otherwise
// use the first permitted winner. Refuse only if both winners block.
// Destination IP conditions belong to connection routing, after resolution.
func compileDNSRules(entries []route.Rule, available map[string]string, platform string) (rules, sets []any) {
	type namedRule struct {
		entry    route.Rule
		matchers []map[string]any
	}
	var named []namedRule
	split := false
	for _, entry := range entries {
		if !entry.Enabled {
			continue
		}
		matchers := nameMatchers(entry, platform)
		if len(matchers) == 0 {
			continue
		}
		named = append(named, namedRule{entry, matchers})
		split = split || entry.Network != ""
	}

	if !split {
		for _, row := range named {
			target := routingTarget(row.entry, "tcp", available)
			for _, matcher := range row.matchers {
				rules = append(rules, dnsAction(matcher, row.entry, target))
			}
		}
		return rules, nil
	}

	var tunnels, other []any
	var earlier [2][]string
	for i, row := range named {
		tag := "mr-dns-" + strconv.Itoa(i)
		sets = append(sets, map[string]any{"type": "inline", "tag": tag, "rules": row.matchers})
		for j, network := range [...]string{"tcp", "udp"} {
			if row.entry.Network != "" && row.entry.Network != network {
				continue
			}
			target := routingTarget(row.entry, network, available)
			if target != "block" {
				match := map[string]any{"rule_set": tag}
				if len(earlier[j]) > 0 {
					// Reuse the inline matchers. Only references are repeated
					// when expressing which rules can shadow this winner.
					match = dnsAnd(match, map[string]any{"rule_set": earlier[j], "invert": true})
				}
				action := dnsAction(match, row.entry, target)
				if row.entry.Route == "vpn" || target == "warp" || strings.HasPrefix(target, "vless-") {
					tunnels = append(tunnels, action)
				} else {
					other = append(other, action)
				}
			}
			earlier[j] = append(earlier[j], tag)
		}
	}
	rules = append(tunnels, other...)
	if len(earlier[0]) > 0 && len(earlier[1]) > 0 {
		both := dnsAnd(map[string]any{"rule_set": earlier[0]}, map[string]any{"rule_set": earlier[1]})
		rules = append(rules, dnsAction(both, route.Rule{}, "block"))
	}
	if len(earlier[0]) > 0 {
		rules = append(rules, map[string]any{"rule_set": earlier[0], "action": "route", "server": "dns-default-udp"})
	}
	return rules, sets
}

func dnsAnd(conditions ...map[string]any) map[string]any {
	return map[string]any{"type": "logical", "mode": "and", "rules": conditions}
}

func dnsAction(match map[string]any, rule route.Rule, target string) map[string]any {
	if target == "block" {
		match["action"] = "predefined"
		match["rcode"] = "REFUSED"
		return match
	}
	server := "dns-probe-" + target
	switch rule.Route {
	case "auto", "vpn":
		server = "dns-" + target
	case "dpi":
		server = "bootstrap"
	}
	match["action"] = "route"
	match["server"] = server
	return match
}
