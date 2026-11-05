package route

import (
	"testing"
	"time"
)

func sample(r string, ok bool, ms int64, now time.Time) Sample {
	return Sample{Route: r, Reachable: ok, UDP: ok, WarpVerified: r == "warp" && ok, LatencyMS: ms, CheckedAt: now}
}
func TestThreeFailuresAndCooldown(t *testing.T) {
	p := New()
	g := Groups()[4]
	now := time.Now()
	c := []string{"vless-a", "warp", "direct"}
	p.Record(g.ID, sample(c[0], true, 20, now))
	p.Record(g.ID, sample("warp", true, 40, now))
	d := p.Choose(g, c, false, now)
	p.Commit(d)
	if d.Route != c[0] {
		t.Fatal(d)
	}
	for n := 1; n <= 3; n++ {
		p.Record(g.ID, sample(c[0], false, 0, now.Add(time.Duration(n)*30*time.Second)))
		got := p.Choose(g, c, false, now.Add(time.Duration(n)*30*time.Second))
		if n < 3 && got.Route != c[0] {
			t.Fatal("switched too early")
		}
		if n == 3 && got.Route != "warp" {
			t.Fatal("did not fail over", got)
		}
	}
}
func TestDirectFallbackAndUDPCapability(t *testing.T) {
	p := New()
	now := time.Now()
	g := Groups()[3]
	p.Record(g.ID, sample("direct", true, 5, now))
	p.Record(g.ID, sample("dpi", true, 3, now))
	d := p.Choose(g, []string{"direct", "dpi"}, false, now)
	if d.Route != "dpi" {
		t.Fatal(d)
	}
	d = p.Choose(g, []string{"direct", "dpi"}, true, now)
	if d.Route != "direct" {
		t.Fatal("TCP-only DPI used for UDP")
	}
	p.DirectFallback = false
	d = p.Choose(g, []string{"direct", "dpi"}, false, now)
	if d.Route != "block" {
		t.Fatal("direct fallback should be disabled")
	}
}
func TestUnverifiedWarpAndStaleSampleRejected(t *testing.T) {
	p := New()
	now := time.Now()
	g := Groups()[0]
	s := sample("warp", true, 1, now)
	s.WarpVerified = false
	p.Record(g.ID, s)
	p.Record(g.ID, sample("direct", true, 2, now.Add(-3*time.Minute)))
	if d := p.Choose(g, []string{"warp", "direct"}, false, now); d.Route != "block" {
		t.Fatal(d)
	}
}
func TestManualRouteDoesNotSilentlyFallback(t *testing.T) {
	p := New()
	p.Manual["ai"] = "vless-missing"
	if d := p.Choose(Groups()[3], []string{"direct"}, false, time.Now()); d.Route != "block" {
		t.Fatal(d)
	}
}
func TestAIUsesVLESSAndDPIAllowsSeparateUDPPath(t *testing.T) {
	p := New()
	now := time.Now()
	ai := Groups()[3]
	p.Record(ai.ID, sample("vless-a", true, 100, now))
	p.Record(ai.ID, sample("warp", true, 10, now))
	if d := p.Choose(ai, []string{"vless-a", "warp"}, false, now); d.Route != "vless-a" {
		t.Fatal("AI should prefer VLESS", d)
	}
	g := Groups()[1]
	p.Manual[g.ID] = "dpi"
	p.Record(g.ID, sample("vless-a", true, 100, now))
	p.Record(g.ID, sample("direct", true, 1, now))
	if d := p.Choose(g, []string{"dpi", "vless-a", "direct"}, true, now); d.Route != "vless-a" {
		t.Fatal("DPI pin must not block independent UDP tunnel", d)
	}
}

func TestWarpIsReserveAndDPIOnlyWinsWhenFaster(t *testing.T) {
	p := New()
	g := Groups()[0]
	now := time.Now()
	c := []string{"vless-a", "dpi", "warp"}
	p.Record(g.ID, sample("vless-a", true, 100, now))
	p.Record(g.ID, sample("warp", true, 1, now))
	p.Record(g.ID, sample("dpi", true, 20, now))
	if d := p.Choose(g, c, false, now); d.Route != "dpi" {
		t.Fatal("faster DPI should win while WARP remains reserve", d)
	}
	p.Record(g.ID, sample("dpi", true, 120, now))
	if d := p.Choose(g, c, false, now); d.Route != "vless-a" {
		t.Fatal("slower DPI or faster WARP replaced working VLESS", d)
	}
	if d := p.Choose(g, c, true, now); d.Route != "vless-a" {
		t.Fatal("UDP did not prefer working VLESS", d)
	}
}

func TestReturnFromWarpAndCompareDPIAfterCooldown(t *testing.T) {
	p := New()
	g := Groups()[0]
	now := time.Now()
	c := []string{"vless-a", "warp", "dpi"}
	p.Record(g.ID, sample("warp", true, 1, now))
	p.Commit(p.Choose(g, c, false, now))
	p.Record(g.ID, sample("vless-a", true, 80, now.Add(30*time.Second)))
	if d := p.Choose(g, c, false, now.Add(30*time.Second)); d.Route != "warp" {
		t.Fatal("cooldown ignored")
	}
	d := p.Choose(g, c, false, now.Add(61*time.Second))
	if d.Route != "vless-a" {
		t.Fatal("healthy VLESS did not replace reserve", d)
	}
	p.Commit(d)
	p.Record(g.ID, sample("vless-a", true, 80, now.Add(122*time.Second)))
	p.Record(g.ID, sample("dpi", true, 20, now.Add(122*time.Second)))
	d = p.Choose(g, c, false, now.Add(122*time.Second))
	if d.Route != "dpi" {
		t.Fatal("faster DPI did not replace VLESS", d)
	}
	p.Commit(d)
	p.Record(g.ID, sample("vless-a", true, 10, now.Add(183*time.Second)))
	p.Record(g.ID, sample("dpi", true, 20, now.Add(183*time.Second)))
	if d = p.Choose(g, c, false, now.Add(183*time.Second)); d.Route != "vless-a" {
		t.Fatal("slower DPI was kept", d)
	}
}
