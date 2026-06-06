package server

import (
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// ComfyUI proxy (Go-only extension, no Python parity constraint).
//
// ComfyUI (aiohttp) rejects any request whose Origin header does not match
// its own host — so the hub page (on :8080) cannot call the ComfyUI API
// (:8188) directly from the browser (403 on every cross-origin request,
// including the CORS preflight). This small same-origin proxy forwards a
// strict whitelist of ComfyUI endpoints from the hub port so the quick
// video-generation UI (static/js/quickgen.js) works:
//
//	POST /api/comfy/prompt           → ComfyUI /prompt            (queue a workflow)
//	GET  /api/comfy/history/{id}    → ComfyUI /history/{id}      (poll until done)
//	GET  /api/comfy/view?…          → ComfyUI /view?…            (stream the artifact)
//
// Auth: same cookie as the rest of the hub API. The forwarded request
// carries NO Origin/Referer headers (that is exactly what ComfyUI 403s).

// comfyBase returns the ComfyUI base URL (tests override s.comfyBaseURL,
// the environment can set AIHUB_COMFYUI_URL, default is the LAN box).
func (s *Server) comfyBase() string {
	if s.comfyBaseURL != "" {
		return strings.TrimRight(s.comfyBaseURL, "/")
	}
	if v := os.Getenv("AIHUB_COMFYUI_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://127.0.0.1:8188"
}

var (
	comfyHistoryID = regexp.MustCompile(`^history/[\w-]+$`)
	comfyBodyMax   = int64(1) << 20 // 1 MiB — a workflow JSON is tens of KB
)

func (s *Server) comfyRouteOK(method, rest string) bool {
	switch {
	case method == http.MethodPost && rest == "prompt":
		return true
	case method == http.MethodGet && comfyHistoryID.MatchString(rest):
		return true
	case method == http.MethodGet && rest == "view":
		return true
	}
	return false
}

// handleComfyProxy forwards a whitelisted ComfyUI endpoint (see package
// comment). It streams the response body back untouched, keeping the
// upstream status code and Content-Type (needed for <video> playback).
func (s *Server) handleComfyProxy(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	rest := r.PathValue("rest")
	if !s.comfyRouteOK(r.Method, rest) {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"detail": "unknown comfy proxy endpoint: " + r.Method + " /api/comfy/" + rest,
		})
		return
	}
	target := s.comfyBase() + "/" + rest
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, nil)
	if err != nil {
		httplog.Printf("comfy proxy: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"detail": "bad upstream request"})
		return
	}
	if r.Method == http.MethodPost {
		body, rerr := io.ReadAll(io.LimitReader(r.Body, comfyBodyMax))
		if rerr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"detail": "read request body: " + rerr.Error()})
			return
		}
		req.Body = io.NopCloser(strings.NewReader(string(body)))
		req.ContentLength = int64(len(body))
		req.Header.Set("Content-Type", "application/json")
	}
	// NO Origin/Referer: ComfyUI 403s any request with a foreign Origin.

	client := s.comfyClient
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		httplog.Printf("comfy proxy: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{
			"detail": "ComfyUI is unreachable — is it started (hub panel → FLUX & Video Studio → Start)?",
		})
		return
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// registerComfyRoutes wires the proxy endpoints (called from Handler).
func (s *Server) registerComfyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/comfy/{rest...}", s.handleComfyProxy)
	mux.HandleFunc("GET /api/comfy/{rest...}", s.handleComfyProxy)
}
