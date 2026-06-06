package status

import (
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"aihub/internal/sh"
)

// round1 mirrors Python's round(x, 1).
func round1(x float64) float64 { return math.Round(x*10) / 10 }

func avg(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

// CpuStats mirrors app.py cpu_stats():
//   - /proc/loadavg -> load1/5/15 (any error -> 0; no psutil dep)
//   - runtime.NumCPU() for cpu_count
//   - grep ^cpu /proc/stat -> per-core busy %, split in half into
//     socket0/socket1 (rc != 0 -> 0,0)
//   - /proc/meminfo -> mem_total (MT) / mem_used (MT - MemAvailable, MB)
func CpuStats() CPU {
	load1, load5, load15 := 0.0, 0.0, 0.0
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		f := strings.Fields(string(data))
		if len(f) >= 3 {
			l1, e1 := strconv.ParseFloat(f[0], 64)
			l2, e2 := strconv.ParseFloat(f[1], 64)
			l3, e3 := strconv.ParseFloat(f[2], 64)
			if e1 == nil && e2 == nil && e3 == nil {
				load1, load5, load15 = l1, l2, l3
			}
		}
	}

	cpuCount := runtime.NumCPU()

	socket0, socket1 := 0.0, 0.0
	rc, out := sh.Run([]string{"grep", "^cpu", "/proc/stat"}, 5*time.Second)
	if rc == 0 {
		per := []float64{}
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 8 || fields[0] == "cpu" {
				continue
			}
			v := make([]int, 7)
			ok := true
			for i := 0; i < 7; i++ {
				n, err := strconv.Atoi(fields[i+1])
				if err != nil {
					ok = false
					break
				}
				v[i] = n
			}
			if !ok {
				continue
			}
			tot := 0
			for _, n := range v {
				tot += n
			}
			idle := v[3]
			per = append(per, round1(100*(1-float64(idle)/float64(max(tot, 1)))))
		}
		half := len(per) / 2
		if half > 0 {
			socket0 = round1(avg(per[:half]))
		}
		if len(per)-half > 0 {
			socket1 = round1(avg(per[half:]))
		}
	}

	mt, mu := 0, 0
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				if f := strings.Fields(line); len(f) > 1 {
					if kb, err := strconv.Atoi(f[1]); err == nil {
						mt = kb / 1024
					}
				}
			} else if strings.HasPrefix(line, "MemAvailable:") {
				if f := strings.Fields(line); len(f) > 1 {
					if kb, err := strconv.Atoi(f[1]); err == nil {
						mu = mt - kb/1024
					}
				}
			}
		}
	}

	return CPU{
		Load1:    load1,
		Load5:    load5,
		Load15:   load15,
		CPUCount: cpuCount,
		Socket0:  socket0,
		Socket1:  socket1,
		MemTotal: mt,
		MemUsed:  mu,
	}
}
