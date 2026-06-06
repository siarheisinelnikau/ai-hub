package status

import (
	"encoding/csv"
	"io"
	"runtime"
	"strconv"
	"strings"
	"time"

	"aihub/internal/sh"
)

// intFromFloat mirrors Python's int(float(s)): parse as float, truncate to int.
func intFromFloat(s string) (int, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, err
	}
	return int(f), nil
}

// ParseGPUCSV parses nvidia-smi --format=csv,noheader,nounits output,
// mirroring app.py gpu_stats() row parsing:
//   - rows with fewer than 6 fields are skipped
//   - any parse error on the first 6 fields skips the whole row
//   - a non-blank 7th field that fails to parse skips the whole row
//     (Python: int(float(row[6])) raises and the ValueError propagates into
//     the outer try, so the row is skipped)
func ParseGPUCSV(out string) []GPU {
	gpus := []GPU{}
	r := csv.NewReader(strings.NewReader(out))
	// Python's csv.reader tolerates variable field counts; Go's default
	// (FieldsPerRecord == 0) would reject a short row after the first one.
	r.FieldsPerRecord = -1
	for {
		row, err := r.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			break // malformed CSV: stop, mirroring the outer try/except pass
		}
		if len(row) < 6 {
			continue
		}
		idx, err1 := strconv.Atoi(strings.TrimSpace(row[0]))
		name := strings.TrimSpace(row[1])
		mu, err2 := intFromFloat(row[2])
		mt, err3 := intFromFloat(row[3])
		u, err4 := intFromFloat(row[4])
		t, err5 := intFromFloat(row[5])
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil {
			continue
		}
		power := 0
		if len(row) > 6 {
			s := strings.TrimSpace(row[6])
			if s != "" {
				p, perr := intFromFloat(s)
				if perr != nil {
					continue // non-blank unparseable -> Python skips the row
				}
				power = p
			}
		}
		gpus = append(gpus, GPU{Index: idx, Name: name, MemUsed: mu, MemTotal: mt, Util: u, Temp: t, Power: power})
	}
	return gpus
}

// GpuStatsOK is like GpuStats but also reports whether the read actually
// succeeded. ok=false means "unknown" (nvidia-smi failed — driver hiccup,
// PCI issue, timeout), NOT "zero GPUs visible". A-02: the poller must not
// treat a failed telemetry read as a GPU drop (that used to trigger StopAll
// on a transient error).
func GpuStatsOK() (gpus []GPU, ok bool) {
	rc, out := sh.Run([]string{
		"nvidia-smi",
		"--query-gpu=index,name,memory.used,memory.total,utilization.gpu,temperature.gpu,power.draw",
		"--format=csv,noheader,nounits",
	}, 20*time.Second)
	if rc == 0 {
		gpus = ParseGPUCSV(out)
		fillVramTemp(gpus)
		return gpus, true
	}
	if runtime.GOOS == "windows" {
		return mockGPUs(), true // mock = a confirmed (fake) read
	}
	return []GPU{}, false
}

// GpuStats mirrors app.py gpu_stats(): real nvidia-smi on success,
// mock GPUs on Windows when nvidia-smi fails, otherwise empty.
// After a successful nvidia-smi read it best-effort enriches each GPU with its
// GDDR6/6X memory temperature (Go-only; see GPU.VramTemp).
func GpuStats() []GPU {
	g, _ := GpuStatsOK()
	return g
}

// vramTempBin is the reverse-engineered GDDR6/6X one-shot reader (see
// olealgoritme/gddr6). Its `-t` mode prints temps in nvidia-smi index order.
const vramTempBin = "/usr/local/sbin/vram-temp"

// fillVramTemp best-effort sets GPU.VramTemp from vram-temp -t. Any failure
// (tool absent, non-zero rc, count mismatch) leaves the fields at zero so they
// are omitted from the JSON — the Windows/parity path stays identical to the
// Python reference.
func fillVramTemp(gpus []GPU) {
	if len(gpus) == 0 {
		return
	}
	rc, out := sh.Run([]string{vramTempBin, "-t"}, 3 * time.Second)
	if rc != 0 {
		return
	}
	temps := parseTempFields(out)
	if len(temps) != len(gpus) {
		return
	}
	for i := range gpus {
		gpus[i].VramTemp = temps[i]
	}
}

// parseTempFields turns "70 74 76 80" (vram-temp -t) into []int; unparseable
// fields (e.g. "NA") become 0.
func parseTempFields(out string) []int {
	parts := strings.Fields(out)
	temps := make([]int, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil {
			v = 0
		}
		temps = append(temps, v)
	}
	return temps
}

// mockGPUs mirrors app.py's Windows fallback.
func mockGPUs() []GPU {
	return []GPU{
		{Index: 0, Name: "RTX 3090 (mock)", MemUsed: 0, MemTotal: 24576, Util: 0, Temp: 45, Power: 30},
		{Index: 1, Name: "RTX 3090 (mock)", MemUsed: 0, MemTotal: 24576, Util: 0, Temp: 44, Power: 28},
		{Index: 2, Name: "RTX 3090 (mock)", MemUsed: 0, MemTotal: 24576, Util: 0, Temp: 43, Power: 27},
		{Index: 3, Name: "RTX 3090 (mock)", MemUsed: 0, MemTotal: 24576, Util: 0, Temp: 42, Power: 26},
	}
}
