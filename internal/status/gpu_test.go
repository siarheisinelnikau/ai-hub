package status

import (
	"reflect"
	"runtime"
	"testing"
)

func TestParseGPUCSV(t *testing.T) {
	in := "0, NVIDIA GeForce RTX 3090, 8704, 24576, 42, 45, 30.42\n" +
		"1, NVIDIA GeForce RTX 3090, 0, 24576, 0, 44, [N/A]\n" +
		"bad\n" +
		"2, GPU X, 100.9, 24576, 5, 50, 100\n"

	got := ParseGPUCSV(in)
	want := []GPU{
		{Index: 0, Name: "NVIDIA GeForce RTX 3090", MemUsed: 8704, MemTotal: 24576, Util: 42, Temp: 45, Power: 30},
		{Index: 2, Name: "GPU X", MemUsed: 100, MemTotal: 24576, Util: 5, Temp: 50, Power: 100},
	}
	if len(got) != len(want) {
		t.Fatalf("len(ParseGPUCSV) = %d, want %d (got = %+v)", len(got), len(want), got)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("gpu[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseGPUCSVEmpty(t *testing.T) {
	got := ParseGPUCSV("")
	if got == nil {
		t.Fatalf("ParseGPUCSV(\"\") = nil, want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("len(ParseGPUCSV(\"\")) = %d, want 0", len(got))
	}
}

func TestGpuStatsWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("not windows")
	}
	want := []GPU{
		{Index: 0, Name: "RTX 3090 (mock)", MemUsed: 0, MemTotal: 24576, Util: 0, Temp: 45, Power: 30},
		{Index: 1, Name: "RTX 3090 (mock)", MemUsed: 0, MemTotal: 24576, Util: 0, Temp: 44, Power: 28},
		{Index: 2, Name: "RTX 3090 (mock)", MemUsed: 0, MemTotal: 24576, Util: 0, Temp: 43, Power: 27},
		{Index: 3, Name: "RTX 3090 (mock)", MemUsed: 0, MemTotal: 24576, Util: 0, Temp: 42, Power: 26},
	}
	// The Windows fallback GpuStats() serves when nvidia-smi is unavailable
	// must be exactly the app.py mock. (On this machine nvidia-smi is
	// present and succeeds, so GpuStats() reports real GPUs instead; the
	// fallback slice is asserted via mockGPUs(), which GpuStats() returns
	// on that path.)
	g := mockGPUs()
	if len(g) != len(want) {
		t.Fatalf("len(mockGPUs) = %d, want %d (got = %+v)", len(g), len(want), g)
	}
	for i := range want {
		if !reflect.DeepEqual(g[i], want[i]) {
			t.Errorf("gpu[%d] = %+v, want %+v", i, g[i], want[i])
		}
	}

	got := GpuStats()
	if got == nil {
		t.Fatal("GpuStats() = nil, want non-nil")
	}
}
