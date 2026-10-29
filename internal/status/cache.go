package status

import (
	"sync"

	"aihub/internal/registry"
)

// Cache is the thread-safe snapshot served by /api/status. CPU and Disk are
// interface{} so the pre-first-poll state marshals to {} (Python parity);
// after a poll they hold CPU / Disk values.
//
// GpuHist holds the GPU core+VRAM temperature history for the per-GPU charts.
// It is intentionally NOT part of Snapshot()'s JSON (a Go-only extension, like
// tps_live) so the parity-sensitive /api/status shape is unchanged; it is read
// directly by the /api/gpu-temps endpoint.
type Cache struct {
	Models         []ModelStatusEntry     `json:"models"`
	Services       []ServiceStatus        `json:"services"`
	GPUs           []GPU                  `json:"gpus"`
	CPU            interface{}            `json:"cpu"`
	Disk           interface{}            `json:"disk"`
	Profiles       map[string]interface{} `json:"profiles"`
	Ports          map[string]int         `json:"ports"`
	TS             float64                `json:"ts"`
	CurrentProfile string                 `json:"current_profile"`
	Builds         map[string]interface{} `json:"builds"`
	Error          string                 `json:"error,omitempty"`

	GpuHist  *GpuHist
	gpuEvent *GpuEvent

	mu sync.RWMutex
}

// NewCache mirrors the Python _cache initial dict: empty lists, empty maps,
// ts=0. Ports come from the registry (may be an empty non-nil map).
func NewCache(reg *registry.File) *Cache {
	ports := map[string]int{}
	if reg != nil {
		for k, v := range reg.Ports {
			ports[k] = v
		}
	}
	return &Cache{
		Models:         []ModelStatusEntry{},
		Services:       []ServiceStatus{},
		GPUs:           []GPU{},
		CPU:            map[string]interface{}{},
		Disk:           map[string]interface{}{},
		Profiles:       map[string]interface{}{},
		Ports:          ports,
		TS:             0,
		CurrentProfile: "",
		Builds:         map[string]interface{}{},
		GpuHist:        NewGpuHist(GpuHistMax),
	}
}

// GpuTemps returns the GPU core+VRAM temperature history (oldest→newest) for
// the per-GPU charts. Never nil.
func (c *Cache) GpuTemps() []GpuSample {
	if c == nil || c.GpuHist == nil {
		return []GpuSample{}
	}
	return c.GpuHist.Items()
}

// Snapshot returns a deep-enough copy for JSON marshaling (Python returns
// dict(_cache)). Slices are copied; the CPU/Disk interfaces are copied by
// value (CPU/Disk are value types, so no aliasing); maps are shared
// references, safe because the poller replaces (not mutates) them under
// lock.
//
// Slices are copied with make+copy rather than append([]T(nil), s...) so
// that an EMPTY slice stays non-nil and marshals to [] (the append idiom
// would yield a nil slice -> JSON null, breaking parity with Python's []).
//
// Note on key order: the result is a map, so encoding/json emits keys in
// alphabetical order, unlike Python's insertion order (models, services,
// gpus, cpu, disk, profiles, ports, ts, current_profile, builds, [error]).
// JSON object key order is not semantically significant and the frontend
// reads by key — acceptable.
func (c *Cache) Snapshot() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	models := make([]ModelStatusEntry, len(c.Models))
	copy(models, c.Models)
	services := make([]ServiceStatus, len(c.Services))
	copy(services, c.Services)
	gpus := make([]GPU, len(c.GPUs))
	copy(gpus, c.GPUs)

	// Build a plain map so the JSON key set matches Python
	// (models, services, gpus, cpu, disk, profiles, ports, ts,
	// current_profile, builds, [error]).
	out := map[string]interface{}{
		"models":          models,
		"services":        services,
		"gpus":            gpus,
		"cpu":             c.CPU,
		"disk":            c.Disk,
		"profiles":        c.Profiles,
		"ports":           c.Ports,
		"ts":              c.TS,
		"current_profile": c.CurrentProfile,
		"builds":          c.Builds,
	}
	if c.Error != "" {
		out["error"] = c.Error
	}
	return out
}

// Timestamp returns TS under a read lock (TS is guarded by mu).
func (c *Cache) Timestamp() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.TS
}

// SetModels replaces the models snapshot under lock (used by tests to
// inject running state directly, bypassing the poller).
func (c *Cache) SetModels(models []ModelStatusEntry) {
	c.mu.Lock()
	c.Models = models
	c.mu.Unlock()
}
