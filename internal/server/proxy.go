package server

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"aihub/internal/credentials"
	"aihub/internal/sh"
	"aihub/internal/status"
)

var httplog = log.New(os.Stderr, "aihub: ", 0)

// Universal proxy (Go-only extension, no Python parity constraint).
//
// OpenAI-compatible endpoints on the hub port that always forward to the
// LAST-STARTED running LLM, so clients (opencode "Universal" models, Open
// WebUI, scripts) keep one stable base URL and never need to re-point at a
// new port after switching models in the hub:
//
//	GET  /v1/models            -> model list (synthesized "universal")
//	POST /v1/chat/completions  -> forwarded to http://127.0.0.1:<port>/v1/chat/completions
//
// Target selection: among running LLMs (category "LLM") from the poller
// cache, pick the one whose docker container started last (docker inspect
// State.StartedAt; container name == model id in every wrapper script) —
// the model the user just switched to, even if it is still loading. llama.cpp
// binds its port only AFTER the model has fully loaded into VRAM (76 GB-class
// models: 1-3 min), so a just-started model is running-but-unhealthy while
// loading: the proxy WAITS for the target's health check to pass (5 min
// deadline, 3 s poll, early exit if the container stops) instead of failing
// the first requests after a model switch. If the target never comes up, the
// proxy falls back to the newest healthy running model; if none, 503 with a
// clear message. The HTTP middleware authenticates clients; hub bearer tokens
// are removed before forwarding to the loopback inference backend.

// universalLoadDeadline is how long the proxy waits for a just-started
// (still loading) target to become healthy before falling back / erroring.
const universalLoadDeadline = 5 * time.Minute

// containerStartedAt returns the docker start time of the container named
// after mid. ok=false when docker is unavailable (dev box) or the container
// does not exist.
func containerStartedAt(mid string) (time.Time, bool) {
	_, out := sh.Run([]string{"docker", "inspect", "-f", "{{.State.StartedAt}}", mid}, 3*time.Second)
	s := strings.TrimSpace(out)
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		// Native profiles run as systemd services. Convert the monotonic
		// start timestamp using host uptime to compare with Docker starts.
		_, raw := sh.Run([]string{"systemctl", "show", "ai-" + mid + ".service", "-p", "ActiveEnterTimestampMonotonic", "--value"}, 2*time.Second)
		us, e := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		b, e2 := os.ReadFile("/proc/uptime")
		fields := strings.Fields(string(b))
		if e != nil || e2 != nil || us <= 0 || len(fields) == 0 {
			return time.Time{}, false
		}
		up, e3 := strconv.ParseFloat(fields[0], 64)
		if e3 != nil {
			return time.Time{}, false
		}
		return time.Now().Add(time.Duration(us)*time.Microsecond - time.Duration(up*float64(time.Second))), true
	}
	return t, true
}

// containerStillRunning reports whether the container named after mid is
// still running (a crashed/stopped container makes the load-wait give up
// early instead of burning the full deadline).
func containerStillRunning(mid string) bool {
	_, out := sh.Run([]string{"docker", "inspect", "-f", "{{.State.Running}}", mid}, 2*time.Second)
	if strings.TrimSpace(out) == "true" {
		return true
	}
	_, out = sh.Run([]string{"systemctl", "show", "ai-" + mid + ".service", "-p", "MainPID", "--value"}, 2*time.Second)
	pid, err := strconv.Atoi(strings.TrimSpace(out))
	return err == nil && pid > 0
}

// pickUniversalTarget chooses the forwarding target from running LLMs: the
// one whose docker container started LAST — the model the user just switched
// to — regardless of its current health (the handler waits for a
// still-loading target to become ready before forwarding). With no usable
// start times (no docker — dev box) a healthy candidate wins, then the first
// in registry order.
func pickUniversalTarget(running []status.ModelStatusEntry, startedAt func(id string) (time.Time, bool)) *status.ModelStatusEntry {
	if len(running) == 0 {
		return nil
	}
	best := &running[0]
	var bestT time.Time
	have := false
	for i := range running {
		t, ok := startedAt(running[i].ID)
		if !ok {
			continue
		}
		if !have || t.After(bestT) {
			best, bestT, have = &running[i], t, true
		}
	}
	if !have {
		for i := range running {
			if running[i].Healthy {
				best = &running[i]
				break
			}
		}
	}
	return best
}

// pickHealthyFallback returns the last-started HEALTHY running LLM (nil when
// none is healthy) — used when the primary target never becomes ready.
func pickHealthyFallback(running []status.ModelStatusEntry, startedAt func(id string) (time.Time, bool)) *status.ModelStatusEntry {
	healthy := []status.ModelStatusEntry{}
	for _, m := range running {
		if m.Healthy {
			healthy = append(healthy, m)
		}
	}
	if len(healthy) == 0 {
		return nil
	}
	return pickUniversalTarget(healthy, startedAt)
}

// runningLLMs returns the running LLMs from the poller cache — the same
// source /api/status serves.
func (s *Server) runningLLMs() []status.ModelStatusEntry {
	var running []status.ModelStatusEntry
	if snap := s.d.Poller.Cache().Snapshot(); snap != nil {
		if models, ok := snap["models"].([]status.ModelStatusEntry); ok {
			for _, m := range models {
				if m.Running && m.Port != 0 && m.Category == "LLM" {
					running = append(running, m)
				}
			}
		}
	}
	return running
}

// freshRunningLLMs re-runs the per-model status checks on demand and returns
// the LLMs running RIGHT NOW. The poller cache lags a fresh model switch by
// up to one poll interval (3 s); this closes that window for the universal
// proxy (a request seconds after "Start" must not get "no LLM is running").
func (s *Server) freshRunningLLMs() []status.ModelStatusEntry {
	if s.d.Reg == nil {
		return nil
	}
	portToIDs := status.BuildPortToIDs(s.d.Reg)
	var running []status.ModelStatusEntry
	for _, m := range s.d.Reg.Models {
		if m.Category != "LLM" || m.Port == 0 {
			continue
		}
		st := status.ModelStatus(m, portToIDs)
		if st.Running {
			running = append(running, st)
		}
	}
	return running
}

// freshRunning returns the on-demand running-LLM list (injected in tests).
func (s *Server) freshRunning() []status.ModelStatusEntry {
	if s.freshLLMs != nil {
		return s.freshLLMs()
	}
	return s.freshRunningLLMs()
}

// targetCandidates is the running-LLM list the proxy selects from: the poller
// cache, with an on-demand re-check when the cache sees nothing running
// (just-started model not yet polled).
func (s *Server) targetCandidates() []status.ModelStatusEntry {
	running := s.runningLLMs()
	if len(running) == 0 {
		running = s.freshRunning()
	}
	return running
}

// startedAtFn returns the injected start-time resolver (or the real docker
// one).
func (s *Server) startedAtFn() func(id string) (time.Time, bool) {
	if s.startedAt != nil {
		return s.startedAt
	}
	return containerStartedAt
}

// universalTarget resolves the current forwarding target (nil when no LLM is
// running): the last-started running model, even if it is still loading.
func (s *Server) universalTarget() *status.ModelStatusEntry {
	return pickUniversalTarget(s.targetCandidates(), s.startedAtFn())
}

// universalHealthyFallback is the last-started healthy running model per the
// poller cache (nil when none is healthy) — the last-resort fallback in
// retryTarget when the on-demand check finds nothing.
func (s *Server) universalHealthyFallback() *status.ModelStatusEntry {
	return pickHealthyFallback(s.targetCandidates(), s.startedAtFn())
}

// freshTarget re-resolves the target from an on-demand status check (NOT the
// poller cache) and picks the last-started running model. Used when the
// cached selection proves stale: the cache lags a model switch by up to one
// 3 s poll, so it can still name a just-stopped model (or nothing) while the
// just-started one is loading.
func (s *Server) freshTarget() *status.ModelStatusEntry {
	if s.d.Reg == nil {
		return nil
	}
	return pickUniversalTarget(s.freshRunning(), s.startedAtFn())
}

// freshHealthyTarget is the last-started healthy model per the on-demand
// check (nil when none is healthy).
func (s *Server) freshHealthyTarget() *status.ModelStatusEntry {
	if s.d.Reg == nil {
		return nil
	}
	return pickHealthyFallback(s.freshRunning(), s.startedAtFn())
}

// retryTarget picks an alternative target after the primary failed to become
// ready: prefer the last-started running model per the on-demand check, then
// the last-started healthy one, then the poller cache's healthy view. It
// never returns the same model again; nil when there is nothing else to try.
func (s *Server) retryTarget(prev *status.ModelStatusEntry) *status.ModelStatusEntry {
	for _, c := range []*status.ModelStatusEntry{s.freshTarget(), s.freshHealthyTarget(), s.universalHealthyFallback()} {
		if c != nil && c.ID != prev.ID {
			return c
		}
	}
	return nil
}

// writeNotReady answers 503 when the target model never became ready and no
// alternative exists.
func (s *Server) writeNotReady(w http.ResponseWriter, target *status.ModelStatusEntry) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
		"error": map[string]interface{}{
			"message": fmt.Sprintf("%s is running but not responding (still loading, or crashed) — try again shortly; check the hub panel for its status", target.ID),
			"type":    "ai_hub_error",
		},
	})
}

// waitForModelReady polls the target port's health check until it passes
// (model finished loading into VRAM and llama.cpp opened its port), the
// deadline elapses, the target's container stops (crashed — no point
// waiting), or the request context is cancelled.
func (s *Server) waitForModelReady(ctx context.Context, id string, port int) bool {
	deadline := s.loadWait
	if deadline <= 0 {
		deadline = universalLoadDeadline
	}
	tick := s.healthPoll
	if tick <= 0 {
		tick = 3 * time.Second
	}
	cr := s.containerRunning
	if cr == nil {
		cr = containerStillRunning
	}
	if status.HealthCheck(port) {
		return true
	}
	// A model that started more than a full load deadline ago should have its
	// port open already: if it does not answer, it is broken, not loading —
	// fail fast instead of making the client wait.
	if t, ok := s.startedAtFn()(id); ok && time.Since(t) > deadline {
		httplog.Printf("universal proxy: target %s (:%d) started %v ago but is not responding — treating as broken, not loading", id, port, time.Since(t).Round(time.Second))
		return false
	}
	httplog.Printf("universal proxy: target %s (:%d) not responding yet — waiting up to %v for it to load", id, port, deadline)
	start := time.Now()
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	ends := time.NewTimer(deadline)
	defer ends.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-ends.C:
			httplog.Printf("universal proxy: target %s (:%d) still not responding after %v", id, port, time.Since(start).Round(time.Second))
			return false
		case <-ticker.C:
			if status.HealthCheck(port) {
				httplog.Printf("universal proxy: target %s (:%d) ready after %v", id, port, time.Since(start).Round(time.Second))
				return true
			}
			if !cr(id) {
				httplog.Printf("universal proxy: target %s container stopped while loading — giving up", id)
				return false
			}
		}
	}
}

// hopHeaders are stripped when copying request/response headers.
var hopHeaders = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"TE":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

// copyFlush copies src to dst flushing after each chunk (SSE pass-through).
func copyFlush(dst io.Writer, src io.Reader) (int64, error) {
	flusher, _ := dst.(http.Flusher)
	buf := make([]byte, 32*1024)
	var written int64
	for {
		n, rerr := src.Read(buf)
		if n > 0 {
			m, werr := dst.Write(buf[:n])
			written += int64(m)
			if flusher != nil {
				flusher.Flush()
			}
			if werr != nil {
				return written, werr
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				return written, nil
			}
			return written, rerr
		}
	}
}

// handleUniversalModels serves the proxy's model list. One synthesized entry
// ("universal") — the concrete backend depends on what is running.
func (s *Server) handleUniversalModels(w http.ResponseWriter, r *http.Request) {
	target := s.universalTarget()
	owned := "ai-hub"
	if target != nil {
		owned = "ai-hub/" + target.ID
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"object": "list",
		"data": []map[string]interface{}{
			{"id": "universal", "object": "model", "created": int64(time.Now().Unix()), "owned_by": owned},
		},
	})
}

// handleUniversalChat forwards POST /v1/chat/completions to the last-started
// running model. Streaming (SSE) bodies are passed through chunk-by-chunk
// with flushes; the upstream client has NO overall timeout (long generations
// are normal; opencode sets its own chunk timeout).
func (s *Server) handleUniversalChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{
			"error": map[string]interface{}{"message": "POST only", "type": "invalid_request_error"},
		})
		return
	}
	target := s.universalTarget()
	if target == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": map[string]interface{}{
				"message": "no LLM is running on ai-hub — start a model in the hub panel first",
				"type":    "ai_hub_error",
			},
		})
		return
	}

	// The poller cache's running/healthy flags lag a model switch by up to
	// one poll interval (3 s), so they are never trusted blindly: before
	// forwarding, verify the target is actually serving right now.
	// waitForModelReady does a fast /health probe (a no-op when the model is
	// ready) and otherwise polls while the model is still loading weights
	// into VRAM (llama.cpp opens its port only after the load — 76 GB-class
	// models take 1-3 min), so the first requests after a model switch wait
	// for the model instead of failing with "connection refused".
	if !s.waitForModelReady(r.Context(), target.ID, target.Port) {
		if rt := s.retryTarget(target); rt != nil {
			httplog.Printf("universal proxy: %s not ready — re-resolving to %s (:%d)", target.ID, rt.ID, rt.Port)
			target = rt
			if !s.waitForModelReady(r.Context(), target.ID, target.Port) {
				s.writeNotReady(w, target)
				return
			}
		} else {
			s.writeNotReady(w, target)
			return
		}
	}

	upURL := fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", target.Port)
	upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upURL, r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{
			"error": map[string]interface{}{"message": err.Error(), "type": "upstream_error"},
		})
		return
	}
	upReq.Header.Set("Content-Type", "application/json")
	// The hub credential must never be disclosed to an inference backend.
	// Legacy passthrough is available only in explicit unauthenticated mode.
	if a := r.Header.Get("Authorization"); a != "" && !s.d.RequireAPIToken {
		upReq.Header.Set("Authorization", a)
	}
	if strings.EqualFold(target.Backend, "Strata") || strings.EqualFold(target.Backend, "Maya") {
		if key := credentials.Key(target.ID); key != "" {
			upReq.Header.Set("Authorization", "Bearer "+key)
		}
	}
	if t := r.Header.Get("X-Title"); t != "" {
		upReq.Header.Set("X-Title", t)
	}

	// No Client.Timeout: generations can run for minutes.
	resp, err := (&http.Client{}).Do(upReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{
			"error": map[string]interface{}{
				"message": fmt.Sprintf("upstream %s (:%d) unreachable: %v", target.ID, target.Port, err),
				"type":    "upstream_error",
			},
		})
		return
	}
	defer resp.Body.Close()

	for k, vs := range resp.Header {
		if hopHeaders[http.CanonicalHeaderKey(k)] {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := copyFlush(w, resp.Body); err != nil && r.Context().Err() == nil {
		// Stream died mid-way and the client is still connected: nothing more
		// can be sent (headers are out), just log.
		httplog.Printf("universal proxy: %s stream interrupted: %v", target.ID, err)
	}
}
