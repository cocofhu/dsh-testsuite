// Command dsh-testsuite is the Docker control plane for DeepSeek Harness
// environments: bake versioned runtime images, then create/start/stop containers
// with API key, provider, model, and preinstalled plugins.
package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/cocofhu/dsh-testsuite/internal/config"
	"github.com/cocofhu/dsh-testsuite/internal/docker"
	"github.com/cocofhu/dsh-testsuite/internal/env"
	"github.com/cocofhu/dsh-testsuite/internal/httpapi"
	"github.com/cocofhu/dsh-testsuite/internal/k8s"
	"github.com/rs/zerolog"
)

var version = "dev"

func main() {
	configPath := flag.String("config", envOr("DSHTS_CONFIG", ""), "config.yaml, or a directory containing it")
	listen := flag.String("listen", envOr("DSHTS_LISTEN", ""), "override server.listen")
	logLevel := flag.String("log-level", envOr("LOG_LEVEL", "info"), "trace|debug|info|warn|error")
	flag.Parse()

	log := newLogger(*logLevel)
	if err := run(*configPath, *listen, log); err != nil {
		log.Fatal().Err(err).Msg("dsh-testsuite stopped")
	}
}

func run(configPath, listen string, log zerolog.Logger) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if listen != "" {
		cfg.Server.Listen = listen
	}
	if v := envOr("DSHTS_PUBLIC_HOST", ""); v != "" {
		cfg.Server.PublicHost = v
	}
	if v := envOr("DSHTS_BIND_IP", ""); v != "" {
		cfg.Docker.BindIP = v
	}
	if v := envOr("DSHTS_RUNTIME", ""); v != "" {
		cfg.Runtime = v
	}

	drv, err := newRuntime(cfg)
	if err != nil {
		return err
	}

	dataDir := envOr("DSHTS_DATA", "data")
	if !filepath.IsAbs(dataDir) {
		abs, err := filepath.Abs(dataDir)
		if err != nil {
			return err
		}
		dataDir = abs
	}
	svc, err := env.NewService(cfg, drv, dataDir, log)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	svc.Reconcile(ctx)
	go sweepLoop(ctx, svc)

	srv := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           httpapi.New(svc, cfg.Server.WebDir, log).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Info().
		Str("version", version).
		Str("listen", cfg.Server.Listen).
		Str("publicHost", cfg.PublicHost()).
		Str("runtime", drv.Name()).
		Str("bindIP", cfg.Docker.BindIP).
		Str("image", cfg.Docker.ImageRepository).
		Str("web", cfg.Server.WebDir).
		Msg("dsh-testsuite ready")

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shut)
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func sweepLoop(ctx context.Context, svc *env.Service) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			svc.SweepIdle(ctx)
		}
	}
}

func newRuntime(cfg *config.Config) (env.Runtime, error) {
	if cfg.IsKubernetes() {
		return k8s.New(k8s.Options{
			Namespace:       cfg.Kubernetes.Namespace,
			Kubeconfig:      cfg.Kubernetes.Kubeconfig,
			EnvHostTemplate: cfg.Kubernetes.EnvHostTemplate,
			IngressClass:    cfg.Kubernetes.IngressClass,
			ImagePullPolicy: cfg.Kubernetes.ImagePullPolicy,
			NamePrefix:      docker.NamePrefix,
			CPUCores:        cfg.Docker.CPUCores,
			MemoryMB:        cfg.Docker.MemoryMB,
		})
	}
	return docker.New(docker.Options{
		BindIP:     cfg.Docker.BindIP,
		Network:    cfg.Docker.Network,
		CPUCores:   cfg.Docker.CPUCores,
		MemoryMB:   cfg.Docker.MemoryMB,
		NamePrefix: docker.NamePrefix,
	}), nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func newLogger(level string) zerolog.Logger {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}
	zerolog.TimeFieldFormat = time.RFC3339
	return zerolog.New(os.Stderr).Level(lvl).With().Timestamp().Logger()
}
