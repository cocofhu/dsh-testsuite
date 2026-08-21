package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cocofhu/dsh-testsuite/internal/config"
	"github.com/cocofhu/dsh-testsuite/internal/docker"
	"github.com/cocofhu/dsh-testsuite/internal/env"
	"github.com/rs/zerolog"
)

type apiFake struct {
	images  map[string]bool
	handles map[string]*docker.Handle
	pulled  []string
	tagged  []string
}

func (f *apiFake) Create(_ context.Context, spec docker.Spec) (*docker.Handle, error) {
	h := &docker.Handle{
		ID: spec.ID, Name: docker.NamePrefix + spec.ID, Status: docker.StatusRunning,
		Endpoints: map[int]string{docker.WebPort: "127.0.0.1:4300"},
	}
	f.handles[spec.ID] = h
	return h, nil
}
func (f *apiFake) Start(_ context.Context, id string) error {
	if h := f.handles[id]; h != nil {
		h.Status = docker.StatusRunning
		h.Endpoints = map[int]string{docker.WebPort: "127.0.0.1:4300"}
	}
	return nil
}
func (f *apiFake) Stop(_ context.Context, id string) error {
	if h := f.handles[id]; h != nil {
		h.Status = docker.StatusStopped
	}
	return nil
}
func (f *apiFake) Destroy(_ context.Context, id string) error {
	delete(f.handles, id)
	return nil
}
func (f *apiFake) Get(_ context.Context, id string) (*docker.Handle, error) {
	if h, ok := f.handles[id]; ok {
		return h, nil
	}
	return &docker.Handle{ID: id, Status: docker.StatusNotFound}, nil
}
func (f *apiFake) List(_ context.Context) ([]*docker.Handle, error) { return nil, nil }
func (f *apiFake) Endpoints(_ context.Context, id string) (map[int]string, error) {
	if h := f.handles[id]; h != nil {
		return h.Endpoints, nil
	}
	return nil, nil
}
func (f *apiFake) Logs(_ context.Context, _ string, _ int) (string, error) {
	return "line1", nil
}
func (f *apiFake) ImageExists(_ context.Context, ref string) (bool, error) {
	return f.images[ref], nil
}
func (f *apiFake) ImagePull(_ context.Context, ref string) error {
	f.pulled = append(f.pulled, ref)
	f.images[ref] = true
	return nil
}
func (f *apiFake) ImageTag(_ context.Context, src, dst string) error {
	f.tagged = append(f.tagged, src+" "+dst)
	f.images[dst] = f.images[src]
	return nil
}
func testAPI(t *testing.T) (*Server, *apiFake) {
	t.Helper()
	fake := &apiFake{
		images:  map[string]bool{"dsh-testsuite-runtime:0.1.0-rc.7": true},
		handles: map[string]*docker.Handle{},
	}
	cfg := config.Default()
	web := t.TempDir()
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("<html>ok</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc, err := env.NewService(cfg, fake, t.TempDir(), zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	return New(svc, web, zerolog.Nop()), fake
}

func do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		raw, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestHealthzAndUI(t *testing.T) {
	s, _ := testAPI(t)
	h := s.Handler()
	w := do(t, h, "GET", "/healthz", nil)
	if w.Code != 200 {
		t.Fatalf("healthz %d %s", w.Code, w.Body)
	}
	w = do(t, h, "GET", "/", nil)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("ok")) {
		t.Fatalf("ui %d %s", w.Code, w.Body)
	}
}

func TestEnvironmentCRUD(t *testing.T) {
	s, _ := testAPI(t)
	h := s.Handler()

	w := do(t, h, "POST", "/api/environments", env.CreateRequest{
		Name: "skillhub", DSHVersion: "0.1.0-rc.7", APIKey: "sk",
		Provider: "deepseek-official", Model: "m",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("unconfigured image: %d %s", w.Code, w.Body)
	}

	w = do(t, h, "POST", "/api/images", env.ImageConfig{Version: "0.1.0-rc.7"})
	if w.Code != 200 {
		t.Fatalf("catalog %d %s", w.Code, w.Body)
	}

	w = do(t, h, "POST", "/api/environments", env.CreateRequest{
		Name: "skillhub", DSHVersion: "0.1.0-rc.7", APIKey: "sk-secret-key",
		Provider: "deepseek-official", Model: "deepseek-v4-flash",
		Plugins: []string{"github:cocofhu/skillhub#main"},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	var created env.View
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.APIKeyHint != "****-key" || created.HostPort != 4300 {
		t.Fatalf("created=%+v", created)
	}

	w = do(t, h, "GET", "/api/environments", nil)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	w = do(t, h, "GET", "/api/environments/"+created.ID, nil)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	w = do(t, h, "GET", "/api/environments/"+created.ID+"/logs", nil)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("line1")) {
		t.Fatalf("logs %s", w.Body)
	}
	w = do(t, h, "POST", "/api/environments/"+created.ID+"/stop", nil)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	w = do(t, h, "POST", "/api/environments/"+created.ID+"/start", nil)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	w = do(t, h, "POST", "/api/environments/"+created.ID+"/restart", nil)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	w = do(t, h, "POST", "/api/environments/"+created.ID+"/renew", nil)
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	var renewed env.View
	if err := json.Unmarshal(w.Body.Bytes(), &renewed); err != nil {
		t.Fatal(err)
	}
	if renewed.DestroyAt == nil {
		t.Fatal("renew missing destroyAt")
	}
	w = do(t, h, "DELETE", "/api/environments/"+created.ID, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete %d", w.Code)
	}
	w = do(t, h, "GET", "/api/environments/"+created.ID, nil)
	if w.Code != 404 {
		t.Fatalf("get after delete %d", w.Code)
	}
}

func TestImagesAPI(t *testing.T) {
	s, _ := testAPI(t)
	h := s.Handler()
	w := do(t, h, "GET", "/api/providers", nil)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("deepseek-official")) || !bytes.Contains(w.Body.Bytes(), []byte("amazon-bedrock")) || !bytes.Contains(w.Body.Bytes(), []byte("deepseek-v4-flash")) {
		t.Fatalf("providers %d %s", w.Code, w.Body)
	}
	w = do(t, h, "GET", "/api/images", nil)
	if w.Code != 200 {
		t.Fatalf("list images %d %s", w.Code, w.Body)
	}
	w = do(t, h, "POST", "/api/images", env.ImageConfig{Version: "0.1.0-rc.7", Ref: "dsh-testsuite-runtime:0.1.0-rc.7"})
	if w.Code != 200 {
		t.Fatalf("upsert %d %s", w.Code, w.Body)
	}
	w = do(t, h, "GET", "/api/images/remote", nil)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("0.1.0-rc.6")) || !bytes.Contains(w.Body.Bytes(), []byte("0.1.0-rc.7")) || !bytes.Contains(w.Body.Bytes(), []byte("0.1.0-rc.8")) || !bytes.Contains(w.Body.Bytes(), []byte("0.1.1-rc.1")) || !bytes.Contains(w.Body.Bytes(), []byte("dsh-testsuite-runtime")) {
		t.Fatalf("remote %d %s", w.Code, w.Body)
	}
	w = do(t, h, "GET", "/api/images", nil)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("0.1.0-rc.7")) || !bytes.Contains(w.Body.Bytes(), []byte(`"present":true`)) {
		t.Fatalf("list after upsert %d %s", w.Code, w.Body)
	}
	w = do(t, h, "DELETE", "/api/images/0.1.0-rc.7", nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete %d %s", w.Code, w.Body)
	}
}

func TestImagesAPIPullsWhenMissing(t *testing.T) {
	s, fake := testAPI(t)
	h := s.Handler()
	w := do(t, h, "POST", "/api/images", map[string]any{"version": "0.1.1-rc.1"})
	if w.Code != 200 {
		t.Fatalf("register %d %s", w.Code, w.Body)
	}
	wantPull := env.PublicRuntimeRepo + ":0.1.1-rc.1"
	if len(fake.pulled) != 1 || fake.pulled[0] != wantPull {
		t.Fatalf("pulled=%v", fake.pulled)
	}
	if len(fake.tagged) != 1 || fake.tagged[0] != wantPull+" dsh-testsuite-runtime:0.1.1-rc.1" {
		t.Fatalf("tagged=%v", fake.tagged)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"present":true`)) {
		t.Fatalf("body %s", w.Body)
	}

	w = do(t, h, "POST", "/api/images", map[string]any{"version": "0.1.0-rc.6", "pull": false})
	if w.Code != 200 {
		t.Fatalf("no-pull %d %s", w.Code, w.Body)
	}
	if len(fake.pulled) != 1 {
		t.Fatalf("pull:false should not pull: %v", fake.pulled)
	}
}

func TestPresetsAPI(t *testing.T) {
	s, _ := testAPI(t)
	h := s.Handler()
	w := do(t, h, "POST", "/api/images", env.ImageConfig{Version: "0.1.0-rc.7"})
	if w.Code != 200 {
		t.Fatalf("catalog %d %s", w.Code, w.Body)
	}
	w = do(t, h, "POST", "/api/presets", env.PresetInput{
		Name: "flash", Provider: "deepseek-official", Model: "deepseek-v4-flash",
		APIKey: "sk-secret-key",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create preset %d %s", w.Code, w.Body)
	}
	var preset env.PresetView
	if err := json.Unmarshal(w.Body.Bytes(), &preset); err != nil {
		t.Fatal(err)
	}
	if preset.APIKeyHint != "****-key" || bytes.Contains(w.Body.Bytes(), []byte("sk-secret-key")) {
		t.Fatalf("leaked key: %s", w.Body)
	}
	w = do(t, h, "GET", "/api/presets", nil)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("flash")) {
		t.Fatalf("list %d %s", w.Code, w.Body)
	}
	w = do(t, h, "PUT", "/api/presets/"+preset.ID, env.PresetInput{
		Name: "flash2", Provider: "deepseek-official", Model: "deepseek-v4-pro",
	})
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("flash2")) {
		t.Fatalf("update %d %s", w.Code, w.Body)
	}
	w = do(t, h, "POST", "/api/environments", env.CreateRequest{
		Name: "from-p", DSHVersion: "0.1.0-rc.7", PresetID: preset.ID,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create env %d %s", w.Code, w.Body)
	}
	var created env.View
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.APIKeyHint != "****-key" || created.Model != "deepseek-v4-pro" {
		t.Fatalf("created=%+v", created)
	}
	w = do(t, h, "DELETE", "/api/presets/"+preset.ID, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete %d %s", w.Code, w.Body)
	}
}
