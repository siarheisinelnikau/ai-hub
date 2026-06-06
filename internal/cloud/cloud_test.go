package cloud

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadDefault(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Enabled {
		t.Fatal("expected cloud disabled by default")
	}
	if cfg.BaseURL != DefaultBaseURL || cfg.Service != DefaultService || cfg.Port != DefaultPort {
		t.Fatalf("defaults = %+v, want base=%s svc=%s port=%d", cfg, DefaultBaseURL, DefaultService, DefaultPort)
	}
	if len(cfg.Models) != 0 {
		t.Fatalf("models = %v, want empty", cfg.Models)
	}
}

func TestSaveLoadRoundTripNormalizes(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{
		Enabled: true,
		BaseURL: "http://example.test:10531/v1/",
		Service: "svc-x",
		Port:    10531,
		Models: []Model{
			{ID: "gpt-6-astra", Enabled: true},          // name/category/in/out defaulted
			{ID: "gpt-6-astra", Enabled: true},          // duplicate -> dropped
			{ID: "  ", Enabled: true},                   // empty -> dropped
			{ID: "gpt-5.5", Name: "N", Short: "S", Enabled: true},
		},
	}
	if err := Save(dir, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.BaseURL != "http://example.test:10531/v1" {
		t.Fatalf("BaseURL = %q, want trailing slash trimmed", got.BaseURL)
	}
	if len(got.Models) != 2 {
		t.Fatalf("models = %d, want 2 (dedup + empty dropped)", len(got.Models))
	}
	m := got.Get("gpt-6-astra")
	if m == nil {
		t.Fatal("gpt-6-astra missing")
	}
	if m.Category != DefaultCategory || m.Name != "gpt-6-astra" || m.Short != "gpt-6-astra" {
		t.Fatalf("defaults not applied: %+v", m)
	}
	if len(m.In) != 1 || m.In[0] != "text" || len(m.Out) != 1 || m.Out[0] != "text" {
		t.Fatalf("in/out defaults not applied: in=%v out=%v", m.In, m.Out)
	}
	if got.Get("gpt-5.5").Short != "S" {
		t.Fatal("explicit short lost")
	}
}

func TestRemoteModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-6-astra","object":"model","owned_by":"codex-oauth"},{"id":"gpt-5.5","object":"model","owned_by":"codex-oauth"}]}`))
	}))
	defer srv.Close()

	cfg := &Config{BaseURL: srv.URL}
	models, err := cfg.RemoteModels()
	if err != nil {
		t.Fatalf("RemoteModels: %v", err)
	}
	if len(models) != 2 || models[0].ID != "gpt-6-astra" || models[0].OwnedBy != "codex-oauth" {
		t.Fatalf("models = %+v", models)
	}
}

func TestRemoteModelsError(t *testing.T) {
	// Closed server / unroutable port -> error (settings UI shows remote_err).
	cfg := &Config{BaseURL: "http://127.0.0.1:1/v1"}
	if _, err := cfg.RemoteModels(); err == nil {
		t.Fatal("expected an error for an unreachable provider")
	}
}

func TestEntriesOnlyEnabledAndShareState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	cfg := &Config{
		Enabled: true,
		BaseURL: srv.URL,
		Service: "ai-hub-nonexistent-unit-xyz",
		Port:    10531,
		Models: []Model{
			{ID: "gpt-6-astra", Name: "GPT-6 Astra", Short: "Astra", Enabled: true},
			{ID: "gpt-5.5", Enabled: false},
		},
	}
	entries := cfg.Entries()
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1 (only enabled)", len(entries))
	}
	e := entries[0]
	if e.ID != "gpt-6-astra" || e.Category != DefaultCategory || e.Backend != DefaultBackend {
		t.Fatalf("entry = %+v", e)
	}
	if e.Port != 10531 || e.Sysd != cfg.Service || e.Group != "cloud" {
		t.Fatalf("entry routing fields = %+v", e)
	}
	if !e.Running || !e.Healthy {
		t.Fatalf("running=%v healthy=%v, want both true (provider answers)", e.Running, e.Healthy)
	}
	if e.Sub == nil || e.GroupSiblings == nil {
		t.Fatal("Sub/GroupSiblings must be non-nil so the card JSON is stable")
	}
}

func TestEntriesDisabledConfig(t *testing.T) {
	cfg := &Config{Enabled: false, Models: []Model{{ID: "gpt-6-astra", Enabled: true}}}
	if got := cfg.Entries(); got != nil {
		t.Fatalf("disabled config produced entries: %+v", got)
	}
}

func TestProbeNoUnitIsExternal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	cfg := &Config{Enabled: true, BaseURL: srv.URL, Service: "ai-hub-nonexistent-unit-xyz"}
	running, healthy, installed := cfg.Probe()
	if installed {
		t.Fatal("installed = true for a non-existent unit")
	}
	if !running || !healthy {
		t.Fatalf("running=%v healthy=%v, want both true", running, healthy)
	}
}

func TestActionRejectsBadAction(t *testing.T) {
	cfg := &Config{Enabled: true, Service: "svc"}
	if _, code := cfg.Action("frobnicate"); code != 400 {
		t.Fatalf("code = %d, want 400", code)
	}
}

func TestActionExternalNoop(t *testing.T) {
	cfg := &Config{Enabled: true, Service: "ai-hub-nonexistent-unit-xyz"}
	res, code := cfg.Action("start")
	if code != 200 {
		t.Fatalf("code = %d, want 200", code)
	}
	if res["ok"] != false || res["state"] != "external" {
		t.Fatalf("res = %+v, want not-ok/external (no local unit)", res)
	}
}
