// Command aihub is the ai-hub model management panel (Go port of app.py).
//
// It is a drop-in replacement for the Python FastAPI panel: same HTTP API,
// same JSON shapes, same static UI, same pollers. See plan.md for the exact
// contract.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"aihub/internal/actions"
	"aihub/internal/auth"
	"aihub/internal/buildinfo"
	"aihub/internal/config"
	"aihub/internal/metrics"
	"aihub/internal/models"
	"aihub/internal/registry"
	"aihub/internal/server"
	"aihub/internal/status"
)

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Println(buildinfo.String("aihub"))
		return
	}
	log.SetPrefix("aihub: ")
	log.SetOutput(os.Stderr)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Resolve the configured root once for registry, static files and token.
	exeDir := cfg.Root
	aihubRoot := cfg.Root
	public, err := cfg.Public()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	reg, err := registry.Load(aihubRoot, exeDir)
	if err != nil {
		if os.Getenv("AIHUB_REGISTRY") != "" {
			log.Fatalf("registry: %v", err)
		}
		log.Printf("registry: %v (starting with empty registry)", err)
		reg = &registry.File{}
	}
	if err := models.ConfigureCatalog(aihubRoot); err != nil {
		log.Fatalf("catalog: %v", err)
	}

	// Token file lives at AIHUB/token (mirrors app.py TOKEN_FILE).
	tokenFile := cfg.TokenPath
	created, err := auth.EnsureToken(tokenFile, cfg.Token)
	if err != nil {
		log.Fatalf("token: %v", err)
	}
	if created {
		log.Printf("created a private login token in %s; use it to sign in", tokenFile)
	}
	store := auth.NewStoreWithToken(tokenFile, cfg.Token)

	poller := status.NewPoller(reg)
	// Stats source: running+healthy models from the status cache
	// (mirrors app.py _stats_poll_loop reading _cache["models"]).
	stats := metrics.NewStatsPoller(func() []metrics.ModelRef {
		snap := poller.Cache().Snapshot()
		models, _ := snap["models"].([]status.ModelStatusEntry)
		out := []metrics.ModelRef{}
		for _, m := range models {
			if m.Running && m.Healthy && m.Port != 0 {
				out = append(out, metrics.ModelRef{ID: m.ID, Port: m.Port, Backend: m.Backend})
			}
		}
		return out
	})

	srv := server.New(server.Deps{
		Reg:             reg,
		Store:           store,
		Actions:         &actions.Deps{Reg: reg},
		Poller:          poller,
		Stats:           stats,
		AIHUBRoot:       aihubRoot,
		ExeDir:          exeDir,
		Public:          public,
		RequireAPIToken: !cfg.AllowUnauthenticatedAPI,
		SecureCookies:   cfg.SecureCookies,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go poller.Run(ctx)
	go stats.Run(ctx)

	// Resume any model downloads left "downloading" (e.g. the box was rebooted
	// mid-download). `hf download` resumes from its cache, so this completes a
	// half-finished model. Best-effort; logs how many were (re)started.
	if n := models.Resume(); n > 0 {
		log.Printf("resumed %d pending model download(s)", n)
	}

	// Wait for first poller data (mirrors app.py startup wait, max 5s).
	if !poller.WaitReady(5 * time.Second) {
		log.Println("warning: first poll did not complete within 5s; serving anyway")
	}

	addr := cfg.Address()
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ErrorLog:          log.New(os.Stderr, "", 0),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		// WriteTimeout stays unset so long-running SSE streams remain usable.
	}

	go func() {
		<-ctx.Done()
		log.Println("shutting down")
		sc, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(sc)
	}()

	log.Printf("listening on %s root=%s registry=%d models token=%s", addr, aihubRoot, len(reg.Models), tokenFile)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server: %v", err)
	}
}
