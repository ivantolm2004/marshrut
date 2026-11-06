package route

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"net/url"
	"strings"
)

// Definition retains matchers when its routing override is removed. Catalogue
// membership alone never creates an active routing rule.
type CatalogueEntry struct {
	ID         string `json:"id"`
	Origin     string `json:"origin"`
	Kind       string `json:"kind"`
	Definition Rule   `json:"definition"`
}

func NormalizeCatalogueEntries(entries []CatalogueEntry) ([]CatalogueEntry, error) {
	if len(entries) > 2048 {
		return nil, errors.New("В каталоге не больше 2048 записей")
	}
	normalized := make([]CatalogueEntry, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if !ruleID.MatchString(entry.ID) || seen[entry.ID] {
			return nil, errors.New("Некорректный или повторяющийся идентификатор записи")
		}
		switch entry.Origin {
		case "builtin", "custom", "discovered":
		default:
			return nil, errors.New("Неизвестный источник записи")
		}
		definition, err := NormalizeRules([]Rule{entry.Definition})
		if err != nil {
			return nil, errors.New("Повреждены условия записи каталога")
		}
		seen[entry.ID] = true
		canonical := EntryFromRule(definition[0], entry.Origin)
		canonical.ID = entry.ID
		normalized = append(normalized, canonical)
	}
	return normalized, nil
}

func EntryFromRule(r Rule, origin string) CatalogueEntry {
	kind := "service"
	if len(r.Processes)+len(r.ProcessPaths) == 0 {
		if len(r.Domains) == 0 {
			kind = "network"
		} else if len(r.IPCIDRs) == 0 {
			kind = "site"
		}
	} else if len(r.Domains)+len(r.IPCIDRs) == 0 {
		kind = "application"
	}
	return CatalogueEntry{ID: r.ID, Origin: origin, Kind: kind, Definition: r}
}

func SeedCatalogue(rules []Rule) []CatalogueEntry {
	entries := []CatalogueEntry{}
	seen := map[string]bool{}
	for _, r := range DefaultRules() {
		entries = append(entries, EntryFromRule(r, "builtin"))
		seen[r.ID] = true
	}
	for _, r := range rules {
		if seen[r.ID] {
			for i := range entries {
				if entries[i].ID == r.ID {
					entries[i] = EntryFromRule(r, "builtin")
				}
			}
		} else {
			entries = append(entries, EntryFromRule(r, "custom"))
			seen[r.ID] = true
		}
	}
	return entries
}

// ParseDestination accepts ordinary HTTP(S) links, IDN hosts, IPs and CIDRs.
// URL paths do not imply path-specific routing: the returned matcher is a host.
func ParseDestination(input string) (domain, prefix string, err error) {
	value := strings.TrimSpace(input)
	if strings.Contains(value, "://") {
		u, e := url.Parse(value)
		if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Hostname() == "" {
			return "", "", errors.New("Нужна ссылка http/https, домен, IP или подсеть")
		}
		value = u.Hostname()
	}
	if ip, e := netip.ParseAddr(value); e == nil {
		if ip.Zone() != "" {
			return "", "", errors.New("IP с зоной интерфейса не поддерживается")
		}
		return "", netip.PrefixFrom(ip, ip.BitLen()).String(), nil
	}
	if p, e := netip.ParsePrefix(value); e == nil {
		return "", p.Masked().String(), nil
	}
	rules, e := NormalizeRules([]Rule{{ID: "input", Name: "Сайт", Category: "other", Route: "auto", Domains: []string{value}}})
	if e != nil {
		return "", "", errors.New("Укажите домен, ссылку http/https, IP или подсеть CIDR")
	}
	return rules[0].Domains[0], "", nil
}

// Fingerprint excludes labels, disabled rules and IDs: these do not change a
// compiled route. Rule order and every active routing field remain significant.
func RulesFingerprint(rules []Rule) string {
	if normalized, err := NormalizeRules(rules); err == nil {
		rules = normalized
	}
	active := []Rule{}
	for _, r := range rules {
		if r.Enabled {
			r.ID = ""
			r.Name = ""
			r.Category = ""
			active = append(active, r)
		}
	}
	data, _ := json.Marshal(active)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
