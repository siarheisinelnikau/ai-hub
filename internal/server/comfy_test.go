package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newComfyTestServer wires a hub Server whose comfy proxy points at a fake
// ComfyUI (httptest). Returns the hub handler, the fake's request recorder,
// and the hub base URL.
func newComfyTestServer(t *testing.T, fake http.HandlerFunc) (*Server, *[]recordedReq, string) {
	t.Helper()
	rec := &[]recordedReq{}
	fakeComfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*rec = append(*rec, recordedReq{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Origin: r.Header.Get("Origin"), Body: string(body)})
		fake(w, r)
	}))
	t.Cleanup(fakeComfy.Close)
	s, token := testServer(t)
	s.comfyBaseURL = fakeComfy.URL
	s.comfyClient = &http.Client{Timeout: 5 * time.Second}
	return s, rec, token
}

type recordedReq struct {
	Method string
	Path   string
	Query  string
	Origin string
	Body   string
}

func TestComfyProxyPrompt_ForwardsBodyNoOrigin(t *testing.T) {
	s, rec, token := newComfyTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prompt" {
			t.Fatalf("fake got %s, want /prompt", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"prompt_id":"abc-123","number":7}`))
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, token)

	resp, err := client.Post(ts.URL+"/api/comfy/prompt", "application/json",
		strings.NewReader(`{"prompt":{"1":{"class_type":"CLIPLoader"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}
	var got struct {
		PromptID string `json:"prompt_id"`
		Number   int    `json:"number"`
	}
	mustDecode(t, resp.Body, &got)
	if got.PromptID != "abc-123" || got.Number != 7 {
		t.Fatalf("got %+v", got)
	}
	if len(*rec) != 1 {
		t.Fatalf("fake saw %d requests, want 1", len(*rec))
	}
	if (*rec)[0].Origin != "" {
		t.Fatalf("forwarded request carries Origin %q — ComfyUI would 403", (*rec)[0].Origin)
	}
	if !strings.Contains((*rec)[0].Body, "CLIPLoader") {
		t.Fatalf("body not forwarded: %q", (*rec)[0].Body)
	}
	if (*rec)[0].Method != "POST" {
		t.Fatalf("method %s, want POST", (*rec)[0].Method)
	}
}

func TestComfyProxyHistory_ForwardsQuerylessPath(t *testing.T) {
	s, rec, token := newComfyTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"abc-123":{"status":{"completed":true},"outputs":{"9":{"videos":[{"filename":"hub_vid.webm","subfolder":"","type":"output"}]}}}}}`))
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, token)

	resp, err := client.Get(ts.URL + "/api/comfy/history/abc-123")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, b)
	}
	var hist map[string]map[string]interface{}
	mustDecode(t, resp.Body, &hist)
	if _, ok := hist["abc-123"]; !ok {
		t.Fatalf("missing entry: %v", hist)
	}
	if (*rec)[0].Path != "/history/abc-123" {
		t.Fatalf("fake got %s", (*rec)[0].Path)
	}
}

func TestComfyProxyView_StreamsBodyAndContentType(t *testing.T) {
	payload := []byte("WEBM-FAKE-BYTES")
	s, rec, token := newComfyTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/webm")
		w.Write(payload)
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, token)

	resp, err := client.Get(ts.URL + "/api/comfy/view?filename=hub_vid.webm&subfolder=&type=output")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "video/webm") {
		t.Fatalf("Content-Type %q", ct)
	}
	got, _ := io.ReadAll(resp.Body)
	if string(got) != string(payload) {
		t.Fatalf("body %q != %q", got, payload)
	}
	if !strings.Contains((*rec)[0].Query, "filename=hub_vid.webm") || !strings.Contains((*rec)[0].Query, "type=output") {
		t.Fatalf("query not forwarded: %q", (*rec)[0].Query)
	}
}

func TestComfyProxy_UnknownEndpoint404(t *testing.T) {
	// Note: a method mismatch on a KNOWN path yields 405 (mux behavior,
	// same as the rest of the hub API) — only unknown paths give 404.
	cases := []struct{ method, rest string }{
		{"GET", "prompt"},      // prompt is POST-only
		{"POST", "history/abc"}, // history is GET-only
		{"GET", "history"},     // missing id
		{"GET", "view/extra"},  // not whitelisted
	}

	s, _, token := newComfyTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("fake should not be reached for %s %s", r.Method, r.URL.Path)
	})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, token)
	for _, c := range cases {
		resp, err := client.Do(mkReq(t, c.method, ts.URL+"/api/comfy/"+c.rest))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("%s /api/comfy/%s: status %d, want 404", c.method, c.rest, resp.StatusCode)
		}
	}
	// Method mismatch on a known path → 405 (mux convention).
	req, _ := http.NewRequest("DELETE", ts.URL+"/api/comfy/prompt", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Fatalf("DELETE /api/comfy/prompt: status %d, want 405", resp.StatusCode)
	}
}

func TestComfyProxy_RequiresAuth(t *testing.T) {
	s, _, _ := newComfyTestServer(t, func(w http.ResponseWriter, r *http.Request) {})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/api/comfy/history/abc")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("status %d, want 401 (no cookie)", resp.StatusCode)
	}
}

func TestComfyProxy_UpstreamDown502(t *testing.T) {
	s, _, token := newComfyTestServer(t, func(w http.ResponseWriter, r *http.Request) {})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	client := testClient(t)
	doLogin(t, client, ts.URL, token)
	// Point at a dead port.
	s.comfyBaseURL = "http://127.0.0.1:1"
	resp, err := client.Post(ts.URL+"/api/comfy/prompt", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 502 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d (%s), want 502", resp.StatusCode, b)
	}
}

func mkReq(t *testing.T, method, url string) *http.Request {
	t.Helper()
	r, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
