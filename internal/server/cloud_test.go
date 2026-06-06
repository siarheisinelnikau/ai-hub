package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aihub/internal/cloud"
)

func TestCloudRequiresAuth(t *testing.T) {
	s, _ := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/cloud")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("GET /api/cloud = %d, want 401", resp.StatusCode)
	}
}

func TestCloudSaveGetAndStatusCard(t *testing.T) {
	s, token := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, token)

	// Persist a mapping with GPT-6 Astra enabled (priority model).
	cfg := &cloud.Config{
		Enabled: true,
		BaseURL: "http://127.0.0.1:1/v1", // unreachable on purpose: card is offline
		Service: "ai-hub-nonexistent-unit-xyz",
		Port:    10531,
		Models: []cloud.Model{
			{ID: "gpt-6-astra", Name: "GPT-6 Astra", Short: "Astra", Enabled: true},
			{ID: "gpt-5.5", Enabled: false},
		},
	}
	if err := cloud.Save(s.d.AIHUBRoot, cfg); err != nil {
		t.Fatalf("cloud.Save: %v", err)
	}

	// GET /api/cloud returns the saved config (disabled model still listed).
	resp, err := client.Get(ts.URL + "/api/cloud")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	mustDecode(t, resp.Body, &got)
	resp.Body.Close()
	if got["enabled"] != true {
		t.Fatalf("enabled = %v, want true", got["enabled"])
	}
	if models, _ := got["models"].([]interface{}); len(models) != 2 {
		t.Fatalf("models = %v, want 2 (mapped, incl. disabled)", got["models"])
	}

	// /api/status merges ONLY the enabled cloud model as a card.
	resp2, err := client.Get(ts.URL + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	var st map[string]interface{}
	mustDecode(t, resp2.Body, &st)
	resp2.Body.Close()
	list, _ := st["models"].([]interface{})
	var card map[string]interface{}
	for _, x := range list {
		m, _ := x.(map[string]interface{})
		if m["id"] == "gpt-6-astra" {
			card = m
		}
		if m["id"] == "gpt-5.5" {
			t.Fatal("disabled cloud model leaked into /api/status")
		}
	}
	if card == nil {
		t.Fatal("enabled cloud model missing from /api/status")
	}
	if card["category"] != "Cloud" || card["backend"] != "openai-oauth" {
		t.Fatalf("cloud card = %v, want category=Cloud backend=openai-oauth", card)
	}
	if card["running"] != false || card["healthy"] != false {
		t.Fatalf("unreachable provider: running=%v healthy=%v, want false", card["running"], card["healthy"])
	}
}

func TestCloudRefresh(t *testing.T) {
	s, token := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, token)

	prov := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-6-astra","object":"model","owned_by":"codex-oauth"},{"id":"gpt-5.6-sol","object":"model","owned_by":"codex-oauth"}]}`))
	}))
	defer prov.Close()

	resp, err := client.Post(ts.URL+"/api/cloud/refresh", "application/json",
		strings.NewReader(`{"base_url":`+quote(prov.URL)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	mustDecode(t, resp.Body, &out)
	resp.Body.Close()
	if out["ok"] != true {
		t.Fatalf("ok = %v, want true", out["ok"])
	}
	models, _ := out["models"].([]interface{})
	if len(models) != 2 {
		t.Fatalf("models = %v, want 2", out["models"])
	}
	if models[0].(map[string]interface{})["id"] != "gpt-6-astra" {
		t.Fatalf("first model = %v, want gpt-6-astra", models[0])
	}
}

func TestCloudRefreshError(t *testing.T) {
	s, token := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, token)

	resp, err := client.Post(ts.URL+"/api/cloud/refresh", "application/json",
		strings.NewReader(`{"base_url":"http://127.0.0.1:1/v1"}`))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	mustDecode(t, resp.Body, &out)
	resp.Body.Close()
	if out["ok"] != false || out["error"] == "" {
		t.Fatalf("out = %v, want ok=false + error", out)
	}
}

// A cloud model id must be intercepted by handleModelAction (start/stop of the
// shared provider unit) even though it is not in the local registry.
func TestCloudModelActionExternal(t *testing.T) {
	s, token := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, token)

	if err := cloud.Save(s.d.AIHUBRoot, &cloud.Config{
		Enabled: true,
		Service: "ai-hub-nonexistent-unit-xyz",
		Port:    10531,
		Models:  []cloud.Model{{ID: "gpt-6-astra", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}

	resp, err := client.Post(ts.URL+"/api/model/gpt-6-astra/start", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	mustDecode(t, resp.Body, &out)
	resp.Body.Close()
	if out["ok"] != false || out["state"] != "external" {
		t.Fatalf("out = %v, want ok=false state=external", out)
	}
}

// When cloud is disabled a cloud id is just an unknown model (404), so the
// feature stays invisible unless switched on.
func TestCloudModelActionDisabledFallsThrough(t *testing.T) {
	s, token := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, token)

	if err := cloud.Save(s.d.AIHUBRoot, &cloud.Config{
		Enabled: false,
		Models:  []cloud.Model{{ID: "gpt-6-astra", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(ts.URL+"/api/model/gpt-6-astra/start", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("status = %d, want 404 (cloud disabled)", resp.StatusCode)
	}
}
