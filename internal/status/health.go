package status

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SystemHealth returns a live snapshot + short history for the system-health
// panel. Go-only (no Python parity constraint); on a box without the sources
// it returns available=false with zeroed values so the UI degrades gracefully.
//
// Live values are read from /proc + /sys (authoritative), not from the
// crash-hunter TSV log (whose format has drifted). The log is used only for
// the best-effort trend history.
//
// Key signals (the box has a history of PSU/power-related hard reboots, so
// RAM / EDAC / MCE / AER are the most valuable):
//   - RAM used/total/%, swap used            (/proc/meminfo)
//   - EDAC correctable/uncorrectable per ctl (/sys/.../edac/mc/mc*)
//   - AER cumulative                          (pcie-aer log, best-effort)
//   - CPU load + socket temps                 (/proc/loadavg, gpu-sensors log)
func SystemHealth() map[string]interface{} {
	res := map[string]interface{}{
		"available": false,
		"ram":       map[string]interface{}{"total_gb": 0.0, "used_gb": 0.0, "avail_gb": 0.0, "pct": 0.0},
		"swap_gb":   0.0,
		"edac":      map[string]interface{}{"ce_total": 0, "ue_total": 0, "by_controller": []int{}},
		"aer":       map[string]interface{}{"correctable": 0, "fatal": 0},
		"load":      map[string]interface{}{"l1": 0.0, "l5": 0.0, "l15": 0.0},
		"cpu":       map[string]interface{}{"sock0_c": 0.0, "sock1_c": 0.0},
		"uptime_s":  0,
		"history":   []map[string]interface{}{},
	}

	// RAM / swap (authoritative)
	if mi := readMeminfo(); len(mi) > 0 {
		total, avail := mi["MemTotal"], mi["MemAvailable"]
		used := total - avail
		if used < 0 {
			used = 0
		}
		pct := 0.0
		if total > 0 {
			pct = round1(float64(used) / float64(total) * 100)
		}
		res["ram"] = map[string]interface{}{
			"total_gb": round1(float64(total) / 1048576),
			"used_gb":  round1(float64(used) / 1048576),
			"avail_gb": round1(float64(avail) / 1048576),
			"pct":      pct,
		}
		swapUsed := mi["SwapTotal"] - mi["SwapFree"]
		if swapUsed < 0 {
			swapUsed = 0
		}
		res["swap_gb"] = round1(float64(swapUsed) / 1048576)
		res["available"] = true
	}

	// CPU load + uptime (authoritative)
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		if f := strings.Fields(string(b)); len(f) >= 3 {
			l1, _ := strconv.ParseFloat(f[0], 64)
			l5, _ := strconv.ParseFloat(f[1], 64)
			l15, _ := strconv.ParseFloat(f[2], 64)
			res["load"] = map[string]interface{}{"l1": l1, "l5": l5, "l15": l15}
		}
	}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		if f := strings.Fields(string(b)); len(f) >= 1 {
			if u, e := strconv.ParseFloat(f[0], 64); e == nil {
				res["uptime_s"] = int64(u)
			}
		}
	}

	// EDAC correctable/uncorrectable (authoritative)
	ce, ue, byCtl := readEdac()
	res["edac"] = map[string]interface{}{"ce_total": ce, "ue_total": ue, "by_controller": byCtl}

	// AER cumulative (best-effort from the pcie-aer log)
	if a := readIntTail("/var/log/pcie-aer.tsv"); a >= 0 {
		res["aer"] = map[string]interface{}{"correctable": a, "fatal": 0}
	}

	// CPU socket temps (from gpu-sensors log)
	fillCpuTemps(res)

	// Uptime / crash history: reboot events + last-words snapshots
	fillReboots(res)
	fillLastWords(res)

	// Trend history (best-effort from crash-hunter log)
	res["history"] = healthHistory()

	return res
}

var isoTS = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:[+-]\d{2}:\d{2}|Z)?`)

// fillReboots parses `last -x reboot --time-format iso` for the boot history
// (each line = a boot event; the first is the current session). Best-effort.
func fillReboots(res map[string]interface{}) {
	out, err := exec.Command("last", "-x", "reboot", "--time-format", "iso").Output()
	if err != nil {
		return
	}
	layouts := []string{"2006-01-02T15:04:05-07:00", "2006-01-02T15:04:05Z07:00", "2006-01-02T15:04:05"}
	reboots := []map[string]interface{}{}
	for _, line := range strings.Split(string(out), "\n") {
		m := isoTS.FindString(line)
		if m == "" {
			continue
		}
		var ts int64
		for _, l := range layouts {
			if t, e := time.Parse(l, m); e == nil {
				ts = t.Unix()
				break
			}
		}
		reboots = append(reboots, map[string]interface{}{"ts": ts, "iso": m})
		if len(reboots) >= 12 {
			break
		}
	}
	res["reboots"] = reboots
	res["reboot_count"] = len(reboots)
}

// fillLastWords lists the most recent /var/log/last-words snapshots (the
// last-words service writes a full system snapshot every ~5s; the one just
// before a reboot gap is the "last words" before a crash).
func fillLastWords(res map[string]interface{}) {
	dir := "/var/log/last-words"
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type lw struct {
		name  string
		mtime int64
		size  int64
	}
	var lws []lw
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		if fi, err := e.Info(); err == nil {
			lws = append(lws, lw{e.Name(), fi.ModTime().Unix(), fi.Size()})
		}
	}
	sort.Slice(lws, func(i, j int) bool { return lws[i].mtime > lws[j].mtime })
	if len(lws) > 10 {
		lws = lws[:10]
	}
	out := make([]map[string]interface{}, len(lws))
	for i, l := range lws {
		out[i] = map[string]interface{}{"name": l.name, "mtime": l.mtime, "size": l.size}
	}
	res["last_words"] = out
}


func readMeminfo() map[string]int64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return nil
	}
	m := map[string]int64{}
	for _, line := range strings.Split(string(b), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSuffix(parts[0], ":")
		if v, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
			m[key] = v
		}
	}
	return m
}

func readEdac() (int, int, []int) {
	base := "/sys/devices/system/edac/mc"
	entries, err := os.ReadDir(base)
	if err != nil {
		return 0, 0, []int{}
	}
	ceTotal, ueTotal := 0, 0
	byCtl := []int{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ce := readIntFile(filepath.Join(base, e.Name(), "ce_count"))
		ue := readIntFile(filepath.Join(base, e.Name(), "ue_count"))
		ceTotal += ce
		ueTotal += ue
		byCtl = append(byCtl, ce)
	}
	return ceTotal, ueTotal, byCtl
}

func readIntFile(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	v, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return v
}

// readIntTail returns the last numeric field of the last non-empty line, or -1.
func readIntTail(path string) int64 {
	lines, err := tailLines(path, 1)
	if err != nil || len(lines) == 0 {
		return -1
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 2 {
		return -1
	}
	v, err := strconv.ParseInt(fields[len(fields)-1], 10, 64)
	if err != nil {
		return -1
	}
	return v
}

// fillCpuTemps reads the most recent gpu-sensors line for CPU socket max temps
// (field order: epoch sock0_maxC sock1_maxC load ...). This logger escapes tabs
// as literal backslash-t, so normalize before splitting.
func fillCpuTemps(res map[string]interface{}) {
	lines, err := tailLines("/var/log/gpu-sensors.tsv", 2)
	if err != nil || len(lines) == 0 {
		return
	}
	vals := strings.Fields(strings.ReplaceAll(lines[len(lines)-1], `\t`, " "))
	if len(vals) < 3 {
		return
	}
	s0, _ := strconv.ParseFloat(vals[1], 64)
	s1, _ := strconv.ParseFloat(vals[2], 64)
	if cpu, ok := res["cpu"].(map[string]interface{}); ok {
		cpu["sock0_c"] = round1(s0)
		cpu["sock1_c"] = round1(s1)
	}
}

// healthHistory best-effort parses the crash-hunter log for a RAM%/EDAC-CE
// trend. The log format has drifted, so this is positional and tolerant:
// a "RAM line" is a line whose first field is a unix timestamp and that has
// >= 15 fields (epoch mem_total mem_avail mem_used swap oom hpf hpt
// ce0 ue0 ce1 ue1 ce2 ue2 ce3 ue3 mce aer ...).
func healthHistory() []map[string]interface{} {
	lines, err := tailLines("/var/log/crash-hunter.tsv", 320)
	if err != nil {
		return []map[string]interface{}{}
	}
	hist := []map[string]interface{}{}
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) < 15 {
			continue
		}
		epoch, _ := strconv.ParseFloat(f[0], 64)
		if epoch < 1_000_000_000 {
			continue
		}
		mtot, _ := strconv.ParseFloat(f[1], 64)
		mused, _ := strconv.ParseFloat(f[3], 64)
		pct := 0.0
		if mtot > 0 {
			pct = round1(mused / mtot * 100)
		}
		ce := 0
		for _, idx := range []int{8, 10, 12, 14} {
			if idx < len(f) {
				ce += atoi(f[idx])
			}
		}
		hist = append(hist, map[string]interface{}{"ts": epoch, "ram_pct": pct, "edac_ce": ce})
	}
	return hist
}

func atoi(s string) int {
	v, _ := strconv.Atoi(s)
	return v
}

// tailLines returns the last n non-empty lines of path without reading the
// whole file (seeks near the end). Used on multi-MB log files.
func tailLines(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	const chunk = 512 * 1024
	off := int64(0)
	if fi.Size() > chunk {
		off = fi.Size() - chunk
	}
	buf := make([]byte, fi.Size()-off)
	if n, err := f.ReadAt(buf, off); err != nil && n == 0 {
		return nil, err
	}
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(string(buf), "\r", ""), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out, nil
}
