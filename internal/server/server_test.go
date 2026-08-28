package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"aihub/internal/actions"
	"aihub/internal/auth"
	"aihub/internal/buildinfo"
	"aihub/internal/metrics"
	"aihub/internal/registry"
	"aihub/internal/status"
)

func testServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	tokFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A random free port keeps the suite hermetic: on the production box
	// :18021 is a live model, and the metrics scraper would find real data.
	port := freePort(t)
	reg := &registry.File{
		Models: []registry.Model{{ID: "m-a", Name: "A", Port: port, Sysd: "ai-m-a", Category: "LLM", Backend: "llamacpp"}},
		Ports:  map[string]int{"m-a": port},
	}
	store := auth.NewStore(tokFile)
	actionsDeps := &actions.Deps{Reg: reg}
	poller := status.NewPoller(reg)
	stats := metrics.NewStatsPoller(nil)
	s := New(Deps{Reg: reg, Store: store, Actions: actionsDeps, Poller: poller, Stats: stats, AIHUBRoot: dir, ExeDir: dir})
	return s, "secret"
}

// testClient returns an http.Client backed by a cookie jar, so the
// aih_session cookie from login is stored and resent automatically.
func testClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}

func doLogin(t *testing.T, client *http.Client, base, token string) *http.Response {
	t.Helper()
	resp, err := client.Post(base+"/api/login", "application/json", strings.NewReader(`{"token":`+quote(token)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func readAll(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustDecode(t *testing.T, body io.Reader, v interface{}) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(v); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}

func TestHealthz(t *testing.T) {
	s, _ := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET /healthz = %d, want 200", resp.StatusCode)
	}
	var body map[string]interface{}
	mustDecode(t, resp.Body, &body)
	if body["ok"] != true {
		t.Fatalf("body = %v, want {\"ok\":true}", body)
	}
	if body["version"] != buildinfo.Version || body["revision"] == "" || body["modified"] == nil {
		t.Fatalf("missing build metadata: %v", body)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("health metadata must not be cached across upgrades")
	}
}

func TestLoginBadToken(t *testing.T) {
	s, _ := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)

	resp := doLogin(t, client, ts.URL, "wrong")
	defer resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("login(wrong) = %d, want 403", resp.StatusCode)
	}
	var body map[string]interface{}
	mustDecode(t, resp.Body, &body)
	if body["detail"] != "bad token" {
		t.Fatalf("body = %v, want {\"detail\":\"bad token\"}", body)
	}
}

func TestLoginGoodToken(t *testing.T) {
	s, secret := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)

	resp := doLogin(t, client, ts.URL, secret)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("login(secret) = %d, want 200", resp.StatusCode)
	}
	var body map[string]interface{}
	mustDecode(t, resp.Body, &body)
	if body["ok"] != true {
		t.Fatalf("body = %v, want {\"ok\":true}", body)
	}
	found := ""
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			found = c.Value
		}
	}
	if found == "" {
		t.Fatalf("Set-Cookie %s missing or empty; got %v", auth.CookieName, resp.Cookies())
	}
}

// TestLoginBodyEdgeCases mirrors app.py login body handling:
//   - empty / invalid JSON body -> {} -> 403 bad token
//   - valid JSON non-dict (null, number, bool, array, string) -> body.get
//     raises AttributeError in Python -> 500 "Internal Server Error" (text)
func TestLoginBodyEdgeCases(t *testing.T) {
	s, _ := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	post := func(body string) (int, string, string) {
		t.Helper()
		resp, err := http.Post(ts.URL+"/api/login", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b), resp.Header.Get("Content-Type")
	}

	// empty body -> 403
	code, body, _ := post("")
	if code != 403 || body != `{"detail":"bad token"}` {
		t.Fatalf("login(\"\") = %d %q, want 403 {\"detail\":\"bad token\"}", code, body)
	}
	// invalid JSON -> 403
	code, body, _ = post("{not json")
	if code != 403 || body != `{"detail":"bad token"}` {
		t.Fatalf("login(invalid) = %d %q, want 403 {\"detail\":\"bad token\"}", code, body)
	}
	// valid JSON non-dict -> 500 text/plain (parity with Python AttributeError)
	for _, v := range []string{"null", "42", "true", "[1,2]", `"str"`} {
		code, body, ct := post(v)
		if code != 500 || body != "Internal Server Error" || !strings.HasPrefix(ct, "text/plain") {
			t.Fatalf("login(%s) = %d %q %q, want 500 \"Internal Server Error\" text/plain", v, code, body, ct)
		}
	}
	// dict with null token -> 403 (str(None) != secret)
	code, body, _ = post(`{"token":null}`)
	if code != 403 || body != `{"detail":"bad token"}` {
		t.Fatalf("login({token:null}) = %d %q, want 403", code, body)
	}
	// dict with number token -> 403 (str(123) != secret)
	code, body, _ = post(`{"token":123}`)
	if code != 403 || body != `{"detail":"bad token"}` {
		t.Fatalf("login({token:123}) = %d %q, want 403", code, body)
	}
}

// TestActionBodyDrained verifies POST action handlers fully read the request
// body (parity with uvicorn, which consumes it). A drained body lets the
// server keep the connection alive: the response must NOT carry
// "Connection: close". An unread body made fast clients race into connection
// resets (WinError 10054).
func TestActionBodyDrained(t *testing.T) {
	s, secret := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)

	resp := doLogin(t, client, ts.URL, secret)
	resp.Body.Close()

	// First POST: a drained body means the server can keep the connection
	// alive (no "Connection: close" on the response). An unread body makes
	// the server close the connection after the response.
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/model/m-a/start", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp2, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if h := resp2.Header.Get("Connection"); h != "" {
		t.Fatalf("first action POST: Connection header = %q, want empty (body drained, keep-alive)", h)
	}
	resp2.Body.Close()

	// Repeated fast POSTs with a body: all must succeed without resets.
	for i := 0; i < 20; i++ {
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/model/m-a/start", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp3, err := client.Do(req)
		if err != nil {
			t.Fatalf("action POST #%d: %v", i, err)
		}
		resp3.Body.Close()
		if resp3.StatusCode != 200 {
			t.Fatalf("action POST #%d = %d, want 200", i, resp3.StatusCode)
		}
	}
}

func TestStatusRequiresAuth(t *testing.T) {
	s, _ := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("GET /api/status (no cookie) = %d, want 401", resp.StatusCode)
	}
	var body map[string]interface{}
	mustDecode(t, resp.Body, &body)
	if body["detail"] != "not logged in" {
		t.Fatalf("body = %v, want {\"detail\":\"not logged in\"}", body)
	}
}

func TestStatusWithAuth(t *testing.T) {
	s, secret := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)

	resp := doLogin(t, client, ts.URL, secret)
	resp.Body.Close()

	resp2, err := client.Get(ts.URL + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("GET /api/status (auth) = %d, want 200", resp2.StatusCode)
	}
	var body map[string]interface{}
	mustDecode(t, resp2.Body, &body)
	for _, k := range []string{"models", "services", "gpus", "cpu", "disk", "profiles", "ports", "ts", "current_profile", "builds"} {
		if _, ok := body[k]; !ok {
			t.Errorf("status body missing key %q (got keys: %v)", k, keys(body))
		}
	}
}

func keys(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestModelActionUnknown(t *testing.T) {
	s, secret := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)

	resp := doLogin(t, client, ts.URL, secret)
	resp.Body.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/model/nope/start", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp2, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 404 {
		t.Fatalf("model_action(nope) = %d, want 404", resp2.StatusCode)
	}
	var body map[string]interface{}
	mustDecode(t, resp2.Body, &body)
	if body["detail"] != "unknown model: nope" {
		t.Fatalf("body = %v, want {\"detail\":\"unknown model: nope\"}", body)
	}
}

func TestModelActionWindowsMock(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only mock")
	}
	s, secret := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)

	resp := doLogin(t, client, ts.URL, secret)
	resp.Body.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/model/m-a/start", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp2, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("model_action(m-a/start) = %d, want 200", resp2.StatusCode)
	}
	var body map[string]interface{}
	mustDecode(t, resp2.Body, &body)
	if body["ok"] != true || body["output"] != "mock start m-a on Windows dev" {
		t.Fatalf("body = %v, want {\"ok\":true,\"output\":\"mock start m-a on Windows dev\"}", body)
	}
}

func TestModelStatsUnknown(t *testing.T) {
	s, secret := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)

	resp := doLogin(t, client, ts.URL, secret)
	resp.Body.Close()

	resp2, err := client.Get(ts.URL + "/api/model/nope/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 404 {
		t.Fatalf("model_stats(nope) = %d, want 404", resp2.StatusCode)
	}
	var body map[string]interface{}
	mustDecode(t, resp2.Body, &body)
	if body["detail"] != "unknown model: nope" {
		t.Fatalf("body = %v, want {\"detail\":\"unknown model: nope\"}", body)
	}
}

func TestModelStatsKnown(t *testing.T) {
	s, secret := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)

	resp := doLogin(t, client, ts.URL, secret)
	resp.Body.Close()

	resp2, err := client.Get(ts.URL + "/api/model/m-a/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("model_stats(m-a) = %d, want 200", resp2.StatusCode)
	}
	var body map[string]interface{}
	mustDecode(t, resp2.Body, &body)
	if body["id"] != "m-a" {
		t.Errorf("id = %v, want m-a", body["id"])
	}
	if body["interval"] != float64(1) {
		t.Errorf("interval = %v, want 1", body["interval"])
	}
	if body["live"] != nil {
		t.Errorf("live = %v, want nil", body["live"])
	}
	hist, ok := body["history"].([]interface{})
	if !ok {
		t.Fatalf("history = %v (%T), want []interface{}", body["history"], body["history"])
	}
	if len(hist) != 0 {
		t.Errorf("history len = %d, want 0", len(hist))
	}
}

func TestLogout(t *testing.T) {
	s, secret := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)

	resp := doLogin(t, client, ts.URL, secret)
	resp.Body.Close()

	req, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/login", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp2, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("logout = %d, want 200", resp2.StatusCode)
	}
	var body map[string]interface{}
	mustDecode(t, resp2.Body, &body)
	if body["ok"] != true {
		t.Fatalf("logout body = %v, want {\"ok\":true}", body)
	}

	resp4, err := client.Get(ts.URL + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp4.Body.Close()
	if resp4.StatusCode != 401 {
		t.Fatalf("status after logout = %d, want 401", resp4.StatusCode)
	}
}

func TestDownloadsWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only mock")
	}
	s, secret := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)

	resp := doLogin(t, client, ts.URL, secret)
	resp.Body.Close()

	resp2, err := client.Get(ts.URL + "/api/downloads")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("downloads = %d, want 200", resp2.StatusCode)
	}
	if ct := resp2.Header.Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	if got := readAll(t, resp2.Body); got != "mock downloads on Windows" {
		t.Fatalf("body = %q, want %q", got, "mock downloads on Windows")
	}
}

func TestLogsWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only mock")
	}
	s, secret := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)

	resp := doLogin(t, client, ts.URL, secret)
	resp.Body.Close()

	resp2, err := client.Get(ts.URL + "/api/logs/m-a")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("logs = %d, want 200", resp2.StatusCode)
	}
	if ct := resp2.Header.Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	want := "mock logs for m-a on Windows dev\nunit: ai-m-a\nno logs"
	if got := readAll(t, resp2.Body); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

// testServerStopAll is like testServer but with a 3-model registry
// (2 LLMs + 1 media) so StopAll's category filter and the
// running/not-running split can be exercised.
func testServerStopAll(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	tokFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokFile, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := &registry.File{
		Models: []registry.Model{
			{ID: "llm-a", Name: "A", Port: 18021, Sysd: "ai-llm-a", Category: "LLM"},
			{ID: "media-x", Name: "X", Port: 18100, Sysd: "ai-media-x", Category: "media"},
			{ID: "llm-b", Name: "B", Port: 18022, Sysd: "ai-llm-b", Category: "LLM"},
		},
	}
	store := auth.NewStore(tokFile)
	actionsDeps := &actions.Deps{Reg: reg}
	poller := status.NewPoller(reg)
	stats := metrics.NewStatsPoller(nil)
	s := New(Deps{Reg: reg, Store: store, Actions: actionsDeps, Poller: poller, Stats: stats, AIHUBRoot: dir, ExeDir: dir})
	return s, "secret"
}

func TestStopAllRequiresAuth(t *testing.T) {
	s, _ := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/stop-all", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("POST /api/stop-all (no cookie) = %d, want 401", resp.StatusCode)
	}
	var body map[string]interface{}
	mustDecode(t, resp.Body, &body)
	if body["detail"] != "not logged in" {
		t.Fatalf("body = %v, want {\"detail\":\"not logged in\"}", body)
	}
}

func TestStopAllWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only mock")
	}
	s, secret := testServerStopAll(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)

	resp := doLogin(t, client, ts.URL, secret)
	resp.Body.Close()

	// Prime the status cache (runningFn's primary source — the same data
	// /api/status serves): llm-a running, llm-b idle, media-x running
	// but not an LLM.
	s.d.Poller.Cache().Models = []status.ModelStatusEntry{
		{ID: "llm-a", Name: "A", Category: "LLM", Port: 18021, Running: true},
		{ID: "media-x", Name: "X", Category: "media", Port: 18100, Running: true},
		{ID: "llm-b", Name: "B", Category: "LLM", Port: 18022, Running: false},
	}

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/stop-all", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp2, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("POST /api/stop-all (auth) = %d, want 200", resp2.StatusCode)
	}
	var body struct {
		OK      bool              `json:"ok"`
		Stopped []string          `json:"stopped"`
		Errors  map[string]string `json:"errors"`
	}
	mustDecode(t, resp2.Body, &body)
	if !body.OK {
		t.Fatalf("body = %+v, want ok=true", body)
	}
	// llm-a stopped (running LLM); media-x skipped (non-LLM); llm-b
	// skipped (not running). The Windows mock stop succeeds, so errors
	// must be {} — and both fields must be non-nil (JSON [] / {}, not
	// null).
	if body.Stopped == nil || len(body.Stopped) != 1 || body.Stopped[0] != "llm-a" {
		t.Fatalf("stopped = %v, want [llm-a]", body.Stopped)
	}
	if body.Errors == nil || len(body.Errors) != 0 {
		t.Fatalf("errors = %v (nil=%v), want non-nil empty", body.Errors, body.Errors == nil)
	}
}

// llamacppFixture serves a minimal llama.cpp /metrics (500 tokens,
// 10 pred_seconds, running 1) and /slots [] — the first scrape yields the
// cumulative 50 tok/s as the live value (deterministic). Same fixture as
// metrics.llamaFixture (duplicated: that one is package-internal).
func llamacppFixture() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			fmt.Fprint(w, "llamacpp:tokens_predicted_total 500\n"+
				"llamacpp:tokens_predicted_seconds_total 10\n"+
				"llamacpp:requests_processing 1\n")
		case "/slots":
			fmt.Fprint(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func fixturePort(t *testing.T, ts *httptest.Server) int {
	t.Helper()
	parts := strings.Split(ts.URL, ":")
	port, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		t.Fatalf("parse fixture port from %q: %v", ts.URL, err)
	}
	return port
}

// primeStats starts p's scrape loop and waits for the first ring entry for
// mid (Run ticks immediately, so this resolves in milliseconds).
func primeStats(t *testing.T, p *metrics.StatsPoller, mid string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go p.Run(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for len(p.Get(mid)) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("stats poller produced no data for %q within 5s", mid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// statusModelJSON fetches /api/status (authed) and returns the first model
// entry as a JSON string (for marshal-level assertions) plus the decoded map.
func statusFirstModel(t *testing.T, base string, client *http.Client) (string, map[string]interface{}) {
	t.Helper()
	resp, err := client.Get(base + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET /api/status = %d, want 200", resp.StatusCode)
	}
	var body map[string]interface{}
	mustDecode(t, resp.Body, &body)
	models, ok := body["models"].([]interface{})
	if !ok || len(models) == 0 {
		t.Fatalf("status models = %v, want non-empty slice", body["models"])
	}
	m0, ok := models[0].(map[string]interface{})
	if !ok {
		t.Fatalf("models[0] type = %T, want map[string]interface{}", models[0])
	}
	b, err := json.Marshal(m0)
	if err != nil {
		t.Fatal(err)
	}
	return string(b), m0
}

// TestStatusTPSLiveRunning proves (a): with a primed StatsPoller and a
// running model, /api/status carries tps_live with the live value (50 —
// the fixture's deterministic cumulative TPS).
func TestStatusTPSLiveRunning(t *testing.T) {
	fix := llamacppFixture()
	defer fix.Close()

	s, secret := testServer(t)
	s.d.Stats = metrics.NewStatsPoller(func() []metrics.ModelRef {
		return []metrics.ModelRef{{ID: "m-a", Port: fixturePort(t, fix), Backend: "llamacpp"}}
	})
	primeStats(t, s.d.Stats, "m-a")

	// Status cache: m-a running (same priming style as TestStopAllWindows).
	s.d.Poller.Cache().Models = []status.ModelStatusEntry{
		{ID: "m-a", Name: "A", Port: 18021, Running: true},
	}

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	resp := doLogin(t, client, ts.URL, secret)
	resp.Body.Close()

	jsonStr, m0 := statusFirstModel(t, ts.URL, client)
	if v, present := m0["tps_live"]; !present {
		t.Fatalf("models[0] = %s: tps_live missing, want 50", jsonStr)
	} else if v != float64(50) {
		t.Fatalf("models[0].tps_live = %v, want 50 (%s)", v, jsonStr)
	}
}

// TestStatusTPSLiveAbsentNoData proves (b): with no scrape data, tps_live
// is omitted from the JSON entirely (marshal check), even for a running
// model — parity-safe zero.
func TestStatusTPSLiveAbsentNoData(t *testing.T) {
	s, secret := testServer(t) // Stats poller has source == nil: no data ever
	s.d.Poller.Cache().Models = []status.ModelStatusEntry{
		{ID: "m-a", Name: "A", Port: 18021, Running: true},
	}

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	resp := doLogin(t, client, ts.URL, secret)
	resp.Body.Close()

	jsonStr, m0 := statusFirstModel(t, ts.URL, client)
	if _, present := m0["tps_live"]; present {
		t.Fatalf("models[0] = %s: tps_live must be omitted when no live data", jsonStr)
	}
	if strings.Contains(jsonStr, "tps_live") {
		t.Fatalf("models[0] = %s: tps_live must not appear in JSON when zero", jsonStr)
	}
}

// TestStatusTPSLiveAbsentNotRunning: live data exists, but the model is
// not running -> tps_live still omitted (the field is for running models).
func TestStatusTPSLiveAbsentNotRunning(t *testing.T) {
	fix := llamacppFixture()
	defer fix.Close()

	s, secret := testServer(t)
	s.d.Stats = metrics.NewStatsPoller(func() []metrics.ModelRef {
		return []metrics.ModelRef{{ID: "m-a", Port: fixturePort(t, fix), Backend: "llamacpp"}}
	})
	primeStats(t, s.d.Stats, "m-a")

	s.d.Poller.Cache().Models = []status.ModelStatusEntry{
		{ID: "m-a", Name: "A", Port: 18021, Running: false},
	}

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	resp := doLogin(t, client, ts.URL, secret)
	resp.Body.Close()

	jsonStr, m0 := statusFirstModel(t, ts.URL, client)
	if _, present := m0["tps_live"]; present {
		t.Fatalf("models[0] = %s: tps_live must be omitted for a non-running model", jsonStr)
	}
}

// TestStatusTPSLiveNilStats: Stats == nil must not panic and must leave
// tps_live absent (defensive; main.go always injects a poller).
func TestStatusTPSLiveNilStats(t *testing.T) {
	s, secret := testServer(t)
	s.d.Stats = nil
	s.d.Poller.Cache().Models = []status.ModelStatusEntry{
		{ID: "m-a", Name: "A", Port: 18021, Running: true},
	}

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	resp := doLogin(t, client, ts.URL, secret)
	resp.Body.Close()

	jsonStr, m0 := statusFirstModel(t, ts.URL, client)
	if _, present := m0["tps_live"]; present {
		t.Fatalf("models[0] = %s: tps_live must be omitted when Stats is nil", jsonStr)
	}
}

// A-02: a CONFIRMED GPU drop stops the running model(s); a recovery (GPU
// came back online) must NEVER stop them — only record the event.
func TestGpuDropHookDropStopsRecoverDoesNot(t *testing.T) {
	s, _ := testServer(t)

	calls := 0
	s.stopAll = func(running func(string) bool) []string {
		calls++
		return []string{"m-a"}
	}

	// The hook is called directly below (the poller's debounce state machine
	// is covered in internal/status/poller_test.go).

	// Recover event: no stop.
	if st := s.d.Poller.OnGpuCountChange("recover", 2, 4); st != 0 {
		t.Fatalf("recover stopped %d model(s), want 0", st)
	}
	if calls != 0 {
		t.Fatalf("stopAll called %d time(s) on recover, want 0", calls)
	}

	// Drop event: stops.
	if st := s.d.Poller.OnGpuCountChange("drop", 4, 2); st != 1 {
		t.Fatalf("drop stopped %d model(s), want 1", st)
	}
	if calls != 1 {
		t.Fatalf("stopAll called %d time(s) on drop, want 1", calls)
	}
}

// C-02: /api/status carries the snapshot age and a stale flag, so clients
// can render "data as of ts" instead of presenting a wedged poller's last
// snapshot as live.
func TestStatusStaleFlag(t *testing.T) {
	s, token := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, token)

	get := func() map[string]interface{} {
		resp, err := client.Get(ts.URL + "/api/status")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("GET /api/status = %d, want 200", resp.StatusCode)
		}
		var body map[string]interface{}
		mustDecode(t, resp.Body, &body)
		return body
	}

	// Simulate a just-completed poll (a brand-new Poller has TS=0, which is
	// correctly reported as stale — no data yet).
	s.d.Poller.Cache().TS = float64(time.Now().Unix())
	if b := get(); b["stale"] != false {
		t.Fatalf("stale = %v, want false for a fresh snapshot", b["stale"])
	}

	// Wedged poller: last successful read 60s ago.
	s.d.Poller.Cache().TS = float64(time.Now().Unix() - 60)
	b := get()
	if b["stale"] != true {
		t.Fatalf("stale = %v, want true for a 60s-old snapshot", b["stale"])
	}
	if age, okk := b["age_s"].(float64); !okk || age < 15 {
		t.Fatalf("age_s = %v, want number >= 15", b["age_s"])
	}
}
