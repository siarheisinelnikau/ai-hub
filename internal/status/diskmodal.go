package status

import (
	"aihub/internal/sh"
	"encoding/json"
	"strings"
	"time"
)

// ReclaimableItem is one line in the "easy to free" list.
type ReclaimableItem struct {
	Name   string  `json:"name"`
	SizeGB float64 `json:"size_gb"`
	Reason string  `json:"reason"`
}

// ReclaimableSummary tells the UI what can be safely freed.
type ReclaimableSummary struct {
	TotalGB float64           `json:"total_gb"`
	Items   []ReclaimableItem `json:"items"`
	Note    string            `json:"note,omitempty"`
}

// computeReclaimableSummary returns what a user can safely free.
// Sources: dangling images and unused volumes. Image sizes alone cannot
// establish deleted layer data; containerd also stores compressed content.
func computeReclaimableSummary() ReclaimableSummary {
	rs := ReclaimableSummary{}

	// 1. Dangling images — fully reclaimable (nothing references them).
	_, out := sh.Run([]string{"docker", "images", "-a",
		"--filter", "dangling=true", "--format", "{{.ID}}|{{.Size}}"}, 15*time.Second)
	if out != "" {
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "|", 2)
			if len(parts) == 2 && parts[0] != "" {
				rs.Items = append(rs.Items, ReclaimableItem{
					Name:   "dangling image " + shortID(parts[0]),
					SizeGB: parseSizeToGB(parts[1]),
					Reason: "not referenced by any container",
				})
				rs.TotalGB += parseSizeToGB(parts[1])
			}
		}
	}

	// 2. Unused volumes (Links==0).
	_, out = sh.Run([]string{"docker", "system", "df", "-v",
		"--format", "{{json .}}"}, 25*time.Second)
	if out != "" {
		var raw struct {
			Volumes []struct {
				Name  string `json:"Name"`
				Size  string `json:"Size"`
				Links string `json:"Links"`
			} `json:"Volumes"`
		}
		if json.Unmarshal([]byte(out), &raw) == nil {
			for _, v := range raw.Volumes {
				if n := atoiSafe(v.Links); n == 0 {
					rs.Items = append(rs.Items, ReclaimableItem{
						Name:   "volume " + v.Name,
						SizeGB: parseSizeToGB(v.Size),
						Reason: "not linked to any container",
					})
					rs.TotalGB += parseSizeToGB(v.Size)
				}
			}
		}
	}

	if len(rs.Items) == 0 {
		rs.Note = "No dangling images or unused volumes detected. Stopped service images may still be required."
		rs.Items = nil
	}

	return rs
}

func volumeService(volName string) string {
	if volName == "" {
		return ""
	}
	vn := strings.ToLower(volName)
	// Known volume → model mapping
	known := map[string]string{
		"qwen-cache":      "ai-qwen-q4k-llama",
		"qwen-q4k-llama":  "ai-qwen-q4k-llama",
		"repo_qwen-cache": "ai-qwen-q4k-llama",
		"music-cache":     "ai-music",
		"threed-cache":    "ai-threed",
	}
	if svc, ok := known[volName]; ok {
		return svc
	}
	// Fallback: try to find a model id that contains the volume name
	models := []string{"qwen-q4k-llama", "qwen-q6k-llama",
		"gemma4-26b-q4k-mtp", "qwen38-flash-next-strata",
		"qwen38-flash-next-uncensored-strata",
		"music", "voice", "threed", "comfyui"}
	for _, m := range models {
		if strings.Contains(vn, m) {
			return "ai-" + m
		}
	}
	return ""
}
