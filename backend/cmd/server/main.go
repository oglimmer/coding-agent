// Command server is the coding-agent backend: OIDC-authenticated API that gates
// feature requests through DeepSeek and spawns Kubernetes worker Jobs.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/oglimmer/coding-agent/backend/internal/buildinfo"
	"github.com/oglimmer/coding-agent/backend/internal/config"
	"github.com/oglimmer/coding-agent/backend/internal/db"
	"github.com/oglimmer/coding-agent/backend/internal/deepseek"
	"github.com/oglimmer/coding-agent/backend/internal/k8s"
	"github.com/oglimmer/coding-agent/backend/internal/server"
)

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	log.Printf("INFO starting coding-agent backend version=%s commit=%s time=%s",
		buildinfo.Version, buildinfo.Commit, buildinfo.Time)

	cfg := config.Load()

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(rootCtx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("FATAL database: %v", err)
	}
	defer pool.Close()

	if err := db.Migrate(rootCtx, pool); err != nil {
		log.Fatalf("FATAL migrations: %v", err)
	}

	ds := deepseek.New(cfg.DeepSeekAPIKey, cfg.DeepSeekBaseURL, cfg.DeepSeekModel)

	// The k8s client is optional: without a cluster the API still serves, but
	// job creation returns 503.
	var kc *k8s.Client
	kc, err = k8s.New(k8s.Options{
		Image:             cfg.WorkerImage,
		ImagePullPolicy:   cfg.WorkerImagePullPolicy,
		Model:             cfg.WorkerModel,
		EditorModel:       cfg.WorkerEditorModel,
		ClaudeImage:       cfg.WorkerClaudeImage,
		ClaudeModel:       cfg.WorkerClaudeModel,
		ClaudeTimeoutSec:  cfg.ClaudeTimeoutSec,
		DeepSeekBaseURL:   cfg.DeepSeekBaseURL,
		Namespace:         cfg.WorkerNamespace,
		SecretName:        cfg.WorkerSecretName,
		ImagePullSecret:   cfg.WorkerImagePullSecret,
		ServiceAccount:    cfg.WorkerServiceAccount,
		GitHubBotLogin:    cfg.GitHubBotLogin,
		ReviewMaxRounds:   cfg.ReviewMaxRounds,
		ActiveDeadlineSec: int64(cfg.JobTimeout.Seconds()),
		AiderTimeoutSec:   cfg.AiderTimeoutSec,
		CPURequest:        cfg.WorkerCPURequest,
		CPULimit:          cfg.WorkerCPULimit,
		MemoryRequest:     cfg.WorkerMemoryRequest,
		MemoryLimit:       cfg.WorkerMemoryLimit,
	})
	if err != nil {
		log.Printf("WARN k8s: cluster not available, worker jobs disabled: %v", err)
		kc = nil
	} else {
		log.Printf("INFO k8s: worker jobs run in namespace %q", kc.Namespace())
	}

	oidcRT, err := server.InitOIDC(rootCtx, cfg)
	if err != nil {
		log.Fatalf("FATAL oidc: %v", err)
	}
	if oidcRT != nil {
		log.Printf("INFO oidc: provider %s ready", cfg.OIDCIssuer)
	}

	app := server.NewApp(cfg, pool, ds, kc, oidcRT)

	var wg sync.WaitGroup
	app.StartWatcher(rootCtx, &wg)

	app.MarkReady()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.NewRouter(app),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("INFO http: listening on %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("FATAL http: %v", err)
		}
	}()

	<-rootCtx.Done()
	log.Printf("INFO shutting down")
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("WARN http shutdown: %v", err)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		log.Printf("WARN background workers did not stop in time")
	}
	log.Printf("INFO stopped")
}
