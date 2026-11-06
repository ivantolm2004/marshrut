package route

import (
	N "github.com/sagernet/sing/common/network"
	"net/netip"
	"strings"
)

type MatchTarget struct {
	Label       string       `json:"label"`
	Domain      string       `json:"domain,omitempty"`
	IP          string       `json:"ip,omitempty"`
	Application *Application `json:"application,omitempty"`
}
type Explanation struct {
	Network  string `json:"network"`
	RuleID   string `json:"rule_id"`
	RuleName string `json:"rule_name"`
	Route    string `json:"route"`
	Group    string `json:"group"`
	Reason   string `json:"reason"`
	Shadowed int    `json:"shadowed"`
}

func Explain(rules []Rule, target MatchTarget, network string) Explanation {
	group := "default"
	if network == "udp" {
		group += "-udp"
	}
	result := Explanation{Network: network, RuleName: "Общий маршрут", Route: "auto", Group: group, Reason: "Подходящего включённого правила нет"}
	ip, _ := netip.ParseAddr(target.IP)
	if ip.IsValid() && !N.IsPublicAddr(ip) {
		result.RuleID = "system-local"
		result.RuleName = "Локальная сеть"
		result.Route = "direct"
		result.Reason = "Системное правило локальной сети выше пользовательских"
		return result
	}
	matched := false
	for _, r := range rules {
		if !r.Enabled || (r.Network != "" && r.Network != network) {
			continue
		}
		reason := ""
		if target.Application != nil && r.MatchesApplication(*target.Application) {
			reason = "Совпало имя или полный путь приложения"
		}
		if target.Domain != "" {
			for _, d := range r.Domains {
				if strings.EqualFold(target.Domain, d) || strings.HasSuffix(strings.ToLower(target.Domain), "."+strings.ToLower(d)) {
					reason = "Совпал домен или его поддомен"
					break
				}
			}
		}
		if ip.IsValid() {
			for _, raw := range r.IPCIDRs {
				p, err := netip.ParsePrefix(raw)
				if err == nil && p.Contains(ip) {
					reason = "IP входит в указанную подсеть"
					break
				}
			}
		}
		if reason == "" {
			continue
		}
		if matched {
			result.Shadowed++
			continue
		}
		matched = true
		result.RuleID = r.ID
		result.RuleName = r.Name
		result.Route = r.Route
		result.Group = r.AutoGroup
		result.Reason = reason
		if network == "udp" {
			result.Group += "-udp"
		}
	}
	return result
}
