package env

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	storeFile   = "environments.json"
	imagesFile  = "images.json"
	presetsFile = "presets.json"
)

// Record is the persisted environment, including the API key.
type Record struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	DSHVersion string     `json:"dshVersion"`
	Provider   string     `json:"provider"`
	Model      string     `json:"model"`
	BaseURL    string     `json:"baseURL,omitempty"`
	API        string     `json:"api,omitempty"`
	Plugins    []string   `json:"plugins,omitempty"`
	Status     string     `json:"status"`
	Container  string     `json:"container,omitempty"`
	HostPort   int        `json:"hostPort,omitempty"`
	OpenURL    string     `json:"openURL,omitempty"`
	APIKey     string     `json:"apiKey"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	DestroyAt  *time.Time `json:"destroyAt,omitempty"`
}

// View is the API shape: the key is reduced to a hint.
type View struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	DSHVersion string     `json:"dshVersion"`
	Provider   string     `json:"provider"`
	Model      string     `json:"model"`
	BaseURL    string     `json:"baseURL,omitempty"`
	API        string     `json:"api,omitempty"`
	Plugins    []string   `json:"plugins,omitempty"`
	Status     string     `json:"status"`
	Container  string     `json:"container,omitempty"`
	HostPort   int        `json:"hostPort,omitempty"`
	OpenURL    string     `json:"openURL,omitempty"`
	Health     string     `json:"health,omitempty"`
	APIKeyHint string     `json:"apiKeyHint"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	DestroyAt  *time.Time `json:"destroyAt,omitempty"`
}

// ToView redacts the API key.
func (r Record) ToView() View {
	plugins := r.Plugins
	if plugins == nil {
		plugins = []string{}
	}
	return View{
		ID:         r.ID,
		Name:       r.Name,
		DSHVersion: r.DSHVersion,
		Provider:   r.Provider,
		Model:      r.Model,
		BaseURL:    r.BaseURL,
		API:        r.API,
		Plugins:    plugins,
		Status:     r.Status,
		Container:  r.Container,
		HostPort:   r.HostPort,
		OpenURL:    r.OpenURL,
		APIKeyHint: hintKey(r.APIKey),
		Error:      r.Error,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
		DestroyAt:  r.DestroyAt,
	}
}

func hintKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 4 {
		return "****"
	}
	return "****" + key[len(key)-4:]
}

// Preset is a saved provider+model+secret used when creating environments.
type Preset struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	BaseURL   string    `json:"baseURL,omitempty"`
	API       string    `json:"api,omitempty"`
	APIKey    string    `json:"apiKey"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// PresetView is the API shape: the key is reduced to a hint.
type PresetView struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Provider   string    `json:"provider"`
	Model      string    `json:"model"`
	BaseURL    string    `json:"baseURL,omitempty"`
	API        string    `json:"api,omitempty"`
	APIKeyHint string    `json:"apiKeyHint"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// ToView redacts the API key.
func (p Preset) ToView() PresetView {
	return PresetView{
		ID:         p.ID,
		Name:       p.Name,
		Provider:   p.Provider,
		Model:      p.Model,
		BaseURL:    p.BaseURL,
		API:        p.API,
		APIKeyHint: hintKey(p.APIKey),
		CreatedAt:  p.CreatedAt,
		UpdatedAt:  p.UpdatedAt,
	}
}

type filePayload struct {
	Environments []Record `json:"environments"`
}

type imagesPayload struct {
	Images []ImageConfig `json:"images"`
}

type presetsPayload struct {
	Presets []Preset `json:"presets"`
}

// ImageConfig is one catalog entry: a dsh version label mapped to a docker image.
type ImageConfig struct {
	Version string `json:"version"`
	Ref     string `json:"ref"`
}

// Store is an atomic JSON file of environment records plus the image catalog.
type Store struct {
	path        string
	imagesPath  string
	presetsPath string
	mu          sync.Mutex
	byID        map[string]*Record
	byVersion   map[string]ImageConfig
	byPreset    map[string]*Preset
}

// OpenStore loads path/environments.json, creating an empty store if missing.
func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{
		path:        filepath.Join(dir, storeFile),
		imagesPath:  filepath.Join(dir, imagesFile),
		presetsPath: filepath.Join(dir, presetsFile),
		byID:        map[string]*Record{},
		byVersion:   map[string]ImageConfig{},
		byPreset:    map[string]*Preset{},
	}
	if err := s.loadEnvs(); err != nil {
		return nil, err
	}
	if err := s.loadImages(); err != nil {
		return nil, err
	}
	if err := s.loadPresets(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) loadEnvs() error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var p filePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("parse %s: %w", s.path, err)
	}
	for i := range p.Environments {
		r := p.Environments[i]
		cp := r
		s.byID[r.ID] = &cp
	}
	return nil
}

func (s *Store) loadImages() error {
	raw, err := os.ReadFile(s.imagesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var p imagesPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("parse %s: %w", s.imagesPath, err)
	}
	for _, img := range p.Images {
		if img.Version == "" {
			continue
		}
		s.byVersion[img.Version] = img
	}
	return nil
}

func (s *Store) loadPresets() error {
	raw, err := os.ReadFile(s.presetsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var p presetsPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return fmt.Errorf("parse %s: %w", s.presetsPath, err)
	}
	for i := range p.Presets {
		r := p.Presets[i]
		if r.ID == "" {
			continue
		}
		cp := r
		s.byPreset[r.ID] = &cp
	}
	return nil
}

func (s *Store) snapshot() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, 0, len(s.byID))
	for _, r := range s.byID {
		out = append(out, *r)
	}
	sortRecords(out)
	return out
}

func sortRecords(out []Record) {
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
}

func (s *Store) get(id string) (*Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.byID[id]
	if !ok {
		return nil, false
	}
	cp := *r
	return &cp, true
}

func (s *Store) put(r Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := r
	s.byID[r.ID] = &cp
	return s.flushLocked()
}

// patchRuntime updates status/address fields only. DestroyAt is never touched,
// so a slow refresh snapshot cannot roll back a concurrent Renew.
func (s *Store) patchRuntime(id, status, errMsg, container, openURL string, hostPort int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.byID[id]
	if !ok {
		return nil
	}
	cur.Status = status
	cur.Error = errMsg
	cur.Container = container
	cur.OpenURL = openURL
	cur.HostPort = hostPort
	cur.UpdatedAt = time.Now()
	return s.flushLocked()
}

func (s *Store) delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, id)
	return s.flushLocked()
}

func (s *Store) flushLocked() error {
	payload := filePayload{Environments: make([]Record, 0, len(s.byID))}
	for _, r := range s.byID {
		payload.Environments = append(payload.Environments, *r)
	}
	sortRecords(payload.Environments)
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) liveCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.byID {
		if r.Status == StatusCreating || r.Status == StatusRunning {
			n++
		}
	}
	return n
}

func (s *Store) imageSnapshot() []ImageConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ImageConfig, 0, len(s.byVersion))
	for _, img := range s.byVersion {
		out = append(out, img)
	}
	sortImages(out)
	return out
}

func sortImages(out []ImageConfig) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].Version != out[j].Version {
			return out[i].Version > out[j].Version
		}
		return out[i].Ref < out[j].Ref
	})
}

func (s *Store) getImage(version string) (ImageConfig, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	img, ok := s.byVersion[version]
	return img, ok
}

func (s *Store) putImage(img ImageConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byVersion[img.Version] = img
	return s.flushImagesLocked()
}

func (s *Store) deleteImage(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byVersion, version)
	return s.flushImagesLocked()
}

func (s *Store) flushImagesLocked() error {
	payload := imagesPayload{Images: make([]ImageConfig, 0, len(s.byVersion))}
	for _, img := range s.byVersion {
		payload.Images = append(payload.Images, img)
	}
	sortImages(payload.Images)
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.imagesPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.imagesPath)
}

func (s *Store) presetSnapshot() []Preset {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Preset, 0, len(s.byPreset))
	for _, p := range s.byPreset {
		out = append(out, *p)
	}
	sortPresets(out)
	return out
}

func sortPresets(out []Preset) {
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
}

func (s *Store) getPreset(id string) (*Preset, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.byPreset[id]
	if !ok {
		return nil, false
	}
	cp := *p
	return &cp, true
}

func (s *Store) putPreset(p Preset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := p
	s.byPreset[p.ID] = &cp
	return s.flushPresetsLocked()
}

func (s *Store) deletePreset(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byPreset, id)
	return s.flushPresetsLocked()
}

func (s *Store) flushPresetsLocked() error {
	payload := presetsPayload{Presets: make([]Preset, 0, len(s.byPreset))}
	for _, p := range s.byPreset {
		payload.Presets = append(payload.Presets, *p)
	}
	sortPresets(payload.Presets)
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.presetsPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.presetsPath)
}
