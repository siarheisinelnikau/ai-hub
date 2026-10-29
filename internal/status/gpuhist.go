package status

import (
	"sync"
)

// GpuHistMax bounds the GPU temperature history (newest evicts oldest).
// At the 3s status-poll cadence this is ~6 minutes of chart data.
const GpuHistMax = 120

// GpuSample is one timestamped snapshot of per-GPU telemetry. All arrays are
// indexed by nvidia-smi index (position i = GPU i); a value of -1 means "no
// reading" for that GPU at that moment. Carries core/VRAM temps plus
// power (W), utilization (%) and memory (MB) so every GPU chart shares one
// data source.
type GpuSample struct {
	TS    float64 `json:"ts"`
	Gpu   []int   `json:"gpu"`
	Vram  []int   `json:"vram"`
	Power []int   `json:"power"`
	Util  []int   `json:"util"`
	Mem   []int   `json:"mem"`
}

// GpuHist is a bounded, thread-safe ring of GpuSample. It feeds the per-GPU
// temperature charts (GPU core + VRAM lines). It is Go-only — never part of
// the /api/status snapshot, so the parity-sensitive JSON shape is untouched.
type GpuHist struct {
	mu    sync.Mutex
	items []GpuSample
	max   int
}

func NewGpuHist(max int) *GpuHist { return &GpuHist{max: max} }

// Append adds a sample, evicting the oldest once over max.
func (h *GpuHist) Append(s GpuSample) {
	if h == nil || h.max <= 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.items = append(h.items, s)
	if len(h.items) > h.max {
		h.items = h.items[len(h.items)-h.max:]
	}
}

// Items returns a copy, oldest→newest (never nil).
func (h *GpuHist) Items() []GpuSample {
	if h == nil {
		return []GpuSample{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]GpuSample, len(h.items))
	copy(out, h.items)
	return out
}

// AppendFromGpus builds a sample from the current GPU stats and appends it.
// Arrays are indexed by GPU.Index so the position always equals the nvidia
// index, regardless of the slice order; missing indices are -1.
func (h *GpuHist) AppendFromGpus(gpus []GPU) {
	if h == nil || len(gpus) == 0 {
		return
	}
	maxIdx := 0
	for _, g := range gpus {
		if g.Index > maxIdx {
			maxIdx = g.Index
		}
	}
	gpu := make([]int, maxIdx+1)
	vram := make([]int, maxIdx+1)
	power := make([]int, maxIdx+1)
	util := make([]int, maxIdx+1)
	mem := make([]int, maxIdx+1)
	for i := 0; i <= maxIdx; i++ {
		gpu[i], vram[i], power[i], util[i], mem[i] = -1, -1, -1, -1, -1
	}
	for _, g := range gpus {
		if g.Index < 0 || g.Index > maxIdx {
			continue
		}
		gpu[g.Index] = g.Temp
		vram[g.Index] = g.VramTemp // 0 when the tool is absent (no VRAM reading)
		power[g.Index] = g.Power
		util[g.Index] = g.Util
		mem[g.Index] = g.MemUsed // MiB
	}
	h.Append(GpuSample{TS: nowEpoch(), Gpu: gpu, Vram: vram, Power: power, Util: util, Mem: mem})
}
