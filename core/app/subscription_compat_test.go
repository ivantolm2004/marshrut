package app

import (
	"encoding/json"
	"marshrut/engine"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWindowsNegotiatesSubscriptionFormatAutomatically(t *testing.T) {
	m := testManager(t)
	m.platform = "windows"
	var agents []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent := r.Header.Get("User-Agent")
		agents = append(agents, agent)
		if strings.HasPrefix(agent, "v2rayN/") {
			w.Write([]byte(uri))
		} else {
			w.Write([]byte("<html>Open in a supported application</html>"))
		}
	}))
	defer server.Close()
	m.subscriptionTransport = server.Client().Transport
	response := m.Handle(Request{Action: "import", Text: server.URL})
	if !response.OK || len(m.settings.Servers) != 1 || len(agents) != 2 {
		t.Fatal("format negotiation failed", response.Error, agents)
	}
}

type occupiedPortRuntime struct {
	testRuntime
	attempts int
	configs  []string
}

func (r *occupiedPortRuntime) Start(config []byte) error {
	r.attempts++
	r.configs = append(r.configs, string(config))
	if r.attempts == 1 {
		return engine.ErrLocalPortBusy
	}
	return nil
}
func TestStartupAutomaticallyRecoversPortCollision(t *testing.T) {
	m := testManager(t)
	r := &occupiedPortRuntime{}
	m.runtime = r
	options := engine.Options{Platform: "windows", APISecret: "test", ProbePassword: "test"}
	plan, err := engine.Build(options)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.startPreparedPlan(&plan, &options); err != nil || r.attempts != 2 || r.configs[0] == r.configs[1] || options.APIPort == 0 {
		t.Fatal("port conflict was not recovered automatically", err)
	}
}

func TestWindowsFormatFailuresPreserveCacheAndHideCredentials(t *testing.T) {
	m := testManager(t)
	m.platform = "windows"
	if r := m.Handle(Request{Action: "import", Text: uri}); !r.OK {
		t.Fatal(r.Error)
	}
	before, _ := json.Marshal(m.settings)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("vless://SENSITIVE_SECRET@host.example:443?type=xhttp"))
	}))
	defer server.Close()
	m.subscriptionTransport = server.Client().Transport
	r := m.Handle(Request{Action: "import", Text: server.URL})
	after, _ := json.Marshal(m.settings)
	if r.OK || strings.Contains(r.Error, "SENSITIVE_SECRET") || string(before) != string(after) {
		t.Fatal("failure changed cache or leaked credentials")
	}
}
