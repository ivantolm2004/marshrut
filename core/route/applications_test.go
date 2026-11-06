package route

import "testing"

func TestApplicationIdentityAndMatching(t *testing.T) {
	a := NewApplication(`C:\Apps\Chrome\chrome.exe`, false)
	if a.Name != "Google Chrome" || a.Executable != "chrome.exe" || a.ID != ApplicationID(`c:/apps/chrome/CHROME.EXE`) {
		t.Fatal("application identity is unstable")
	}
	rules := []Rule{{ID: "disabled", Processes: []string{"CHROME.EXE"}}, {ID: "specific", Enabled: true, ProcessPaths: []string{`c:\apps\chrome\CHROME.exe`}}}
	if ApplicationRuleID(a, rules) != "specific" {
		t.Fatal("active matching rule not preferred")
	}
	if (Rule{ProcessPaths: []string{`C:\Other\chrome.exe`}}).MatchesApplication(a) {
		t.Fatal("same-named apps were merged")
	}
	if (Rule{Domains: []string{"google.com"}}).MatchesApplication(a) {
		t.Fatal("domain rule incorrectly treated as an app rule")
	}
	if ApplicationRuleID(a, []Rule{{ID: "off-specific", ProcessPaths: []string{a.Path}}, {ID: "general", Enabled: true, Processes: []string{a.Executable}}}) != "off-specific" {
		t.Fatal("disabled dedicated rule became inaccessible")
	}
}
