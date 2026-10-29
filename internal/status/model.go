package status

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"aihub/internal/registry"
	"aihub/internal/sh"
)

// Sub mirrors a registry.yaml sub-model entry (app.py passes m.get("sub", [])
// straight into JSON). HF is optional in YAML (voice subs have no hf).
type Sub struct {
	Name string  `json:"name"`
	Tag  string  `json:"tag"`
	VRAM string  `json:"vram"`
	HF   *string `json:"hf,omitempty"`
}

// ModelStatusEntry mirrors app.py model_status() output dict exactly.
// (Named ModelStatusEntry because the function ModelStatus already takes
// that name; Go type and func names share the package namespace.)
type ModelStatusEntry struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Short         string            `json:"short"`
	Category      string            `json:"category"`
	Backend       string            `json:"backend"`
	Quant         string            `json:"quant"`
	Port          int               `json:"port"`
	VRAM          string            `json:"vram"`
	TPS           string            `json:"tps"`
	Ctx           string            `json:"ctx"`
	In            []string          `json:"in"`
	Out           []string          `json:"out"`
	Desc          string            `json:"desc"`
	Warn          string            `json:"warn"`
	Sub           []Sub             `json:"sub"`
	Accel         string            `json:"accel"`
	Group         string            `json:"group"`
	Sysd          string            `json:"sysd"`
	Running       bool              `json:"running"`
	Healthy       bool              `json:"healthy"`
	Enabled       bool              `json:"enabled"`
	PortConflict  bool              `json:"port_conflict"`
	GroupSiblings []string          `json:"group_siblings"`
	HF            string            `json:"hf,omitempty"`
	Uncensored    bool              `json:"uncensored,omitempty"`
	Parallel      registry.Parallel `json:"parallel"`
	// Launch selection actually in use, read from /srv/ai-hub/state/{mid}.json
	// (the same file the panel writes on start, which wrapper scripts read).
	// Populated when parallel.enabled; omitted (zero values) on the Windows
	// dev path, keeping the mock-branch JSON identical to the Python reference.
	ChosenGpus     int   `json:"chosen_gpus,omitempty"`
	ChosenCtx      int   `json:"chosen_ctx,omitempty"`
	ChosenParallel int   `json:"chosen_parallel,omitempty"`
	GpusUsed       []int `json:"gpus_used,omitempty"`
	// TPSLive is the live decode rate (tok/s) from the most recent
	// StatsPoller scrape — Go-only extension for the UI live badge.
	// Set only for running models with a usable live value; zero (idle,
	// no recent scrape, no data) omits it, keeping the /api/status JSON
	// identical to the Python reference (parity).
	TPSLive float64 `json:"tps_live,omitempty"`
	// PromptTPSLive is the live prompt/prefill rate (tok/s) from the most
	// recent scrape. Go-only extension (omitempty): llama.cpp reports it; vLLM
	// does not, so it is omitted there and the JSON stays Python-identical.
	PromptTPSLive float64 `json:"prompt_tps_live,omitempty"`
	// CtxUsedLive is the live context tokens in use from the most recent
	// scrape (llama.cpp ctx_used). Go-only extension (omitempty): omitted when
	// there is no live value (vLLM, idle, no scrape), keeping parity.
	CtxUsedLive int `json:"ctx_used_live,omitempty"`
	// Measured context curves; absent until benchmark data is available.
	BenchmarkTPS20k float64 `json:"benchmark_tps_20k,omitempty"`
	BenchmarkPoints int     `json:"benchmark_points,omitempty"`
	BenchmarkStatus string  `json:"benchmark_status,omitempty"`
}

// BuildPortToIDs mirrors the PORT_TO_IDS building block in app.py
// (~lines 250-258): first every model id is appended under its port, then
// every key of the top-level `ports` map is appended under its port value.
// A port entry that names a model with the same id therefore appears twice
// (Python double-appends too). The ports-map keys are sorted before
// insertion for determinism (Python dict order is insertion order, Go map
// iteration is random; self-duplicates are filtered out later, so order is
// only observable for multi-key ports entries).
func BuildPortToIDs(reg *registry.File) map[int][]string {
	r := map[int][]string{}
	for _, m := range reg.Models {
		r[m.Port] = append(r[m.Port], m.ID)
	}
	keys := make([]string, 0, len(reg.Ports))
	for k := range reg.Ports {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		r[reg.Ports[k]] = append(r[reg.Ports[k]], k)
	}
	return r
}

// HealthCheck mirrors app.py health_check(): probe each endpoint with
// curl -sf against 127.0.0.1 (-m 4) then localhost (-m 2); any success is
// healthy. curl -f fails on HTTP >= 400, so status < 400 = success.
func HealthCheck(port int) bool {
	for _, path := range []string{"/health", "/v1/models", "/", "/system_stats"} {
		for _, h := range []struct {
			host    string
			timeout time.Duration
		}{
			{"127.0.0.1", 4 * time.Second},
			{"localhost", 2 * time.Second},
		} {
			client := &http.Client{Timeout: h.timeout}
			resp, err := client.Get(fmt.Sprintf("http://%s:%d%s", h.host, port, path))
			if err == nil {
				var health struct {
					Loaded *bool `json:"loaded"`
				}
				if path == "/health" && resp.StatusCode < 400 {
					b, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
					resp.Body.Close()
					if json.Unmarshal(b, &health) == nil && health.Loaded != nil && !*health.Loaded {
						return false
					}
				}
				if resp.StatusCode < 400 {
					resp.Body.Close()
					return true
				}
				resp.Body.Close()
			}
		}
	}
	return false
}

// ModelStatus mirrors app.py model_status(): build the static fields,
// resolve the systemd unit (default "ai-<id>"), detect port-conflict
// siblings via portToIDs, then on the server check systemd/docker for
// running state and health; on Windows mock enabled-by-port and return
// early (running/healthy stay false).
func ModelStatus(m registry.Model, portToIDs map[int][]string) ModelStatusEntry {
	mid := m.ID
	unit := m.Sysd
	if unit == "" {
		unit = "ai-" + mid
	}

	siblings := []string{}
	if ids, ok := portToIDs[m.Port]; ok {
		for _, x := range ids {
			if x != mid {
				siblings = append(siblings, x)
			}
		}
	}

	in := m.In
	if in == nil {
		in = []string{}
	}
	out := m.Out
	if out == nil {
		out = []string{}
	}

	sub := []Sub{}
	for _, s := range m.Sub {
		e := Sub{Name: s.Name, Tag: s.Tag, VRAM: s.VRAM}
		if s.HF != "" {
			v := s.HF
			e.HF = &v
		}
		sub = append(sub, e)
	}

	short := m.Short
	if short == "" {
		short = m.Name
	}

	st := ModelStatusEntry{
		ID:            mid,
		Name:          m.Name,
		Short:         short,
		Category:      m.Category,
		Backend:       m.Backend,
		Quant:         m.Quant,
		Uncensored:    m.Uncensored,
		Port:          m.Port,
		VRAM:          m.VRAM,
		TPS:           m.TPS,
		Ctx:           m.Ctx,
		In:            in,
		Out:           out,
		Desc:          m.Desc,
		Warn:          "",
		Sub:           sub,
		Accel:         m.Accel,
		Group:         m.Group,
		Sysd:          unit,
		Running:       false,
		Healthy:       false,
		Enabled:       false,
		PortConflict:  len(siblings) > 0,
		GroupSiblings: siblings,
		HF:            m.HF,
		Parallel:      m.Parallel,
	}

	if runtime.GOOS == "windows" {
		st.Enabled = m.Port != 0 // Python: mock enabled by port, EARLY return
		return st
	}

	if _, err := os.Stat("/etc/systemd/system/" + unit + ".service"); err == nil {
		st.Enabled = true
	}
	if st.Enabled {
		_, out := sh.Run([]string{"systemctl", "is-active", unit + ".service"}, 10*time.Second)
		if strings.TrimSpace(out) == "active" {
			st.Running = true
			st.Healthy = HealthCheck(m.Port)
		} else {
			// also check if docker container is running directly (fallback without systemd)
			_, out2 := sh.Run([]string{"docker", "ps", "--filter", "name=" + mid, "--format", "{{.Names}}"}, 10*time.Second)
			if strings.Contains(out2, mid) || strings.Contains(out2, unit) {
				st.Running = true
				st.Healthy = HealthCheck(m.Port)
			}
		}
	}

	// Launch selection in use (state file written by the panel on start, read
	// by the wrapper scripts). Mirrors the Python reference's chosen_* block.
	if st.Parallel.Enabled {
		maxG := st.Parallel.MaxGpus
		if maxG < 1 {
			maxG = 1
		}
		g, c, p := readLaunchState(mid)
		if g < 1 {
			g = 1
		}
		if g > maxG {
			g = maxG
		}
		st.ChosenGpus = g
		st.ChosenCtx = c
		st.ChosenParallel = p
		if st.Running {
			st.GpusUsed = readGPUIds(mid, g)
		}
	}
	return st
}

func readGPUIds(mid string, count int) []int {
	var state struct {
		IDs []int `json:"gpu_ids"`
	}
	data, err := os.ReadFile(filepath.Join(stateDir(), mid+".json"))
	if err == nil && json.Unmarshal(data, &state) == nil && len(state.IDs) == count {
		seen := map[int]bool{}
		valid := true
		for _, id := range state.IDs {
			if id < 0 || seen[id] {
				valid = false
			}
			seen[id] = true
		}
		if valid {
			return state.IDs
		}
	}
	ids := make([]int, count)
	for i := range ids {
		ids[i] = i
	}
	return ids
}

// stateDir returns the launch-state directory (default /srv/ai-hub/state;
// tests may override via AIHUB_STATE_DIR).
func stateDir() string {
	if v := os.Getenv("AIHUB_STATE_DIR"); v != "" {
		return v
	}
	return "/srv/ai-hub/state"
}

// readLaunchState reads {"gpus":N,"ctx":C,"parallel":P} from the state file
// the panel writes on start. Missing file / bad JSON / missing key -> 0.
func readLaunchState(mid string) (gpus, ctx, parallel int) {
	data, err := os.ReadFile(filepath.Join(stateDir(), mid+".json"))
	if err != nil {
		return 0, 0, 0
	}
	var sd map[string]interface{}
	if err := json.Unmarshal(data, &sd); err != nil {
		return 0, 0, 0
	}
	toInt := func(v interface{}) int {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case string:
			i, _ := strconv.Atoi(n)
			return i
		}
		return 0
	}
	return toInt(sd["gpus"]), toInt(sd["ctx"]), toInt(sd["parallel"])
}

// CurrentProfile returns the trimmed contents of /srv/ai/.current-profile,
// or "" when the file is missing.
func CurrentProfile() string {
	b, err := os.ReadFile("/srv/ai/.current-profile")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
