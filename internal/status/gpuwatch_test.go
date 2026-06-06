package status

import (
	"fmt"
	"testing"
)

// TestCheckGpuChange proves the detector fires exactly when the visible-GPU
// count changes and STAYS changed: first confirmed poll only sets the
// baseline (no fire), a no-change poll is quiet, a change confirmed on two
// consecutive polls fires (A-02 debounce), a drop fires "drop", a recovery
// fires "recover", and a subsequent drop fires again (baseline tracks the
// last-seen count).
func TestCheckGpuChange(t *testing.T) {
	p := NewPoller(nil)
	var calls []string
	p.OnGpuCountChange = func(kind string, from, to int) int {
		calls = append(calls, fmt.Sprintf("%s %d->%d", kind, from, to))
		return 0
	}

	step := func(n int) { p.checkGpuChange(n, true) }

	step(4) // first poll: baseline, no fire
	if len(calls) != 0 {
		t.Fatalf("first poll fired: %v", calls)
	}
	step(4) // no change: quiet
	if len(calls) != 0 {
		t.Fatalf("no-change fired: %v", calls)
	}
	step(3) // first sighting of 3: debounce, no fire yet
	if len(calls) != 0 {
		t.Fatalf("single sighting fired: %v", calls)
	}
	step(3) // confirmed: drop
	if len(calls) != 1 || calls[0] != "drop 4->3" {
		t.Fatalf("drop: got %v, want [drop 4->3]", calls)
	}
	step(2)
	step(2) // another drop
	if calls[len(calls)-1] != "drop 3->2" {
		t.Fatalf("second drop: %v", calls[len(calls)-1])
	}
	step(4)
	step(4) // recovery
	if calls[len(calls)-1] != "recover 2->4" {
		t.Fatalf("recover: %v", calls[len(calls)-1])
	}
	step(3)
	step(3) // drop after recovery
	if calls[len(calls)-1] != "drop 4->3" {
		t.Fatalf("drop after recover: %v", calls[len(calls)-1])
	}
	if len(calls) != 4 {
		t.Fatalf("total calls = %d, want 4 (%v)", len(calls), calls)
	}
}

// TestCheckGpuChangeNilHook: a Poller with no hook set must not panic.
func TestCheckGpuChangeNilHook(t *testing.T) {
	p := NewPoller(nil)
	p.checkGpuChange(4, true)
	p.checkGpuChange(2, true)
	p.checkGpuChange(2, true)
	p.checkGpuChange(3, false) // unknown read, no hook
}

// TestGpuEventCacheAccessors: the event round-trips through the Cache.
func TestGpuEventCacheAccessors(t *testing.T) {
	c := NewCache(nil)
	if c.GpuEvent() != nil {
		t.Fatalf("fresh cache has an event")
	}
	ev := &GpuEvent{Kind: "drop", From: 4, To: 3, Stopped: 2, Message: "x", TS: 1, TSHuman: "t"}
	c.SetGpuEvent(ev)
	got := c.GpuEvent()
	if got == nil || got.From != 4 || got.To != 3 || got.Stopped != 2 || got.Kind != "drop" {
		t.Fatalf("event round-trip = %+v", got)
	}
}
