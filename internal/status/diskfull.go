package status

import (
	"fmt"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"aihub/internal/sh"
)

// This file implements the full disk breakdown served by GET /api/disk
// (Go-only extension, no Python parity constraint). Unlike DiskUsage() — which
// runs every poll for /api/status and must stay cheap — the full report walks
// the whole filesystem and shells out to the docker CLI, so it is computed on
// demand and cached for diskReportTTL.

// DirSize is one `du -sh` row.
type DirSize struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Path  string `json:"path"`
	Size  string `json:"size"`
}

// DockerImage is one container image row (from `docker system df -v`).
type DockerImage struct {
	Name        string `json:"name"`
	ID          string `json:"id"`
	Size        string `json:"size"`
	UniqueSize  string `json:"unique_size,omitempty"`
	SharedSize  string `json:"shared_size,omitempty"`
	Containers  int    `json:"containers"`
	InUse       bool   `json:"in_use"`
	Reclaimable bool   `json:"reclaimable"`
	Dangling    bool   `json:"dangling,omitempty"`
}

// DockerVolume is one volume row.
type DockerVolume struct {
	Name    string `json:"name"`
	Size    string `json:"size"`
	Links   int    `json:"links"`
	InUse   bool   `json:"in_use"`
	Service string `json:"service,omitempty"` // systemd unit that owns this volume, e.g. "ai-qwen-q4k-llama"
}

// DockerTotals mirrors `docker system df` (one row per type).
type DockerTotals struct {
	ImagesTotal        string `json:"images_total"`
	ImagesReclaimable  string `json:"images_reclaimable"`
	Containers         string `json:"containers"`
	ContainersReclaim  string `json:"containers_reclaimable"`
	Volumes            string `json:"volumes"`
	VolumesReclaimable string `json:"volumes_reclaimable"`
	BuildCache         string `json:"build_cache"`
}

// DockerReport groups everything docker-related.
type DockerReport struct {
	OK      bool           `json:"ok"`
	Totals  DockerTotals   `json:"totals"`
	Images  []DockerImage  `json:"images"`
	Volumes []DockerVolume `json:"volumes"`
}

// SubItem is one child of a sub-breakdown group (`du -sh <dir>/*`).
type SubItem struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size string `json:"size"`
}

// SubGroup is the one-level breakdown of a big directory (e.g. every model
// folder inside /home/user/models, or comfyui/music/huggingface inside
// /srv/ai/models).
type SubGroup struct {
	Key   string    `json:"key"`
	Label string    `json:"label"`
	Path  string    `json:"path"`
	Items []SubItem `json:"items"`
}

// DiskReport is the JSON served by GET /api/disk.
type DiskReport struct {
	TS            float64              `json:"ts"`
	AgeS          float64              `json:"age_s"`
	Total         string               `json:"total"`
	Used          string               `json:"used"`
	Free          string               `json:"free"`
	Dirs          []DirSize            `json:"dirs"`
	Subdirs       []SubGroup           `json:"subdirs"`
	Docker        DockerReport         `json:"docker"`
	Reclaimable   ReclaimableSummary   `json:"reclaimable"`
}

// diskDirs are the paths measured with `du -sh` for the full report. Together
// they account for essentially the whole used space of the root filesystem.
var diskDirs = []struct{ key, label, path string }{
	{"llm", "LLM models", "/home/user/models"},
	{"media", "Media models", "/srv/ai/models"},
	{"containerd", "Container images", "/var/lib/containerd"},
	{"dockerroot", "Docker rootfs", "/var/lib/docker"},
	{"usr", "OS (/usr)", "/usr"},
	{"snapd", "Snaps", "/var/lib/snapd"},
	{"log", "Logs", "/var/log"},
	{"varcache", "Package cache", "/var/cache"},
	{"root", "root home", "/root"},
	{"tmp", "Temp", "/tmp"},
	{"boot", "Boot", "/boot"},
	{"hub", "Hub", "/srv/ai-hub"},
	{"vllmcache", "vLLM cache", "/home/user/vllm-cache"},
	{"deepseek", "deepseek-harness", "/home/user/deepseek-harness"},
	{"downloads", "Downloads", "/home/user/Downloads"},
	{"snapuser", "Snap (user)", "/home/user/snap"},
	{"hfcache", "HF cache", "/home/user/.cache/huggingface"},
	{"optim", "vLLM models", "/home/user/models-optim"},
}

func duOne(path string) string {
	rc, out := sh.Run([]string{"du", "-sh", path}, 20*time.Second)
	if rc != 0 || strings.TrimSpace(out) == "" {
		return "—"
	}
	first := strings.SplitN(out, "\n", 2)[0]
	return strings.SplitN(first, "\t", 2)[0]
}

func dfRoot() (total, used, free string) {
	rc, out := sh.Run([]string{"df", "-h", "/"}, 10*time.Second)
	if rc != 0 {
		return "—", "—", "—"
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		return "—", "—", "—"
	}
	p := strings.Fields(lines[1])
	get := func(i int) string {
		if len(p) > i {
			return p[i]
		}
		return "—"
	}
	return get(1), get(2), get(3)
}

// parseDockerDF parses rows of
// `docker system df --format '{{.Type}}|{{.TotalCount}}|{{.Active}}|{{.Size}}|{{.Reclaimable}}'`.
func parseDockerDF(out string) DockerTotals {
	var t DockerTotals
	stripPct := func(s string) string {
		if i := strings.Index(s, " ("); i >= 0 {
			return s[:i]
		}
		return s
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		p := strings.Split(line, "|")
		if len(p) < 5 {
			continue
		}
		switch p[0] {
		case "Images":
			t.ImagesTotal = p[3]
			t.ImagesReclaimable = stripPct(p[4])
		case "Containers":
			t.Containers = p[3]
			t.ContainersReclaim = stripPct(p[4])
		case "Local Volumes":
			t.Volumes = p[3]
			t.VolumesReclaimable = stripPct(p[4])
		case "Build Cache":
			t.BuildCache = p[3]
		}
	}
	return t
}

// parseDockerDFVerbose parses the single JSON object printed by
// `docker system df -v --format '{{json .}}'` into image and volume rows.
func parseDockerDFVerbose(out string) ([]DockerImage, []DockerVolume, bool) {
	var raw struct {
		Images []struct {
			ID         string `json:"ID"`
			Repository string `json:"Repository"`
			Tag        string `json:"Tag"`
			Size       string `json:"Size"`
			UniqueSize string `json:"UniqueSize"`
			SharedSize string `json:"SharedSize"`
			Containers string `json:"Containers"`
		} `json:"Images"`
		Volumes []struct {
			Name  string `json:"Name"`
			Size  string `json:"Size"`
			Links string `json:"Links"`
		} `json:"Volumes"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &raw); err != nil {
		return nil, nil, false
	}
	images := make([]DockerImage, 0, len(raw.Images))
	for _, im := range raw.Images {
		name := im.Repository
		if im.Tag != "" {
			name += ":" + im.Tag
		}
		if name == "" || name == ":" {
			name = "<none>"
		}
		n := atoiSafe(im.Containers)
		images = append(images, DockerImage{
			Name:        name,
			ID:          shortID(im.ID),
			Size:        im.Size,
			UniqueSize:  im.UniqueSize,
			SharedSize:  im.SharedSize,
			Containers:  n,
			InUse:       n > 0,
			Reclaimable: n == 0,
		})
	}
	volumes := make([]DockerVolume, 0, len(raw.Volumes))
	for _, v := range raw.Volumes {
		n := atoiSafe(v.Links)
		volumes = append(volumes, DockerVolume{Name: v.Name, Size: v.Size, Links: n, InUse: n > 0, Service: volumeService(v.Name)})
	}
	return images, volumes, true
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// parseSizeToGB mirrors the JS diskGB() — parses "1.5G" / "48K" / "2.1M" into GB float64.
func parseSizeToGB(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "â" {
		return 0
	}
	val, unit := parseSizeNum(s)
	pct := 1.0
	if i := strings.Index(s, "("); i >= 0 {
		if j := strings.Index(s[i+1:], ")"); j >= 0 {
			var p float64
			if _, err := fmt.Sscanf(s[i+1:i+1+j], "%f", &p); err == nil {
				pct = 1.0 - p/100.0
			}
		}
	}
	p := 1.0
	switch unit {
	case 'T': p = 1024
	case 'G': p = 1
	case 'M': p = 1.0 / 1024
	case 'K': p = 1.0 / 1048576
	case 'B': p = 1.0 / 1048576
	}
	return val * p * pct
}

// parseSizeNum splits "23.8G" into (value, unit).
func parseSizeNum(s string) (float64, byte) {
	var val float64
	var n int
	if n, _ = fmt.Sscanf(s, "%f", &val); n == 0 {
		return 0, 'G'
	}
	var unit byte
	for i := len(s) - 1; i >= 0 && i > 0; i-- {
		c := s[i]
		if c >= 'A' && c <= 'Z' { unit = c; break }
		if c >= 'a' && c <= 'z' { unit = byte(c - 32); break }
	}
	return val, unit
}

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// dockerReport shells out to the docker CLI. Best-effort: on any failure the
// report comes back with OK=false and whatever rows were parsed.
func dockerReport() DockerReport {
	rep := DockerReport{Images: []DockerImage{}, Volumes: []DockerVolume{}}
	rc, out := sh.Run([]string{"docker", "system", "df",
		"--format", "{{.Type}}|{{.TotalCount}}|{{.Active}}|{{.Size}}|{{.Reclaimable}}"}, 25*time.Second)
	if rc != 0 {
		return rep
	}
	rep.Totals = parseDockerDF(out)

	if rc2, out2 := sh.Run([]string{"docker", "system", "df", "-v",
		"--format", "{{json .}}"}, 25*time.Second); rc2 == 0 {
		imgs, vols, ok := parseDockerDFVerbose(out2)
		if ok {
			rep.Images = imgs
			rep.Volumes = vols
			rep.OK = true
		}
	}

	// Dangling images are not listed by `docker system df -v` but can hold
	// tens of GB (e.g. the old comfyui build kept alive by an exited container).
	if rc3, out3 := sh.Run([]string{"docker", "images", "-a",
		"--filter", "dangling=true", "--format", "{{.ID}}|{{.Size}}"}, 15*time.Second); rc3 == 0 {
		for _, line := range strings.Split(strings.TrimSpace(out3), "\n") {
			p := strings.Split(line, "|")
			if len(p) == 2 && p[0] != "" {
				rep.Images = append(rep.Images, DockerImage{
					Name: "<dangling>", ID: p[0], Size: p[1], Dangling: true,
				})
			}
		}
	}
	return rep
}

// subGroups are the one-level sub-breakdowns of the biggest directories, so
// the modal can show WHAT is inside the 240G models / 297G media lumps.
var subGroups = []struct{ key, label, path string }{
	{"llm", "LLM models", "/home/user/models"},
	{"media", "Media models", "/srv/ai/models"},
	{"comfyui", "ComfyUI models", "/srv/ai/models/comfyui"},
	{"music", "Music models", "/srv/ai/models/music"},
}

// parseDuChildren parses `du -sh <dir>/*` output ("1.5G\t/path/name" lines).
func parseDuChildren(out string) []SubItem {
	items := []SubItem{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		p := strings.SplitN(line, "\t", 2)
		if len(p) != 2 {
			continue
		}
		size := strings.TrimSpace(p[0])
		path := strings.TrimSpace(p[1])
		if size == "" || path == "" {
			continue
		}
		name := path[strings.LastIndex(path, "/")+1:]
		items = append(items, SubItem{Name: name, Path: path, Size: size})
	}
	return items
}

// duChildren runs `du -sh <dir>/*` through a shell (globs need one) and parses
// the rows. Returns an empty slice when the directory is missing/empty.
func duChildren(dir string) []SubItem {
	rc, out := sh.Run([]string{"bash", "-c", "du -sh " + dir + "/* 2>/dev/null"}, 30*time.Second)
	if rc != 0 && strings.TrimSpace(out) == "" {
		return []SubItem{}
	}
	return parseDuChildren(out)
}

func computeDiskReport() DiskReport {
	rep := DiskReport{
		TS:      nowEpoch(),
		Dirs:    make([]DirSize, 0, len(diskDirs)),
		Subdirs: make([]SubGroup, 0, len(subGroups)),
	}
	rep.Total, rep.Used, rep.Free = dfRoot()
	for _, d := range diskDirs {
		rep.Dirs = append(rep.Dirs, DirSize{Key: d.key, Label: d.label, Path: d.path, Size: duOne(d.path)})
	}
	for _, g := range subGroups {
		rep.Subdirs = append(rep.Subdirs, SubGroup{Key: g.key, Label: g.label, Path: g.path, Items: duChildren(g.path)})
	}
	rep.Docker = dockerReport()
	rep.Reclaimable = computeReclaimableSummary()
	return rep
}

var (
	diskRepMu sync.Mutex
	diskRep   DiskReport
	diskRepAt time.Time
)

const diskReportTTL = 10 * time.Second

// FullDiskReportCached returns the full disk report. A cached copy younger
// than diskReportTTL is served as-is; fresh=true (the modal's Rescan button)
// forces a new scan. Concurrent callers wait for the single in-flight
// computation (the scan takes a few seconds).
func FullDiskReportCached(fresh bool) DiskReport {
	diskRepMu.Lock()
	defer diskRepMu.Unlock()
	if !fresh && !diskRepAt.IsZero() && time.Since(diskRepAt) < diskReportTTL {
		r := diskRep
		r.AgeS = time.Since(diskRepAt).Seconds()
		return r
	}
	r := computeDiskReport()
	diskRep = r
	diskRepAt = time.Now()
	r.AgeS = 0
	return r
}
