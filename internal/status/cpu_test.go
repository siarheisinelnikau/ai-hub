package status

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func TestCpuStatsWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("not windows")
	}
	c := CpuStats()
	if c.Load1 != 0 || c.Load5 != 0 || c.Load15 != 0 {
		t.Errorf("loads = %v/%v/%v, want 0/0/0 (no /proc on windows)", c.Load1, c.Load5, c.Load15)
	}
	if c.Socket0 != 0 || c.Socket1 != 0 {
		t.Errorf("sockets = %v/%v, want 0/0", c.Socket0, c.Socket1)
	}
	if c.MemTotal != 0 || c.MemUsed != 0 {
		t.Errorf("mem = %v/%v, want 0/0", c.MemTotal, c.MemUsed)
	}
	if c.CPUCount <= 0 {
		t.Errorf("CPUCount = %d, want > 0", c.CPUCount)
	}
}

func TestCpuStatsJSON(t *testing.T) {
	c := CpuStats()
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("json.Marshal(CPU) error: %v", err)
	}
	s := string(b)
	for _, key := range []string{`"cpu_count"`, `"load1"`, `"socket0"`, `"mem_total"`} {
		if !strings.Contains(s, key) {
			t.Errorf("marshal = %s, missing %s", s, key)
		}
	}
}
