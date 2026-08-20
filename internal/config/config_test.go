package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Server.Listen != ":8090" {
		t.Fatalf("listen=%q", cfg.Server.Listen)
	}
	if cfg.Docker.ImageRepository != "dsh-testsuite-runtime" {
		t.Fatalf("repo=%q", cfg.Docker.ImageRepository)
	}
	if cfg.Limits.IdleTTL.D() != 2*time.Hour {
		t.Fatalf("ttl=%s", cfg.Limits.IdleTTL.D())
	}
}

func TestLoadEmptyPath(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(cfg.Server.WebDir) {
		t.Fatalf("webDir should be absolute, got %q", cfg.Server.WebDir)
	}
}

func TestLoadFileAndPublicHost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	raw := []byte(`
server:
  listen: ":9000"
  publicHost: "env.example"
docker:
  bindIP: "0.0.0.0"
  imageRepository: "my-runtime"
limits:
  maxEnvs: 3
  idleTTL: 15m
`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Listen != ":9000" {
		t.Fatalf("listen=%q", cfg.Server.Listen)
	}
	if cfg.PublicHost() != "env.example" {
		t.Fatalf("publicHost=%q", cfg.PublicHost())
	}
	if cfg.ImageRef("0.1.0-rc.7") != "my-runtime:0.1.0-rc.7" {
		t.Fatalf("ref=%q", cfg.ImageRef("0.1.0-rc.7"))
	}
	if cfg.Limits.MaxEnvs != 3 || cfg.Limits.IdleTTL.D() != 15*time.Minute {
		t.Fatalf("limits=%+v", cfg.Limits)
	}
}

func TestPublicHostLoopback(t *testing.T) {
	cfg := Default()
	cfg.Docker.BindIP = "0.0.0.0"
	if got := cfg.PublicHost(); got != "127.0.0.1" {
		t.Fatalf("got %q", got)
	}
}

func TestValidateRejectsWhitespaceRepo(t *testing.T) {
	cfg := Default()
	cfg.Docker.ImageRepository = "bad repo"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error")
	}
}
