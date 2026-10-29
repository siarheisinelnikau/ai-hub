package server

import (
	"encoding/json"
	"net/http"

	"aihub/internal/cloud"
)

// Cloud-provider settings (Go-only extension, no Python parity constraint).
//
// The hub stores a mapping of upstream models (OpenAI OAuth / ChatGPT Codex,
// e.g. gpt-6-astra) that should appear as cards. This file wires the settings
// API; the cards themselves are merged into /api/status in server.go and the
// start/stop controls are intercepted in handleModelAction.

// handleCloudGet returns the saved mapping plus the live provider state and
// the upstream model list (best-effort — a down/blocked provider still
// returns the saved config with a remote_error).
func (s *Server) handleCloudGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	cfg := cloud.MustLoad(s.d.AIHUBRoot)
	running, healthy, installed := cfg.Probe()
	remote, rerr := cfg.RemoteModels()
	if remote == nil {
		remote = []cloud.RemoteModel{}
	}
	resp := map[string]interface{}{
		"enabled":    cfg.Enabled,
		"base_url":   cfg.BaseURL,
		"service":    cfg.Service,
		"port":       cfg.Port,
		"running":    running,
		"healthy":    healthy,
		"installed":  installed,
		"models":     cfg.Models,
		"remote":     remote,
		"remote_err": "",
	}
	if rerr != nil {
		resp["remote_err"] = rerr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleCloudSave persists the mapping. Body is the Config JSON
// ({enabled, base_url, service, port, models:[…]}); models are normalized and
// de-duplicated by id.
func (s *Server) handleCloudSave(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	var cfg cloud.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "detail": "bad request body"})
		return
	}
	if err := cloud.Save(s.d.AIHUBRoot, &cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "detail": "save failed: " + err.Error()})
		return
	}
	// Re-read so the response reflects normalization.
	saved := cloud.MustLoad(s.d.AIHUBRoot)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "saved": len(saved.Models), "enabled": saved.Enabled})
}

// handleCloudRefresh fetches the provider's /v1/models and returns it. The
// settings UI uses this to discover which models the ChatGPT account can
// expose.
func (s *Server) handleCloudRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	var body struct {
		BaseURL string `json:"base_url"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	cfg := cloud.MustLoad(s.d.AIHUBRoot)
	if body.BaseURL != "" {
		cfg.BaseURL = body.BaseURL
	}
	models, err := cfg.RemoteModels()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error(), "models": []cloud.RemoteModel{}})
		return
	}
	if models == nil {
		models = []cloud.RemoteModel{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "models": models})
}

// handleCloudAction starts/stops the local provider systemd unit.
// Body: {"action":"start"|"stop"}.
func (s *Server) handleCloudAction(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuth(w, r) {
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Action == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "detail": "missing action"})
		return
	}
	cfg := cloud.MustLoad(s.d.AIHUBRoot)
	res, code := cfg.Action(body.Action)
	writeJSON(w, code, res)
}
