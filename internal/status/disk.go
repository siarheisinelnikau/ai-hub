package status

import (
	"fmt"
	"strings"
	"time"

	"aihub/internal/sh"
)

// fmtHuman mirrors app.py disk_usage()'s fmt(): B/K/M/G/T ladder,
// then a .1f P fallback (only reachable at >=1024 TiB).
func fmtHuman(b int64) string {
	x := float64(b)
	for _, unit := range []string{"B", "K", "M", "G", "T"} {
		if x < 1024 {
			return fmt.Sprintf("%d%s", int64(x), unit)
		}
		x /= 1024
	}
	return fmt.Sprintf("%.1fP", x)
}

// win32Root fills d's root-disk fields from win32DiskUsage("C:\"):
// fmtHuman on success, "—" if the call failed (all zero).
func win32Root(d *Disk) {
	total, used, free := win32DiskUsage("C:\\")
	if total == 0 && used == 0 && free == 0 {
		d.DiskTotal, d.DiskUsed, d.DiskFree = "—", "—", "—"
		return
	}
	d.DiskTotal = fmtHuman(total)
	d.DiskUsed = fmtHuman(used)
	d.DiskFree = fmtHuman(free)
}

// DiskUsage mirrors app.py disk_usage() exactly, on ALL platforms:
//   - sh(["df","-h","/"],10); rc==0 → lines[1].split() →
//     disk_total=p[1], disk_used=p[2], disk_free=p[3] (guarded by len, "—" default).
//     NOTE: on Windows with Git for Windows, df exists and its Filesystem
//     column is the CWD label ("C:/Program Files/Git"), so the split yields
//     disk_total="Files/Git" — Python produces the SAME value, so we must
//     NOT "fix" this or parity breaks.
//   - rc!=0 → app.py's shutil.disk_usage("/") fallback: win32 GetDiskFreeSpaceExW
//     on Windows (fmtHuman), "—" via stub elsewhere.
//   - du -sh for hf_cache/models_optim/models_local/comfy_models,
//     "—" on any failure (du absent on this platform)
func DiskUsage() Disk {
	d := Disk{}
	rc, out := sh.Run([]string{"df", "-h", "/"}, 10*time.Second)
	if rc == 0 {
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) >= 2 {
			p := strings.Fields(lines[1])
			if len(p) > 1 {
				d.DiskTotal = p[1]
			} else {
				d.DiskTotal = "—"
			}
			if len(p) > 2 {
				d.DiskUsed = p[2]
			} else {
				d.DiskUsed = "—"
			}
			if len(p) > 3 {
				d.DiskFree = p[3]
			} else {
				d.DiskFree = "—"
			}
		}
	} else {
		win32Root(&d)
	}

	entries := []struct {
		path  string
		field *string
	}{
		{"/home/user/.cache/huggingface", &d.HFCache},
		{"/home/user/models-optim", &d.ModelsOptim},
		{"/home/user/models", &d.ModelsLocal},
		{"/srv/ai/models", &d.ComfyModels},
	}
	for _, e := range entries {
		rc2, out2 := sh.Run([]string{"du", "-sh", e.path}, 10*time.Second)
		if rc2 == 0 && strings.TrimSpace(out2) != "" {
			first := strings.SplitN(out2, "\n", 2)[0]
			*e.field = strings.SplitN(first, "\t", 2)[0]
		} else {
			*e.field = "—"
		}
	}

	return d
}
