package status

import (
	"fmt"
	"runtime"
	"testing"
)

// recordHook builds a Poller whose OnGpuCountChange appends "kind:from-to"
// to *events.
func recordHook(t *testing.T, events *[]string) *Poller {
	t.Helper()
	p := NewPoller(nil)
	p.OnGpuCountChange = func(kind string, from, to int) int {
		*events = append(*events, fmt.Sprintf("%s:%d-%d", kind, from, to))
		return 0
	}
	return p
}

// A-02: a failed nvidia-smi read (ok=false) is "unknown", not "0 GPUs".
// Several consecutive telemetry failures must not fire the hook and must
// not move the baseline.
func TestGpuChangeUnknownReadsNeverFire(t *testing.T) {
	var events []string
	p := recordHook(t, &events)

	p.checkGpuChange(4, true) // baseline
	p.checkGpuChange(0, false)
	p.checkGpuChange(0, false)
	p.checkGpuChange(0, false) // 3 failed reads in a row
	p.checkGpuChange(4, true)  // confirmed back at 4

	if len(events) != 0 {
		t.Fatalf("events = %v, want none (unknown reads are not a drop)", events)
	}
}

// A-02: a one-poll blip (4→2→4) is transient noise — debounce swallows it,
// the hook never fires.
func TestGpuChangeTransientBlipNeverFires(t *testing.T) {
	var events []string
	p := recordHook(t, &events)

	p.checkGpuChange(4, true) // baseline
	p.checkGpuChange(2, true) // first sighting of 2 (candidate, no fire)
	p.checkGpuChange(4, true) // back to 4 -> candidate cancelled
	p.checkGpuChange(2, true) // blip again
	p.checkGpuChange(4, true)

	if len(events) != 0 {
		t.Fatalf("events = %v, want none (single-poll blips are transient)", events)
	}
}

// A-02: a SUSTAINED drop (4→2 confirmed on two consecutive polls) fires
// exactly once, and the new baseline is 2 (a later 2 does not re-fire).
func TestGpuChangeSustainedDropFiresOnce(t *testing.T) {
	var events []string
	p := recordHook(t, &events)

	p.checkGpuChange(4, true) // baseline
	p.checkGpuChange(2, true) // candidate
	p.checkGpuChange(2, true) // confirmed -> fire
	p.checkGpuChange(2, true) // steady -> no re-fire

	if len(events) != 1 || events[0] != "drop:4-2" {
		t.Fatalf("events = %v, want [drop:4-2]", events)
	}
}

// A-02: recovery (2→4) fires "recover" after confirmation; the server side
// stops models on "drop" only, never on "recover" (see server_test.go).
func TestGpuChangeRecoverFiresAfterConfirm(t *testing.T) {
	var events []string
	p := recordHook(t, &events)

	p.checkGpuChange(4, true) // baseline
	p.checkGpuChange(2, true)
	p.checkGpuChange(2, true) // drop fires
	p.checkGpuChange(4, true) // recover candidate
	p.checkGpuChange(4, true) // recover fires

	if len(events) != 2 || events[0] != "drop:4-2" || events[1] != "recover:2-4" {
		t.Fatalf("events = %v, want [drop:4-2 recover:2-4]", events)
	}
}

// A-02: an unknown read between two confirmed sightings neither resets nor
// confirms the pending candidate — the change still fires once.
func TestGpuChangeUnknownBetweenSightings(t *testing.T) {
	var events []string
	p := recordHook(t, &events)

	p.checkGpuChange(4, true) // baseline
	p.checkGpuChange(2, true) // candidate
	p.checkGpuChange(0, false) // nvidia-smi hiccup in between
	p.checkGpuChange(2, true) // confirmed again -> fire

	if len(events) != 1 || events[0] != "drop:4-2" {
		t.Fatalf("events = %v, want [drop:4-2]", events)
	}
}

// GpuStatsOK contract: ok=false yields an empty (non-nil) list, ok=true a
// parsed one. (Windows returns the mock with ok=true.)
func TestGpuStatsOKShape(t *testing.T) {
	g, ok := GpuStatsOK()
	if ok && len(g) == 0 && runtime.GOOS == "linux" {
		t.Fatalf("ok=true with zero GPUs on linux — should be ok=false (unknown)")
	}
	if !ok && len(g) != 0 {
		t.Fatalf("ok=false must carry no GPUs, got %v", g)
	}
}
