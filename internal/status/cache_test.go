package status

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"

	"aihub/internal/registry"
)

// TestNewCacheJSONShape checks that the pre-first-poll snapshot marshals to
// the same JSON key set and initial values as Python's _cache initial dict
// ({"models": [], "services": [], "gpus": [], "cpu": {}, "disk": {},
// "profiles": {}, "ports": {...}, "ts": 0, "current_profile": ""}).
func TestNewCacheJSONShape(t *testing.T) {
	reg := &registry.File{Ports: map[string]int{"a": 18020, "b": 18021}}
	c := NewCache(reg)
	b, err := json.Marshal(c.Snapshot())
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	s := string(b)
	for _, want := range []string{
		`"models":[]`,
		`"services":[]`,
		`"gpus":[]`,
		`"cpu":{}`,
		`"disk":{}`,
		`"profiles":{}`,
		`"current_profile":""`,
		`"builds":{}`,
		`"ts":0`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON %s: missing %s", s, want)
		}
	}
	if strings.Contains(s, `"error"`) {
		t.Errorf("JSON %s: unexpected error key (must be absent when empty)", s)
	}
	// Ports map: keys are sorted by encoding/json, so check both entries
	// individually.
	if !strings.Contains(s, `"a":18020`) || !strings.Contains(s, `"b":18021`) {
		t.Errorf("JSON %s: ports entries missing", s)
	}
}

// TestPollerFirstPollWindows checks that pollOnce on Windows produces a full
// snapshot: one model entry (mock enabled-by-port, not running), the mock
// GPU list, and TS > 0.
func TestPollerFirstPollWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only: model_status/poller windows mock branch")
	}
	p := NewPoller(&registry.File{Models: []registry.Model{{
		ID: "m-a", Name: "A", Port: 18099, Sysd: "ai-m-a", Category: "LLM",
	}}})
	p.pollOnce()
	snap := p.Cache().Snapshot()

	models, ok := snap["models"].([]ModelStatusEntry)
	if !ok || len(models) != 1 {
		t.Fatalf("snap[models] = %#v, want 1 entry", snap["models"])
	}
	m := models[0]
	if m.ID != "m-a" {
		t.Errorf("models[0].ID = %q, want m-a", m.ID)
	}
	if m.Running {
		t.Error("models[0].Running = true, want false (windows mock)")
	}
	if !m.Enabled {
		t.Error("models[0].Enabled = false, want true (windows, port != 0)")
	}
	gpus, ok := snap["gpus"].([]GPU)
	if !ok {
		t.Fatalf("snap[gpus] = %#v, want []GPU", snap["gpus"])
	}
	if len(gpus) != len(GpuStats()) {
		t.Errorf("len(gpus) = %d, want %d (len of GpuStats())", len(gpus), len(GpuStats()))
	}
	if ts := p.Cache().Timestamp(); ts <= 0 {
		t.Errorf("TS = %v, want > 0 after first poll", ts)
	}
}

// TestWaitReady checks that Run polls immediately (before any sleep) and
// WaitReady observes the first poll.
func TestWaitReady(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := NewPoller(nil)
	go p.Run(ctx)
	if !p.WaitReady(3 * time.Second) {
		t.Fatal("WaitReady(3s) = false, first poll did not complete in time")
	}
}
