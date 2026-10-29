package metrics

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func tsPort(ts *httptest.Server) int {
	u, _ := url.Parse(ts.URL)
	p, _ := strconv.Atoi(u.Port())
	return p
}

func TestScrapeVLLM(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			fmt.Fprint(w, "vllm:num_requests_running 2\n")
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	m := ScrapeModelMetrics(tsPort(ts), "vllm", "m", NewScrapper())
	if m == nil {
		t.Fatal("ScrapeModelMetrics(vllm) = nil, want stats")
	}
	if got := m["backend"]; got != "vllm" {
		t.Errorf("backend = %v, want vllm", got)
	}
	if got := m["running"]; got != 0 {
		t.Errorf("running = %v, want 0 (vllm regex quirk: colon keys never match)", got)
	}
}

func TestScrapeLlamaWithSlots(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			fmt.Fprint(w, "llamacpp:tokens_predicted_total 500\n"+
				"llamacpp:tokens_predicted_seconds_total 10\n"+
				"llamacpp:requests_processing 1\n")
		case "/slots":
			fmt.Fprint(w, `[{"is_processing":true,"n_prompt_tokens":1234},`+
				`{"is_processing":false,"n_prompt_tokens":99}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	m := ScrapeModelMetrics(tsPort(ts), "llamacpp", "m", NewScrapper())
	if m == nil {
		t.Fatal("ScrapeModelMetrics(llamacpp) = nil, want stats")
	}
	if got := m["backend"]; got != "llamacpp" {
		t.Errorf("backend = %v, want llamacpp", got)
	}
	if got := m["tokens"]; got != int64(500) {
		t.Errorf("tokens = %v, want int64(500)", got)
	}
	if got := m["ctx_used"]; got != int64(1234) {
		t.Errorf("ctx_used = %v, want int64(1234) (max n_prompt_tokens of processing slots)", got)
	}
	if got := m["tps"]; got != 50.0 {
		t.Errorf("tps = %v, want 50.0 (cumulative fallback)", got)
	}
}

func TestScrapeClosedPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	if m := ScrapeModelMetrics(port, "llamacpp", "m", NewScrapper()); m != nil {
		t.Errorf("ScrapeModelMetrics(closed port) = %v, want nil", m)
	}
}

func TestScrapeHTTP404(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ts.Close()

	if m := ScrapeModelMetrics(tsPort(ts), "llamacpp", "m", NewScrapper()); m != nil {
		t.Errorf("ScrapeModelMetrics(404) = %v, want nil", m)
	}
}

func TestScrapeMayaCredentialsAndLiveMetrics(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AIHUB_KEYS_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "maya-test"), []byte("maya-private-key"), 0600); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" || r.Header.Get("Authorization") != "Bearer maya-private-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"engine":{"max_context":65536},"live":{"state":"generating","tok_s":46.2,"generated":200,"prompt_tokens":20480},"totals":{"output_tokens":200,"decode_ms":4329}}`)
	}))
	defer ts.Close()
	m := ScrapeModelMetrics(tsPort(ts), "Maya", "maya-test", NewScrapper())
	if m == nil || m["backend"] != "Maya" || m["tps"] != 46.2 || m["n_ctx"] != 65536 || m["running"] != 1 {
		t.Fatalf("Maya metrics lost authentication, backend or live values: %#v", m)
	}
}
