package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aihub/internal/status"
)

func TestContextBenchmarksAuthAndUnavailable(t *testing.T) {
	s, token := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/benchmarks")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("unauthenticated status = %d", resp.StatusCode)
	}
	client := testClient(t)
	doLogin(t, client, ts.URL, token).Body.Close()
	resp, err = client.Get(ts.URL + "/api/benchmarks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || data["status"] != "unavailable" {
		t.Fatalf("missing file: %#v", data)
	}
}

func TestContextBenchmarksMeasuredBaselineAndLiveTPS(t *testing.T) {
	s, token := testServer(t)
	dir := filepath.Join(s.d.AIHUBRoot, "benchmarks")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	fixture := `{"version":1,"status":"running","models":[{"id":"m-a","short":"A","status":"complete","quality_note":"Occasional malformed words","sampling":{"temperature":0,"repeat_penalty":1.05,"repeat_last_n":64},"tps_20k":91.5,"points":[{"input_tokens":20476,"decode_tps":91.5},{"input_tokens":65530,"decode_tps":87}]}]}`
	if err := os.WriteFile(filepath.Join(dir, "context.json"), []byte(fixture), 0644); err != nil {
		t.Fatal(err)
	}
	models := []status.ModelStatusEntry{
		{ID: "m-a", Category: "LLM", TPS: "obsolete short-prompt result", TPSLive: 170},
		{ID: "pending", Category: "LLM", TPS: "old value"},
		{ID: "media", Category: "Image", TPS: "1 image/s"},
	}
	s.annotateContextBenchmarks(models)
	if models[0].TPS != "91.5 tok/s @20k" || models[0].BenchmarkPoints != 2 || models[0].TPSLive != 170 {
		t.Fatalf("baseline changed live TPS: %#v", models[0])
	}
	if models[1].TPS != "— @20k" || models[2].TPS != "1 image/s" {
		t.Fatalf("pending/media: %#v", models)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, token).Body.Close()
	resp, err := client.Get(ts.URL + "/api/benchmarks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := readAll(t, resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(body, `"input_tokens":20476`) || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("curve response: %s", body)
	}
	var returned contextBenchmarks
	if err := json.Unmarshal([]byte(body), &returned); err != nil {
		t.Fatal(err)
	}
	if len(returned.Models) != 1 || returned.Models[0].QualityNote != "Occasional malformed words" {
		t.Fatalf("quality note dropped: %s", body)
	}
	var sampling map[string]interface{}
	if err := json.Unmarshal(returned.Models[0].Sampling, &sampling); err != nil {
		t.Fatal(err)
	}
	if sampling["repeat_penalty"] != 1.05 || sampling["repeat_last_n"] != float64(64) {
		t.Fatalf("sampling metadata dropped: %s", body)
	}
	if err := os.WriteFile(filepath.Join(dir, "context.json"), []byte(`{broken`), 0644); err != nil {
		t.Fatal(err)
	}
	resp2, err := client.Get(ts.URL + "/api/benchmarks")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != 500 {
		t.Fatalf("corrupt data status=%d", resp2.StatusCode)
	}
}
