package status

import (
	"context"
	"fmt"
	"sync"
	"time"

	"aihub/internal/registry"
	"aihub/internal/webhooks"
)

// Poller is the background loop that keeps the Cache filled, mirroring
// app.py's _poll_loop thread (POLL_INTERVAL = 3).
type Poller struct {
	reg       *registry.File
	portToIDs map[int][]string
	cache     *Cache
	interval  time.Duration

	// OnGpuCountChange is called (from the poll goroutine) when the number of
	// visible GPUs changes from the baseline established at the first poll. The
	// server sets it to stop all running LLMs (drop only) and record the event;
	// it returns how many models were stopped (used in the event message).
	// Nil = no-op.
	OnGpuCountChange func(kind string, from, to int) int
	gpuBase          int
	gpuInit          bool
	// Debounce (A-02): a count change fires only after it has been observed on
	// two CONSECUTIVE confirmed polls. A transient one-poll blip (4→2→4) never
	// fires. gpuCand=0 means "no pending change".
	gpuCand     int
	gpuCandSeen int
	gpuMu       sync.Mutex
}

// NewPoller builds a Poller over the given registry file. reg may be nil
// (no models); the guard is required because BuildPortToIDs dereferences
// reg.Models and would panic on a nil *registry.File.
func NewPoller(reg *registry.File) *Poller {
	var portToIDs map[int][]string
	if reg != nil {
		portToIDs = BuildPortToIDs(reg)
	}
	return &Poller{
		reg:       reg,
		portToIDs: portToIDs,
		cache:     NewCache(reg),
		interval:  3 * time.Second, // Python: POLL_INTERVAL = 3
	}
}

// Cache returns the shared cache that Run keeps filled.
func (p *Poller) Cache() *Cache { return p.cache }

// Run polls immediately, then every p.interval, until ctx is done.
// Mirrors app.py _poll_loop: poll first, sleep after.
func (p *Poller) Run(ctx context.Context) {
	for {
		p.pollOnce()
		select {
		case <-ctx.Done():
			return
		case <-time.After(p.interval):
		}
	}
}

// nowEpoch mirrors Python time.time(): fractional Unix epoch as float64
// (e.g. 1788123456.789), not integer Unix seconds.
func nowEpoch() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

// pollOnce mirrors one iteration of _poll_loop, including its outer
// try/except: on any panic the loop stays alive and only ts + error are
// updated, keeping the previous values (Python: _cache["ts"] = time.time();
// _cache["error"] = str(e)[:500]).
func (p *Poller) pollOnce() {
	func() {
		defer func() {
			if r := recover(); r != nil {
				p.cache.mu.Lock()
				p.cache.TS = nowEpoch()
				p.cache.Error = fmt.Sprint(r)[:500]
				p.cache.mu.Unlock()
			}
		}()

		// Per-model recover, mirroring Python's per-model try/except with
		// its minimal fallback dict.
		models := []ModelStatusEntry{}
		if p.reg != nil {
			for _, m := range p.reg.Models {
				entry := func() (e ModelStatusEntry) {
					defer func() {
						if r := recover(); r != nil {
							e = ModelStatusEntry{
								ID:       m.ID,
								Name:     m.Name,
								Short:    m.Short,
								Category: m.Category,
								Port:     m.Port,
								Desc:     fmt.Sprint(r)[:100],
							}
							if e.Short == "" {
								e.Short = e.Name
							}
							e.In = []string{}
							e.Out = []string{}
							e.Sub = []Sub{}
							e.GroupSiblings = []string{}
						}
					}()
					return ModelStatus(m, p.portToIDs)
				}()
				models = append(models, entry)
			}
		}

		// Webhook (best-effort): a running model that fails its health check is
		// a crash/degradation signal. Coalesced so a persistently-unhealthy
		// model fires once, not every poll cycle.
		for _, m := range models {
			if m.Running && !m.Healthy {
				webhooks.FireCoalesced("model.unhealthy", m.ID, "running but failed health check", 90*time.Second)
			}
		}

		gpus, gpusOK := GpuStatsOK()
		// Detect a change in the number of visible GPUs (a GPU/Riser dropping
		// out while models run) and let the server react (stop all + record).
		// gpusOK=false = nvidia-smi failed (unknown), not "0 GPUs" (A-02).
		p.checkGpuChange(len(gpus), gpusOK)
		cpu := CpuStats()
		disk := DiskUsage()
		cur := CurrentProfile()

		// Record a GPU core+VRAM temperature sample for the per-GPU charts
		// (Go-only; not part of the /api/status JSON).
		p.cache.GpuHist.AppendFromGpus(gpus)
		// Long-term retention: also append to the disk-backed telemetry log
		// (bounded by logrotate) so CSV export has data beyond the in-memory
		// 6-minute ring. Best-effort; no-op if the path is not writable.
		if last := p.cache.GpuHist.Items(); len(last) > 0 {
			AppendGpu(last[len(last)-1])
		}

		p.cache.mu.Lock()
		p.cache.Models = models
		p.cache.Services = []ServiceStatus{} // current registry has no top-level "services"
		p.cache.GPUs = gpus
		p.cache.CPU = cpu
		p.cache.Disk = disk
		p.cache.CurrentProfile = cur
		// Python writes PROFILES (dict from the registry "profiles" list)
		// every iteration; registry.File does not parse that key, so the
		// Go equivalent is the empty map. On the current registry both
		// sides marshal to {}.
		p.cache.Profiles = map[string]interface{}{}
		p.cache.TS = nowEpoch() // Python time.time() is a float
		p.cache.Error = ""
		p.cache.mu.Unlock()
	}()
}

// checkGpuChange compares the current visible-GPU count against the baseline
// established at the first confirmed poll. A decrease ("drop") or increase
// ("recover") fires OnGpuCountChange (a drop is the dangerous case: a
// Riser/GPU fell off). The baseline tracks the last-seen count so a single
// drop fires once, then a later recovery fires "recover", and a subsequent
// drop fires again. The first confirmed poll only sets the baseline (no fire)
// — a box that boots with 3 GPUs (one already dead) is not an event.
//
// A-02 semantics (no destructive action on transient telemetry):
//   - ok=false (nvidia-smi failed) is "unknown": it neither confirms a
//     change nor resets the pending candidate. A failed read is never
//     treated as "GPU count = 0".
//   - a change fires only after being observed on two consecutive confirmed
//     polls (debounce). A single-poll blip (4→2→4) never fires.
func (p *Poller) checkGpuChange(n int, ok bool) {
	hook, fire, kind, from, to := p.decideGpuChange(n, ok)
	if fire && hook != nil {
		hook(kind, from, to)
	}
}

// decideGpuChange applies the A-02 state machine under the lock and returns
// (hook, shouldFire, kind, from, to). The hook is invoked by the caller,
// OUTSIDE the lock, so a re-entrant hook cannot deadlock the poller.
func (p *Poller) decideGpuChange(n int, ok bool) (func(string, int, int) int, bool, string, int, int) {
	if !ok {
		return nil, false, "", 0, 0 // unknown: leave baseline + candidate untouched
	}
	p.gpuMu.Lock()
	defer p.gpuMu.Unlock()
	if !p.gpuInit {
		p.gpuInit = true
		p.gpuBase = n
		return nil, false, "", 0, 0
	}
	if n == p.gpuBase {
		p.gpuCand, p.gpuCandSeen = 0, 0 // back to baseline: cancel any pending change
		return nil, false, "", 0, 0
	}
	if p.gpuCand == n {
		p.gpuCandSeen++
	} else {
		p.gpuCand = n
		p.gpuCandSeen = 1
	}
	if p.gpuCandSeen < 2 {
		return nil, false, "", 0, 0 // debounce: need one more confirming poll
	}
	from := p.gpuBase
	kind := "drop"
	if n > p.gpuBase {
		kind = "recover"
	}
	p.gpuBase = n
	p.gpuCand, p.gpuCandSeen = 0, 0
	return p.OnGpuCountChange, true, kind, from, n
}

// WaitReady blocks until the first poll (TS>0) or timeout. Mirrors the
// Python startup wait (up to 5s, 0.5s steps; here 200ms steps).
func (p *Poller) WaitReady(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if p.cache.Timestamp() > 0 {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return p.cache.Timestamp() > 0
}
