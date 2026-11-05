package route

import (
	"sort"
	"strings"
	"time"
)

type Sample struct {
	Route        string    `json:"route"`
	Reachable    bool      `json:"reachable"`
	UDP          bool      `json:"udp"`
	WarpVerified bool      `json:"warp_verified"`
	LatencyMS    int64     `json:"latency_ms"`
	CheckedAt    time.Time `json:"checked_at"`
	Failures     int       `json:"failures"`
	Successes    int       `json:"successes"`
	Attempts     int       `json:"attempts"`
	Error        string    `json:"error,omitempty"`
}
type Decision struct {
	Group     string    `json:"group"`
	Route     string    `json:"route"`
	Reason    string    `json:"reason"`
	ChangedAt time.Time `json:"changed_at"`
}
type Group struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Domains      []string `json:"domains"`
	Probe        string   `json:"probe"`
	PreferTunnel bool     `json:"prefer_tunnel"`
}

func Groups() []Group {
	return []Group{
		{"youtube", "YouTube", []string{"youtube.com", "youtu.be", "googlevideo.com", "ytimg.com", "youtubei.googleapis.com"}, "www.youtube.com:443", false},
		{"discord", "Discord", []string{"discord.com", "discord.gg", "discordapp.com", "discordapp.net", "discord.media"}, "discord.com:443", false},
		{"telegram", "Telegram", []string{"telegram.org", "t.me", "telegram.me", "telesco.pe"}, "telegram.org:443", false},
		{"ai", "AI-сервисы", []string{"openai.com", "chatgpt.com", "oaistatic.com", "oaiusercontent.com", "anthropic.com", "claude.ai", "gemini.google.com", "generativelanguage.googleapis.com"}, "chatgpt.com:443", true},
		{"default", "Остальной интернет", nil, "www.cloudflare.com:443", true},
	}
}

type Policy struct {
	Current        map[string]Decision
	Samples        map[string]map[string]Sample
	Manual         map[string]string
	DirectFallback bool
	Cooldown       time.Duration
}

func New() *Policy {
	return &Policy{Current: map[string]Decision{}, Samples: map[string]map[string]Sample{}, Manual: map[string]string{}, DirectFallback: true, Cooldown: 60 * time.Second}
}
func (p *Policy) Record(group string, s Sample) {
	if p.Samples[group] == nil {
		p.Samples[group] = map[string]Sample{}
	}
	old := p.Samples[group][s.Route]
	s.Attempts = old.Attempts + 1
	s.Successes = old.Successes
	if s.Reachable {
		s.Successes++
		s.Failures = 0
	} else {
		s.Failures = old.Failures + 1
	}
	p.Samples[group][s.Route] = s
}
func (p *Policy) Choose(g Group, candidates []string, udp bool, now time.Time) Decision {
	key := g.ID
	if udp {
		key += "-udp"
	}
	if pinned := p.Manual[g.ID]; pinned != "" && !(udp && pinned == "dpi") {
		for _, r := range candidates {
			if r == pinned && (!udp || r != "dpi") {
				return Decision{key, r, "Закреплено вручную", now}
			}
		}
		return Decision{key, "block", "Закреплённый маршрут недоступен", now}
	}
	old, hasOld := p.Current[key]
	available := map[string]bool{}
	for _, r := range candidates {
		available[r] = true
	}
	var working []Sample
	for _, r := range candidates {
		if udp && r == "dpi" {
			continue
		}
		if udp && g.ID == "youtube" && p.Current[g.ID].Route == "dpi" && r == "direct" {
			continue
		}
		if !p.DirectFallback && g.PreferTunnel && (r == "direct" || r == "dpi") {
			continue
		}
		s := p.Samples[g.ID][r]
		if !s.Reachable || s.CheckedAt.IsZero() || now.Sub(s.CheckedAt) > 2*time.Minute || (udp && !s.UDP) || (r == "warp" && !s.WarpVerified) {
			continue
		}
		working = append(working, s)
	}
	hasVLESS := false
	bestVLESSLatency := int64(1 << 62)
	for _, s := range working {
		if strings.HasPrefix(s.Route, "vless-") {
			hasVLESS = true
			if s.LatencyMS < bestVLESSLatency {
				bestVLESSLatency = s.LatencyMS
			}
		}
	}
	filtered := working[:0]
	for _, s := range working {
		if hasVLESS && s.Route == "warp" {
			continue
		} // WARP is a reserve, never a faster primary.
		if hasVLESS && s.Route == "dpi" && s.LatencyMS >= bestVLESSLatency {
			continue
		}
		filtered = append(filtered, s)
	}
	working = filtered
	sort.SliceStable(working, func(i, j int) bool {
		a, b := working[i], working[j]
		if g.ID == "ai" {
			av, bv := strings.HasPrefix(a.Route, "vless-"), strings.HasPrefix(b.Route, "vless-")
			if av != bv {
				return av
			}
		}
		if g.PreferTunnel || udp {
			at, bt := a.Route != "direct" && a.Route != "dpi", b.Route != "direct" && b.Route != "dpi"
			if at != bt {
				return at
			}
		}
		ar, br := float64(a.Successes)/float64(max(1, a.Attempts)), float64(b.Successes)/float64(max(1, b.Attempts))
		if ar != br {
			return ar > br
		}
		return a.LatencyMS < b.LatencyMS
	})
	if hasOld && available[old.Route] {
		s := p.Samples[g.ID][old.Route]
		if now.Sub(old.ChangedAt) < p.Cooldown {
			return old
		}
		if s.Failures < 3 && (!udp || s.UDP) && !s.CheckedAt.IsZero() && now.Sub(s.CheckedAt) < 2*time.Minute {
			promote := old.Route == "warp" && hasVLESS
			if len(working) > 0 && working[0].Route != old.Route {
				best := working[0]
				if !udp && !g.PreferTunnel && (old.Route == "dpi" || best.Route == "dpi") && best.LatencyMS <= s.LatencyMS {
					promote = true
				}
				if g.PreferTunnel && (old.Route == "direct" || old.Route == "dpi") && strings.HasPrefix(best.Route, "vless-") {
					promote = true
				}
			}
			if !promote {
				return old
			}
		}
	}
	if len(working) == 0 {
		return Decision{key, "block", "Нет проверенного рабочего маршрута", now}
	}
	reason := "Проверка доступности и задержки"
	if working[0].Route == "warp" {
		reason = "VLESS недоступен: резерв WARP"
	}
	if working[0].Route == "dpi" && hasVLESS {
		reason = "DPI быстрее проверенного VLESS"
	}
	if g.PreferTunnel && (working[0].Route == "direct" || working[0].Route == "dpi") {
		reason = "Туннели недоступны: прямой резерв"
	}
	return Decision{key, working[0].Route, reason, now}
}

// Commit is called only after the engine acknowledges switching the selector.
func (p *Policy) Commit(d Decision) { p.Current[d.Group] = d }
