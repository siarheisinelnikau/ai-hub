package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"aihub/internal/config"
)

func TestPublicRuntimeConfigContainsOnlyAddresses(t *testing.T) {
	t.Setenv("AIHUB_TOKEN", "never-publish-this-secret")
	s, _ := testServer(t)
	s.d.Public = config.Public{ServerIP: "10.0.0.5", WebUIURL: "https://chat.example.test/"}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/runtime-config.js", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("config missing or cached")
	}
	body := w.Body.String()
	if !strings.Contains(body, `"serverIP":"10.0.0.5"`) || strings.Contains(body, "secret") || strings.Contains(body, "AIHUB_TOKEN") {
		t.Fatal("wrong runtime payload")
	}
}

func TestProjectRootSecretsAreNeverMounted(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"index.html", ".env", "token", "server_token.txt"} {
		writeFile(t, filepath.Join(dir, name), "private-fixture")
	}
	mux := http.NewServeMux()
	staticHandlers(mux, FindStaticFile(dir, dir), chooseStaticDir(dir, dir))
	for _, path := range []string{"/.env", "/token", "/static/.env", "/static/token", "/static/server_token.txt"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 || strings.Contains(w.Body.String(), "private-fixture") {
			t.Fatalf("secret exposed at %s", path)
		}
	}
}
