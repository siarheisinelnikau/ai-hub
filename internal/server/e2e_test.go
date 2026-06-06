package server

import (
	"aihub/internal/models"
	"aihub/internal/registry"
	"aihub/internal/status"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// These exercise the Go-only extension endpoints end-to-end over HTTP (auth +
// handler + response shape), complementing the Python parity suite.

func TestE2E_Metrics(t *testing.T) {
	s, secret := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, secret)

	resp, err := client.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET /metrics = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", ct)
	}
	body := readAll(t, resp.Body)
	if !strings.Contains(body, "aihub_up 1") {
		t.Fatalf("metrics missing 'aihub_up 1'")
	}
	if !strings.Contains(body, "# TYPE") {
		t.Fatalf("metrics missing '# TYPE' exposition lines")
	}
}

func TestModelDeletePreviewKeepsReusableProfile(t *testing.T) {
	t.Setenv("AIHUB_DOWNLOADS_DIR", t.TempDir())
	id := "qwen38-35b-q8-llama"
	original := models.Catalog[id]
	info := original
	info.LocalPath = t.TempDir()
	models.Catalog[id] = info
	defer func() { models.Catalog[id] = original }()
	weight := filepath.Join(info.LocalPath, info.Files[0])
	if err := os.WriteFile(weight, []byte("weights fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	s, secret := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, secret)
	resp, err := client.Post(ts.URL+"/api/models/delete", "application/json", strings.NewReader(`{"id":"`+id+`","dry_run":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := readAll(t, resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(body, "keep download source and launch configuration") || strings.Contains(body, "remove registry entry") {
		t.Fatalf("unexpected preview: %s", body)
	}
	if got, _ := os.ReadFile(weight); string(got) != "weights fixture" {
		t.Fatal("preview touched model weights")
	}
	// No profile is registered in this fixture. A real stop would fail before deletion.
	if !strings.Contains(body, "no services stopped") {
		t.Fatal("preview attempted a stop")
	}
	if models.Catalog[id].HFRepo != original.HFRepo {
		t.Fatal("preview lost download source")
	}
}

func TestMissingWeightsCannotLaunch(t *testing.T) {
	id := "qwen38-35b-q8-llama"
	original := models.Catalog[id]
	info := original
	info.LocalPath = t.TempDir()
	models.Catalog[id] = info
	defer func() { models.Catalog[id] = original }()
	s, secret := testServer(t)
	s.d.Reg.Models = append(s.d.Reg.Models, registry.Model{ID: id, RequiresWeights: true})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, secret)
	resp, err := client.Post(ts.URL+"/api/model/"+id+"/start", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict || !strings.Contains(readAll(t, resp.Body), "Model manager") {
		t.Fatal("missing weights launched a service")
	}
}

func TestServingCardReturnsAfterWeightsDownloaded(t *testing.T) {
	t.Setenv("AIHUB_DOWNLOADS_DIR", t.TempDir())
	id := "qwen38-35b-q8-llama"
	original := models.Catalog[id]
	info := original
	info.LocalPath = t.TempDir()
	models.Catalog[id] = info
	defer func() { models.Catalog[id] = original }()
	s, secret := testServer(t)
	s.d.Reg.Models = append(s.d.Reg.Models, registry.Model{ID: id, RequiresWeights: true})
	s.d.Poller.Cache().Models = []status.ModelStatusEntry{{ID: id}, {ID: "unmanaged-media"}}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, secret)
	check := func(visible bool) {
		t.Helper()
		r, err := client.Get(ts.URL + "/api/status")
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		body := readAll(t, r.Body)
		if strings.Contains(body, `"id":"`+id+`"`) != visible {
			t.Fatalf("weights visibility = %v, response %s", visible, body)
		}
		if !strings.Contains(body, "unmanaged-media") {
			t.Fatal("unmanaged profile was hidden")
		}
	}
	check(false)
	for _, file := range info.Files {
		if err := os.WriteFile(filepath.Join(info.LocalPath, file), []byte("weights"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	check(true)
	if err := os.Remove(filepath.Join(info.LocalPath, info.Files[0])); err != nil {
		t.Fatal(err)
	}
	check(false)
}

func TestE2E_GpuTemps(t *testing.T) {
	s, secret := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, secret)

	resp, err := client.Get(ts.URL + "/api/gpu-temps")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET /api/gpu-temps = %d, want 200", resp.StatusCode)
	}
	if b := strings.TrimSpace(readAll(t, resp.Body)); !strings.HasPrefix(b, "[") {
		t.Fatalf("gpu-temps body %q, want a JSON array", b)
	}
}

func TestE2E_SystemHealth(t *testing.T) {
	s, secret := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, secret)

	resp, err := client.Get(ts.URL + "/api/system-health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET /api/system-health = %d, want 200", resp.StatusCode)
	}
	var body map[string]interface{}
	mustDecode(t, resp.Body, &body)
	if _, ok := body["available"]; !ok {
		t.Fatalf("system-health missing 'available' field: %v", body)
	}
}

func TestE2E_Export(t *testing.T) {
	s, secret := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, secret)

	resp, err := client.Get(ts.URL + "/api/export?what=bogus")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("GET /api/export?what=bogus = %d, want 400", resp.StatusCode)
	}

	resp2, err := client.Get(ts.URL + "/api/export?what=gpu&hours=1")
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != 404 && resp2.StatusCode != 200 {
		t.Fatalf("GET /api/export?what=gpu = %d, want 200 or 404", resp2.StatusCode)
	}
}

func TestE2E_BenchmarkNotRunning(t *testing.T) {
	s, secret := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, secret)

	resp, err := client.Post(ts.URL+"/api/model/m-a/benchmark", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("POST benchmark = %d, want 200", resp.StatusCode)
	}
	var body map[string]interface{}
	mustDecode(t, resp.Body, &body)
	if body["ok"] != false {
		t.Fatalf("benchmark ok = %v, want false (model not running)", body["ok"])
	}
}

func TestE2E_LoginRateLimit(t *testing.T) {
	s, secret := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)

	for i := 0; i < 5; i++ {
		resp := doLogin(t, client, ts.URL, "wrong")
		if resp.StatusCode != 403 {
			t.Fatalf("bad login #%d = %d, want 403", i+1, resp.StatusCode)
		}
		resp.Body.Close()
	}
	resp := doLogin(t, client, ts.URL, "wrong")
	if resp.StatusCode != 429 {
		t.Fatalf("6th bad login = %d, want 429", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Fatalf("429 missing Retry-After header")
	}
	resp.Body.Close()

	resp2 := doLogin(t, client, ts.URL, secret)
	if resp2.StatusCode != 429 {
		t.Fatalf("good login while locked = %d, want 429", resp2.StatusCode)
	}
	resp2.Body.Close()
}

// TestE2E_Settings exercises the notifications/webhooks settings endpoints:
// GET (empty) -> POST (save) -> GET (round-trip) -> test fire.
func TestE2E_Settings(t *testing.T) {
	t.Setenv("AIHUB_WEBHOOK_URLS", "")
	t.Setenv("AIHUB_WEBHOOKS_FILE", filepath.Join(t.TempDir(), "wh.txt"))

	s, secret := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, secret)

	// GET: no webhooks configured yet -> urls []
	resp, err := client.Get(ts.URL + "/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	mustDecode(t, resp.Body, &got)
	resp.Body.Close()
	wh := got["webhooks"].(map[string]interface{})
	if urls, _ := wh["urls"].([]interface{}); len(urls) != 0 {
		t.Fatalf("initial urls = %v, want empty", urls)
	}

	// POST: save two URLs
	resp2, err := client.Post(ts.URL+"/api/settings", "application/json", strings.NewReader(`{"webhooks":{"urls":["https://a","https://b"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]interface{}
	mustDecode(t, resp2.Body, &saved)
	resp2.Body.Close()
	if saved["ok"] != true {
		t.Fatalf("save ok = %v, want true (%v)", saved["ok"], saved)
	}

	// GET: round-trip (2 urls)
	resp3, err := client.Get(ts.URL + "/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	var got2 map[string]interface{}
	mustDecode(t, resp3.Body, &got2)
	resp3.Body.Close()
	urls2 := got2["webhooks"].(map[string]interface{})["urls"].([]interface{})
	if len(urls2) != 2 {
		t.Fatalf("round-trip urls = %v, want 2", urls2)
	}

	// test fire against a live local server -> per-bot result should be ok
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer okSrv.Close()
	resp4, err := client.Post(ts.URL+"/api/settings/test", "application/json", strings.NewReader(`{"webhooks":{"urls":["`+okSrv.URL+`"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	var tested map[string]interface{}
	mustDecode(t, resp4.Body, &tested)
	resp4.Body.Close()
	if tested["ok"] != true {
		t.Fatalf("test ok = %v, want true", tested)
	}
	if oc, _ := tested["ok_count"].(float64); oc != 1 {
		t.Fatalf("test ok_count = %v, want 1 (%v)", tested["ok_count"], tested)
	}
}

// TestE2E_ModelsList exercises the model-manager inventory endpoint (catalog +
// download state + hub-connection object) over HTTP.
func TestE2E_ModelsList(t *testing.T) {
	t.Setenv("AIHUB_DOWNLOADS_DIR", t.TempDir())
	s, secret := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, secret)

	resp, err := client.Get(ts.URL + "/api/models")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Models []struct {
			ID       string `json:"id"`
			Short    string `json:"short"`
			Download struct {
				Status string `json:"status"`
			} `json:"download"`
			Connection struct {
				Registry bool `json:"registry"`
				Service  bool `json:"service"`
				WebUI    bool `json:"webui"`
				All      bool `json:"all_connected"`
			} `json:"connection"`
		} `json:"models"`
	}
	mustDecode(t, resp.Body, &got)
	resp.Body.Close()
	if len(got.Models) != 14 {
		t.Fatalf("models = %d, want 14 (including downloadable models)", len(got.Models))
	}
	for _, m := range got.Models {
		if m.ID == "" || m.Download.Status == "" {
			t.Fatalf("model %q missing id or download.status", m.ID)
		}
	}
}

// TestE2E_ModelDelete exercises the delete helpers (registry block removal +
// Open WebUI URL/key removal + service removal) against temp files via the env
// path overrides, verifying block boundaries and that neighbours survive.
func TestE2E_ModelDelete(t *testing.T) {
	dir := t.TempDir()
	reg := "models:\n" +
		"- id: alpha-llm\n  name: Alpha\n  port: 18100\n  sysd: ai-alpha-llm\n" +
		"- id: beta-llm\n  name: Beta\n  port: 18101\n  sysd: ai-beta-llm\n" +
		"- id: media-svc\n  name: Media\n  port: 18200\n  sysd: ai-media-svc\n"
	regPath := filepath.Join(dir, "registry.yaml")
	if err := os.WriteFile(regPath, []byte(reg), 0644); err != nil {
		t.Fatal(err)
	}
	webui := "ExecStart=/usr/bin/docker run -e OPENAI_API_BASE_URLS=\"http://h:18100/v1;http://h:18101/v1;http://h:18200/v1\" -e OPENAI_API_KEYS=\"k1;k2;k3\" img\n"
	webuiPath := filepath.Join(dir, "ai-open-webui.service")
	if err := os.WriteFile(webuiPath, []byte(webui), 0644); err != nil {
		t.Fatal(err)
	}
	svcPath := filepath.Join(dir, "ai-alpha-llm.service")
	if err := os.WriteFile(svcPath, []byte("ExecStart=/bin/true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AIHUB_REGISTRY", regPath)
	t.Setenv("AIHUB_SYSTEMD_DIR", dir)

	// 1. dry-run must not change anything
	if done, note := removeRegistryEntry("alpha-llm", true); !done {
		t.Fatalf("dry-run registry: %s", note)
	}
	if got, _ := os.ReadFile(regPath); !strings.Contains(string(got), "alpha-llm") {
		t.Fatalf("dry-run modified registry")
	}

	// 2. registry removal: alpha gone, beta + media (neighbours) survive
	if done, note := removeRegistryEntry("alpha-llm", false); !done {
		t.Fatalf("registry removal: %s", note)
	}
	s, _ := os.ReadFile(regPath)
	if strings.Contains(string(s), "alpha-llm") {
		t.Fatalf("alpha-llm still in registry: %s", s)
	}
	for _, keep := range []string{"beta-llm", "media-svc"} {
		if !strings.Contains(string(s), keep) {
			t.Fatalf("%s lost after deleting alpha-llm: %s", keep, s)
		}
	}

	// 3. service removal
	if done, note := removeModelService("alpha-llm", false); !done {
		t.Fatalf("service removal: %s", note)
	}
	if _, err := os.Stat(svcPath); !os.IsNotExist(err) {
		t.Fatalf("service file still present after removal")
	}

	// 4. webui URL + key removal: :18100 + k1 gone, :18101/:18200 + 2 keys remain
	if done, note := removeWebuiEntry(18100, false); !done {
		t.Fatalf("webui removal: %s", note)
	}
	w, _ := os.ReadFile(webuiPath)
	ws := string(w)
	if strings.Contains(ws, ":18100/v1") {
		t.Fatalf(":18100 still in Open WebUI: %s", ws)
	}
	for _, keep := range []string{":18101/v1", ":18200/v1"} {
		if !strings.Contains(ws, keep) {
			t.Fatalf("%s lost from Open WebUI: %s", keep, ws)
		}
	}
	if km := regexp.MustCompile(`OPENAI_API_KEYS="([^"]*)"`).FindStringSubmatch(ws); km == nil || len(strings.Split(km[1], ";")) != 2 {
		t.Fatalf("expected 2 keys after removal, got: %s", ws)
	}
}
