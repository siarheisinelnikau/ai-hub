package metrics

import (
	"context"
	"sync"
	"time"
)

// StatsPoller mirrors app.py _stats: a ring buffer (maxlen 180) per model id,
// fed by a background loop that scrapes /metrics of running+healthy models
// every StatsPoll seconds.
type StatsPoller struct {
	mu       sync.Mutex
	stats    map[string]*Ring
	scrapper *Scrapper
	interval time.Duration
	// source returns the models to scrape (server supplies running+healthy
	// ones from the status cache; may be nil in tests).
	source func() []ModelRef
}

func NewStatsPoller(source func() []ModelRef) *StatsPoller {
	return &StatsPoller{
		stats:    map[string]*Ring{},
		scrapper: NewScrapper(),
		interval: StatsPoll * time.Second,
		source:   source,
	}
}

// Run loops until ctx done: tick, then sleep interval (Python order).
func (p *StatsPoller) Run(ctx context.Context) {
	for {
		p.tick()
		select {
		case <-ctx.Done():
			return
		case <-time.After(p.interval):
		}
	}
}

// tick mirrors one _stats_poll_loop iteration.
func (p *StatsPoller) tick() {
	defer func() { recover() }() // Python: except: pass
	if p.source == nil {
		return
	}
	for _, m := range p.source() {
		if m.Port == 0 {
			continue
		}
		data := ScrapeModelMetrics(m.Port, m.Backend, m.ID, p.scrapper)
		if data == nil {
			continue
		}
		data["ts"] = float64(time.Now().UnixNano()) / 1e9
		p.mu.Lock()
		ring, ok := p.stats[m.ID]
		if !ok {
			ring = NewRing(StatsMax)
			p.stats[m.ID] = ring
		}
		ring.Append(data)
		p.mu.Unlock()
	}
}

// Scrapper exposes the internal scrapper so one-shot scrapes (e.g.
// /api/model/{mid}/stats) share the prev-counter state with the poller,
// mirroring app.py's single global _prev_counters.
func (p *StatsPoller) Scrapper() *Scrapper { return p.scrapper }

// Get mirrors `buf = _stats.get(mid); history = list(buf) if buf else []`.
// Returns non-nil empty slice when the model has no history.
func (p *StatsPoller) Get(mid string) []interface{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	ring, ok := p.stats[mid]
	if !ok {
		return []interface{}{}
	}
	return ring.Items()
}

// LastEntry returns the most recent scrape entry for mid (the "live =
// history[-1]" rule) as a map, and whether one exists. Used to annotate
// running models with live prompt-TPS / context-usage in /api/status.
func (p *StatsPoller) LastEntry(mid string) (map[string]interface{}, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ring, ok := p.stats[mid]
	if !ok {
		return nil, false
	}
	last, ok := ring.Last()
	if !ok {
		return nil, false
	}
	entry, ok := last.(map[string]interface{})
	if !ok {
		return nil, false
	}
	return entry, true
}

// LiveTPS returns the most recent live TPS (tok/s) recorded for mid and
// whether a usable value exists. Same "live = most recent history entry"
// rule the stats endpoint uses (live = history[-1] in app.py): the tps
// field of the last ring entry. Zero/absent (no scrape, idle, bad entry)
// reports (0, false) so callers can omit the field entirely.
func (p *StatsPoller) LiveTPS(mid string) (float64, bool) {
	entry, ok := p.LastEntry(mid)
	if !ok {
		return 0, false
	}
	tps := f64of(entry["tps"])
	if tps <= 0 {
		return 0, false
	}
	return tps, true
}
