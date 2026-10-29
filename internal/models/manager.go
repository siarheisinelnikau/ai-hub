package models

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// State is the download state for a single model, persisted in the manifest so
// it survives reboots (Resume() re-launches anything still "downloading").
type State struct {
	ID         string `json:"id"`
	Status     string `json:"status"` // "idle" | "downloading" | "complete" | "error"
	Started    int64  `json:"started"`
	Updated    int64  `json:"updated"`
	PID        int    `json:"pid,omitempty"`
	Error      string `json:"error,omitempty"`
	TotalBytes int64  `json:"total_bytes,omitempty"` // progress denominator (HF API)
}

// DL describes the live download status for the UI. The progress fields are
// only set while status == "downloading".
type DL struct {
	Status       string  `json:"status"`
	Updated      int64   `json:"updated"`
	Error        string  `json:"error,omitempty"`
	DownloadedGB float64 `json:"downloaded_gb,omitempty"`
	TotalGB      float64 `json:"total_gb,omitempty"`
	Pct          float64 `json:"pct,omitempty"`
	SpeedMBps    float64 `json:"speed_mbps,omitempty"`
}

// Connection is the hub-connection status for a model: which serving
// touchpoints are present (registry entry, systemd service, Open WebUI URL).
// Filled by the server (it has the registry + filesystem access); Notes lists
// what is missing so the UI can show a remediation hint.
type Connection struct {
	Registry     bool     `json:"registry"`
	Service      bool     `json:"service"`
	WebUI        bool     `json:"webui"`
	AllConnected bool     `json:"all_connected"`
	Notes        []string `json:"notes,omitempty"`
}

// Status is one row of the model inventory.
type Status struct {
	ID         string     `json:"id"`
	Short      string     `json:"short"`
	Backend    string     `json:"backend"`
	Quant      string     `json:"quant"`
	Port       int        `json:"port"`
	HFRepo     string     `json:"hf_repo"`
	LocalPath  string     `json:"local_path"`
	OnDisk     bool       `json:"on_disk"`
	SizeGB     float64    `json:"size_gb"`
	Download   DL         `json:"download"`
	Connection Connection `json:"connection"`
}

var (
	mu sync.Mutex

	// runDownload launches a download for (repo, localDir, files) and blocks
	// until it finishes, returning the process exit error. Overridable in tests.
	runDownload = func(repo, localDir, logFile string, files []string) error {
		if localDir == "/home/user/models/GLM-5.3-Flash-Maya-M" || localDir == "/home/user/models/GLM-5.3-Flash-Maya-M-Derisked" {
			f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				return err
			}
			defer f.Close()
			installer := "/srv/ai-hub/maya/install.py"
			if strings.HasSuffix(localDir, "-Derisked") {
				installer = "/srv/ai-hub/maya/install_derisked.py"
			}
			cmd := exec.Command("/usr/bin/python3", installer)
			cmd.Stdout, cmd.Stderr = f, f
			return cmd.Run()
		}
		if localDir == "/home/user/models/Nemotron-3.5-Super-VL-120B-Q4_K_M" || localDir == "/home/user/models/Nemotron-3.5-Super-VL-120B-NVFP4" || localDir == "/home/user/models/Nemotron-3.5-Super-VL-120B-Uncensored-AD-Q4_K_S" {
			f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				return err
			}
			defer f.Close()
			installer := "/srv/ai-hub/tools/install_nemotron35.py"
			if strings.HasSuffix(localDir, "-NVFP4") {
				installer = "/srv/ai-hub/tools/install_nemotron35_nvfp4.py"
			} else if strings.HasSuffix(localDir, "-Uncensored-AD-Q4_K_S") {
				installer = "/srv/ai-hub/tools/install_nemotron35_uncensored.py"
			}
			cmd := exec.Command("/usr/bin/python3", installer)
			cmd.Stdout, cmd.Stderr = f, f
			return cmd.Run()
		}
		if localDir == "/home/user/models/Strata-uncensored-iq4-data" {
			f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				return err
			}
			defer f.Close()
			cmd := exec.Command("/srv/ai/strata/.venv/bin/python", "/srv/ai-hub/strata/install_uncensored_iq4.py")
			cmd.Dir = "/srv/ai/strata"
			cmd.Stdout, cmd.Stderr = f, f
			return cmd.Run()
		}
		if localDir == "/home/user/models/Qwen3.5-122B-A10B-Uncensored-HauhauCS" {
			f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				return err
			}
			defer f.Close()
			cmd := exec.Command("/srv/ai/strata/.venv/bin/python", "/srv/ai-hub/tools/install_qwen122_mtp.py")
			cmd.Stdout, cmd.Stderr = f, f
			return cmd.Run()
		}
		if localDir == "/home/user/models/Strata-data" || localDir == "/home/user/models/Strata-iq3-data" {
			f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				return err
			}
			defer f.Close()
			setupArgs, quant := strataSetupArgs(localDir)
			cmd := exec.Command("/srv/ai/strata/setup.sh", setupArgs...)
			cmd.Dir = "/srv/ai/strata"
			// Setup remembers one data directory and migrates previous downloads to it.
			// Keep installer settings separate from runtime configuration.
			cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME=/srv/ai/strata/setup-state/"+quant)
			mtp := filepath.Join(localDir, "mtp")
			if _, err := os.Lstat(mtp); os.IsNotExist(err) {
				if _, sharedErr := os.Stat("/home/user/models/Strata-shared/mtp"); sharedErr == nil {
					if err := os.MkdirAll(localDir, 0o755); err != nil {
						return err
					}
					if err := os.Symlink("/home/user/models/Strata-shared/mtp", mtp); err != nil {
						return err
					}
				}
			}
			cmd.Stdout, cmd.Stderr = f, f
			return cmd.Run()
		}
		killStuck(localDir)
		cleanStaleIncomplete(localDir)
		var logf *os.File
		if f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			logf = f
			defer logf.Close()
		}
		run := func(cmd *exec.Cmd) error {
			if logf != nil {
				cmd.Stdout = logf
				cmd.Stderr = logf
			}
			return cmd.Run()
		}
		if _, err := exec.LookPath("aria2c"); err == nil {
			// aria2c -c resumes partial files across hub restarts. `hf` 1.x
			// cannot: it downloads to a process-unique *.incomplete and deletes
			// it on failure (huggingface_hub file_download.py), so every restart
			// used to start the file from zero.
			for _, file := range files {
				cmd := exec.Command("aria2c",
					"--continue=true",
					"--max-connection-per-server=8",
					"--split=8",
					"--min-split-size=16M",
					"--auto-file-renaming=false",
					"--allow-overwrite=true",
					"--file-allocation=none",
					"--console-log-level=warn",
					"--summary-interval=10",
					"--dir", localDir,
					"--out", file,
					hfFileURL(repo, file),
				)
				if err := run(cmd); err != nil {
					return fmt.Errorf("aria2c %s: %w", file, err)
				}
			}
			return nil
		}
		// Fallback when aria2c is not installed.
		args := append([]string{"download", repo}, files...)
		args = append(args, "--local-dir", localDir)
		return run(exec.Command(downloadCmd(), args...))
	}
)

func downloadCmd() string {
	if v := os.Getenv("AIHUB_DOWNLOAD_CMD"); v != "" {
		return v
	}
	return "hf"
}

func strataSetupArgs(localDir string) ([]string, string) {
	family, quant, port := "unsloth", "UD-IQ4_XS", "18029"
	if localDir == "/home/user/models/Strata-iq3-data" {
		// The setup CLI calls the ISTA-DASLab/GSQ-RCO family "qwen".
		family, quant, port = "qwen", "IQ3_XXS", "18033"
	}
	return []string{"--yes", "--family", family, "--model", quant, "--data-dir", localDir, "--context", "262144", "--gpus", "0,1,2,3", "--low-ram", "off", "--vision", "no", "--port", port, "--no-browser", "--no-start"}, quant
}

// hfFileURL builds the public resolve URL for a repo file, escaping each path
// segment (HF serves subdirectory files at the same path).
func hfFileURL(repo, file string) string {
	segs := strings.Split(file, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return "https://huggingface.co/" + repo + "/resolve/main/" + strings.Join(segs, "/")
}

// killStuck best-effort kills any `hf download` whose command line references
// localDir. A stuck download holds the HuggingFace lock and would block a fresh
// or resumed download for the same model. pkill exit 1 (no match) is ignored.
func killStuck(localDir string) {
	if localDir == "" {
		return
	}
	_ = exec.Command("pkill", "-f", localDir).Run()
}

// cleanStaleIncomplete removes *.incomplete files left behind by earlier killed
// `hf` runs. huggingface_hub creates a new random-suffixed temp file per run
// and does not resume another run's partial, so stale ones only waste disk and
// inflate the progress probe (the dir grew past the expected total while the
// percentage pinned at 100%).
func cleanStaleIncomplete(localDir string) {
	if localDir == "" {
		return
	}
	_ = filepath.WalkDir(localDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".incomplete") {
			_ = os.Remove(p)
		}
		return nil
	})
}

// downloadBytes is the progress numerator: all completed files in the target
// dir plus the current (largest) .incomplete file; smaller stale leftovers are
// ignored so the numbers stay honest even if one was not cleaned up yet.
func downloadBytes(path string) int64 {
	if path == "" {
		return 0
	}
	var done, biggest int64
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".incomplete") {
			if info.Size() > biggest {
				biggest = info.Size()
			}
			return nil
		}
		done += info.Size()
		return nil
	})
	return done + biggest
}

func ensureDir() {
	_ = os.MkdirAll(downloadsDir(), 0o755)
	_ = os.MkdirAll("/srv/ai-hub", 0o755) // no-op if not present
}

// manifest I/O — all file access is under mu (atomic read-modify-write).
func loadStatesLocked() map[string]State {
	st := map[string]State{}
	data, err := os.ReadFile(manifestPath())
	if err != nil {
		return st
	}
	_ = json.Unmarshal(data, &st)
	return st
}

func saveStatesLocked(m map[string]State) {
	ensureDir()
	data, _ := json.MarshalIndent(m, "", "  ")
	_ = os.WriteFile(manifestPath(), data, 0o644)
}

// stateGet returns the state for id (idle if absent).
func stateGet(id string) State {
	mu.Lock()
	defer mu.Unlock()
	m := loadStatesLocked()
	s, ok := m[id]
	if !ok {
		s = State{ID: id, Status: "idle"}
	}
	return s
}

// stateSet stores s.
func stateSet(s State) {
	mu.Lock()
	defer mu.Unlock()
	m := loadStatesLocked()
	m[s.ID] = s
	saveStatesLocked(m)
}

// List returns the full inventory (catalog + on-disk + size + download state).
func List() []Status {
	out := make([]Status, 0, len(IDs()))
	for _, id := range IDs() {
		info := Catalog[id]
		st := stateGet(id)
		_, size, _ := probeDir(info.LocalPath)
		onDisk := Ready(id) && st.Status != "downloading"
		dl := DL{Status: st.Status, Updated: st.Updated, Error: st.Error}
		if st.Status == "downloading" {
			bytes := downloadBytes(info.LocalPath)
			size = round1(float64(bytes) / (1024 * 1024 * 1024))
			dl.DownloadedGB = size
			if st.TotalBytes > 0 {
				pct := float64(bytes) / float64(st.TotalBytes) * 100
				if pct > 100 {
					pct = 100
				}
				dl.TotalGB = round1(float64(st.TotalBytes) / (1024 * 1024 * 1024))
				dl.Pct = round1(pct)
			}
			dl.SpeedMBps = round1(probeSpeed(id, bytes))
		}
		out = append(out, Status{
			ID: id, Short: info.Short, Backend: info.Backend, Quant: info.Quant, Port: info.Port,
			HFRepo: info.HFRepo, LocalPath: info.LocalPath,
			OnDisk: onDisk, SizeGB: size,
			Download: dl,
		})
	}
	return out
}

// IsDownloading reports whether the model's files are still being fetched.
// The server hides such models from the main /api/status list (the card would
// be dead: no files, cannot start) while 📦 Model manager shows the progress.
func IsDownloading(id string) bool {
	return stateGet(id).Status == "downloading"
}

type probeSample struct {
	bytes int64
	at    time.Time
}

var (
	probeMu   sync.Mutex
	probePrev = map[string]probeSample{}
)

// probeSpeed returns the download rate (MB/s) since the previous List() call.
func probeSpeed(id string, bytes int64) float64 {
	now := time.Now()
	probeMu.Lock()
	defer probeMu.Unlock()
	prev, ok := probePrev[id]
	probePrev[id] = probeSample{bytes: bytes, at: now}
	if !ok {
		return 0
	}
	dt := now.Sub(prev.at).Seconds()
	if dt < 0.5 {
		return 0
	}
	delta := bytes - prev.bytes
	if delta < 0 {
		delta = 0
	}
	return float64(delta) / dt / 1e6
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

// probeDir reports whether dir exists and has content, its size in GB and its
// exact byte count (in-progress .cache files included).
func probeDir(path string) (bool, float64, int64) {
	if path == "" {
		return false, 0, 0
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.IsDir() {
		return false, 0, 0
	}
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if !d.IsDir() {
			if info, e2 := d.Info(); e2 == nil {
				total += info.Size()
			}
		}
		return nil
	})
	gb := float64(total) / (1024 * 1024 * 1024)
	// round to 1 decimal for display
	gb = float64(int(gb*10)) / 10
	return total > 0, gb, total
}

// Download starts (or confirms an in-flight) resumable download for a model.
// Resuming: re-running `hf download <repo> --local-dir <path>` picks up where a
// previous run left off (partially-downloaded files are completed), so this is
// the same call whether the download is new or resuming after a reboot.
func Download(id string) (State, error) {
	repo, err := repoFor(id)
	if err != nil {
		return State{}, err
	}
	if repo == "" {
		return State{}, fmt.Errorf("%s has no plain-HF download source (image-based)", id)
	}
	cur := stateGet(id)
	if cur.Status == "downloading" {
		return cur, nil // already running
	}
	now := time.Now().Unix()
	total := hfTotalBytes(repo, Catalog[id].Files)
	if extra := Catalog[id]; extra.ExtraRepo != "" {
		total += hfTotalBytes(extra.ExtraRepo, extra.ExtraFiles)
	}
	if total == 0 && cur.TotalBytes > 0 {
		total = cur.TotalBytes
	}
	st := State{ID: id, Status: "downloading", Started: cur.Started, Updated: now, TotalBytes: total}
	if cur.Started == 0 {
		st.Started = now
	}
	stateSet(st)

	ensureDir()
	go func() {
		err := runModelDownload(Catalog[id])
		s := stateGet(id)
		s.Updated = time.Now().Unix()
		if err != nil {
			s.Status = "error"
			s.Error = strings.TrimSpace(err.Error())
		} else {
			s.Status = "complete"
			s.Error = ""
		}
		stateSet(s)
	}()
	return st, nil
}

// Resume re-launches any download still marked "downloading" (e.g. the process
// died on a reboot). Returns how many were (re)started. Called at server start
// so a reboot does not leave a half-finished model stuck.
func Resume() int {
	// Decide what to relaunch (under the lock), then launch outside the lock so
	// runDownload can run and update state without blocking the manifest.
	var toRelaunch []string
	mu.Lock()
	m := loadStatesLocked()
	for id, s := range m {
		if s.Status != "downloading" {
			continue
		}
		repo, err := repoFor(id)
		if err != nil || repo == "" {
			s.Status = "error"
			if err != nil {
				s.Error = err.Error()
			} else {
				s.Error = "no HF source"
			}
			s.Updated = time.Now().Unix()
			m[id] = s
			continue
		}
		s.Updated = time.Now().Unix()
		m[id] = s
		toRelaunch = append(toRelaunch, id)
	}
	if len(toRelaunch) > 0 {
		saveStatesLocked(m)
	}
	mu.Unlock()

	for _, id := range toRelaunch {
		go func(id string) {
			err := runModelDownload(Catalog[id])
			mu.Lock()
			m2 := loadStatesLocked()
			if cur, ok := m2[id]; ok {
				cur.Updated = time.Now().Unix()
				if err != nil {
					cur.Status = "error"
					cur.Error = strings.TrimSpace(err.Error())
				} else {
					cur.Status = "complete"
					cur.Error = ""
				}
				m2[id] = cur
				saveStatesLocked(m2)
			}
			mu.Unlock()
		}(id)
	}
	return len(toRelaunch)
}

func runModelDownload(info Info) error {
	if err := runDownload(info.HFRepo, info.LocalPath, logPath(info.ID), info.Files); err != nil {
		return err
	}
	if info.ExtraRepo != "" {
		return runDownload(info.ExtraRepo, info.LocalPath, logPath(info.ID), info.ExtraFiles)
	}
	return nil
}

// hfTotalBytes asks the HuggingFace API for the total size of the files a
// download will fetch (progress denominator). Best-effort: 0 on any error.
func hfTotalBytes(repo string, files []string) int64 {
	if repo == "" {
		return 0
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get("https://huggingface.co/api/models/" + repo + "?blobs=true")
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return 0
	}
	var d struct {
		Siblings []struct {
			Name string `json:"rfilename"`
			Size int64  `json:"size"`
		} `json:"siblings"`
	}
	if json.Unmarshal(body, &d) != nil {
		return 0
	}
	want := map[string]bool{}
	for _, f := range files {
		want[f] = true
	}
	var total int64
	for _, s := range d.Siblings {
		if len(want) == 0 || want[s.Name] {
			total += s.Size
		}
	}
	return total
}

// Clear resets a model's download state (e.g. after the files were deleted).
func Clear(id string) error {
	mu.Lock()
	defer mu.Unlock()
	m := loadStatesLocked()
	if s, ok := m[id]; ok {
		s.Status = "idle"
		s.Error = ""
		s.Updated = time.Now().Unix()
		m[id] = s
		saveStatesLocked(m)
	}
	return nil
}
