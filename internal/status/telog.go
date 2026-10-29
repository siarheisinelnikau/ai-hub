package status

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// TelogPath is the long-term, disk-backed GPU telemetry log (append-only TSV).
// Bounded by logrotate (like the other /var/log monitoring logs). Go-only —
// never part of the /api/status snapshot, so the parity JSON shape is untouched.
const TelogPath = "/var/log/ai-hub-gpu.tsv"

// TelogRow is one parsed telemetry sample. Per-GPU arrays are indexed by
// nvidia-smi index; -1 = no reading for that GPU.
type TelogRow struct {
	TS    float64
	Core  []int
	Vram  []int
	Power []int
	Util  []int
	Mem   []int // MiB
}

// AppendGpu writes one sample line. Best-effort: silently skips if the path is
// not writable (e.g. non-root in tests).
func AppendGpu(s GpuSample) {
	line := fmt.Sprintf("%.0f\t%s\t%s\t%s\t%s\t%s\n",
		s.TS, joinN(s.Gpu), joinN(s.Vram), joinN(s.Power), joinN(s.Util), joinN(s.Mem))
	f, err := os.OpenFile(TelogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() == 0 {
		f.WriteString("ts\tcore\tvram\tpower\tutil\tmem\n")
	}
	f.WriteString(line)
}

func joinN(a []int) string {
	if len(a) == 0 {
		return ""
	}
	parts := make([]string, len(a))
	for i, v := range a {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ",")
}

// ReadTelog returns the samples with TS >= cutoff, oldest→newest. Returns nil
// if the log does not exist yet (vs an empty slice for "exists, no matches").
func ReadTelog(cutoff float64) []TelogRow {
	f, err := os.Open(TelogPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	var out []TelogRow
	for sc.Scan() {
		fields := strings.Split(sc.Text(), "\t")
		if len(fields) < 6 {
			continue
		}
		ts, err := strconv.ParseFloat(fields[0], 64)
		if err != nil || ts < cutoff {
			continue
		}
		out = append(out, TelogRow{
			TS:    ts,
			Core:  parseN(fields[1]),
			Vram:  parseN(fields[2]),
			Power: parseN(fields[3]),
			Util:  parseN(fields[4]),
			Mem:   parseN(fields[5]),
		})
	}
	return out
}

func parseN(s string) []int {
	if s == "" {
		return []int{}
	}
	parts := strings.Split(s, ",")
	out := make([]int, len(parts))
	for i, p := range parts {
		v, _ := strconv.Atoi(p)
		out[i] = v
	}
	return out
}
