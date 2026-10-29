// Package server wires the ai-hub HTTP API + static UI, mirroring app.py.
package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// staticFallbackDir is the installed-UI directory probed between aihubRoot
// and exeDir (candidate #2 in FindStaticFile). AIHUB_STATIC_FALLBACK lets
// tests run hermetically on a machine that has a real /srv/ai-hub (e.g. the
// production box itself) — production leaves the env unset.
func staticFallbackDir() string {
	if v := os.Getenv("AIHUB_STATIC_FALLBACK"); v != "" {
		return v
	}
	return "/srv/ai-hub/static"
}

// FindStaticFile mirrors app.py find_static_file(name): returns the first
// existing file from the candidate chain, or "" if none.
// Candidates (in order):
//  1. aihubRoot/static/name
//  2. /srv/ai-hub/static/name
//  3. exeDir/name
//  4. exeDir/static/name
//     5-8. (index.html only) exeDir/server_fixed.html, server_current.html,
//     new_index.html, index.html
//  9. (chat.html only) exeDir/new_chat.html
func FindStaticFile(aihubRoot, exeDir string) func(name string) string {
	return func(name string) string {
		cands := []string{
			filepath.Join(aihubRoot, "static", name),
			filepath.Join(staticFallbackDir(), name),
			filepath.Join(exeDir, name),
			filepath.Join(exeDir, "static", name),
		}
		if name == "index.html" {
			cands = append(cands,
				filepath.Join(exeDir, "server_fixed.html"),
				filepath.Join(exeDir, "server_current.html"),
				filepath.Join(exeDir, "new_index.html"),
				filepath.Join(exeDir, "index.html"),
			)
		}
		if name == "chat.html" {
			cands = append(cands, filepath.Join(exeDir, "new_chat.html"))
		}
		for _, p := range cands {
			if p == "" {
				continue
			}
			if fileExists(p) {
				return p
			}
		}
		return ""
	}
}

// chooseStaticDir mirrors app.py's /static mount dir selection: first of
// [aihubRoot/static, exeDir/static] containing index.html OR
// favicon.svg. Returns "" if none.
func chooseStaticDir(aihubRoot, exeDir string) string {
	for _, d := range []string{
		filepath.Join(aihubRoot, "static"),
		filepath.Join(exeDir, "static"),
	} {
		if fileExists(filepath.Join(d, "index.html")) || fileExists(filepath.Join(d, "favicon.svg")) {
			return d
		}
	}
	return ""
}

// serveFile writes the file with its content type.
func serveFile(w http.ResponseWriter, r *http.Request, p string) {
	http.ServeFile(w, r, p)
}

// text404 mirrors FastAPI PlainTextResponse(msg, status_code=404).
func text404(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprint(w, msg)
}

// staticHandlers returns the route registrations for static/UI endpoints.
// It registers: GET / , GET /chat, GET /{asset} for the known asset list,
// and the /static/* mount. find resolves file paths; staticDir is the mount
// dir ("" disables the mount).
func staticHandlers(mux *http.ServeMux, find func(name string) string, staticDir string) {
	// "GET /" serves the root UI AND (Go ServeMux special case) is the
	// fallback for every other unmatched GET path. app.py: / -> index,
	// /{asset} allow-list -> files (above), anything else -> 404 JSON.
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"detail": "Not Found"})
			return
		}
		f := find("index.html")
		if f != "" {
			serveFile(w, r, f)
			return
		}
		// app.py / fallback: try server_fixed/current/new_index/index in
		// exeDir (FindStaticFile's index.html candidates already cover this),
		// then PlainTextResponse 404.
		text404(w, "ai-hub static not found")
	})
	mux.HandleFunc("GET /chat", func(w http.ResponseWriter, r *http.Request) {
		f := find("chat.html")
		if f != "" {
			serveFile(w, r, f)
			return
		}
		// app.py chat_page fallback: PlainTextResponse 404.
		text404(w, "chat.html not found")
	})
	for _, asset := range []string{"bg.svg", "favicon.svg", "favicon.ico", "favicon_light.svg", "bg.png"} {
		a := asset
		mux.HandleFunc("GET /"+a, func(w http.ResponseWriter, r *http.Request) {
			f := find(a)
			if f != "" {
				serveFile(w, r, f)
				return
			}
			// app.py /{asset}: HTTPException(404) -> JSON body.
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"detail": "Not Found"})
		})
	}
	if staticDir != "" {
		mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))
	}
}
