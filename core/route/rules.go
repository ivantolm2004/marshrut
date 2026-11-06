package route

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/idna"
)

const MaxRules = 256

// Matchers within one rule are alternatives. Enabled rules are evaluated in
// list order; private destinations and DNS interception remain system rules.
type Rule struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Category     string   `json:"category"`
	Enabled      bool     `json:"enabled"`
	Route        string   `json:"route"`
	AutoGroup    string   `json:"auto_group"`
	Network      string   `json:"network"`
	Domains      []string `json:"domains"`
	Processes    []string `json:"processes"`
	ProcessPaths []string `json:"process_paths"`
	IPCIDRs      []string `json:"ip_cidr"`
}

var ruleID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,80}$`)
var windowsPath = regexp.MustCompile(`^[a-zA-Z]:\\`)
var tunnelRoute = regexp.MustCompile(`^vless-[a-zA-Z0-9_-]{1,100}$`)

func NormalizeRules(rules []Rule) ([]Rule, error) {
	if len(rules) > MaxRules {
		return nil, errors.New("Не больше 256 правил")
	}
	result := make([]Rule, 0, len(rules))
	ids := map[string]bool{}
	for i, r := range rules {
		fail := func(message string) ([]Rule, error) { return nil, fmt.Errorf("Правило %d: %s", i+1, message) }
		if !ruleID.MatchString(r.ID) || ids[r.ID] {
			return fail("некорректный или повторяющийся идентификатор")
		}
		ids[r.ID] = true
		r.Name = strings.TrimSpace(r.Name)
		if r.Name == "" || len([]rune(r.Name)) > 100 || strings.IndexFunc(r.Name, unicode.IsControl) >= 0 {
			return fail("укажите название до 100 символов")
		}
		switch r.Category {
		case "apps", "games", "launchers", "video", "social", "ai", "work", "downloads", "other":
		default:
			r.Category = "other"
		}
		if r.Route != "auto" && r.Route != "vpn" && r.Route != "direct" && r.Route != "warp" && r.Route != "dpi" && r.Route != "block" && !tunnelRoute.MatchString(r.Route) {
			return fail("неизвестный маршрут")
		}
		if r.AutoGroup == "" {
			r.AutoGroup = "default"
		}
		validGroup := false
		for _, g := range Groups() {
			validGroup = validGroup || g.ID == r.AutoGroup
		}
		if !validGroup {
			return fail("неизвестная группа автовыбора")
		}
		if r.Network != "" && r.Network != "tcp" && r.Network != "udp" {
			return fail("выберите TCP, UDP или оба протокола")
		}
		if r.Route == "dpi" && r.Network != "tcp" {
			return fail("DPI-обход работает только с TCP; выберите протокол TCP")
		}
		if len(r.Domains)+len(r.Processes)+len(r.ProcessPaths)+len(r.IPCIDRs) > 256 {
			return fail("не больше 256 условий на правило")
		}
		domains := make([]string, 0, len(r.Domains))
		for _, raw := range r.Domains {
			if strings.Contains(raw, "://") {
				u, err := url.Parse(strings.TrimSpace(raw))
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Hostname() == "" {
					return fail("нужна ссылка http/https без логина и пароля")
				}
				raw = u.Hostname()
			}
			domain := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(raw), "*."), "."), ".")
			domain, err := idna.Lookup.ToASCII(strings.ToLower(domain))
			if err != nil || len(domain) > 253 || domain == "" || strings.ContainsAny(domain, "/:@?#*\\ \t\r\n") {
				return fail("нужны домены без https://, путей и портов")
			}
			if _, err := netip.ParseAddr(domain); err == nil {
				return fail("IP-адрес добавляется в поле IP/подсети")
			}
			for _, label := range strings.Split(domain, ".") {
				if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
					return fail("некорректный домен")
				}
				for _, c := range label {
					if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
						return fail("некорректный домен")
					}
				}
			}
			domains = append(domains, domain)
		}
		r.Domains = unique(domains)
		processes := make([]string, 0, len(r.Processes))
		for _, raw := range r.Processes {
			name := strings.TrimSpace(raw)
			if len(name) > 150 || !strings.HasSuffix(strings.ToLower(name), ".exe") || strings.ContainsAny(name, `\/:*?"<>|`) || strings.IndexFunc(name, unicode.IsControl) >= 0 {
				return fail("в процессах нужны имена вида dota2.exe без пути")
			}
			processes = append(processes, name)
		}
		r.Processes = unique(processes)
		paths := make([]string, 0, len(r.ProcessPaths))
		for _, raw := range r.ProcessPaths {
			path := strings.ReplaceAll(strings.TrimSpace(raw), "/", `\`)
			if len(path) > 1024 || !windowsPath.MatchString(path) || !strings.HasSuffix(strings.ToLower(path), ".exe") || strings.ContainsAny(path[3:], `:*?"<>|`) || strings.IndexFunc(path, unicode.IsControl) >= 0 {
				return fail("в путях нужен полный путь вида C:\\Games\\game.exe")
			}
			for _, part := range strings.Split(path[3:], `\`) {
				if part == ".." || part == "." || part == "" {
					return fail("некорректный путь программы")
				}
			}
			paths = append(paths, path)
		}
		r.ProcessPaths = unique(paths)
		prefixes := make([]string, 0, len(r.IPCIDRs))
		for _, raw := range r.IPCIDRs {
			value := strings.TrimSpace(raw)
			if ip, err := netip.ParseAddr(value); err == nil {
				if ip.Is4() {
					value += "/32"
				} else {
					value += "/128"
				}
			}
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				return fail("некорректный IP-адрес или подсеть CIDR")
			}
			prefixes = append(prefixes, prefix.Masked().String())
		}
		r.IPCIDRs = unique(prefixes)
		if len(r.Domains)+len(r.Processes)+len(r.ProcessPaths)+len(r.IPCIDRs) == 0 {
			return fail("добавьте хотя бы домен, процесс, путь или IP/подсеть")
		}
		result = append(result, r)
	}
	return result, nil
}

func unique(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, v := range values {
		key := strings.ToLower(v)
		if !seen[key] {
			seen[key] = true
			result = append(result, v)
		}
	}
	return result
}

// The Windows kernel may report differently cased process names. Match escaped
// complete paths case-insensitively; user-supplied regular expressions never run.
func (r Rule) ProcessPatterns() []string {
	var patterns []string
	if len(r.Processes) > 0 {
		var names []string
		for _, name := range r.Processes {
			names = append(names, regexp.QuoteMeta(name))
		}
		patterns = append(patterns, `(?i)(?:^|[\\/])(?:`+strings.Join(names, "|")+`)$`)
	}
	for _, path := range r.ProcessPaths {
		patterns = append(patterns, `(?i)^`+regexp.QuoteMeta(path)+`$`)
	}
	return patterns
}

func (p *Policy) ChooseTunnel(candidates []string, udp bool, now time.Time) Decision {
	return p.ChooseTunnelFor("default", candidates, udp, now)
}

func (p *Policy) ChooseTunnelFor(group string, candidates []string, udp bool, now time.Time) Decision {
	var tunnels []string
	for _, tag := range candidates {
		if strings.HasPrefix(tag, "vless-") || tag == "warp" {
			tunnels = append(tunnels, tag)
		}
	}
	copy := *p
	copy.DirectFallback = false
	copy.Manual = nil
	key, tag := group, "vpn"
	if group != "default" {
		tag += "-" + group
	}
	if udp {
		key += "-udp"
		tag += "-udp"
	}
	copy.Current = map[string]Decision{key: p.Current[tag]}
	d := copy.Choose(Group{ID: group, PreferTunnel: true}, tunnels, udp, now)
	d.Group = tag
	return d
}
