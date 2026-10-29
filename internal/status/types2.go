package status

// CPU mirrors the dict returned by app.py cpu_stats().
type CPU struct {
	Load1    float64 `json:"load1"`
	Load5    float64 `json:"load5"`
	Load15   float64 `json:"load15"`
	CPUCount int     `json:"cpu_count"`
	Socket0  float64 `json:"socket0"`
	Socket1  float64 `json:"socket1"`
	MemTotal int     `json:"mem_total"`
	MemUsed  int     `json:"mem_used"`
}

// Disk mirrors the dict returned by app.py disk_usage().
type Disk struct {
	DiskTotal   string `json:"disk_total"`
	DiskUsed    string `json:"disk_used"`
	DiskFree    string `json:"disk_free"`
	HFCache     string `json:"hf_cache"`
	ModelsOptim string `json:"models_optim"`
	ModelsLocal string `json:"models_local"`
	ComfyModels string `json:"comfy_models"`
}
