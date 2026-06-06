package server

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// freePort returns a TCP port that is (briefly) free, so tests that scrape
// a model port stay hermetic even on a machine where the port is live
// (e.g. running the suite on the production box, where :18021 is q4k).
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return p
}

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindStaticFilePriority(t *testing.T) {
	aihub := t.TempDir()
	exeDir := t.TempDir()
	writeFile(t, filepath.Join(aihub, "static", "index.html"), "A")
	writeFile(t, filepath.Join(exeDir, "index.html"), "B")
	writeFile(t, filepath.Join(aihub, "static", "chat.html"), "C")

	find := FindStaticFile(aihub, exeDir)

	want := filepath.Join(aihub, "static", "index.html")
	if got := find("index.html"); got != want {
		t.Fatalf("find(index.html) = %q, want %q", got, want)
	}
	want = filepath.Join(aihub, "static", "chat.html")
	if got := find("chat.html"); got != want {
		t.Fatalf("find(chat.html) = %q, want %q", got, want)
	}
}

func TestFindStaticFileExeFallback(t *testing.T) {
	aihub := t.TempDir() // empty, no static
	exeDir := t.TempDir()
	writeFile(t, filepath.Join(exeDir, "server_fixed.html"), "F")
	t.Setenv("AIHUB_STATIC_FALLBACK", t.TempDir()) // hermetic: no real /srv/ai-hub

	find := FindStaticFile(aihub, exeDir)

	// aihub/static/index.html, /srv/ai-hub/static/index.html, exeDir/index.html
	// and exeDir/static/index.html are all absent, so server_fixed.html wins.
	want := filepath.Join(exeDir, "server_fixed.html")
	if got := find("index.html"); got != want {
		t.Fatalf("find(index.html) = %q, want %q", got, want)
	}
}

func TestFindStaticFileMissing(t *testing.T) {
	aihub := t.TempDir()
	exeDir := t.TempDir()
	t.Setenv("AIHUB_STATIC_FALLBACK", t.TempDir()) // hermetic: no real /srv/ai-hub

	find := FindStaticFile(aihub, exeDir)

	if got := find("index.html"); got != "" {
		t.Fatalf("find(index.html) = %q, want \"\"", got)
	}
	if got := find("nope.svg"); got != "" {
		t.Fatalf("find(nope.svg) = %q, want \"\"", got)
	}
}

func TestChooseStaticDir(t *testing.T) {
	aihub := t.TempDir() // empty
	exeDir := t.TempDir()
	writeFile(t, filepath.Join(exeDir, "static", "favicon.svg"), "f")
	if got := chooseStaticDir(aihub, exeDir); got != filepath.Join(exeDir, "static") {
		t.Fatalf("chooseStaticDir = %q, want %q", got, filepath.Join(exeDir, "static"))
	}

	aihub2 := t.TempDir() // empty
	exeDir2 := t.TempDir()
	writeFile(t, filepath.Join(exeDir2, "index.html"), "home")
	if got := chooseStaticDir(aihub2, exeDir2); got != "" {
		t.Fatalf("project root must never be mounted as /static: %q", got)
	}
}

func TestStaticHandlersServe(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "index.html"), "<html>home</html>")
	writeFile(t, filepath.Join(dir, "bg.svg"), "<svg/>")
	writeFile(t, filepath.Join(dir, "favicon.svg"), "f")
	writeFile(t, filepath.Join(dir, "static", "index.html"), "<html>home</html>")
	t.Setenv("AIHUB_STATIC_FALLBACK", t.TempDir()) // hermetic: no real /srv/ai-hub

	find := FindStaticFile(dir, dir) // exeDir/name hits dir/index.html, dir/bg.svg
	staticDir := chooseStaticDir(dir, dir)
	if staticDir != filepath.Join(dir, "static") {
		t.Fatalf("chooseStaticDir = %q, want static directory", staticDir)
	}

	mux := http.NewServeMux()
	staticHandlers(mux, find, staticDir)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	get := func(path string) (int, string) {
		t.Helper()
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(b)
	}

	code, body := get("/")
	if code != 200 || body != "<html>home</html>" {
		t.Fatalf("GET / = %d %q, want 200 %q", code, body, "<html>home</html>")
	}

	code, body = get("/bg.svg")
	if code != 200 || body != "<svg/>" {
		t.Fatalf("GET /bg.svg = %d %q, want 200 %q", code, body, "<svg/>")
	}

	code, _ = get("/chat")
	if code != 404 {
		t.Fatalf("GET /chat = %d, want 404", code)
	}

	code, body = get("/static/index.html")
	if code != 200 || body != "<html>home</html>" {
		t.Fatalf("GET /static/index.html = %d %q, want 200 %q", code, body, "<html>home</html>")
	}

	// "GET /{$}" catch-all: app.py 404s any path that is not /, /chat, an
	// allowed asset, or under /static/ — with FastAPI's exact JSON body.
	for _, p := range []string{"/index.html", "/app.js", "/nope.svg", "/chat.html", "/deep/path"} {
		code, body := get(p)
		if code != 404 || body != `{"detail":"Not Found"}` {
			t.Fatalf("GET %s = %d %q, want 404 {\"detail\":\"Not Found\"}", p, code, body)
		}
	}
}
