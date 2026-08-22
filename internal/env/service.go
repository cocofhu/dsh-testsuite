// Package env manages dsh environment lifecycle on top of the docker driver.
package env

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/cocofhu/dsh-testsuite/internal/config"
	"github.com/cocofhu/dsh-testsuite/internal/docker"
	"github.com/cocofhu/dsh-testsuite/internal/settings"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

const (
	StatusCreating = "creating"
	StatusRunning  = "running"
	StatusStopped  = "stopped"
	StatusError    = "error"

	// RenewDuration is how far one click in the UI pushes DestroyAt.
	RenewDuration = 6 * time.Hour
)

// ErrNotFound is returned when an environment id is unknown.
var ErrNotFound = errors.New("environment not found")

// ErrNotConfigured is returned when the dsh version is not in the image catalog.
var ErrNotConfigured = errors.New("image version not configured")

// ErrImageMissing is returned when the catalog ref is not present in docker.
var ErrImageMissing = errors.New("runtime image not present")

// ErrConflict is returned when maxEnvs would be exceeded.
var ErrConflict = errors.New("too many live environments")

var (
	versionRe  = regexp.MustCompile(`^[a-zA-Z0-9._+-]+$`)
	providerRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
)

// CreateRequest is the public create payload.
type CreateRequest struct {
	Name       string   `json:"name"`
	DSHVersion string   `json:"dshVersion"`
	PresetID   string   `json:"presetId,omitempty"`
	APIKey     string   `json:"apiKey"`
	Provider   string   `json:"provider"`
	Model      string   `json:"model"`
	BaseURL    string   `json:"baseURL"`
	API        string   `json:"api"`
	Plugins    []string `json:"plugins"`
}

// PresetInput is the create/update payload for a model preset (includes secret).
type PresetInput struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"baseURL,omitempty"`
	API      string `json:"api,omitempty"`
	APIKey   string `json:"apiKey"`
}

// Service is the environment control plane.
type Service struct {
	cfg   *config.Config
	drv   Runtime
	store *Store
	log   zerolog.Logger
	data  string
}

// NewService wires store + driver. dataDir holds environments.json and per-env files.
func NewService(cfg *config.Config, drv Runtime, dataDir string, log zerolog.Logger) (*Service, error) {
	if !filepath.IsAbs(dataDir) {
		abs, err := filepath.Abs(dataDir)
		if err != nil {
			return nil, err
		}
		dataDir = abs
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	store, err := OpenStore(dataDir)
	if err != nil {
		return nil, err
	}
	return &Service{
		cfg:   cfg,
		drv:   drv,
		store: store,
		log:   log,
		data:  dataDir,
	}, nil
}

// Reconcile syncs JSON records with docker ps and GCs unlabeled-from-store orphans.
func (s *Service) Reconcile(ctx context.Context) {
	handles, err := s.drv.List(ctx)
	if err != nil {
		s.log.Warn().Err(err).Msg("reconcile: docker list failed")
		return
	}
	live := map[string]*docker.Handle{}
	for _, h := range handles {
		live[h.ID] = h
	}
	now := time.Now()
	for _, rec := range s.store.snapshot() {
		h, ok := live[rec.ID]
		if !ok {
			if rec.Status == StatusRunning || rec.Status == StatusCreating {
				rec.Status = StatusError
				rec.Error = "container missing"
				rec.HostPort = 0
				rec.OpenURL = ""
				rec.UpdatedAt = now
				_ = s.store.put(rec)
			}
			continue
		}
		delete(live, rec.ID)
		switch h.Status {
		case docker.StatusRunning:
			rec.Status = StatusRunning
			rec.Error = ""
			s.applyHandle(&rec, h)
		case docker.StatusStopped, docker.StatusNotFound:
			rec.Status = StatusStopped
			rec.HostPort = 0
			rec.OpenURL = ""
		case docker.StatusPending:
			rec.Status = StatusCreating
		}
		rec.UpdatedAt = now
		_ = s.store.put(rec)
	}
	for id, h := range live {
		s.log.Warn().Str("id", id).Str("container", h.Name).Msg("reconcile: destroying orphan container")
		_ = s.drv.Destroy(ctx, id)
	}
}

// List returns redacted environment views, refreshed from docker.
func (s *Service) List(ctx context.Context) []View {
	s.refresh(ctx)
	recs := s.store.snapshot()
	out := make([]View, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.ToView())
	}
	s.attachHealth(ctx, out, recs)
	return out
}

func (s *Service) refresh(ctx context.Context) {
	for _, rec := range s.store.snapshot() {
		h, err := s.drv.Get(ctx, rec.ID)
		if err != nil {
			continue
		}
		changed := false
		switch h.Status {
		case docker.StatusRunning:
			if rec.Status != StatusRunning {
				rec.Status = StatusRunning
				rec.Error = ""
				changed = true
			}
			if s.applyHandle(&rec, h) {
				changed = true
			}
		case docker.StatusNotFound:
			if rec.Status == StatusRunning || rec.Status == StatusCreating {
				rec.Status = StatusError
				rec.Error = "container missing"
				rec.HostPort = 0
				rec.OpenURL = ""
				changed = true
			}
		case docker.StatusStopped:
			if rec.Status != StatusStopped && rec.Status != StatusError {
				rec.Status = StatusStopped
				rec.HostPort = 0
				rec.OpenURL = ""
				changed = true
			}
		}
		if changed {
			rec.UpdatedAt = time.Now()
			_ = s.store.put(rec)
		}
	}
}

func (s *Service) applyHandle(rec *Record, h *docker.Handle) bool {
	if h == nil {
		return false
	}
	if u := strings.TrimSpace(h.OpenURL); u != "" {
		const port = 80
		if rec.HostPort == port && rec.OpenURL == u {
			return false
		}
		rec.HostPort = port
		rec.OpenURL = u
		return true
	}
	return s.applyEndpoints(rec, h.Endpoints)
}

func (s *Service) applyEndpoints(rec *Record, eps map[int]string) bool {
	port := docker.HostPortFromEndpoints(eps)
	url := ""
	if port > 0 {
		url = fmt.Sprintf("http://%s:%d/", s.cfg.PublicHost(), port)
	}
	if rec.HostPort == port && rec.OpenURL == url {
		return false
	}
	rec.HostPort = port
	rec.OpenURL = url
	return true
}

// DriverName is the active sandbox driver (docker or kubernetes).
func (s *Service) DriverName() string {
	if s == nil || s.drv == nil {
		return "docker"
	}
	return s.drv.Name()
}

// Get returns one environment.
func (s *Service) Get(ctx context.Context, id string) (View, error) {
	s.refresh(ctx)
	rec, ok := s.store.get(id)
	if !ok {
		return View{}, ErrNotFound
	}
	return s.withHealth(ctx, *rec), nil
}

// Create provisions a container from a pre-built version image.
func (s *Service) Create(ctx context.Context, req CreateRequest) (View, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.DSHVersion = strings.TrimSpace(req.DSHVersion)
	req.PresetID = strings.TrimSpace(req.PresetID)
	req.APIKey = strings.TrimSpace(req.APIKey)
	req.Provider = strings.TrimSpace(req.Provider)
	req.Model = strings.TrimSpace(req.Model)
	req.BaseURL = strings.TrimSpace(req.BaseURL)
	req.API = strings.TrimSpace(req.API)
	plugins := cleanPlugins(req.Plugins)

	if req.PresetID != "" {
		p, ok := s.store.getPreset(req.PresetID)
		if !ok {
			return View{}, fmt.Errorf("%w: preset %s", ErrNotFound, req.PresetID)
		}
		req.Provider = p.Provider
		req.Model = p.Model
		req.BaseURL = p.BaseURL
		req.API = p.API
		req.APIKey = p.APIKey
	}

	if req.Name == "" {
		return View{}, fmt.Errorf("name is required")
	}
	if err := validateVersion(req.DSHVersion); err != nil {
		return View{}, err
	}
	if req.APIKey == "" {
		return View{}, fmt.Errorf("apiKey is required")
	}
	if strings.ContainsAny(req.APIKey, "\n\r") {
		return View{}, fmt.Errorf("apiKey must be a single line")
	}
	if !providerRe.MatchString(req.Provider) {
		return View{}, fmt.Errorf("provider must be a lowercase id")
	}
	if req.Model == "" {
		return View{}, fmt.Errorf("model is required")
	}

	yamlBody, err := settings.Render(settings.Input{
		Provider: req.Provider,
		Model:    req.Model,
		BaseURL:  req.BaseURL,
		API:      req.API,
	})
	if err != nil {
		return View{}, err
	}

	if s.store.liveCount() >= s.cfg.Limits.MaxEnvs {
		return View{}, fmt.Errorf("%w: max %d", ErrConflict, s.cfg.Limits.MaxEnvs)
	}

	img, configured := s.store.getImage(req.DSHVersion)
	if !configured {
		return View{}, fmt.Errorf("%w: %s (add it under 镜像版本 first)", ErrNotConfigured, req.DSHVersion)
	}
	present, err := s.drv.ImageExists(ctx, img.Ref)
	if err != nil {
		return View{}, err
	}
	if !present {
		return View{}, fmt.Errorf("%w: %s", ErrImageMissing, img.Ref)
	}
	ref := img.Ref

	id := uuid.NewString()[:8]
	now := time.Now()
	destroyAt := now.Add(s.cfg.Limits.IdleTTL.D())
	rec := Record{
		ID:         id,
		Name:       req.Name,
		DSHVersion: req.DSHVersion,
		Provider:   req.Provider,
		Model:      req.Model,
		BaseURL:    req.BaseURL,
		API:        req.API,
		Plugins:    plugins,
		Status:     StatusCreating,
		Container:  docker.NamePrefix + id,
		APIKey:     req.APIKey,
		CreatedAt:  now,
		UpdatedAt:  now,
		DestroyAt:  &destroyAt,
	}
	if err := s.store.put(rec); err != nil {
		return View{}, err
	}

	home := s.envDir(id)
	bootstrap := filepath.Join(home, "bootstrap")
	dshHome := filepath.Join(home, "home")
	workspace := filepath.Join(home, "workspace")
	for _, dir := range []string{bootstrap, dshHome, workspace} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			s.fail(&rec, err)
			return View{}, err
		}
	}
	if err := os.WriteFile(filepath.Join(bootstrap, "settings.yaml"), []byte(yamlBody), 0o600); err != nil {
		s.fail(&rec, err)
		return View{}, err
	}
	if err := os.WriteFile(filepath.Join(bootstrap, "plugins.txt"), []byte(settings.PluginsFile(plugins)), 0o644); err != nil {
		s.fail(&rec, err)
		return View{}, err
	}

	h, err := s.drv.Create(ctx, s.specFor(rec, ref))
	if err != nil {
		s.fail(&rec, err)
		return View{}, err
	}
	rec.Status = StatusRunning
	rec.Error = ""
	rec.Container = h.Name
	s.applyHandle(&rec, h)
	rec.UpdatedAt = time.Now()
	if err := s.store.put(rec); err != nil {
		return View{}, err
	}
	return s.withHealth(ctx, rec), nil
}

func (s *Service) fail(rec *Record, err error) {
	rec.Status = StatusError
	rec.Error = err.Error()
	rec.UpdatedAt = time.Now()
	_ = s.store.put(*rec)
}

func (s *Service) specFor(rec Record, ref string) docker.Spec {
	home := s.envDir(rec.ID)
	return docker.Spec{
		ID:    rec.ID,
		Image: ref,
		Env: map[string]string{
			"DSH_HOME":               "/data/dsh",
			"DSH_API_KEY":            rec.APIKey,
			"DEEPSEEK_API_KEY":       rec.APIKey,
			"DSH_PROVIDER":           rec.Provider,
			"DSH_MODEL":              rec.Model,
			"DSH_BASE_URL":           rec.BaseURL,
			"DSH_API":                rec.API,
			"DSH_TRUSTED_HOST":       s.cfg.EnvHost(rec.ID),
			"DSH_TELEMETRY_DISABLED": "1",
		},
		Mounts: []string{
			filepath.Join(home, "bootstrap") + ":/bootstrap:ro",
			filepath.Join(home, "home") + ":/data/dsh",
			filepath.Join(home, "workspace") + ":/workspace",
		},
	}
}

// Start resumes a stopped environment.
func (s *Service) Start(ctx context.Context, id string) (View, error) {
	rec, ok := s.store.get(id)
	if !ok {
		return View{}, ErrNotFound
	}
	if rec.Status == StatusRunning {
		if h, err := s.drv.Get(ctx, id); err == nil && h.Status == docker.StatusRunning {
			return s.withHealth(ctx, *rec), nil
		}
	}
	if s.store.liveCount() >= s.cfg.Limits.MaxEnvs && rec.Status != StatusCreating && rec.Status != StatusRunning {
		// a stopped env starting counts as a new live one
		live := 0
		for _, r := range s.store.snapshot() {
			if r.ID != id && (r.Status == StatusRunning || r.Status == StatusCreating) {
				live++
			}
		}
		if live >= s.cfg.Limits.MaxEnvs {
			return View{}, fmt.Errorf("%w: max %d", ErrConflict, s.cfg.Limits.MaxEnvs)
		}
	}
	img, configured := s.store.getImage(rec.DSHVersion)
	if !configured {
		return View{}, fmt.Errorf("%w: %s", ErrNotConfigured, rec.DSHVersion)
	}
	present, err := s.drv.ImageExists(ctx, img.Ref)
	if err != nil {
		return View{}, err
	}
	if !present {
		return View{}, fmt.Errorf("%w: %s", ErrImageMissing, img.Ref)
	}
	// Recreate the workload so Start picks up a rebuilt runtime image.
	// Kubernetes keeps the env PVC (deleted only by Destroy).
	h, err := s.drv.Recreate(ctx, s.specFor(*rec, img.Ref))
	if err != nil {
		s.fail(rec, err)
		return View{}, err
	}
	destroyAt := time.Now().Add(s.cfg.Limits.IdleTTL.D())
	rec.DestroyAt = &destroyAt
	rec.Status = StatusRunning
	rec.Error = ""
	rec.Container = h.Name
	rec.UpdatedAt = time.Now()
	s.applyHandle(rec, h)
	if err := s.store.put(*rec); err != nil {
		return View{}, err
	}
	return s.withHealth(ctx, *rec), nil
}

// Renew extends DestroyAt by RenewDuration from the later of now and the current deadline.
func (s *Service) Renew(ctx context.Context, id string) (View, error) {
	rec, ok := s.store.get(id)
	if !ok {
		return View{}, ErrNotFound
	}
	now := time.Now()
	base := now
	if rec.DestroyAt != nil && rec.DestroyAt.After(now) {
		base = *rec.DestroyAt
	}
	destroyAt := base.Add(RenewDuration)
	rec.DestroyAt = &destroyAt
	rec.UpdatedAt = now
	if err := s.store.put(*rec); err != nil {
		return View{}, err
	}
	return s.withHealth(ctx, *rec), nil
}

// Restart stops a running environment then starts it again (recreates the container).
func (s *Service) Restart(ctx context.Context, id string) (View, error) {
	rec, ok := s.store.get(id)
	if !ok {
		return View{}, ErrNotFound
	}
	if rec.Status == StatusRunning {
		if _, err := s.Stop(ctx, id); err != nil {
			return View{}, err
		}
	}
	return s.Start(ctx, id)
}

// Stop keeps the container.
func (s *Service) Stop(ctx context.Context, id string) (View, error) {
	rec, ok := s.store.get(id)
	if !ok {
		return View{}, ErrNotFound
	}
	if err := s.drv.Stop(ctx, id); err != nil {
		return View{}, err
	}
	rec.Status = StatusStopped
	rec.HostPort = 0
	rec.OpenURL = ""
	rec.UpdatedAt = time.Now()
	if err := s.store.put(*rec); err != nil {
		return View{}, err
	}
	return s.withHealth(ctx, *rec), nil
}

// Destroy removes the container, volumes, and record.
func (s *Service) Destroy(ctx context.Context, id string) error {
	if _, ok := s.store.get(id); !ok {
		return ErrNotFound
	}
	if err := s.drv.Destroy(ctx, id); err != nil {
		return err
	}
	_ = os.RemoveAll(s.envDir(id))
	return s.store.delete(id)
}

// Logs returns container logs.
func (s *Service) Logs(ctx context.Context, id string, tail int) (string, error) {
	if _, ok := s.store.get(id); !ok {
		return "", ErrNotFound
	}
	return s.drv.Logs(ctx, id, tail)
}

// SweepIdle destroys environments past DestroyAt.
func (s *Service) SweepIdle(ctx context.Context) {
	now := time.Now()
	for _, rec := range s.store.snapshot() {
		if rec.DestroyAt == nil || rec.DestroyAt.After(now) {
			continue
		}
		s.log.Info().Str("id", rec.ID).Msg("idle ttl reached, destroying")
		if err := s.Destroy(ctx, rec.ID); err != nil {
			s.log.Warn().Err(err).Str("id", rec.ID).Msg("idle destroy failed")
		}
	}
}

// ListImages returns the configured catalog, with a present flag from docker.
func (s *Service) ListImages(ctx context.Context) ([]ImageView, error) {
	cfgs := s.store.imageSnapshot()
	out := make([]ImageView, 0, len(cfgs))
	for _, img := range cfgs {
		view := ImageView{Version: img.Version, Ref: img.Ref}
		ok, err := s.drv.ImageExists(ctx, img.Ref)
		if err != nil {
			view.Error = err.Error()
		} else {
			view.Present = ok
		}
		out = append(out, view)
	}
	return out, nil
}

// ListPresets returns redacted model presets.
func (s *Service) ListPresets() []PresetView {
	recs := s.store.presetSnapshot()
	out := make([]PresetView, 0, len(recs))
	for _, p := range recs {
		out = append(out, p.ToView())
	}
	return out
}

func (s *Service) normalizePreset(in PresetInput, requireKey bool) (PresetInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Provider = strings.TrimSpace(in.Provider)
	in.Model = strings.TrimSpace(in.Model)
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	in.API = strings.TrimSpace(in.API)
	in.APIKey = strings.TrimSpace(in.APIKey)
	if in.Name == "" {
		return in, fmt.Errorf("name is required")
	}
	if !providerRe.MatchString(in.Provider) {
		return in, fmt.Errorf("provider must be a lowercase id")
	}
	if in.Model == "" {
		return in, fmt.Errorf("model is required")
	}
	if requireKey && in.APIKey == "" {
		return in, fmt.Errorf("apiKey is required")
	}
	if in.APIKey != "" && strings.ContainsAny(in.APIKey, "\n\r") {
		return in, fmt.Errorf("apiKey must be a single line")
	}
	if _, err := settings.Render(settings.Input{
		Provider: in.Provider,
		Model:    in.Model,
		BaseURL:  in.BaseURL,
		API:      in.API,
	}); err != nil {
		return in, err
	}
	return in, nil
}

// CreatePreset stores a named provider+model+secret.
func (s *Service) CreatePreset(in PresetInput) (PresetView, error) {
	in, err := s.normalizePreset(in, true)
	if err != nil {
		return PresetView{}, err
	}
	now := time.Now()
	p := Preset{
		ID:        uuid.NewString()[:8],
		Name:      in.Name,
		Provider:  in.Provider,
		Model:     in.Model,
		BaseURL:   in.BaseURL,
		API:       in.API,
		APIKey:    in.APIKey,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.putPreset(p); err != nil {
		return PresetView{}, err
	}
	return p.ToView(), nil
}

// UpdatePreset changes a preset. Empty apiKey keeps the stored secret.
func (s *Service) UpdatePreset(id string, in PresetInput) (PresetView, error) {
	id = strings.TrimSpace(id)
	cur, ok := s.store.getPreset(id)
	if !ok {
		return PresetView{}, ErrNotFound
	}
	in, err := s.normalizePreset(in, false)
	if err != nil {
		return PresetView{}, err
	}
	cur.Name = in.Name
	cur.Provider = in.Provider
	cur.Model = in.Model
	cur.BaseURL = in.BaseURL
	cur.API = in.API
	if in.APIKey != "" {
		cur.APIKey = in.APIKey
	}
	cur.UpdatedAt = time.Now()
	if err := s.store.putPreset(*cur); err != nil {
		return PresetView{}, err
	}
	return cur.ToView(), nil
}

// DeletePreset removes a saved preset. Running environments are not touched.
func (s *Service) DeletePreset(id string) error {
	id = strings.TrimSpace(id)
	if _, ok := s.store.getPreset(id); !ok {
		return ErrNotFound
	}
	return s.store.deletePreset(id)
}

// ImageView is one catalog row plus whether docker has the ref locally.
type ImageView struct {
	Version string `json:"version"`
	Ref     string `json:"ref"`
	Present bool   `json:"present"`
	Error   string `json:"error,omitempty"`
}

// UpsertImage registers or updates a version → docker image mapping without pulling.
func (s *Service) UpsertImage(req ImageConfig) (ImageView, error) {
	return s.RegisterImage(context.Background(), req, false)
}

// RegisterImage writes a catalog row. When pull is true and the ref is not
// already on the host, it docker-pulls (GHCR for short local names) and tags
// the result to req.Ref.
func (s *Service) RegisterImage(ctx context.Context, req ImageConfig, pull bool) (ImageView, error) {
	req.Version = strings.TrimSpace(req.Version)
	req.Ref = strings.TrimSpace(req.Ref)
	if err := validateVersion(req.Version); err != nil {
		return ImageView{}, err
	}
	if req.Ref == "" {
		req.Ref = s.cfg.ImageRef(req.Version)
	}
	if err := validateRef(req.Ref); err != nil {
		return ImageView{}, err
	}
	if pull {
		if err := s.ensureImage(ctx, req); err != nil {
			return ImageView{}, err
		}
	}
	if err := s.store.putImage(req); err != nil {
		return ImageView{}, err
	}
	view := ImageView{Version: req.Version, Ref: req.Ref}
	ok, err := s.drv.ImageExists(ctx, req.Ref)
	if err != nil {
		view.Error = err.Error()
	} else {
		view.Present = ok
	}
	return view, nil
}

func (s *Service) ensureImage(ctx context.Context, req ImageConfig) error {
	present, err := s.drv.ImageExists(ctx, req.Ref)
	if err != nil {
		return err
	}
	if present {
		return nil
	}
	src := s.pullSource(req)
	if err := s.drv.ImagePull(ctx, src); err != nil {
		return err
	}
	if src == req.Ref {
		return nil
	}
	return s.drv.ImageTag(ctx, src, req.Ref)
}

func (s *Service) pullSource(req ImageConfig) string {
	if strings.Contains(req.Ref, "/") {
		return req.Ref
	}
	if req.Ref == s.cfg.ImageRef(req.Version) {
		return PublicRuntimeRepo + ":" + req.Version
	}
	return req.Ref
}

// DeleteImage removes a catalog entry. It does not delete the docker image.
func (s *Service) DeleteImage(version string) error {
	version = strings.TrimSpace(version)
	if _, ok := s.store.getImage(version); !ok {
		return ErrNotConfigured
	}
	return s.store.deleteImage(version)
}

func (s *Service) envDir(id string) string {
	return filepath.Join(s.data, "envs", id)
}

func validateVersion(v string) error {
	if v == "" {
		return fmt.Errorf("dshVersion is required")
	}
	if !versionRe.MatchString(v) {
		return fmt.Errorf("dshVersion %q is not a valid image tag", v)
	}
	return nil
}

func validateRef(ref string) error {
	if ref == "" {
		return fmt.Errorf("image ref is required")
	}
	if strings.ContainsAny(ref, " \t\n") {
		return fmt.Errorf("image ref must not contain whitespace")
	}
	return nil
}

func cleanPlugins(in []string) []string {
	var out []string
	for _, p := range in {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
