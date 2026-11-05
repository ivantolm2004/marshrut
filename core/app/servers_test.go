package app

import (
	"context"
	"errors"
	"fmt"
	"marshrut/profile"
	"marshrut/route"
	"marshrut/secure"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func hundredServers(m *Manager) []string {
	all := []string{"direct", "warp", "dpi"}
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("fixture-%03d", i)
		m.settings.Servers = append(m.settings.Servers, profile.Server{ID: id, Name: id, Outbound: map[string]any{"server": "proxy.example", "server_port": 443}})
		all = append(all, "vless-"+id)
	}
	return all
}

func TestHundredServersUseBoundedBatchAndRotateFailures(t *testing.T) {
	m := testManager(t)
	all := hundredServers(m)
	m.serverPings["fixture-090"] = ServerPing{Reachable: true, LatencyMS: 15, CheckedAt: time.Now()}
	m.serverPings["fixture-080"] = ServerPing{Reachable: true, LatencyMS: 2, CheckedAt: time.Now()}
	m.settings.LastServer = "fixture-070"
	batch := m.probeCandidatesLocked(all)
	if len(batch) != 3+autoServerLimit || batch[3] != "vless-fixture-070" || batch[4] != "vless-fixture-080" {
		t.Fatalf("unexpected bounded priority batch %v", batch)
	}
	for _, candidate := range batch[3:] {
		for _, group := range route.Groups() {
			for i := 0; i < 3; i++ {
				m.policy.Record(group.ID, route.Sample{Route: candidate, Reachable: false, CheckedAt: time.Now()})
			}
		}
	}
	next := m.probeCandidatesLocked(all)
	for _, failed := range batch[3:] {
		for _, candidate := range next {
			if candidate == failed {
				t.Fatal("failed batch was not rotated")
			}
		}
	}
}

func TestManualServerDoesNotProbeOtherSubscriptionServersAndPinsRemain(t *testing.T) {
	m := testManager(t)
	all := hundredServers(m)
	if r := m.Handle(Request{Action: "select_server", ID: "fixture-099"}); !r.OK {
		t.Fatal(r.Error)
	}
	m.settings.Pins["ai"] = "vless-fixture-088"
	batch := m.probeCandidatesLocked(all)
	if len(batch) != 5 || batch[3] != "vless-fixture-099" || batch[4] != "vless-fixture-088" {
		t.Fatal(batch)
	}
	loaded, err := New("test", testRuntime{}, m.store)
	if err != nil || loaded.settings.SelectedServer != "fixture-099" {
		t.Fatal("selection not persisted", err)
	}
	if r := m.Handle(Request{Action: "select_server", ID: "does-not-exist"}); r.OK {
		t.Fatal("unknown server accepted")
	}
	if r := m.Handle(Request{Action: "select_server"}); !r.OK || m.settings.SelectedServer != "" {
		t.Fatal("automatic mode not restored")
	}
}

func TestAutomaticBatchesReachAllHundredEvenWhenEarlierFailuresAreStale(t *testing.T) {
	m := testManager(t)
	all := hundredServers(m)
	seen := map[string]bool{}
	for cycle := 0; cycle < 34; cycle++ {
		batch := m.probeCandidatesLocked(all)
		for _, candidate := range batch[3:] {
			seen[candidate] = true
			for _, group := range route.Groups() {
				for i := 0; i < 3; i++ {
					m.policy.Record(group.ID, route.Sample{Route: candidate, CheckedAt: time.Now().Add(-5 * time.Minute)})
				}
			}
		}
	}
	if len(seen) != 100 {
		t.Fatalf("only reached %d of 100 servers", len(seen))
	}
}

func TestHundredServerPingScanIsBoundedCancellableAndRedactsErrors(t *testing.T) {
	m := testManager(t)
	hundredServers(m)
	var active, peak atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	var resultsMu sync.Mutex
	results := map[string]ServerPing{}
	measureServerPings(ctx, m.settings.Servers, func(c context.Context, target string) (net.Conn, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		if calls.Add(1) == serverPingWorkers {
			cancel()
		}
		<-c.Done()
		return nil, errors.New("SENSITIVE_ADDRESS_AND_TOKEN")
	}, func(id string, value ServerPing) { resultsMu.Lock(); defer resultsMu.Unlock(); results[id] = value })
	if peak.Load() > serverPingWorkers || calls.Load() > serverPingWorkers {
		t.Fatal("worker limit or cancellation ignored")
	}
	measureServerPings(context.Background(), m.settings.Servers[:1], func(context.Context, string) (net.Conn, error) { return nil, errors.New("SENSITIVE_ADDRESS_AND_TOKEN") }, func(_ string, value ServerPing) {
		if strings.Contains(value.Error, "SENSITIVE") || value.Reachable {
			t.Fatal("raw dial error was exposed")
		}
	})
}

func TestPingWithoutVPNPersistsResultsAndDoesNotConnectRuntime(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		connection, e := listener.Accept()
		if e == nil {
			connection.Close()
		}
	}()
	store := secure.Store{Path: filepath.Join(t.TempDir(), "settings"), Codec: testCodec{}}
	m, err := New("test", testRuntime{}, store)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	address := listener.Addr().(*net.TCPAddr)
	m.settings.Servers = []profile.Server{{ID: "fixture", Outbound: map[string]any{"server": "127.0.0.1", "server_port": address.Port}}}
	if r := m.Handle(Request{Action: "ping_servers"}); !r.OK {
		t.Fatal(r.Error)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		m.mu.Lock()
		done := !m.pingRun.Progress.Running
		phase := m.phase
		m.mu.Unlock()
		if phase != "disconnected" {
			t.Fatal("ping started VPN")
		}
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ping did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	loaded, err := New("test", testRuntime{}, store)
	if err != nil || !loaded.serverPings["fixture"].Reachable {
		t.Fatal("ping cache not restored", err)
	}
}
