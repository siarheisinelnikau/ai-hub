// Command mcp runs the ai-hub MCP server.
// It exposes the local media models as MCP tools that LLMs can call:
//
//	images:  generate_image  (ComfyUI · FLUX.1-dev)
//	video:   generate_video  (ComfyUI · Wan 2.1 T2V)
//	audio:   text_to_speech  (Kokoro TTS)
//	        speech_to_text  (faster-whisper)
//	        generate_music  (ACE-Step 1.5)
//	3D:      image_to_3d     (Hunyuan3D-2.1)
//	plus:    media_status
//
// Usage:
//
//	./mcp                          # stdio transport (Claude Desktop / MCP clients)
//	./mcp -http :8090              # HTTP transport (POST /mcp, SSE GET /mcp)
//
// Service discovery defaults to localhost ports matching /srv/ai/stack
// docker-compose (8188/8010/8016/8020). Override per-service:
//
//	./mcp -comfyui http://192.0.2.10:8188 -voice http://192.0.2.10:8010 ...
//
// Configure in Claude Desktop (claude_desktop_config.json):
//
//	{
//	  "mcpServers": {
//	    "ai-hub-media": {
//	      "command": "/path/to/mcp",
//	      "args": ["-comfyui", "http://192.0.2.10:8188",
//	               "-voice", "http://192.0.2.10:8010",
//	               "-music", "http://192.0.2.10:8016",
//	               "-threed", "http://192.0.2.10:8020"]
//	    }
//	  }
//	}
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"aihub/internal/auth"
	"aihub/internal/config"
	"aihub/internal/mcp"
)

func main() {
	if err := config.LoadEnvironment(); err != nil {
		log.Fatalf("config: %v", err)
	}
	httpAddr := flag.String("http", os.Getenv("AIHUB_MCP_LISTEN"), "HTTP listen address (e.g. :8090). Empty = stdio transport.")
	cfg := mcp.NewMediaConfig("127.0.0.1")
	cfg.ComfyUI = config.EnvOr("AIHUB_COMFYUI_URL", cfg.ComfyUI)
	cfg.Voice = config.EnvOr("AIHUB_VOICE_URL", cfg.Voice)
	cfg.Music = config.EnvOr("AIHUB_MUSIC_URL", cfg.Music)
	cfg.Threed = config.EnvOr("AIHUB_THREED_URL", cfg.Threed)
	cfg.OutDir = config.EnvOr("AIHUB_MCP_OUTDIR", cfg.OutDir)
	cfg.ThreedOutDir = config.EnvOr("AIHUB_MCP_THREED_OUTDIR", cfg.ThreedOutDir)
	flag.StringVar(&cfg.ComfyUI, "comfyui", cfg.ComfyUI, "ComfyUI base URL")
	flag.StringVar(&cfg.Voice, "voice", cfg.Voice, "Voice (TTS/STT) base URL")
	flag.StringVar(&cfg.Music, "music", cfg.Music, "Music (ACE-Step) base URL")
	flag.StringVar(&cfg.Threed, "threed", cfg.Threed, "Hunyuan3D base URL")
	flag.StringVar(&cfg.OutDir, "outdir", cfg.OutDir, "Directory for saved artifacts")
	flag.StringVar(&cfg.ThreedOutDir, "threed-outdir", cfg.ThreedOutDir, "Host dir of Hunyuan3D outputs (fallback)")
	flag.Func("read-roots", "Comma-separated dirs the audio_path/image_path args may read from (E-01; default: <outdir>, /srv/ai, /home/user)", func(v string) error {
		var roots []string
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				roots = append(roots, p)
			}
		}
		if len(roots) > 0 {
			cfg.ReadRoots = roots
		}
		return nil
	})
	corsOrigins := flag.String("cors", os.Getenv("AIHUB_MCP_CORS"), "Comma-separated allowed CORS origins; empty disables browser access.")
	flag.Parse()

	s := mcp.New()
	api := mcp.NewMediaAPI(cfg)
	api.RegisterTools(s)

	log.SetOutput(os.Stderr)
	log.Printf("ai-hub MCP server (transport=%s)", map[bool]string{true: "http", false: "stdio"}[*httpAddr != ""])
	log.Printf("  comfyui: %s", cfg.ComfyUI)
	log.Printf("  voice:   %s", cfg.Voice)
	log.Printf("  music:   %s", cfg.Music)
	log.Printf("  threed:  %s", cfg.Threed)
	log.Printf("  outdir:  %s", cfg.OutDir)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	if *httpAddr != "" {
		panelConfig, err := config.Load()
		if err != nil {
			log.Fatalf("config: %v", err)
		}
		if _, err := auth.EnsureToken(panelConfig.TokenPath, panelConfig.Token); err != nil {
			log.Fatalf("token: %v", err)
		}
		store := auth.NewStoreWithToken(panelConfig.TokenPath, panelConfig.Token)
		log.Printf("Endpoint: POST %s/mcp", trimHost(*httpAddr))
		allow := parseCorsOrigins(*corsOrigins)
		httpServer := &http.Server{
			Addr:              *httpAddr,
			Handler:           protectHTTP(s.Handler(), store, allow),
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       120 * time.Second,
		}
		errc := make(chan error, 1)
		go func() { errc <- httpServer.ListenAndServe() }()
		select {
		case <-sig:
			log.Println("Shutting down...")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = httpServer.Shutdown(ctx)
		case err := <-errc:
			if !errors.Is(err, http.ErrServerClosed) {
				log.Fatalf("HTTP server error: %v", err)
			}
		}
	} else {
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		s.ServeStdio(ctx)
	}

	fmt.Fprintln(os.Stderr, "ai-hub MCP server stopped.")
}

func trimHost(addr string) string {
	if len(addr) > 0 && addr[0] == ':' {
		return "http://127.0.0.1" + addr
	}
	return "http://" + addr
}

// parseCorsOrigins splits a comma-separated allowlist into trimmed entries.
func parseCorsOrigins(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func protectHTTP(next http.Handler, store *auth.Store, allow []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Add("Vary", "Origin")
		if origin := r.Header.Get("Origin"); origin != "" {
			matched := matchOrigin(allow, origin)
			if matched == "" {
				http.Error(w, "Origin is not allowed", http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", matched)
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, MCP-Protocol-Version, Mcp-Session-Id")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.URL.Path != "/health" {
			parts := strings.Fields(r.Header.Get("Authorization"))
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !store.CheckToken(parts[1]) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="ai-hub-mcp"`)
				http.Error(w, "A valid ai-hub bearer token is required", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// matchOrigin returns the allowlist entry that matches origin (reflecting it
// in Access-Control-Allow-Origin), or "" if the origin is not allowed.
// "*" allows any origin; a trailing "*" is a suffix wildcard
// (e.g. http://192.0.2.* matches http://192.0.2.10:8080). An empty
// Origin (non-browser client) is always allowed.
func matchOrigin(allow []string, origin string) string {
	if origin == "" {
		return "*"
	}
	for _, a := range allow {
		if a == "*" {
			return "*"
		}
		if strings.HasSuffix(a, "*") {
			if strings.HasPrefix(origin, strings.TrimSuffix(a, "*")) {
				return origin
			}
			continue
		}
		if a == origin {
			return origin
		}
	}
	return ""
}
