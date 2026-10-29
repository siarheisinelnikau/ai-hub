package metrics

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// llamaFixture serves a minimal llama.cpp /metrics (tokens 500,
// pred_seconds 10.0, running 1) and /slots `[]`.
func llamaFixture() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			fmt.Fprint(w, "llamacpp:tokens_predicted_total 500\n"+
				"llamacpp:tokens_predicted_seconds_total 10\n"+
				"llamacpp:requests_processing 1\n")
		case "/slots":
			fmt.Fprint(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestStatsPollerGetEmpty(t *testing.T) {
	p := NewStatsPoller(nil)
	got := p.Get("nope")
	if got == nil {
		t.Fatal("Get = nil, want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("len(Get) = %d, want 0", len(got))
	}
}

func TestStatsPollerTickAppends(t *testing.T) {
	ts := llamaFixture()
	defer ts.Close()
	port := tsPort(ts)

	p := NewStatsPoller(func() []ModelRef {
		return []ModelRef{{ID: "m1", Port: port, Backend: "llamacpp"}}
	})
	p.tick()

	got := p.Get("m1")
	if len(got) != 1 {
		t.Fatalf("len(Get) = %d, want 1", len(got))
	}
	entry, ok := got[0].(map[string]interface{})
	if !ok {
		t.Fatalf("entry type = %T, want map[string]interface{}", got[0])
	}
	tsv, ok := entry["ts"].(float64)
	if !ok {
		t.Fatalf("ts type = %T, want float64", entry["ts"])
	}
	if tsv <= 0 {
		t.Errorf("ts = %v, want > 0", tsv)
	}
	if entry["backend"] != "llamacpp" {
		t.Errorf("backend = %v, want llamacpp", entry["backend"])
	}
}

func TestStatsPollerRingBounded(t *testing.T) {
	ts := llamaFixture()
	defer ts.Close()
	port := tsPort(ts)

	p := NewStatsPoller(func() []ModelRef {
		return []ModelRef{{ID: "m1", Port: port, Backend: "llamacpp"}}
	})
	for i := 0; i < StatsMax+5; i++ {
		p.tick()
	}

	if got := p.Get("m1"); len(got) != StatsMax {
		t.Errorf("len(Get) = %d, want StatsMax (%d)", len(got), StatsMax)
	}
}

func TestStatsPollerSkipsZeroPort(t *testing.T) {
	p := NewStatsPoller(func() []ModelRef {
		return []ModelRef{{ID: "m2", Port: 0, Backend: "llamacpp"}}
	})
	p.tick()

	if got := p.Get("m2"); len(got) != 0 {
		t.Errorf("len(Get) = %d, want 0", len(got))
	}
}

func TestStatsPollerLiveTPSEmpty(t *testing.T) {
	p := NewStatsPoller(nil)
	if v, ok := p.LiveTPS("m1"); ok || v != 0 {
		t.Fatalf("LiveTPS(no data) = %v, %v; want 0, false", v, ok)
	}
}

func TestStatsPollerLiveTPSPrimed(t *testing.T) {
	ts := llamaFixture()
	defer ts.Close()
	port := tsPort(ts)

	p := NewStatsPoller(func() []ModelRef {
		return []ModelRef{{ID: "m1", Port: port, Backend: "llamacpp"}}
	})
	p.tick()

	// Fixture: 500 tokens / 10 pred_seconds, /slots empty, no gauge,
	// first tick -> cumulative 50 tok/s is the live value (deterministic).
	if v, ok := p.LiveTPS("m1"); !ok || v != 50 {
		t.Fatalf("LiveTPS(primed) = %v, %v; want 50, true", v, ok)
	}
	// unknown id stays absent
	if v, ok := p.LiveTPS("nope"); ok || v != 0 {
		t.Fatalf("LiveTPS(nope) = %v, %v; want 0, false", v, ok)
	}
}

func TestStatsPollerLiveTPSZero(t *testing.T) {
	// Zero-counter fixture: no slots / gauge / delta / cumulative, so the
	// entry's tps is 0 — stored in the ring, but not a usable live value.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			fmt.Fprint(w, "llamacpp:tokens_predicted_total 0\n"+
				"llamacpp:tokens_predicted_seconds_total 0\n"+
				"llamacpp:requests_processing 0\n")
		case "/slots":
			fmt.Fprint(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	p := NewStatsPoller(func() []ModelRef {
		return []ModelRef{{ID: "m0", Port: tsPort(ts), Backend: "llamacpp"}}
	})
	p.tick()

	if got := p.Get("m0"); len(got) != 1 {
		t.Fatalf("len(Get) = %d, want 1 (zero-tps entry must still be stored)", len(got))
	}
	if v, ok := p.LiveTPS("m0"); ok || v != 0 {
		t.Fatalf("LiveTPS(zero tps) = %v, %v; want 0, false (zero is not live)", v, ok)
	}
}

func TestStatsPollerSkipsUnreachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	p := NewStatsPoller(func() []ModelRef {
		return []ModelRef{{ID: "m3", Port: port, Backend: "llamacpp"}}
	})
	p.tick()

	if got := p.Get("m3"); len(got) != 0 {
		t.Errorf("len(Get) = %d, want 0", len(got))
	}
}
