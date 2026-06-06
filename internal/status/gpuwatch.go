package status

// GpuEvent records the most recent change in the number of visible GPUs. It
// lives in the Cache but is intentionally NOT part of Snapshot()'s JSON (a
// Go-only extension, like GpuHist) so the parity-sensitive /api/status shape
// is unchanged; it is surfaced via /api/system-health as "gpu_event".
//
// Kind is "drop" (a GPU disappeared — the dangerous case: a Riser/GPU fell off
// while models were running) or "recover" (a GPU came back). On a drop the
// server stops all running LLM models and fires a webhook.
type GpuEvent struct {
	Kind    string  `json:"kind"` // "drop" | "recover"
	From    int     `json:"from"`
	To      int     `json:"to"`
	Stopped int     `json:"stopped"` // running LLMs stopped (drop only, 0 on recover)
	Message string  `json:"message"`
	TS      float64 `json:"ts"`
	TSHuman string  `json:"ts_human"`
}

// SetGpuEvent stores the most recent GPU-count-change event (thread-safe).
func (c *Cache) SetGpuEvent(ev *GpuEvent) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.gpuEvent = ev
	c.mu.Unlock()
}

// GpuEvent returns the most recent GPU-count-change event, or nil if none.
func (c *Cache) GpuEvent() *GpuEvent {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.gpuEvent
}
