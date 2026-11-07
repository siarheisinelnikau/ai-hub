package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"aihub/internal/registry"
	"aihub/internal/status"
)

func TestModelProxyUsesHubAuthAndPreservesRequests(t *testing.T) {
	for _, backend := range []string{"Strata", "Maya", "llama.cpp"} {
		t.Run(backend, func(t *testing.T) {
			s, token := testServer(t)
			s.d.RequireAPIToken = true
			dir := t.TempDir()
			t.Setenv("AIHUB_KEYS_DIR", dir)
			if err := os.WriteFile(filepath.Join(dir, "m-a"), []byte("server-private-key"), 0600); err != nil {
				t.Fatal(err)
			}
			payload := `{"model":"existing-model-alias","reasoning_effort":"max","stream":true,"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"messages":[{"role":"user","content":[{"type":"text","text":"Привет"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}}]}]}`
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/health" {
					fmt.Fprint(w, `{"loaded":true}`)
					return
				}
				wantKey := "Bearer server-private-key"
				if backend == "llama.cpp" {
					wantKey = ""
				}
				if r.Header.Get("Authorization") != wantKey || r.Header.Get("Cookie") != "" {
					t.Error("upstream received the wrong credential or a client cookie")
				}
				switch r.URL.Path {
				case "/v1/models":
					if r.Method != http.MethodGet {
						t.Error("model discovery must remain GET")
					}
					fmt.Fprint(w, `{"object":"list","data":[{"id":"existing-model-alias"}]}`)
				case "/v1/chat/completions":
					body, _ := io.ReadAll(r.Body)
					if r.Method != http.MethodPost || string(body) != payload {
						t.Error("model ID, reasoning, tools or image request changed")
					}
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"choices\":[]}\n\n")
					w.(http.Flusher).Flush()
					fmt.Fprint(w, "data: [DONE]\n\n")
				default:
					t.Errorf("unexpected upstream path: %s", r.URL.Path)
				}
			}))
			defer up.Close()
			u, _ := url.Parse(up.URL)
			port, _ := strconv.Atoi(u.Port())
			s.d.Reg.Models[0].Port = port
			s.d.Poller.Cache().SetModels([]status.ModelStatusEntry{
				{ID: "m-a", Category: "LLM", Backend: backend, Port: port, Running: true, Healthy: true},
				{ID: "newer-model", Category: "LLM", Port: port + 1, Running: true, Healthy: true},
			})
			s.startedAt = func(string) (time.Time, bool) {
				t.Fatal("pinned routing must not select by start time")
				return time.Time{}, false
			}
			handler := s.Handler()
			for _, endpoint := range []struct{ method, path, body string }{
				{http.MethodGet, "/v1/model/m-a/models", ""},
				{http.MethodPost, "/v1/model/m-a/chat/completions", payload},
			} {
				for _, key := range []string{"", "wrong-key", "server-private-key", token} {
					req := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(endpoint.body))
					if key != "" {
						req.Header.Set("Authorization", "Bearer "+key)
					}
					req.Header.Set("Cookie", "unrelated=private")
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, req)
					want := http.StatusUnauthorized
					if key == token {
						want = http.StatusOK
					}
					if w.Code != want {
						t.Fatalf("%s: status %d, want %d: %s", endpoint.path, w.Code, want, w.Body.String())
					}
					if key == token && endpoint.method == http.MethodPost && (!w.Flushed || w.Body.String() != "data: {\"choices\":[]}\n\ndata: [DONE]\n\n") {
						t.Fatal("SSE response did not stream unchanged")
					}
				}
			}
		})
	}
}

func TestModelProxyNeverFallsBackFromStoppedOrUnknownProfile(t *testing.T) {
	s, _ := testServer(t)
	s.d.Reg.Models = append(s.d.Reg.Models, registry.Model{ID: "media", Category: "Media", Port: 8188})
	s.d.Poller.Cache().SetModels([]status.ModelStatusEntry{{ID: "other", Category: "LLM", Running: true, Healthy: true, Port: 12345}})
	s.freshLLMs = func() []status.ModelStatusEntry { return nil }
	for _, tc := range []struct {
		id   string
		want int
	}{{"m-a", 503}, {"unknown", 404}, {"media", 404}} {
		r := httptest.NewRequest(http.MethodPost, "/v1/model/"+tc.id+"/chat/completions", strings.NewReader(`{"model":"alias","messages":[]}`))
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != tc.want || !strings.Contains(w.Body.String(), tc.id) {
			t.Fatalf("%s: %d %s", tc.id, w.Code, w.Body.String())
		}
	}
}

func TestModelProxyFreshStartAndMissingServerKey(t *testing.T) {
	s, _ := testServer(t)
	t.Setenv("AIHUB_KEYS_DIR", t.TempDir())
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Error("native inference must fail before forwarding without its server key")
		}
		fmt.Fprint(w, `{"loaded":true}`)
	}))
	defer up.Close()
	u, _ := url.Parse(up.URL)
	port, _ := strconv.Atoi(u.Port())
	s.d.Poller.Cache().SetModels([]status.ModelStatusEntry{{ID: "other", Category: "LLM", Running: true, Port: 12345}})
	s.freshLLMs = func() []status.ModelStatusEntry {
		return []status.ModelStatusEntry{{ID: "m-a", Category: "LLM", Backend: "Maya", Running: true, Port: port}}
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/model/m-a/chat/completions", strings.NewReader(`{"messages":[]}`))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "AIHUB_KEYS_DIR on the server") {
		t.Fatalf("missing server key: %d %s", w.Code, w.Body.String())
	}
}
