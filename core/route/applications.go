package route

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

type Application struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Executable   string    `json:"executable"`
	Path         string    `json:"path"`
	System       bool      `json:"system"`
	Running      bool      `json:"running"`
	ProcessCount int       `json:"process_count"`
	LastSeen     time.Time `json:"last_seen"`
	RuleID       string    `json:"rule_id,omitempty"`
}

func ApplicationID(path string) string {
	hash := sha256.Sum256([]byte(strings.ToLower(strings.ReplaceAll(path, "/", `\`))))
	return hex.EncodeToString(hash[:8])
}

func NewApplication(path string, system bool) Application {
	normalized := strings.ReplaceAll(path, `\`, "/")
	executable := normalized[strings.LastIndex(normalized, "/")+1:]
	name := executable
	if strings.HasSuffix(strings.ToLower(name), ".exe") {
		name = name[:len(name)-4]
	}
	if friendly, ok := map[string]string{"chrome.exe": "Google Chrome", "msedge.exe": "Microsoft Edge", "firefox.exe": "Mozilla Firefox", "code.exe": "Visual Studio Code", "explorer.exe": "Проводник Windows", "msedgewebview2.exe": "Microsoft Edge WebView2", "steam.exe": "Steam", "discord.exe": "Discord", "telegram.exe": "Telegram"}[strings.ToLower(executable)]; ok {
		name = friendly
	}
	return Application{ID: ApplicationID(path), Name: name, Executable: executable, Path: path, System: system, Running: true, ProcessCount: 1}
}

func (r Rule) MatchesApplication(app Application) bool {
	for _, name := range r.Processes {
		if strings.EqualFold(name, app.Executable) {
			return true
		}
	}
	for _, path := range r.ProcessPaths {
		if strings.EqualFold(strings.ReplaceAll(path, "/", `\`), strings.ReplaceAll(app.Path, "/", `\`)) {
			return true
		}
	}
	return false
}

func ApplicationRuleID(app Application, rules []Rule) string {
	for _, r := range rules {
		for _, path := range r.ProcessPaths {
			if strings.EqualFold(strings.ReplaceAll(path, "/", `\`), strings.ReplaceAll(app.Path, "/", `\`)) {
				return r.ID
			}
		}
	}
	firstDisabled := ""
	for _, r := range rules {
		if !r.MatchesApplication(app) {
			continue
		}
		if r.Enabled {
			return r.ID
		}
		if firstDisabled == "" {
			firstDisabled = r.ID
		}
	}
	return firstDisabled
}
