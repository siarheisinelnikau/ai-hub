package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"aihub/internal/status"
)

func TestStrataBenchCredentialsAndTokenUsage(t *testing.T) {
	testNativeBenchCredentialsAndTokenUsage(t, "Strata")
}

func TestMayaBenchCredentialsAndTokenUsage(t *testing.T) {
	testNativeBenchCredentialsAndTokenUsage(t, "Maya")
}

func testNativeBenchCredentialsAndTokenUsage(t *testing.T, backend string) {
	dir := t.TempDir()
	t.Setenv("AIHUB_KEYS_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "strata-bench"), []byte("private-test-key"), 0600); err != nil {
		t.Fatal(err)
	}
	s, token := testServer(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-test-key" {
			t.Error("missing Strata credentials")
			w.WriteHeader(401)
			return
		}
		var p map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Fatal(err)
		}
		if p["reasoning_effort"] != "none" {
			t.Error("benchmark should disable reasoning")
		}
		if p["stream_options"].(map[string]interface{})["include_usage"] != true {
			t.Error("benchmark must request actual token counts")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"1\\n2\\n3\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"completion_tokens\":256}}\n\ndata: [DONE]\n\n")
	}))
	defer up.Close()
	u, _ := url.Parse(up.URL)
	port, _ := strconv.Atoi(u.Port())
	s.d.Poller.Cache().SetModels([]status.ModelStatusEntry{{ID: "strata-bench", Port: port, Backend: backend, Category: "LLM", Running: true, Healthy: true}})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, token).Body.Close()
	resp, err := client.Post(ts.URL+"/api/model/strata-bench/benchmark", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got map[string]interface{}
	mustDecode(t, resp.Body, &got)
	if resp.StatusCode != 200 || got["ok"] != true || got["tokens"] != float64(256) {
		t.Fatalf("bad benchmark result: %#v", got)
	}
}
