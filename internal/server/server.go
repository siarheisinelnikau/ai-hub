package server

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"aihub/internal/actions"
	"aihub/internal/auth"
	"aihub/internal/buildinfo"
	"aihub/internal/cloud"
	"aihub/internal/config"
	"aihub/internal/metrics"
	"aihub/internal/models"
	"aihub/internal/registry"
	"aihub/internal/status"
	"aihub/internal/webhooks"
)

// Deps are the server's dependencies (injected for testability).
type Deps struct {
	Reg             *registry.File
	Store           *auth.Store
	Actions         *actions.Deps
	Poller          *status.Poller
	Stats           *metrics.StatsPoller
	AIHUBRoot       string
	ExeDir          string
	Public          config.Public
	RequireAPIToken bool
	SecureCookies   bool
}

// Server wires all ai-hub HTTP routes, mirroring app.py.
type Server struct {
	d        Deps
	find     func(name string) string
	static   string
	loginLim *loginLimiter
	// startedAt resolves a model id to its docker container start time
	// (universal proxy target ordering). Tests override it.
	startedAt func(id string) (time.Time, bool)
	// containerRunning reports whether the target's container is still up
	// (universal proxy load-wait early exit). Tests override it.
	containerRunning func(id string) bool
	// freshLLMs is the on-demand running-LLM re-check (universal proxy, when
	// the poller cache sees nothing). Tests override it.
	freshLLMs func() []status.ModelStatusEntry
	// healthPoll / loadWait: load-wait poll tick and deadline (tests shorten
	// them). Zero values fall back to the production defaults.
	healthPoll time.Duration
	loadWait   time.Duration
	// comfyBaseURL / comfyClient: ComfyUI proxy overrides (tests point the
	// proxy at an httptest server). Zero values use the production defaults.
	comfyBaseURL string
	comfyClient  *http.Client
	// stopAll stops every running LLM (GPU-drop reaction). Tests override it
	// to observe calls without touching systemd/docker. Default: Actions.StopAll.
	stopAll func(running func(string) bool) []string
}

// New builds a Server. It does NOT start pollers (the caller does).
func New(d Deps) *Server {
	s := &Server{
		d:                d,
		find:             FindStaticFile(d.AIHUBRoot, d.ExeDir),
		static:           chooseStaticDir(d.AIHUBRoot, d.ExeDir),
		loginLim:         newLoginLimiter(5, 60*time.Second, 30*time.Second),
		startedAt:        containerStartedAt,
		containerRunning: containerStillRunning,
		freshLLMs:        nil, // nil -> Server.freshRunningLLMs (real check)
		healthPoll:       3 * time.Second,
		loadWait:         universalLoadDeadline,
	}
	s.wireGpuDrop()
	return s
}

// wireGpuDrop makes the poller react to a CONFIRMED change in the visible GPU
// count (A-02: the poller debounces and never fires on a failed nvidia-smi
// read). A GPU/Riser dropping out while models run stops all running LLMs;
// a recovery (GPU came back) must NOT stop them — only record the event. Both
// record a GpuEvent (surfaced via /api/system-health as "gpu_event") and fire
// a webhook. Harmless in tests (mock GPUs are a stable 4, so it never fires).
func (s *Server) wireGpuDrop() {
	if s.d.Poller == nil {
		return
	}
	if s.stopAll == nil && s.d.Actions != nil {
		s.stopAll = func(running func(string) bool) []string {
			st, _ := s.d.Actions.StopAll(running)
			return st
		}
	}
	s.d.Poller.OnGpuCountChange = func(kind string, from, to int) int {
		stopped := 0
		if kind == "drop" && s.stopAll != nil {
			st := s.stopAll(s.runningFn())
			stopped = len(st)
			for _, mid := range st {
				webhooks.Fire("model.stop", mid, "stopped (GPU count "+kind+")")
			}
		}
		var msg string
		if kind == "drop" {
			msg = fmt.Sprintf("GPU count dropped %d → %d (visible GPUs changed).", from, to)
			if stopped > 0 {
				msg += fmt.Sprintf(" Stopped %d running LLM model(s).", stopped)
			} else {
				msg += " No running models to stop."
			}
			msg += " Likely a Riser/GPU fell off or a PCIe link dropped — check the hardware."
		} else {
			msg = fmt.Sprintf("GPU count recovered %d → %d (a GPU came back online).", from, to)
		}
		now := time.Now()
		s.d.Poller.Cache().SetGpuEvent(&status.GpuEvent{
			Kind: kind, From: from, To: to, Stopped: stopped, Message: msg,
			TS: float64(now.UnixNano()) / 1e9, TSHuman: now.Format(time.RFC3339),
		})
		webhooks.FireCoalesced("gpu.count-change", fmt.Sprintf("%s:%d-%d", kind, from, to), msg, 120*time.Second)
		return stopped
	}
}

// Handler builds the full mux (API + static + healthz).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /runtime-config.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		b, _ := json.Marshal(s.d.Public)
		_, _ = fmt.Fprintf(w, "window.AIHUB_CONFIG=%s;\n", b)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, struct {
			OK bool `json:"ok"`
			buildinfo.Info
		}{OK: true, Info: buildinfo.Current()})
	})
	// Auth
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("DELETE /api/login", s.handleLogout)
	// Status
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("GET /api/benchmarks", s.handleContextBenchmarks)
	// GPU core+VRAM temperature history (Go-only extension — app.py has no
	// such endpoint, so no parity constraint). Feeds the per-GPU temp charts.
	mux.HandleFunc("GET /api/gpu-temps", s.handleGpuTemps)
	// System health (RAM/swap/EDAC/MCE/AER/CPU) from the monitoring TSV logs.
	// Go-only extension (no parity constraint).
	mux.HandleFunc("GET /api/system-health", s.handleSystemHealth)
	// Full disk breakdown (df + du rows + docker images/volumes) for the
	// Disk Free modal. Go-only extension, cached 5 min (no parity constraint).
	mux.HandleFunc("GET /api/disk", s.handleDiskReport)
	// CSV export of the long-term GPU telemetry log (Go-only extension).
	// ?what=gpu&hours=N — one row per sample, per-GPU columns.
	mux.HandleFunc("GET /api/export", s.handleExport)
	// View a last-words snapshot (pre-crash system state). Go-only extension.
	mux.HandleFunc("GET /api/last-words/{name}", s.handleLastWords)
	// Settings — notifications / Telegram bot (webhook URL list). Go-only
	// extension (no parity constraint).
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("POST /api/settings", s.handleSaveSettings)
	mux.HandleFunc("POST /api/settings/test", s.handleTestSettings)
	// Cloud providers — OpenAI OAuth (ChatGPT) models mapped to hub cards.
	// Go-only extension (no parity constraint).
	mux.HandleFunc("GET /api/cloud", s.handleCloudGet)
	mux.HandleFunc("POST /api/cloud", s.handleCloudSave)
	mux.HandleFunc("POST /api/cloud/refresh", s.handleCloudRefresh)
	mux.HandleFunc("POST /api/cloud/action", s.handleCloudAction)
	// Model manager — inventory + resumable HuggingFace download + hub
	// connection check. Go-only extension (no parity constraint).
	mux.HandleFunc("GET /api/models", s.handleModelsList)
	mux.HandleFunc("POST /api/models/download", s.handleModelDownload)
	mux.HandleFunc("POST /api/models/resume", s.handleModelsResume)
	mux.HandleFunc("POST /api/models/connect", s.handleModelConnect)
	mux.HandleFunc("POST /api/models/delete", s.handleModelDelete)
	// Prometheus metrics (text exposition). No auth — read-only telemetry for
	// scrapers (Grafana/Prometheus) on the private LAN; standard for /metrics.
	mux.HandleFunc("GET /metrics", s.handleMetrics)
	// Model actions + stats
	mux.HandleFunc("POST /api/model/{mid}/{action}", s.handleModelAction)
	mux.HandleFunc("GET /api/model/{mid}/stats", s.handleModelStats)
	// Benchmark a running model (TTFT + decode TPS) — Go-only extension.
	// Declared before the {action} route is irrelevant (different method/shape),
	// but kept grouped with the model routes for clarity.
	mux.HandleFunc("POST /api/model/{mid}/benchmark", s.handleModelBench)
	// Stop ALL running LLM models in one call (Go extension; app.py has
	// no stop-all, so no parity constraint).
	mux.HandleFunc("POST /api/stop-all", s.handleStopAll)
	// Universal LLM proxy (Go-only extension, no parity constraint):
	// OpenAI-compatible endpoints that always forward to the LAST-STARTED
	// running model, so clients keep one stable base URL across model
	// switches. No auth (model endpoints are unauthenticated on the LAN).
	mux.HandleFunc("GET /v1/models", s.handleUniversalModels)
	mux.HandleFunc("POST /v1/chat/completions", s.handleUniversalChat)
	// Service actions. NOTE: no dedicated /build route — in app.py the
	// {action} route is declared before /build, so "build" is handled by
	// service_action (action="build") exactly like any other action.
	mux.HandleFunc("POST /api/service/{name}/{action}", s.handleServiceAction)
	// Profile
	mux.HandleFunc("POST /api/profile/{pname}", s.handleProfile)
	// Logs + downloads
	mux.HandleFunc("GET /api/logs/{mid}", s.handleLogs)
	mux.HandleFunc("GET /api/downloads", s.handleDownloads)
	// ComfyUI proxy (quick video generation UI). Go-only extension, no
	// parity constraint.
	s.registerComfyRoutes(mux)
	// Static UI + GET catch-all (see staticHandlers). Method mismatches on
	// known paths still yield 405.
	staticHandlers(mux, s.find, s.static)
	return s.securityHeaders(http.NewCrossOriginProtection().Handler(mux))
}

// writeJSON mirrors FastAPI JSONResponse: compact JSON, no trailing newline.
// (json.Marshal sorts map keys, like starlette's JSON dumps.)
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	_, _ = w.Write(b)
}

func sessionToken(r *http.Request) string {
	c, err := r.Cookie(auth.CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// drainBody reads and discards the request body (capped at 1 MiB), mirroring
// uvicorn which consumes the body as part of the HTTP protocol state machine.
// Without this, Go's http server cannot keep/reuse the connection after the
// response (unread body), and fast clients that close right after the
// response can race into a connection reset (WinError 10054).
func drainBody(r *http.Request) {
	if r.Body != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	}
}

func (s *Server) requireAuth(w http.ResponseWriter, r *http.Request) bool {
	if !s.d.Store.Check(sessionToken(r)) {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"detail": "not logged in"})
		return false
	}
	return true
}

// handleLogin mirrors app.py login exactly:
//
//	try: body = await req.json()
//	except: body = {}
//	if not secrets.compare_digest(str(body.get("token", "")), real): 403
//
// So: empty/invalid JSON body -> {} -> 403; valid JSON NON-dict (null,
// number, bool, string, array) -> body.get raises AttributeError ->
// uvicorn 500 "Internal Server Error" (text/plain).
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	// Rate-limit per client IP: 5 bad tokens in 60s -> 30s lockout.
	if s.loginLim != nil {
		ip := clientIP(r)
		if allowed, ra := s.loginLim.check(ip); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(ra))
			writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
				"detail": "too many failed logins — try again in " + strconv.Itoa(ra) + "s",
			})
			return
		}
	}
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var body interface{}
	parsed := false
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &body); err == nil {
			parsed = true
		}
	}
	provided := ""
	if parsed {
		obj, ok := body.(map[string]interface{})
		if !ok {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, "Internal Server Error")
			return
		}
		if v, present := obj["token"]; present && v != nil {
			provided = fmt.Sprintf("%v", v)
		}
	}
	tok := s.d.Store.Login(provided)
	if tok == "" {
		if s.loginLim != nil {
			s.loginLim.recordFailure(clientIP(r))
		}
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"detail": "bad token"})
		return
	}
	if s.loginLim != nil {
		s.loginLim.reset(clientIP(r))
	}
	cookie := auth.SessionCookie(tok)
	cookie.Secure = s.d.SecureCookies || r.TLS != nil
	http.SetCookie(w, cookie)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// handleLogout mirrors app.py logout: discard the session.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	drainBody(r)
	if !s.requireAuth(w, r) {
		return
	}
	s.d.Store.Logout(sessionToken(r))
	cookie := auth.SessionCookie("")
	cookie.MaxAge = -1
	cookie.Secure = s.d.SecureCookies || r.TLS != nil
	http.SetCookie(w, cookie)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

// statusStaleAfter — a snapshot older than this is labelled "stale" in the
// response (C-02 contract: the UI must show "data as of ts", not present a
// dead poller's last snapshot as live). Poller interval is 3s; 5x headroom.
const statusStaleAfter = int64(15)

// handleStatus mirrors app.py status: serve the poller's cache snapshot.
// Go extension: running models are annotated with tps_live from the
// StatsPoller's most recent scrape (same "live = last entry" rule as
// handleModelStats, via LiveTPS). The field is omitempty and only set for
// running models with a usable live value, so idle/stale/absent data keeps
// the JSON shape identical to the Python reference (parity-safe).
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	snap := s.d.Poller.Cache().Snapshot()
	if models, ok := snap["models"].([]status.ModelStatusEntry); ok {
		s.annotateContextBenchmarks(models)
	}
	if s.d.Stats != nil {
		// Snapshot() returns a fresh models copy, so annotating here does
		// not mutate the cache. Annotate running models with the live decode
		// rate, prompt rate and context usage from the most recent scrape
		// (Go-only, omitempty fields — parity-safe).
		if models, ok := snap["models"].([]status.ModelStatusEntry); ok {
			for i := range models {
				m := &models[i]
				if !m.Running {
					continue
				}
				entry, ok := s.d.Stats.LastEntry(m.ID)
				if !ok {
					continue
				}
				if tps := metrics.F64(entry["tps"]); tps > 0 {
					m.TPSLive = tps
				}
				if pt := metrics.F64(entry["prompt_tps"]); pt > 0 {
					m.PromptTPSLive = pt
				}
				if cu := metrics.I64(entry["ctx_used"]); cu > 0 {
					m.CtxUsedLive = int(cu)
				}
			}
		}
	}
	// Hide models whose files are still downloading from the main list: their
	// card would be dead (no files, cannot start). They stay in 📦 Model
	// manager with live progress. No-op when nothing is downloading, so the
	// parity JSON shape is unchanged in that case.
	if ms, ok := snap["models"].([]status.ModelStatusEntry); ok {
		kept := ms[:0]
		for _, m := range ms {
			if models.IsDownloading(m.ID) {
				continue
			}
			if s.d.Reg != nil {
				if profile := s.d.Reg.Get(m.ID); profile != nil && profile.RequiresWeights && !m.Running && !models.Ready(m.ID) {
					continue
				}
			}
			kept = append(kept, m)
		}
		snap["models"] = kept
	}
	// Cloud (OpenAI OAuth) models mapped to cards: merged into the snapshot
	// here (not the poller cache) so the stats poller never scrapes the
	// provider port and stop-all/running-detection stay local-only. When no
	// cloud config exists (parity/dev box) this is a no-op, so the JSON shape
	// is unchanged (parity-safe).
	if extra := s.cloudEntries(); len(extra) > 0 {
		if models, ok := snap["models"].([]status.ModelStatusEntry); ok {
			snap["models"] = append(models, extra...)
		} else {
			snap["models"] = extra
		}
	}
	// C-02 snapshot contract: expose the age of the last successful poll so
	// clients can distinguish live data from a stale snapshot (e.g. the
	// poller wedged). ts itself is already in the snapshot; parity-safe
	// (run_parity iterates the Python side's keys only).
	if ts, okk := snap["ts"].(float64); okk {
		age := time.Now().Unix() - int64(ts)
		if age < 0 {
			age = 0
		}
		snap["age_s"] = age
		snap["stale"] = age > statusStaleAfter
	}
	writeJSON(w, http.StatusOK, snap)
}

// handleGpuTemps serves the GPU core+VRAM temperature history (oldest→newest)
// for the per-GPU temperature charts. Go-only endpoint (no Python parity
// constraint). Shape:
//
//	[{"ts":1788260232.1,"gpu":[54,57,58,59],"vram":[70,74,76,80]}, ...]
//
// gpu[i]/vram[i] are indexed by nvidia-smi index; -1 = no reading.
func (s *Server) handleGpuTemps(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, s.d.Poller.Cache().GpuTemps())
}

// handleSystemHealth serves RAM/swap/EDAC/MCE/AER/CPU health from the
// monitoring TSV logs (Go-only endpoint; degrades to available=false on boxes
// without the logs, e.g. the Windows parity/dev environment).
func (s *Server) handleSystemHealth(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	out := status.SystemHealth()
	// Go-only extension: the most recent GPU-count-change event (a dropped
	// Riser/GPU). /api/system-health has no parity constraint, so this is safe.
	if s.d.Poller != nil {
		if ev := s.d.Poller.Cache().GpuEvent(); ev != nil {
			out["gpu_event"] = ev
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleGetSettings returns the current notification (webhook) settings.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	urls := webhooks.Load()
	if urls == nil {
		urls = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"webhooks": map[string]interface{}{"urls": urls, "source": webhooks.Source()},
	})
}

// handleSaveSettings persists the webhook URL list to the config file.
// Body: {"webhooks":{"urls":["https://..."]}}
func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	var body struct {
		Webhooks struct {
			URLs []string `json:"urls"`
		} `json:"webhooks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "detail": "bad request body"})
		return
	}
	if err := webhooks.Save(body.Webhooks.URLs); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "detail": "save failed: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "saved": len(body.Webhooks.URLs)})
}

// handleTestSettings sends a test notification SYNCHRONOUSLY and reports each
// bot's delivery result (ok / HTTP status / error). Body may include
// {"webhooks":{"urls":[...]}} to test specific URL(s) only; otherwise all
// configured URLs are tested.
func (s *Server) handleTestSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	var body struct {
		Webhooks struct {
			URLs []string `json:"urls"`
		} `json:"webhooks"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	msg := "ai-hub test — notifications are working ✅ · " + time.Now().Format("2006-01-02 15:04")
	results := webhooks.Send(msg, body.Webhooks.URLs)
	if len(results) == 0 {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "detail": "no webhook URLs configured", "results": []interface{}{}})
		return
	}
	okCount := 0
	for _, rr := range results {
		if rr.OK {
			okCount++
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":       okCount == len(results),
		"ok_count": okCount,
		"total":    len(results),
		"results":  results,
	})
}

// --- Model manager (inventory + resumable download + hub connection) -------

// handleModelsList returns the model inventory: catalog + on-disk size +
// download state + hub connection status (registry / systemd / Open WebUI).
func (s *Server) handleModelsList(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	list := models.List()
	for i := range list {
		c := s.connectCheck(list[i].ID, list[i].Port)
		list[i].Connection = c
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"models": list})
}

// handleModelDownload starts (or confirms) a resumable download for one model.
// Body: {"id": "..."}.
func (s *Server) handleModelDownload(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "detail": "missing model id"})
		return
	}
	st, err := models.Download(body.ID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "state": st})
}

// handleModelsResume re-launches any download left "downloading" (e.g. after a
// reboot). Returns how many were (re)started.
func (s *Server) handleModelsResume(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	n := models.Resume()
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "resumed": n})
}

// handleModelConnect reports the hub-connection state for one model (which
// touchpoints are present) without mutating anything. Body: {"id":"..."}.
func (s *Server) handleModelConnect(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "detail": "missing model id"})
		return
	}
	info, ok := models.Catalog[body.ID]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"ok": false, "detail": "unknown model"})
		return
	}
	c := s.connectCheck(body.ID, info.Port)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": c.AllConnected, "connection": c})
}

// connectCheck inspects the server-side touchpoints for a model: registry entry,
// systemd service file, and Open WebUI base-URL. It does not mutate anything.
func (s *Server) connectCheck(id string, port int) models.Connection {
	rep := models.Connection{}
	if s.d.Reg != nil {
		rep.Registry = s.d.Reg.Get(id) != nil
	}
	// systemd service: /etc/systemd/system/ai-<id>.service
	if _, err := os.Stat("/etc/systemd/system/ai-" + id + ".service"); err == nil {
		rep.Service = true
	}
	// Open WebUI: port must be present in OPENAI_API_BASE_URLS
	if data, err := os.ReadFile("/etc/systemd/system/ai-open-webui.service"); err == nil {
		if strings.Contains(string(data), fmt.Sprintf(":%d/v1", port)) {
			rep.WebUI = true
		}
	}
	rep.AllConnected = rep.Registry && rep.Service && rep.WebUI
	missing := []string{}
	if !rep.Registry {
		missing = append(missing, "registry entry")
	}
	if !rep.Service {
		missing = append(missing, "systemd service (ai-"+id+".service)")
	}
	if !rep.WebUI {
		missing = append(missing, "Open WebUI base URL (:"+strconv.Itoa(port)+")")
	}
	rep.Notes = missing
	return rep
}

// handleModelDelete frees model weights while retaining catalog and launch configuration.
// Body: {"id":"...", "disk":true, "hub":true, "dry_run":false}. disk/hub default
// disk defaults true; hub defaults false (explicit legacy removal remains available).
// dry_run reports what WOULD be changed without touching anything.
func (s *Server) handleModelDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	var body struct {
		ID     string `json:"id"`
		Disk   *bool  `json:"disk"`
		Hub    *bool  `json:"hub"`
		DryRun bool   `json:"dry_run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "detail": "missing model id"})
		return
	}
	info, ok := models.Catalog[body.ID]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"ok": false, "detail": "unknown model"})
		return
	}
	doDisk := body.Disk == nil || *body.Disk
	doHub := body.Hub != nil && *body.Hub
	if models.IsDownloading(body.ID) {
		writeJSON(w, http.StatusConflict, map[string]interface{}{"ok": false, "detail": "download is in progress"})
		return
	}
	type step struct {
		What string `json:"what"`
		Done bool   `json:"done"`
		Note string `json:"note,omitempty"`
	}
	steps := []step{}

	// 1. stop the model (best-effort; harmless if not running)
	if body.DryRun {
		steps = append(steps, step{"stop ai-" + body.ID, true, "dry-run: no services stopped"})
	} else if s.d.Actions != nil {
		result, code := s.d.Actions.ModelAction(body.ID, "stop")
		if code != http.StatusOK || result["ok"] != true {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"ok": false, "detail": "cannot safely remove weights while model is running", "stop": result})
			return
		}
		steps = append(steps, step{"stop ai-" + body.ID, true, "container stop verified"})
	} else if out, err := exec.Command("systemctl", "stop", "ai-"+body.ID).CombinedOutput(); err != nil {
		note := strings.TrimSpace(string(out))
		if note == "" {
			note = "not running / no service"
		}
		steps = append(steps, step{"stop ai-" + body.ID, true, note})
	} else {
		steps = append(steps, step{"stop ai-" + body.ID, true, ""})
	}

	// 2. delete the model files from disk
	if doDisk {
		if info.LocalPath != "" && dirExists(info.LocalPath) {
			if body.DryRun {
				steps = append(steps, step{"delete files", true, "dry-run: would remove " + info.LocalPath})
			} else if err := os.RemoveAll(info.LocalPath); err != nil {
				steps = append(steps, step{"delete files", false, err.Error()})
			} else {
				steps = append(steps, step{"delete files", true, info.LocalPath})
			}
		} else {
			steps = append(steps, step{"delete files", true, "nothing on disk"})
		}
	}

	// 3-5. unwire from the hub (registry + service + Open WebUI)
	if doHub {
		d, n := removeRegistryEntry(body.ID, body.DryRun)
		steps = append(steps, step{"remove registry entry", d, n})
		d, n = removeModelService(body.ID, body.DryRun)
		steps = append(steps, step{"remove systemd service", d, n})
		d, n = removeWebuiEntry(info.Port, body.DryRun)
		steps = append(steps, step{"remove Open WebUI URL + key", d, n})
	} else {
		steps = append(steps, step{"keep download source and launch configuration", true, "model can be downloaded again"})
	}

	allDone := true
	for _, st := range steps {
		if !st.Done {
			allDone = false
		}
	}
	if allDone && doDisk && !body.DryRun {
		_ = models.Clear(body.ID)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": allDone, "id": body.ID, "dry_run": body.DryRun, "steps": steps})
}

// --- model-delete helpers (path overridable via env for tests) --------------

func modelRegistryPath() string {
	if v := os.Getenv("AIHUB_REGISTRY"); v != "" {
		return v
	}
	return "/srv/ai-hub/registry.yaml"
}

func modelServiceDir() string {
	if v := os.Getenv("AIHUB_SYSTEMD_DIR"); v != "" {
		return v
	}
	return "/etc/systemd/system"
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func backupFile(p string) string {
	bak := p + ".bak-del-" + time.Now().Format("20060102-150405")
	if data, err := os.ReadFile(p); err == nil {
		_ = os.WriteFile(bak, data, 0644)
	}
	return bak
}

// removeRegistryEntry removes the "- id: <id>" block from the registry YAML.
// Text-based + line-anchored so the rest of the file (formatting, other models)
// is untouched. Returns (done, note).
func removeRegistryEntry(id string, dryRun bool) (bool, string) {
	p := modelRegistryPath()
	data, err := os.ReadFile(p)
	if err != nil {
		return false, "cannot read registry: " + err.Error()
	}
	lines := strings.Split(string(data), "\n")
	start := -1
	for i, l := range lines {
		if l == "- id: "+id {
			start = i
			break
		}
	}
	if start < 0 {
		return true, "not in registry (already removed)"
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "- id: ") {
			end = i
			break
		}
	}
	removed := end - start
	if dryRun {
		return true, fmt.Sprintf("dry-run: would remove %d lines (block for %s)", removed, id)
	}
	backupFile(p)
	newLines := append(append([]string{}, lines[:start]...), lines[end:]...)
	if err := os.WriteFile(p, []byte(strings.Join(newLines, "\n")), 0644); err != nil {
		return false, err.Error()
	}
	return true, fmt.Sprintf("removed %d lines (block for %s)", removed, id)
}

// removeModelService removes /etc/systemd/system/ai-<id>.service + daemon-reload.
func removeModelService(id string, dryRun bool) (bool, string) {
	svc := filepath.Join(modelServiceDir(), "ai-"+id+".service")
	if _, err := os.Stat(svc); os.IsNotExist(err) {
		return true, "no service file (already removed)"
	}
	if dryRun {
		return true, "dry-run: would remove " + svc + " + daemon-reload"
	}
	backupFile(svc)
	if err := os.Remove(svc); err != nil {
		return false, err.Error()
	}
	exec.Command("systemctl", "daemon-reload").Run()
	return true, "removed " + svc + " + daemon-reload"
}

// removeWebuiEntry removes the model's :PORT/v1 URL (and its matching API key)
// from the Open WebUI service, then restarts it so the change takes effect.
func removeWebuiEntry(port int, dryRun bool) (bool, string) {
	p := filepath.Join(modelServiceDir(), "ai-open-webui.service")
	data, err := os.ReadFile(p)
	if err != nil {
		return false, "cannot read Open WebUI service: " + err.Error()
	}
	s := string(data)
	urlRe := regexp.MustCompile(`OPENAI_API_BASE_URLS="([^"]*)"`)
	m := urlRe.FindStringSubmatch(s)
	if m == nil {
		return true, "no OPENAI_API_BASE_URLS (already removed?)"
	}
	urls := strings.Split(m[1], ";")
	idx := -1
	for i, u := range urls {
		if strings.Contains(u, fmt.Sprintf(":%d/v1", port)) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return true, fmt.Sprintf(":%d not in Open WebUI (already removed)", port)
	}
	if dryRun {
		return true, fmt.Sprintf("dry-run: would remove :%d URL + key #%d", port, idx)
	}
	backupFile(p)
	if idx < len(urls)-1 {
		s = strings.Replace(s, urls[idx]+";", "", 1)
	} else {
		s = strings.Replace(s, ";"+urls[idx], "", 1)
	}
	keyRe := regexp.MustCompile(`OPENAI_API_KEYS="([^"]*)"`)
	if kv := keyRe.FindStringSubmatch(s); kv != nil {
		keys := strings.Split(kv[1], ";")
		if idx < len(keys) {
			keys = append(keys[:idx], keys[idx+1:]...)
			s = keyRe.ReplaceAllString(s, `OPENAI_API_KEYS="`+strings.Join(keys, ";")+`"`)
		}
	}
	if err := os.WriteFile(p, []byte(s), 0644); err != nil {
		return false, err.Error()
	}
	exec.Command("systemctl", "daemon-reload").Run()
	exec.Command("systemctl", "restart", "ai-open-webui").Run()
	return true, fmt.Sprintf("removed :%d from Open WebUI (URL + key #%d)", port, idx)
}

// handleExport streams the long-term GPU telemetry log as CSV (Go-only
// endpoint). ?what=gpu&hours=N — one row per sample with per-GPU columns.
// The log is bounded by logrotate, so this is the authoritative long-term
// source (the in-memory ring only keeps ~6 minutes).
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	q := r.URL.Query()
	what := q.Get("what")
	if what == "" {
		what = "gpu"
	}
	hours := 24
	if h, err := strconv.Atoi(q.Get("hours")); err == nil && h > 0 {
		hours = h
	}
	if hours > 24*30 {
		hours = 24 * 30 // cap: 30 days
	}
	if what != "gpu" {
		http.Error(w, "unknown 'what' (expected: gpu)", http.StatusBadRequest)
		return
	}
	cutoff := float64(time.Now().Unix() - int64(hours*3600))
	rows := status.ReadTelog(cutoff)
	if rows == nil {
		http.Error(w, "telemetry log not available yet (no samples recorded)", http.StatusNotFound)
		return
	}
	maxGpu := 0
	for _, r := range rows {
		if len(r.Core) > maxGpu {
			maxGpu = len(r.Core)
		}
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="ai-hub-gpu-%dh.csv"`, hours))
	w.Header().Set("Cache-Control", "no-store")
	wr := csv.NewWriter(w)
	header := []string{"ts", "iso_utc"}
	for i := 0; i < maxGpu; i++ {
		header = append(header,
			fmt.Sprintf("gpu%d_core_c", i), fmt.Sprintf("gpu%d_vram_c", i),
			fmt.Sprintf("gpu%d_power_w", i), fmt.Sprintf("gpu%d_util_pct", i),
			fmt.Sprintf("gpu%d_mem_gb", i))
	}
	if err := wr.Write(header); err != nil {
		return
	}
	for _, r := range rows {
		row := []string{strconv.FormatFloat(r.TS, 'f', 0, 64), time.Unix(int64(r.TS), 0).UTC().Format(time.RFC3339)}
		for i := 0; i < maxGpu; i++ {
			row = append(row, telogVal(r.Core, i), telogVal(r.Vram, i), telogVal(r.Power, i), telogVal(r.Util, i), telogMemGB(r.Mem, i))
		}
		if err := wr.Write(row); err != nil {
			return
		}
	}
	wr.Flush()
}

// telogVal renders a per-GPU telemetry value ("-" for no reading / out of range).
func telogVal(a []int, i int) string {
	if i >= len(a) || a[i] < 0 {
		return "-"
	}
	return strconv.Itoa(a[i])
}

func telogMemGB(a []int, i int) string {
	if i >= len(a) || a[i] < 0 {
		return "-"
	}
	return strconv.FormatFloat(float64(a[i])/1024.0, 'f', 1, 64)
}

// handleLastWords returns one /var/log/last-words snapshot as plain text.
// The name is validated (snapshot-*.txt, no path separators) to prevent
// traversal; content is size-capped at 512KB.
func (s *Server) handleLastWords(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	name := r.PathValue("name")
	if !lastWordsNameRe.MatchString(name) {
		http.Error(w, "invalid snapshot name", http.StatusBadRequest)
		return
	}
	data, err := os.ReadFile(filepath.Join("/var/log/last-words", name))
	if err != nil {
		http.Error(w, "snapshot not found", http.StatusNotFound)
		return
	}
	if len(data) > 512*1024 {
		data = data[:512*1024]
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
}

var lastWordsNameRe = regexp.MustCompile(`^snapshot-\d{8}-\d{6}\.txt$`)

// handleMetrics exposes the live hub state in Prometheus text exposition
// format (version 0.0.4). Read-only; no auth so scrapers need no cookie.
// GPU per-index gauges, per-model running/healthy, and system RAM/swap/EDAC/
// uptime from the monitoring logs.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	snap := s.d.Poller.Cache().Snapshot()
	gpus, _ := snap["gpus"].([]status.GPU)
	models, _ := snap["models"].([]status.ModelStatusEntry)
	h := status.SystemHealth()

	var b strings.Builder
	gauge := func(name, help string) {
		b.WriteString("# HELP " + name + " " + help + "\n")
		b.WriteString("# TYPE " + name + " gauge\n")
	}
	metric := func(name string, labels string, val string) {
		b.WriteString(name)
		if labels != "" {
			b.WriteString("{")
			b.WriteString(labels)
			b.WriteString("}")
		}
		b.WriteString(" ")
		b.WriteString(val)
		b.WriteByte('\n')
	}

	// GPU per-index gauges
	gauge("aihub_gpu_temp_c", "GPU core temperature in Celsius")
	for _, g := range gpus {
		metric("aihub_gpu_temp_c", fmt.Sprintf(`index="%d"`, g.Index), strconv.Itoa(g.Temp))
	}
	gauge("aihub_gpu_vram_temp_c", "GPU GDDR6X memory temperature in Celsius")
	for _, g := range gpus {
		if g.VramTemp > 0 {
			metric("aihub_gpu_vram_temp_c", fmt.Sprintf(`index="%d"`, g.Index), strconv.Itoa(g.VramTemp))
		}
	}
	gauge("aihub_gpu_power_w", "GPU power draw in watts")
	for _, g := range gpus {
		metric("aihub_gpu_power_w", fmt.Sprintf(`index="%d"`, g.Index), strconv.Itoa(g.Power))
	}
	gauge("aihub_gpu_util_percent", "GPU utilization in percent")
	for _, g := range gpus {
		metric("aihub_gpu_util_percent", fmt.Sprintf(`index="%d"`, g.Index), strconv.Itoa(g.Util))
	}
	gauge("aihub_gpu_mem_used_bytes", "GPU memory used in bytes")
	for _, g := range gpus {
		metric("aihub_gpu_mem_used_bytes", fmt.Sprintf(`index="%d"`, g.Index), strconv.Itoa(g.MemUsed*1024*1024))
	}

	// Per-model running / healthy
	gauge("aihub_model_running", "1 if the model is running, else 0")
	gauge("aihub_model_healthy", "1 if a running model passed its health check, else 0")
	for _, m := range models {
		running := 0
		if m.Running {
			running = 1
		}
		healthy := 0
		if m.Running && m.Healthy {
			healthy = 1
		}
		labels := fmt.Sprintf(`model="%s",port="%d"`, m.ID, m.Port)
		metric("aihub_model_running", labels, strconv.Itoa(running))
		metric("aihub_model_healthy", labels, strconv.Itoa(healthy))
	}

	// System (from the monitoring TSV logs)
	const gib = 1073741824
	if ram, ok := h["ram"].(map[string]interface{}); ok {
		if u, ok := ram["used_gb"].(float64); ok {
			metric("aihub_ram_used_bytes", "", strconv.FormatInt(int64(u*gib), 10))
		}
		if t, ok := ram["total_gb"].(float64); ok {
			metric("aihub_ram_total_bytes", "", strconv.FormatInt(int64(t*gib), 10))
		}
	}
	if sw, ok := h["swap_gb"].(float64); ok {
		metric("aihub_swap_used_bytes", "", strconv.FormatInt(int64(sw*gib), 10))
	}
	if edac, ok := h["edac"].(map[string]interface{}); ok {
		if ce, ok := edac["ce_total"].(int); ok {
			metric("aihub_edac_correctable_total", "", strconv.Itoa(ce))
		}
		if ue, ok := edac["ue_total"].(int); ok {
			metric("aihub_edac_uncorrectable_total", "", strconv.Itoa(ue))
		}
	}
	if up, ok := h["uptime_s"].(int64); ok {
		metric("aihub_uptime_seconds", "", strconv.FormatInt(up, 10))
	}

	metric("aihub_up", "", "1")

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(b.String()))
}

// handleModelAction mirrors app.py model_action (body is ignored, like the
// Python endpoint which takes Request but never parses it).
// Query params: ?gpus=N&ctx=C&parallel=P — written to state file before
// starting, so wrapper scripts can read the GPU/ctx/agents selection from the
// launch controls on the model card.
func (s *Server) handleModelAction(w http.ResponseWriter, r *http.Request) {
	drainBody(r)
	if !s.requireAuth(w, r) {
		return
	}
	mid := r.PathValue("mid")
	action := r.PathValue("action")
	// Cloud (OpenAI OAuth) models: the card's Start/Stop controls the shared
	// provider unit instead of a local registry model. Intercepted before the
	// registry lookup so an unknown-to-registry id still works.
	if cfg := cloud.MustLoad(s.d.AIHUBRoot); cfg.Enabled && cfg.Get(mid) != nil {
		body, code := cfg.Action(action)
		if code == http.StatusOK {
			if ok, _ := body["ok"].(bool); ok {
				switch action {
				case "start":
					webhooks.Fire("model.start", mid, "cloud provider (openai-oauth)")
				case "stop":
					webhooks.Fire("model.stop", mid, "cloud provider (openai-oauth)")
				}
			}
		}
		writeJSON(w, code, body)
		return
	}
	q := r.URL.Query()
	if (action == "start" || action == "restart") && s.d.Reg != nil {
		if m := s.d.Reg.Get(mid); m != nil && m.RequiresWeights && !models.Ready(mid) {
			writeJSON(w, http.StatusConflict, map[string]interface{}{"ok": false, "detail": "Download this model in Model manager before starting it"})
			return
		}
	}
	gpus, ctx, par := actions.ParseLaunchParams(q.Get("gpus"), q.Get("ctx"), q.Get("parallel"))
	body, code := s.d.Actions.ModelActionWithParams(mid, action, gpus, ctx, par)
	// Fire a webhook on a successful start/stop (best-effort, never blocks).
	if code == http.StatusOK {
		if ok, _ := body["ok"].(bool); ok {
			switch action {
			case "start":
				webhooks.Fire("model.start", mid, fmt.Sprintf("gpus=%d ctx=%d parallel=%d", gpus, ctx, par))
			case "stop":
				webhooks.Fire("model.stop", mid, "stopped")
			}
		}
	}
	writeJSON(w, code, body)
}

// handleStopAll stops ALL running LLM models in one call (Go extension —
// app.py has no stop-all endpoint, so no parity constraint). Auth, body
// drain and the 401 body match the other mutation endpoints exactly.
// Response:
//
//	{"ok":true,"stopped":["m-a","m-b"],"errors":{"m-c":"<message>"}}
//
// "stopped" lists the models actually stopped, in registry order;
// "errors" is an object (possibly empty {}) mapping model id -> message.
// A per-model failure does not abort the others.
func (s *Server) handleStopAll(w http.ResponseWriter, r *http.Request) {
	drainBody(r)
	if !s.requireAuth(w, r) {
		return
	}
	stopped, errs := s.d.Actions.StopAll(s.runningFn())
	for _, mid := range stopped {
		webhooks.Fire("model.stop", mid, "stopped (stop-all)")
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"stopped": stopped,
		"errors":  errs,
	})
}

// cloudEntries builds the status entries for enabled cloud models (nil when
// the cloud provider is disabled or has no enabled models). Loaded fresh from
// disk so a settings save takes effect on the next poll without a restart.
func (s *Server) cloudEntries() []status.ModelStatusEntry {
	cfg := cloud.MustLoad(s.d.AIHUBRoot)
	if !cfg.Enabled {
		return nil
	}
	return cfg.Entries()
}

// runningFn returns a "is model mid running right now" predicate for
// StopAll, reusing the status package's running detection (systemctl
// is-active + docker ps fallback via status.ModelStatus). It prefers the
// poller's cache — the same source /api/status serves, so stop-all stops
// exactly what the UI shows as running — and falls back to a fresh
// status.ModelStatus call for ids the cache does not cover yet (e.g.
// before the first poll completes).
func (s *Server) runningFn() func(string) bool {
	running := map[string]bool{}
	if snap := s.d.Poller.Cache().Snapshot(); snap != nil {
		if models, ok := snap["models"].([]status.ModelStatusEntry); ok {
			for _, m := range models {
				running[m.ID] = m.Running
			}
		}
	}
	var portToIDs map[int][]string
	if s.d.Reg != nil {
		portToIDs = status.BuildPortToIDs(s.d.Reg)
	}
	return func(mid string) bool {
		if v, ok := running[mid]; ok {
			return v
		}
		if s.d.Reg == nil {
			return false
		}
		if m := s.d.Reg.Get(mid); m != nil {
			return status.ModelStatus(*m, portToIDs).Running
		}
		return false
	}
}

// handleModelStats mirrors app.py model_stats: history from the poller's
// ring buffer, live = most recent history entry, one-shot scrape (with ts)
// when the poller has nothing yet and the model has a port.
func (s *Server) handleModelStats(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	mid := r.PathValue("mid")
	m := s.d.Reg.Get(mid)
	if m == nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"detail": "unknown model: " + mid})
		return
	}
	history := s.d.Stats.Get(mid)
	var live interface{}
	if n := len(history); n > 0 {
		live = history[n-1]
	}
	if live == nil && m.Port != 0 {
		one := metrics.ScrapeModelMetrics(m.Port, m.Backend, mid, s.d.Stats.Scrapper())
		if one != nil {
			one["ts"] = float64(time.Now().UnixNano()) / 1e9
			live = one
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":       mid,
		"live":     live,
		"history":  history,
		"interval": metrics.StatsPoll,
	})
}

// handleServiceAction mirrors app.py service_action (service first, model
// fallback).
func (s *Server) handleServiceAction(w http.ResponseWriter, r *http.Request) {
	drainBody(r)
	if !s.requireAuth(w, r) {
		return
	}
	name := r.PathValue("name")
	action := r.PathValue("action")
	body, code := s.d.Actions.ServiceAction(name, action)
	writeJSON(w, code, body)
}

// handleServiceBuild mirrors app.py service_build.
func (s *Server) handleServiceBuild(w http.ResponseWriter, r *http.Request) {
	drainBody(r)
	if !s.requireAuth(w, r) {
		return
	}
	name := r.PathValue("name")
	body, code := s.d.Actions.ServiceBuild(name)
	writeJSON(w, code, body)
}

// handleProfile mirrors app.py apply_profile.
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	drainBody(r)
	if !s.requireAuth(w, r) {
		return
	}
	pname := r.PathValue("pname")
	body, code := s.d.Actions.ApplyProfile(pname)
	writeJSON(w, code, body)
}

// handleLogs mirrors app.py logs: plain-text journalctl tail, JSON error body
// on 404.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	mid := r.PathValue("mid")
	tail := 200
	if q := r.URL.Query().Get("tail"); q != "" {
		if n, err := strconv.Atoi(q); err == nil {
			tail = n
		}
	}
	text, code := s.d.Actions.Logs(mid, tail)
	if code != 200 {
		writeJSON(w, code, map[string]interface{}{"detail": "unknown model/service: " + mid})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, text)
}

// handleDownloads mirrors app.py downloads: plain-text body.
func (s *Server) handleDownloads(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	text, code := s.d.Actions.Downloads()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	fmt.Fprint(w, text)
}
