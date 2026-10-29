// Package actions mirrors app.py's model/service/profile/logs/downloads
// endpoints (systemctl / systemd-run / journalctl via internal/sh).
package actions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"aihub/internal/registry"
	"aihub/internal/sh"
)

// Deps carries the registry (models + profiles) and the (deprecated) services
// map from registry.yaml "services" list. Current registry: services empty.
type Deps struct {
	Reg      *registry.File
	Services map[string]map[string]interface{}
}

func isWindows() bool { return runtime.GOOS == "windows" }

// runCmd is the command runner; tests inject a scripted fake (A-03).
var runCmd = sh.Run

// lastRunes mirrors Python s[-n:].
func lastRunes(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[len(rs)-n:])
}

func (d *Deps) model(id string) *registry.Model {
	if d.Reg == nil {
		return nil
	}
	return d.Reg.Get(id)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ModelAction mirrors app.py model_action (POST /api/model/{mid}/{action}).
// 404 for unknown model; 400 for bad action; start/restart 3600s, stop 300s.
func (d *Deps) ModelAction(mid, action string) (map[string]interface{}, int) {
	return d.ModelActionWithParams(mid, action, 0, 0, 0)
}

// ModelActionWithParams is like ModelAction but optionally writes GPU/ctx/
// parallel (agent slots) params to /srv/ai-hub/state/{mid}.json before
// starting, so wrapper scripts (ai-qwen-q4k-llama.sh, ai-qwen-q6k-llama.sh,
// ai-qwen-fp8-vllm.sh, …) can read them. Pass 0 to skip writing a field.
func (d *Deps) ModelActionWithParams(mid, action string, gpus, ctx, parallel int) (map[string]interface{}, int) {
	m := d.model(mid)
	if m == nil {
		return map[string]interface{}{"detail": "unknown model: " + mid}, 404
	}
	unit := m.Sysd
	if unit == "" {
		unit = "ai-" + mid
	}
	if isWindows() {
		return map[string]interface{}{"ok": true, "output": fmt.Sprintf("mock %s %s on Windows dev", action, mid)}, 200
	}
	if !exists("/etc/systemd/system/" + unit + ".service") {
		return map[string]interface{}{"ok": false, "output": unit + ".service not installed"}, 200
	}
	// Write state file for wrapper scripts on start/restart with params
	if (action == "start" || action == "restart") && (gpus > 0 || ctx > 0 || parallel > 0) {
		writeState(mid, gpus, ctx, parallel)
	}
	if m.Native || strings.EqualFold(m.Backend, "Strata") || strings.EqualFold(m.Backend, "Maya") {
		return nativeAction(unit, action)
	}
	switch action {
	case "start", "restart":
		state, ok, out := startModel(mid, unit, action)
		return map[string]interface{}{"ok": ok, "state": state, "output": out}, 200
	case "stop":
		ok, out := stopModel(mid, unit)
		state := "stopped"
		if !ok {
			state = "failed"
		}
		return map[string]interface{}{"ok": ok, "state": state, "output": out}, 200
	default:
		return map[string]interface{}{"detail": "bad action"}, 400
	}
}

// startModel runs the start/restart lifecycle for one model and reports the
// resulting state (A-03 idempotency matrix):
//
//	unit inactive + no container         -> "started"   (systemctl start issued)
//	unit active   + no container (stale) -> "restarted" (bare start would be a
//	                                        silent no-op on the oneshot unit)
//	container running (any unit state)   -> "running"   for action "start": the
//	                                        container is up, so NO systemctl
//	                                        call is made and the no-op is
//	                                        reported as such — never a false
//	                                        "ok" that hides inactivity
//
// An explicit "restart" always issues `systemctl restart` (user intent);
// the wrapper scripts are themselves idempotent ("already running" -> rc 0).
func startModel(mid, unit, action string) (state string, ok bool, out string) {
	if action == "start" && modelRunning(mid) {
		return "running", true, "already running — no systemctl call (container up)"
	}
	a := action
	if a == "start" && unitActive(unit) {
		a = "restart" // stale 'active (exited)' unit: start would be a no-op
	}
	rc, o := runCmd([]string{"sudo", "-n", "systemctl", a, unit + ".service"}, 3600*time.Second)
	if rc != 0 {
		return "failed", false, lastRunes(o, 2000)
	}
	if a == "restart" {
		return "restarted", true, lastRunes(o, 2000)
	}
	return "started", true, lastRunes(o, 2000)
}

// unitActive reports whether the systemd unit is in 'active' state
// (includes the stale 'active (exited)' state of oneshot units).
func unitActive(unit string) bool {
	rc, out := runCmd([]string{"sudo", "-n", "systemctl", "is-active", unit + ".service"}, 10*time.Second)
	return rc == 0 && strings.Contains(out, "active")
}

// Native servers have a live MainPID, unlike the Docker oneshot wrappers.
func nativeAction(unit, action string) (map[string]interface{}, int) {
	if action != "start" && action != "restart" && action != "stop" {
		return map[string]interface{}{"detail": "bad action"}, 400
	}
	if action == "start" && unitActive(unit) {
		return map[string]interface{}{"ok": true, "state": "running", "output": "already running"}, 200
	}
	rc, out := runCmd([]string{"sudo", "-n", "systemctl", action, unit + ".service"}, 300*time.Second)
	ok := rc == 0
	if action == "stop" && unitActive(unit) {
		ok = false
		out += "\nservice still active"
	}
	state := map[string]string{"start": "started", "restart": "restarted", "stop": "stopped"}[action]
	if !ok {
		state = "failed"
	}
	return map[string]interface{}{"ok": ok, "state": state, "output": lastRunes(out, 2000)}, 200
}

// dockerRunning returns the names of running containers whose names contain
// mid (model id == container name in all wrappers/status checks).
func dockerRunning(mid string) []string {
	_, out := runCmd([]string{"docker", "ps", "--filter", "name=" + mid, "--format", "{{.Names}}"}, 10*time.Second)
	return parseDockerNames(out)
}

func parseDockerNames(out string) []string {
	var names []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			names = append(names, l)
		}
	}
	return names
}

// modelRunning reports whether a container for this model is currently up.
func modelRunning(mid string) bool { return len(dockerRunning(mid)) > 0 }

// stopModel stops a model and VERIFIES the container is actually gone.
// `systemctl stop` on a oneshot unit is a silent no-op when the unit is
// inactive — which is exactly the state after the container was started
// directly via `docker compose` (bypassing the unit). In that case (and any
// other leftover) we stop+remove the container ourselves.
func stopModel(mid, unit string) (bool, string) {
	rc, out := runCmd([]string{"sudo", "-n", "systemctl", "stop", unit + ".service"}, 300*time.Second)
	for _, n := range dockerRunning(mid) {
		if r2, o2 := runCmd([]string{"docker", "stop", "--time", "20", n}, 90*time.Second); r2 != 0 {
			out += "\ndocker stop " + n + ": rc=" + strconv.Itoa(r2) + " " + strings.TrimSpace(o2)
		}
		runCmd([]string{"docker", "rm", "-f", n}, 30*time.Second)
	}
	if still := dockerRunning(mid); len(still) > 0 {
		return false, lastRunes(out+"\nstill running: "+strings.Join(still, ", "), 2000)
	}
	return rc == 0, lastRunes(out, 2000)
}

// StateDir returns the launch-state directory (default /srv/ai-hub/state).
// Tests override via AIHUB_STATE_DIR.
func StateDir() string {
	if v := os.Getenv("AIHUB_STATE_DIR"); v != "" {
		return v
	}
	return "/srv/ai-hub/state"
}

// writeState writes GPU/ctx/parallel params to /srv/ai-hub/state/{mid}.json
// so wrapper scripts can read them. Fields with value 0 are not written
// (keep existing).
func writeState(mid string, gpus, ctx, parallel int) {
	dir := StateDir()
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, mid+".json")
	// Read existing state (if any) to preserve other fields
	state := map[string]interface{}{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &state)
	}
	if gpus > 0 {
		state["gpus"] = gpus
	}
	if ctx > 0 {
		state["ctx"] = ctx
	}
	if parallel > 0 {
		state["parallel"] = parallel
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// ParseLaunchParams extracts gpus, ctx and parallel from query string values.
// Returns 0 for missing or invalid values (wrapper scripts use their defaults).
func ParseLaunchParams(gpusStr, ctxStr, parStr string) (int, int, int) {
	gpus, _ := strconv.Atoi(gpusStr)
	ctx, _ := strconv.Atoi(ctxStr)
	par, _ := strconv.Atoi(parStr)
	if gpus < 0 {
		gpus = 0
	}
	if ctx < 0 {
		ctx = 0
	}
	if par < 0 {
		par = 0
	}
	return gpus, ctx, par
}

// ServiceAction mirrors app.py service_action (POST /api/service/{name}/{action}).
// Services are tried first (unit = ai-{name}.service, start/restart 1800s, stop
// 300s); unknown services fall back to models (start/restart 3600s, stop 300s);
// both unknown -> 404 "unknown service/model: {name}".
func (d *Deps) ServiceAction(name, action string) (map[string]interface{}, int) {
	if _, isSvc := d.Services[name]; isSvc {
		unit := "ai-" + name + ".service"
		if isWindows() {
			return map[string]interface{}{"ok": true, "output": fmt.Sprintf("mock %s %s", action, name)}, 200
		}
		if !exists("/etc/systemd/system/" + unit) {
			return map[string]interface{}{"ok": false, "output": unit + " not installed yet"}, 200
		}
		var to time.Duration
		switch action {
		case "start", "restart":
			to = 1800 * time.Second
		case "stop":
			to = 300 * time.Second
		default:
			return map[string]interface{}{"detail": "bad action"}, 400
		}
		rc, out := sh.Run([]string{"sudo", "-n", "systemctl", action, unit}, to)
		return map[string]interface{}{"ok": rc == 0, "output": lastRunes(out, 2000)}, 200
	}
	if m := d.model(name); m != nil {
		if strings.EqualFold(m.Backend, "Strata") || strings.EqualFold(m.Backend, "Maya") {
			return d.ModelAction(name, action)
		}
		unit := m.Sysd
		if unit == "" {
			unit = "ai-" + name
		}
		if isWindows() {
			return map[string]interface{}{"ok": true, "output": fmt.Sprintf("mock %s %s", action, name)}, 200
		}
		if !exists("/etc/systemd/system/" + unit + ".service") {
			return map[string]interface{}{"ok": false, "output": unit + ".service not installed"}, 200
		}
		switch action {
		case "start", "restart":
			state, ok, out := startModel(name, unit, action)
			return map[string]interface{}{"ok": ok, "state": state, "output": out}, 200
		case "stop":
			ok, out := stopModel(name, unit)
			return map[string]interface{}{"ok": ok, "output": out}, 200
		default:
			return map[string]interface{}{"detail": "bad action"}, 400
		}
	}
	return map[string]interface{}{"detail": "unknown service/model: " + name}, 404
}

// ServiceBuild mirrors app.py service_build (POST /api/service/{name}/build).
// Accepts a service or a model name (compat); unknown -> 404 "unknown service".
// The build itself is detached via systemd-run (30s to launch).
func (d *Deps) ServiceBuild(name string) (map[string]interface{}, int) {
	if d.Services[name] == nil && d.model(name) == nil {
		return map[string]interface{}{"detail": "unknown service"}, 404
	}
	if isWindows() {
		return map[string]interface{}{"ok": true, "output": "mock build on Windows"}, 200
	}
	cmd := "cd /srv/ai/stack && docker compose build " + name + " > /var/log/build-" + name + ".log 2>&1; echo \"EXIT:$?\" >> /var/log/build-" + name + ".log"
	rc, out := sh.Run([]string{"sudo", "-n", "systemd-run", "--unit", "build-" + name, "--remain-after-exit", "bash", "-c", cmd}, 30*time.Second)
	return map[string]interface{}{"ok": rc == 0, "output": lastRunes(out, 1000)}, 200
}

// ApplyProfile mirrors app.py apply_profile (POST /api/profile/{pname}).
// 404 for unknown profile; stops StopBefore units (300s each), copies the
// profile env file to /srv/ai/vllm.env + writes /srv/ai/.current-profile,
// restarts ai-vllm.service (3600s; on failure appends 30 lines of journalctl),
// then starts StartAfter units except "vllm" (600s each).
func (d *Deps) ApplyProfile(pname string) (map[string]interface{}, int) {
	if isWindows() {
		return map[string]interface{}{"ok": true, "output": "mock profile apply on Windows"}, 200
	}
	var p *registry.Profile
	if d.Reg != nil {
		p = d.Reg.GetProfile(pname)
	}
	if p == nil {
		return map[string]interface{}{"detail": "unknown profile"}, 404
	}
	msgs := []string{}
	for _, s := range p.StopBefore {
		if exists("/etc/systemd/system/ai-" + s + ".service") {
			rc, _ := sh.Run([]string{"sudo", "-n", "systemctl", "stop", "ai-" + s + ".service"}, 300*time.Second)
			msgs = append(msgs, fmt.Sprintf("stop %s: rc=%d", s, rc))
		}
	}
	// env file copy + marker (mirrors the Path logic in app.py)
	src := "/srv/ai/profiles/" + p.EnvFile
	envTarget := "/srv/ai/vllm.env"
	marker := "/srv/ai/.current-profile"
	if exists(src) {
		if b, err := os.ReadFile(src); err == nil {
			tmp := envTarget + ".tmp"
			_ = os.WriteFile(tmp, b, 0o644)
			_ = os.Rename(tmp, envTarget)
			_ = os.WriteFile(marker, []byte(pname), 0o644)
		}
	}
	rc, out := sh.Run([]string{"sudo", "-n", "systemctl", "restart", "ai-vllm.service"}, 3600*time.Second)
	msgs = append(msgs, fmt.Sprintf("restart vllm: rc=%d", rc))
	if rc != 0 {
		_, o3 := sh.Run([]string{"journalctl", "-u", "ai-vllm.service", "-n", "30", "--no-pager"}, 20*time.Second)
		out = out + "\n" + o3
	}
	for _, s := range p.StartAfter {
		if s != "vllm" && exists("/etc/systemd/system/ai-"+s+".service") {
			rc2, _ := sh.Run([]string{"sudo", "-n", "systemctl", "start", "ai-" + s + ".service"}, 600*time.Second)
			msgs = append(msgs, fmt.Sprintf("start %s: rc=%d", s, rc2))
		}
	}
	return map[string]interface{}{"ok": rc == 0, "output": lastRunes(strings.Join(msgs, "\n")+"\n"+out, 2000)}, 200
}

// Logs mirrors app.py logs (GET /api/logs/{mid}) and returns a TEXT body.
// unit resolution: model sysd (fallback "ai-{mid}") > service ("ai-{mid}") >
// generic "ai-{mid}" with an existence check. On 404 the body is "" and the
// HTTP handler must respond {"detail":"unknown model/service: "+mid}.
// journalctl: -u {unit}.service -n {tail} --no-pager, 30s; empty -> "No logs yet";
// otherwise last 16000 runes.
func (d *Deps) Logs(mid string, tail int) (string, int) {
	if tail <= 0 {
		tail = 200
	}
	var unit string
	if m := d.model(mid); m != nil {
		unit = m.Sysd
		if unit == "" {
			unit = "ai-" + mid
		}
	} else if _, isSvc := d.Services[mid]; isSvc {
		unit = "ai-" + mid
	} else {
		// try generic
		unit = "ai-" + mid
		if !exists("/etc/systemd/system/" + unit + ".service") {
			return "", 404
		}
	}
	if isWindows() {
		return fmt.Sprintf("mock logs for %s on Windows dev\nunit: %s\nno logs", mid, unit), 200
	}
	_, out := sh.Run([]string{"journalctl", "-u", unit + ".service", "-n", fmt.Sprintf("%d", tail), "--no-pager"}, 30*time.Second)
	if out == "" {
		return "No logs yet", 200
	}
	return lastRunes(out, 16000), 200
}

// Downloads mirrors app.py downloads (GET /api/downloads) and returns a TEXT
// body: du of the HF cache, any running "hf download" procs, and the tail of
// the dl log (15s).
func (d *Deps) Downloads() (string, int) {
	if isWindows() {
		return "mock downloads on Windows", 200
	}
	_, out := sh.Run([]string{"bash", "-c", "du -sh /home/user/.cache/huggingface 2>/dev/null; pgrep -af 'hf download' | head -3; tail -c 400 /home/user/work/logs/dl-qwen38-uncensored.log 2>/dev/null | tr '\\r' '\\n' | tail -2"}, 15*time.Second)
	return out, 200
}
