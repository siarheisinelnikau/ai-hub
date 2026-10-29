package metrics

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// fakeLlamaSlots serves /slots responses from a scripted list of slot maps.
// Each response is consumed in order (the last one repeats).
type fakeLlamaSlots struct {
	srv       *httptest.Server
	poll      int
	responses [][]map[string]interface{}
}

func newFakeLlamaSlots(t *testing.T, responses [][]map[string]interface{}) *fakeLlamaSlots {
	t.Helper()
	f := &fakeLlamaSlots{responses: responses}
	mux := http.NewServeMux()
	mux.HandleFunc("/slots", func(w http.ResponseWriter, r *http.Request) {
		i := f.poll
		if i >= len(f.responses) {
			i = len(f.responses) - 1
		}
		f.poll++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, slotsJSON(f.responses[i]))
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeLlamaSlots) port() int {
	// srv.URL is http://127.0.0.1:PORT
	s := strings.TrimPrefix(f.srv.URL, "http://")
	n, _ := strconv.Atoi(strings.SplitN(s, ":", 2)[1])
	return n
}

func slotsJSON(slots []map[string]interface{}) string {
	var b strings.Builder
	b.WriteString("[")
	for i, m := range slots {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"id":%v,"is_processing":%v,"n_prompt_tokens":%v,"n_decoded":%v,"next_token":[{"n_decoded":%v}]}`,
			m["id"], m["is_processing"], m["n_prompt_tokens"], m["n_decoded"], m["n_decoded"])
	}
	b.WriteString("]")
	return b.String()
}

func slot(id int, proc bool, prompt, decoded int64) map[string]interface{} {
	return map[string]interface{}{"id": id, "is_processing": proc, "n_prompt_tokens": prompt, "n_decoded": decoded}
}

func portData(p int) map[string]interface{} {
	d := baseData()
	d["port"] = p
	return d
}

// TestMultiSlotTPSActiveOnNonZeroSlot is the regression test for the
// gpt-oss-20b case: llama-server --parallel 4, request decoding on slot 2
// while the rest are idle. The old code read n_decoded from slot 0 only,
// so live TPS fell back to the uptime average (~37) instead of the real rate.
func TestMultiSlotTPSActiveOnNonZeroSlot(t *testing.T) {
	f := newFakeLlamaSlots(t, [][]map[string]interface{}{
		{slot(0, false, 0, 0), slot(1, false, 0, 0), slot(2, true, 8128, 30), slot(3, false, 0, 0)},
		{slot(0, false, 0, 0), slot(1, false, 0, 0), slot(2, true, 8168, 90), slot(3, false, 0, 0)},
	})
	s := NewScrapper()
	clock := 1000.0
	s.nowFn = func() float64 { return clock }

	s.ComputeLlamaCPPTPS("m", portData(f.port())) // first poll: snapshot only

	clock = 1001.0 // +1s, slot2 decoded 30 -> 90 => 60 tok/s
	out := s.ComputeLlamaCPPTPS("m", portData(f.port()))
	if got := fOf(t, out, "tps"); got != 60.0 {
		t.Errorf("tps = %v, want 60.0 (slot 2 delta 60 tokens / 1s)", got)
	}
	if got := fOf(t, out, "tps_instant"); got != 60.0 {
		t.Errorf("tps_instant = %v, want 60.0", got)
	}
	if got := i64Of(t, out, "ctx_used"); got != int64(8168) {
		t.Errorf("ctx_used = %v, want 8168 (processing slot prompt tokens)", got)
	}
}

// TestMultiSlotParallelTwoSlots: two slots decoding concurrently — deltas sum.
func TestMultiSlotParallelTwoSlots(t *testing.T) {
	f := newFakeLlamaSlots(t, [][]map[string]interface{}{
		{slot(0, true, 100, 40), slot(1, true, 200, 40)},
		{slot(0, true, 100, 70), slot(1, true, 200, 90)},
	})
	s := NewScrapper()
	clock := 2000.0
	s.nowFn = func() float64 { return clock }

	s.ComputeLlamaCPPTPS("m", portData(f.port()))
	clock = 2001.0 // slot0 +30, slot1 +50 -> 80 tok/s over 1s
	out := s.ComputeLlamaCPPTPS("m", portData(f.port()))
	if got := fOf(t, out, "tps"); got != 80.0 {
		t.Errorf("tps = %v, want 80.0 (sum of two slot deltas)", got)
	}
}

// TestSlotResetMidInterval: the slot finished its request and started a new
// one between polls — n_decoded reset below the previous value. The new
// request's tokens so far must be counted instead of a bogus negative delta.
func TestSlotResetMidInterval(t *testing.T) {
	f := newFakeLlamaSlots(t, [][]map[string]interface{}{
		{slot(0, true, 100, 500)},
		// old request done (500), new request already decoded 40
		{slot(0, true, 100, 40)},
	})
	s := NewScrapper()
	clock := 3000.0
	s.nowFn = func() float64 { return clock }

	s.ComputeLlamaCPPTPS("m", portData(f.port()))
	clock = 3001.0
	out := s.ComputeLlamaCPPTPS("m", portData(f.port()))
	if got := fOf(t, out, "tps"); got != 40.0 {
		t.Errorf("tps = %v, want 40.0 (slot restarted, 40 tokens since reset)", got)
	}
}

// TestSlotFreshAfterIdle: slot idle at previous poll, request started (and
// decoded 60 tokens) by this poll — all 60 belong to this interval.
func TestSlotFreshAfterIdle(t *testing.T) {
	f := newFakeLlamaSlots(t, [][]map[string]interface{}{
		{slot(0, false, 0, 500)}, // stale decoded count from an old request
		{slot(0, true, 9000, 60)},
	})
	s := NewScrapper()
	clock := 4000.0
	s.nowFn = func() float64 { return clock }

	s.ComputeLlamaCPPTPS("m", portData(f.port()))
	clock = 4001.0
	out := s.ComputeLlamaCPPTPS("m", portData(f.port()))
	if got := fOf(t, out, "tps"); got != 60.0 {
		t.Errorf("tps = %v, want 60.0 (fresh request on previously idle slot)", got)
	}
}

// TestSingleSlotStillWorks: classic 1-slot llama.cpp (qwen-q4k-llama) —
// delta on slot 0 unchanged.
func TestSingleSlotStillWorks(t *testing.T) {
	f := newFakeLlamaSlots(t, [][]map[string]interface{}{
		{slot(0, true, 14000, 42)},
		{slot(0, true, 14042, 84)},
	})
	s := NewScrapper()
	clock := 5000.0
	s.nowFn = func() float64 { return clock }

	s.ComputeLlamaCPPTPS("m", portData(f.port()))
	clock = 5001.0
	out := s.ComputeLlamaCPPTPS("m", portData(f.port()))
	if got := fOf(t, out, "tps"); got != 42.0 {
		t.Errorf("tps = %v, want 42.0 (single-slot delta)", got)
	}
}
