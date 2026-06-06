package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"aihub/internal/status"
)

func TestPickUniversalTarget_Empty(t *testing.T) {
	if got := pickUniversalTarget(nil, nil); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
	if got := pickUniversalTarget([]status.ModelStatusEntry{}, nil); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestPickUniversalTarget_LatestStartedWins(t *testing.T) {
	old, recent := time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour)
	running := []status.ModelStatusEntry{
		{ID: "old", Port: 1, Category: "LLM", Healthy: true},
		{ID: "new", Port: 2, Category: "LLM", Healthy: true},
	}
	sa := func(id string) (time.Time, bool) {
		switch id {
		case "old":
			return old, true
		case "new":
			return recent, true
		}
		return time.Time{}, false
	}
	if got := pickUniversalTarget(running, sa); got.ID != "new" {
		t.Fatalf("got %q, want new (last started)", got.ID)
	}
}

func TestPickUniversalTarget_LatestWinsEvenIfLoading(t *testing.T) {
	old, recent := time.Now().Add(-2*time.Hour), time.Now()
	running := []status.ModelStatusEntry{
		{ID: "old-healthy", Port: 1, Category: "LLM", Healthy: true},
		{ID: "new-loading", Port: 2, Category: "LLM", Healthy: false},
	}
	sa := func(id string) (time.Time, bool) {
		switch id {
		case "old-healthy":
			return old, true
		case "new-loading":
			return recent, true
		}
		return time.Time{}, false
	}
	// The last-started model wins even while still loading: the handler waits
	// for it to become healthy instead of silently answering with the old one.
	if got := pickUniversalTarget(running, sa); got.ID != "new-loading" {
		t.Fatalf("got %q, want new-loading (last started)", got.ID)
	}
}

func TestPickUniversalTarget_NoStartTimes_PrefersHealthy(t *testing.T) {
	running := []status.ModelStatusEntry{
		{ID: "a-unhealthy", Port: 1, Category: "LLM", Healthy: false},
		{ID: "b-healthy", Port: 2, Category: "LLM", Healthy: true},
	}
	sa := func(string) (time.Time, bool) { return time.Time{}, false }
	// No docker start times (dev box): the healthy candidate wins.
	if got := pickUniversalTarget(running, sa); got.ID != "b-healthy" {
		t.Fatalf("got %q, want b-healthy", got.ID)
	}
}

func TestPickHealthyFallback(t *testing.T) {
	old, recent := time.Now().Add(-2*time.Hour), time.Now()
	sa := func(id string) (time.Time, bool) {
		switch id {
		case "old-healthy":
			return old, true
		case "new-loading":
			return recent, true
		}
		return time.Time{}, false
	}
	running := []status.ModelStatusEntry{
		{ID: "old-healthy", Port: 1, Category: "LLM", Healthy: true},
		{ID: "new-loading", Port: 2, Category: "LLM", Healthy: false},
	}
	if got := pickHealthyFallback(running, sa); got == nil || got.ID != "old-healthy" {
		t.Fatalf("got %v, want old-healthy", got)
	}
	// No healthy model -> nil.
	unhealthy := []status.ModelStatusEntry{{ID: "x", Port: 1, Category: "LLM", Healthy: false}}
	if got := pickHealthyFallback(unhealthy, sa); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
	// Empty -> nil.
	if got := pickHealthyFallback(nil, sa); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestPickUniversalTarget_AllUnhealthy_LatestWins(t *testing.T) {
	old, recent := time.Now().Add(-2*time.Hour), time.Now()
	running := []status.ModelStatusEntry{
		{ID: "a", Port: 1, Category: "LLM", Healthy: false},
		{ID: "b", Port: 2, Category: "LLM", Healthy: false},
	}
	sa := func(id string) (time.Time, bool) {
		if id == "b" {
			return recent, true
		}
		if id == "a" {
			return old, true
		}
		return time.Time{}, false
	}
	if got := pickUniversalTarget(running, sa); got.ID != "b" {
		t.Fatalf("got %q, want b (no healthy fallback)", got.ID)
	}
}

func TestPickUniversalTarget_NoStartTimes_FirstCandidate(t *testing.T) {
	running := []status.ModelStatusEntry{
		{ID: "a", Port: 1, Category: "LLM", Healthy: true},
		{ID: "b", Port: 2, Category: "LLM", Healthy: true},
	}
	sa := func(string) (time.Time, bool) { return time.Time{}, false }
	if got := pickUniversalTarget(running, sa); got.ID != "a" {
		t.Fatalf("got %q, want a (registry order fallback)", got.ID)
	}
}

func TestUniversalChat_NoRunning_503(t *testing.T) {
	s, _ := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"universal","messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	body := readAll(t, resp.Body)
	if !strings.Contains(body, "no LLM is running") {
		t.Fatalf("body = %q, want 'no LLM is running'", body)
	}
}

func TestUniversalChat_MethodNotAllowed(t *testing.T) {
	s, _ := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	// GET is swallowed by the "GET /" catch-all (404 JSON, same as the other
	// API routes); other methods yield the mux's 405.
	resp, err := http.Get(ts.URL + "/v1/chat/completions")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET status = %d, want 404 (catch-all)", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/v1/chat/completions", strings.NewReader("{}"))
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("PUT status = %d, want 405", resp2.StatusCode)
	}
}

func TestUniversalModels_NoRunning(t *testing.T) {
	s, _ := testServer(t)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if body := readAll(t, resp.Body); !strings.Contains(body, `"universal"`) {
		t.Fatalf("body = %q, want universal entry", body)
	}
}

func TestUniversalChat_ProxiesStreaming(t *testing.T) {
	s, _ := testServer(t)
	s.d.RequireAPIToken = true

	// Fake upstream LLM: records the forwarded body + auth, streams SSE.
	var gotBody, gotAuth string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// llama.cpp-style /health (the proxy probes it before forwarding).
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"status":"ok"}`)
			return
		}
		b, _ := io.ReadAll(r.Body)
		gotBody, gotAuth = string(b), r.Header.Get("Authorization")
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("upstream path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl := w.(http.Flusher)
		for _, chunk := range []string{`data: {"id":"1","choices":[]}` + "\n\n", "data: [DONE]\n\n"} {
			fmt.Fprint(w, chunk)
			fl.Flush()
		}
	}))
	defer up.Close()

	u, err := url.Parse(up.URL)
	if err != nil {
		t.Fatal(err)
	}
	upPort, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}

	// Mark m-a (LLM) running+healthy on the fake upstream port.
	s.d.Poller.Cache().SetModels([]status.ModelStatusEntry{
		{ID: "m-a", Name: "A", Category: "LLM", Port: upPort, Running: true, Healthy: true},
	})
	s.startedAt = func(id string) (time.Time, bool) {
		if id == "m-a" {
			return time.Now(), true
		}
		return time.Time{}, false
	}

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	reqBody := `{"model":"universal","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}
	body := readAll(t, resp.Body)
	want := `data: {"id":"1","choices":[]}` + "\n\n" + "data: [DONE]\n\n"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
	if gotBody != reqBody {
		t.Fatalf("forwarded body = %q, want %q", gotBody, reqBody)
	}
	if gotAuth != "" {
		t.Fatal("hub credential leaked to inference backend")
	}
}

func TestUniversalChat_StrataUsesPrivateUpstreamKey(t *testing.T) {
	testUniversalNativeKey(t, "Strata")
}

func TestUniversalChat_MayaUsesPrivateUpstreamKey(t *testing.T) {
	testUniversalNativeKey(t, "Maya")
}

func testUniversalNativeKey(t *testing.T, backend string) {
	dir := t.TempDir()
	t.Setenv("AIHUB_KEYS_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "strata-test"), []byte("upstream-only-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, _ := testServer(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			fmt.Fprint(w, `{"loaded":true}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer upstream-only-key" {
			t.Error("Strata did not receive its private upstream key")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer up.Close()
	u, _ := url.Parse(up.URL)
	port, _ := strconv.Atoi(u.Port())
	s.d.Poller.Cache().SetModels([]status.ModelStatusEntry{{ID: "strata-test", Backend: backend, Category: "LLM", Port: port, Running: true, Healthy: true}})
	s.startedAt = func(string) (time.Time, bool) { return time.Now(), true }
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"universal","messages":[]}`))
	req.Header.Set("Authorization", "Bearer none")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "ok") {
		t.Fatalf("proxy response: %d %s", w.Code, w.Body.String())
	}
}

func TestUniversalChat_UpstreamDown_503(t *testing.T) {
	s, _ := testServer(t)
	// Cache says m-a is running+healthy, but its port is closed (connection
	// refused) and its container is gone — a stale cache entry for a model
	// that just died. The proxy must not forward into the void.
	s.d.Poller.Cache().SetModels([]status.ModelStatusEntry{
		{ID: "m-a", Name: "A", Category: "LLM", Port: 18099, Running: true, Healthy: true},
	})
	s.startedAt = func(id string) (time.Time, bool) {
		if id == "m-a" {
			return time.Now(), true
		}
		return time.Time{}, false
	}
	s.containerRunning = func(id string) bool { return id != "m-a" } // m-a's container is gone
	s.healthPoll = 50 * time.Millisecond

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if body := readAll(t, resp.Body); !strings.Contains(body, "m-a") || !strings.Contains(body, "not responding") {
		t.Fatalf("body = %q, want a clear 'm-a ... not responding' message", body)
	}
}

// closedPort reserves a TCP port and returns it after closing the listener —
// the port then refuses connections (a model whose port is not open yet).
func closedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func TestUniversalChat_WaitsForLoadingModel(t *testing.T) {
	s, _ := testServer(t)

	// Port that is closed while the fake upstream "loads", then opens.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	var gotBody string
	up := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer up.Close()
	go func() {
		time.Sleep(400 * time.Millisecond) // simulate model loading into VRAM
		l2, err := net.Listen("tcp", addr)
		if err != nil {
			t.Errorf("rebind %s: %v", addr, err)
			return
		}
		up.Listener = l2
		up.Start()
	}()

	// Target is running but still loading (port closed -> unhealthy).
	s.d.Poller.Cache().SetModels([]status.ModelStatusEntry{
		{ID: "m-a", Name: "A", Category: "LLM", Port: port, Running: true, Healthy: false},
	})
	s.startedAt = func(id string) (time.Time, bool) {
		if id == "m-a" {
			return time.Now(), true
		}
		return time.Time{}, false
	}
	s.containerRunning = func(string) bool { return true }
	s.healthPoll = 50 * time.Millisecond
	s.loadWait = 10 * time.Second

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	reqBody := `{"model":"universal","messages":[{"role":"user","content":"hi"}]}`
	start := time.Now()
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (request must survive the load window)", resp.StatusCode)
	}
	if body := readAll(t, resp.Body); body != "data: [DONE]\n\n" {
		t.Fatalf("body = %q", body)
	}
	if gotBody != reqBody {
		t.Fatalf("forwarded body = %q, want %q", gotBody, reqBody)
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Fatalf("request completed in %v — it did not wait for the model to load", elapsed)
	}
}

func TestUniversalChat_LoadingFallsBackToHealthy(t *testing.T) {
	s, _ := testServer(t)

	// Older model: healthy, fake upstream (answers with a marker).
	upB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(w, r.Body)
		fmt.Fprint(w, "from-b")
	}))
	defer upB.Close()
	bPort, err := strconv.Atoi(mustURLPort(t, upB.URL))
	if err != nil {
		t.Fatal(err)
	}

	// Newer model: still loading, port closed, never comes up in time.
	aPort := closedPort(t)

	s.d.Poller.Cache().SetModels([]status.ModelStatusEntry{
		{ID: "m-b", Name: "B", Category: "LLM", Port: bPort, Running: true, Healthy: true},
		{ID: "m-a", Name: "A", Category: "LLM", Port: aPort, Running: true, Healthy: false},
	})
	s.startedAt = func(id string) (time.Time, bool) {
		switch id {
		case "m-a":
			return time.Now(), true // last started
		case "m-b":
			return time.Now().Add(-time.Hour), true
		}
		return time.Time{}, false
	}
	s.containerRunning = func(string) bool { return true }
	s.healthPoll = 50 * time.Millisecond
	s.loadWait = 250 * time.Millisecond // expires while m-a is still "loading"

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"universal","messages":[{"role":"user","content":"ping"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (fallback to healthy m-b)", resp.StatusCode)
	}
	if body := readAll(t, resp.Body); !strings.Contains(body, "from-b") {
		t.Fatalf("body = %q, want answer from m-b", body)
	}
}

func TestUniversalChat_LoadingNoFallback_503(t *testing.T) {
	s, _ := testServer(t)
	s.d.Poller.Cache().SetModels([]status.ModelStatusEntry{
		{ID: "m-a", Name: "A", Category: "LLM", Port: closedPort(t), Running: true, Healthy: false},
	})
	s.startedAt = func(id string) (time.Time, bool) {
		if id == "m-a" {
			return time.Now(), true
		}
		return time.Time{}, false
	}
	s.containerRunning = func(string) bool { return true }
	s.healthPoll = 50 * time.Millisecond
	s.loadWait = 200 * time.Millisecond

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	body := readAll(t, resp.Body)
	if !strings.Contains(body, "m-a") || !strings.Contains(body, "not responding") {
		t.Fatalf("body = %q, want a clear 'm-a ... not responding' message", body)
	}
}

func TestWaitForModelReady_ContainerStopped_EarlyExit(t *testing.T) {
	s, _ := testServer(t)
	s.containerRunning = func(string) bool { return false } // crashed container
	s.healthPoll = 50 * time.Millisecond
	s.loadWait = 10 * time.Second

	start := time.Now()
	if ok := s.waitForModelReady(context.Background(), "m-a", closedPort(t)); ok {
		t.Fatal("waitForModelReady = true, want false (port closed, container stopped)")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("took %v — must bail early when the container stopped", elapsed)
	}
}

func TestWaitForModelReady_BecomesHealthy(t *testing.T) {
	s, _ := testServer(t)
	// HealthCheck passes on any 2xx endpoint.
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer up.Close()
	port, err := strconv.Atoi(mustURLPort(t, up.URL))
	if err != nil {
		t.Fatal(err)
	}
	s.containerRunning = func(string) bool { return true }
	if ok := s.waitForModelReady(context.Background(), "m-a", port); !ok {
		t.Fatal("waitForModelReady = false, want true (port already serving)")
	}
}

func TestWaitForModelReady_BrokenOldModel_FastFail(t *testing.T) {
	s, _ := testServer(t)
	// A model that started 2 hours ago (default load deadline is 5 min) and
	// whose port is closed is broken, not loading — fail fast.
	s.startedAt = func(id string) (time.Time, bool) {
		if id == "m-a" {
			return time.Now().Add(-2 * time.Hour), true
		}
		return time.Time{}, false
	}
	s.containerRunning = func(string) bool { return true }
	start := time.Now()
	if ok := s.waitForModelReady(context.Background(), "m-a", closedPort(t)); ok {
		t.Fatal("waitForModelReady = true, want false (broken old model)")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("took %v — must fail fast for a broken model", elapsed)
	}
}

func TestUniversalChat_StaleCacheReResolves(t *testing.T) {
	s, _ := testServer(t)

	// The poller cache still names the just-stopped model m-a (running+
	// healthy, stale), while the on-demand check finds the just-started m-b.
	upB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upB.Close()
	bPort, err := strconv.Atoi(mustURLPort(t, upB.URL))
	if err != nil {
		t.Fatal(err)
	}

	s.d.Poller.Cache().SetModels([]status.ModelStatusEntry{
		{ID: "m-a", Name: "A", Category: "LLM", Port: closedPort(t), Running: true, Healthy: true},
	})
	s.freshLLMs = func() []status.ModelStatusEntry {
		return []status.ModelStatusEntry{
			{ID: "m-b", Name: "B", Category: "LLM", Port: bPort, Running: true, Healthy: true},
		}
	}
	s.startedAt = func(string) (time.Time, bool) { return time.Now(), true }
	s.containerRunning = func(id string) bool { return id != "m-a" } // m-a's container is gone
	s.healthPoll = 50 * time.Millisecond

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 (stale cache entry must be re-resolved to the new model): %s", resp.StatusCode, body)
	}
	if body := readAll(t, resp.Body); body != "data: [DONE]\n\n" {
		t.Fatalf("body = %q, want the new model's answer", body)
	}
}

func TestUniversalChat_FreshCheckWhenCacheEmpty(t *testing.T) {
	s, _ := testServer(t)

	// Upstream is running, but the poller cache is empty (request arrives
	// seconds after a model switch, before the next 3 s poll).
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer up.Close()
	port, err := strconv.Atoi(mustURLPort(t, up.URL))
	if err != nil {
		t.Fatal(err)
	}

	s.freshLLMs = func() []status.ModelStatusEntry {
		return []status.ModelStatusEntry{
			{ID: "m-a", Name: "A", Category: "LLM", Port: port, Running: true, Healthy: true},
		}
	}
	s.startedAt = func(id string) (time.Time, bool) {
		if id == "m-a" {
			return time.Now(), true
		}
		return time.Time{}, false
	}

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 (on-demand re-check must find the just-started model): %s", resp.StatusCode, body)
	}
}

func mustURLPort(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Port()
}
