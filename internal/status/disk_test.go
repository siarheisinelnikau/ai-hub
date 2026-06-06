package status

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"aihub/internal/sh"
)

// expectedRootDisk replicates app.py disk_usage() root-disk logic (the parity
// target): df -h / first; rc==0 → lines[1].split() → p[1],p[2],p[3];
// rc!=0 → win32 GetDiskFreeSpaceExW formatted. On Windows-with-Git the df
// label contains a space ("C:/Program Files/Git") so p[1] is "Files/Git" —
// Python returns that same value, so parity means we must too.
func expectedRootDisk(t *testing.T) (total, used, free string) {
	t.Helper()
	rc, out := sh.Run([]string{"df", "-h", "/"}, 10*time.Second)
	if rc == 0 {
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) >= 2 {
			p := strings.Fields(lines[1])
			total, used, free = "—", "—", "—"
			if len(p) > 1 {
				total = p[1]
			}
			if len(p) > 2 {
				used = p[2]
			}
			if len(p) > 3 {
				free = p[3]
			}
			return
		}
	}
	// df unavailable → win32 fallback (same as Go's win32Root).
	tt, uu, ff := win32DiskUsage("C:\\")
	if tt == 0 && uu == 0 && ff == 0 {
		return "—", "—", "—"
	}
	return fmtHuman(tt), fmtHuman(uu), fmtHuman(ff)
}

func TestDiskUsageWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("not windows")
	}
	d := DiskUsage()
	wantT, wantU, wantF := expectedRootDisk(t)
	if d.DiskTotal != wantT {
		t.Errorf("DiskTotal = %q, want %q (Python parity)", d.DiskTotal, wantT)
	}
	if d.DiskUsed != wantU {
		t.Errorf("DiskUsed = %q, want %q (Python parity)", d.DiskUsed, wantU)
	}
	if d.DiskFree != wantF {
		t.Errorf("DiskFree = %q, want %q (Python parity)", d.DiskFree, wantF)
	}
	for name, got := range map[string]string{
		"HFCache":     d.HFCache,
		"ModelsOptim": d.ModelsOptim,
		"ModelsLocal": d.ModelsLocal,
		"ComfyModels": d.ComfyModels,
	} {
		if got != "—" {
			t.Errorf("%s = %q, want — (du unavailable on windows)", name, got)
		}
	}
}

func TestFmtHuman(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{500, "500B"},
		{1023, "1023B"},
		{1024, "1K"},
		{1024 * 1024, "1M"},
		{1024 * 1024 * 1024, "1G"},
		{1024 * 1024 * 1024 * 1024, "1T"},
	}
	for _, c := range cases {
		if got := fmtHuman(c.in); got != c.want {
			t.Errorf("fmtHuman(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
