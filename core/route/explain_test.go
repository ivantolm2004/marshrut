package route

import "testing"

func TestExplanationUsesProtocolPriorityAndCombinedAppSite(t *testing.T) {
	a := NewApplication(`C:\Apps\browser.exe`, false)
	rules, err := NormalizeRules([]Rule{
		{ID: "off", Name: "Disabled", Enabled: false, Route: "block", Domains: []string{"example.org"}},
		{ID: "udp", Name: "Voice", Enabled: true, Route: "vpn", Network: "udp", Domains: []string{"example.org"}},
		{ID: "app", Name: "App", Enabled: true, Route: "direct", ProcessPaths: []string{a.Path}},
		{ID: "site", Name: "Site", Enabled: true, Route: "block", Domains: []string{"example.org"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	target := MatchTarget{Domain: "sub.example.org", Application: &a}
	tcp, udp := Explain(rules, target, "tcp"), Explain(rules, target, "udp")
	if tcp.RuleID != "app" || tcp.Shadowed != 1 || udp.RuleID != "udp" || udp.Shadowed != 2 {
		t.Fatal("preview disagrees with ordered OR matchers", tcp, udp)
	}
	local := Explain(rules, MatchTarget{IP: "192.168.1.1", Application: &a}, "udp")
	if local.RuleID != "system-local" || local.Route != "direct" {
		t.Fatal("LAN precedence lost")
	}
}
