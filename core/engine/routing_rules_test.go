package engine

import (
	"encoding/json"
	"marshrut/route"
	"strings"
	"testing"
)

func TestRulesCompileInPriorityOrderWithAlternativeConditions(t *testing.T) {
	rules := route.DefaultRules()[:3]
	rules[0].IPCIDRs = []string{"203.0.113.0/24"}
	rules[1].Route = "vpn"
	p, err := Build(Options{Platform: "windows", APISecret: "test", ProbePassword: "test", Rules: rules})
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Route struct{ Rules []map[string]any }
	}
	if err = json.Unmarshal(p.Config, &config); err != nil {
		t.Fatal(err)
	}
	var custom []map[string]any
	for _, rule := range config.Route.Rules {
		if rule["process_path_regex"] != nil || rule["domain_suffix"] != nil || rule["ip_cidr"] != nil {
			custom = append(custom, rule)
		}
	}
	if len(custom) != 14 || custom[0]["process_path_regex"] == nil || custom[0]["domain_suffix"] != nil || custom[2]["domain_suffix"] == nil || custom[4]["ip_cidr"] == nil || custom[6]["outbound"] != "vpn-udp" {
		t.Fatal("conditions were combined as AND or priority changed", len(custom))
	}
	if custom[0]["outbound"] != "direct" || custom[0]["network"] != "udp" {
		t.Fatal("game routing changed")
	}
}

func TestEmptyRuleListDisablesLegacyDomainRulesAndMissingRoutesBlock(t *testing.T) {
	p, err := Build(Options{Platform: "windows", APISecret: "test", ProbePassword: "test", Rules: []route.Rule{}})
	if err != nil || strings.Contains(string(p.Config), `"domain_suffix"`) {
		t.Fatal("disabled catalogue fell back to hidden legacy rules", err)
	}
	for _, target := range []string{"warp", "vless-removed", "dpi"} {
		r := route.Rule{Route: target, Domains: []string{"example.com"}, Enabled: true}
		if got := routingTarget(r, "tcp", map[string]string{"direct": "direct"}); got != "block" {
			t.Fatal("missing route leaked", target, got)
		}
	}
}

func TestAndroidCatalogueOverridePrecedesServiceFallbackInTrafficAndDNS(t *testing.T) {
	p, err := Build(Options{Platform: "android", APISecret: "test", ProbePassword: "test",
		Rules: []route.Rule{{ID: "video", Name: "YouTube", Category: "video", Route: "direct",
			Enabled: true, Domains: []string{"youtube.com"}}}})
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Route struct{ Rules []map[string]any }
		DNS   struct{ Rules []map[string]any }
	}
	if err := json.Unmarshal(p.Config, &config); err != nil {
		t.Fatal(err)
	}
	first := func(rules []map[string]any, host string) map[string]any {
		for _, r := range rules {
			domains, _ := r["domain_suffix"].([]any)
			for _, domain := range domains {
				if domain == host {
					return r
				}
			}
		}
		return nil
	}
	if first(config.Route.Rules, "youtube.com")["outbound"] != "direct" ||
		first(config.DNS.Rules, "youtube.com")["server"] != "dns-probe-direct" ||
		first(config.Route.Rules, "discord.com")["outbound"] != "discord-udp" ||
		first(config.DNS.Rules, "discord.com")["server"] != "dns-discord" {
		t.Fatal("Android override or inherited service routing was lost")
	}
}
