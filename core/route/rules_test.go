package route

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestDefaultRulesIncludeRequestedGamesAndSpecificDomainsFirst(t *testing.T) {
	rules, err := NormalizeRules(DefaultRules())
	if err != nil || len(rules) != 31 {
		t.Fatal("invalid preset catalogue", err)
	}
	for i, id := range []string{"game-dota2", "game-cs2", "game-pubg"} {
		if rules[i].ID != id || rules[i].Route != "direct" || !rules[i].Enabled || len(rules[i].Processes) == 0 {
			t.Fatal("requested game does not default to direct")
		}
	}
	ai, google := -1, -1
	for i, r := range rules {
		if r.ID == "ai-gemini" {
			ai = i
		}
		if r.ID == "work-google" {
			google = i
		}
		if r.ID == "downloads-torrent" && r.Enabled {
			t.Fatal("torrent bypass should require an explicit opt-in")
		}
	}
	if ai < 0 || ai >= google {
		t.Fatal("broad Google domain shadows Gemini")
	}
}

func TestRuleValidationNormalizationAndEscapedProcessMatching(t *testing.T) {
	r := DefaultRules()[0]
	r.Domains = []string{"*.EXAMPLE.COM.", "example.com", "пример.рф"}
	r.IPCIDRs = []string{"203.0.113.7", "2001:db8::1", "203.0.113.9/24"}
	r.ProcessPaths = []string{`C:\Games\Dota (test)\dota2.exe`}
	rules, err := NormalizeRules([]Rule{r})
	if err != nil {
		t.Fatal(err)
	}
	r = rules[0]
	if len(r.Domains) != 2 || r.Domains[0] != "example.com" || r.IPCIDRs[0] != "203.0.113.7/32" || r.IPCIDRs[2] != "203.0.113.0/24" {
		t.Fatal("normalization failed")
	}
	pattern := regexp.MustCompile(r.ProcessPatterns()[0])
	if !pattern.MatchString(`C:\Games\DOTA2.EXE`) || pattern.MatchString(`C:\Games\undota2.exe`) || pattern.MatchString(`C:\Games\dota2.exe.bak`) {
		t.Fatal("unsafe or case-sensitive process matching")
	}
	if !regexp.MustCompile(r.ProcessPatterns()[1]).MatchString(`c:\games\Dota (test)\dota2.exe`) {
		t.Fatal("full path was not escaped correctly")
	}
}

func TestRulesRejectInvalidInputsWithoutLeakingConditions(t *testing.T) {
	for _, mutate := range []func(*Rule){
		func(r *Rule) { r.Domains = []string{"https://user:SENSITIVE@example.com/path"} },
		func(r *Rule) { r.Processes = []string{`C:\SENSITIVE\game.exe`} },
		func(r *Rule) { r.ProcessPaths = []string{`C:\SENSITIVE\..\game.exe`} },
		func(r *Rule) { r.IPCIDRs = []string{"SENSITIVE"} },
		func(r *Rule) { r.Route = "SENSITIVE" },
		func(r *Rule) { r.Route = "dpi"; r.Network = "udp" },
		func(r *Rule) { r.ID = "SENSITIVE\n" },
	} {
		r := DefaultRules()[0]
		mutate(&r)
		_, err := NormalizeRules([]Rule{r})
		if err == nil || strings.Contains(err.Error(), "SENSITIVE") {
			t.Fatal("invalid rule accepted or condition leaked", err)
		}
	}
}

func TestStrictVPNNeverFallsBackToDirectOrDPINorManualDefault(t *testing.T) {
	p := New()
	now := time.Now()
	p.Manual["default"] = "direct"
	p.Record("default", Sample{Route: "direct", Reachable: true, UDP: true, CheckedAt: now})
	p.Record("default", Sample{Route: "dpi", Reachable: true, CheckedAt: now})
	if d := p.ChooseTunnel([]string{"direct", "dpi"}, false, now); d.Route != "block" || d.Group != "vpn" {
		t.Fatal("strict VPN leaked to direct")
	}
	p.Record("default", Sample{Route: "warp", Reachable: true, UDP: true, WarpVerified: true, CheckedAt: now})
	if d := p.ChooseTunnel([]string{"direct", "warp"}, true, now); d.Route != "warp" || d.Group != "vpn-udp" {
		t.Fatal("verified UDP reserve not selected")
	}
	p.Record("default", Sample{Route: "vless-test", Reachable: true, UDP: true, CheckedAt: now})
	if d := p.ChooseTunnel([]string{"direct", "warp", "vless-test"}, true, now); d.Route != "vless-test" {
		t.Fatal("VLESS priority lost")
	}
}

func TestStrictAISelectionUsesAIServiceProbes(t *testing.T) {
	p := New()
	now := time.Now()
	p.Record("default", Sample{Route: "vless-general", Reachable: true, CheckedAt: now})
	p.Record("ai", Sample{Route: "vless-ai", Reachable: true, CheckedAt: now})
	d := p.ChooseTunnelFor("ai", []string{"vless-general", "vless-ai"}, false, now)
	if d.Route != "vless-ai" || d.Group != "vpn-ai" {
		t.Fatal("AI-specific reachability ignored")
	}
}
