// Package status collects system/model state, mirroring app.py.
package status

// GPU mirrors one nvidia-smi row (app.py gpu_stats).
type GPU struct {
	Index    int    `json:"index"`
	Name     string `json:"name"`
	MemUsed  int    `json:"mem_used"`
	MemTotal int    `json:"mem_total"`
	Util     int    `json:"util"`
	Temp     int    `json:"temp"`
	Power    int    `json:"power"`
	// VramTemp is the GDDR6/GDDR6X memory temperature (°C). Go-only extension
	// (the Python reference has no such field): NVML does not expose VRAM temp
	// on GeForce, so it is read via the reverse-engineered helper
	// /usr/local/sbin/vram-temp. Zero (tool absent, e.g. the Windows dev box)
	// omits the field, keeping the JSON shape identical to Python (parity).
	VramTemp int `json:"vram_temp,omitempty"`
}
