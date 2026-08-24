package env

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/dsh-testsuite/internal/config"
	"github.com/cocofhu/dsh-testsuite/internal/docker"
	"github.com/rs/zerolog"
)

type fakeRuntime struct {
	images    map[string]bool
	handles   map[string]*docker.Handle
	createFn  func(docker.Spec) (*docker.Handle, error)
	startErr  error
	stopErr   error
	logs      string
	destroyed []string
	pulled    []string
	tagged    []string
}

func newFake() *fakeRuntime {
	return &fakeRuntime{
		images:  map[string]bool{},
		handles: map[string]*docker.Handle{},
		logs:    "boot ok",
	}
}

func (f *fakeRuntime) Name() string { return "docker" }

func (f *fakeRuntime) Create(_ context.Context, spec docker.Spec) (*docker.Handle, error) {
	if f.createFn != nil {
		return f.createFn(spec)
	}
	h := &docker.Handle{
		ID:        spec.ID,
		Name:      docker.NamePrefix + spec.ID,
		Status:    docker.StatusRunning,
		Endpoints: map[int]string{docker.WebPort: "127.0.0.1:4100"},
	}
	f.handles[spec.ID] = h
	return h, nil
}

func (f *fakeRuntime) Start(_ context.Context, id string) error {
	if f.startErr != nil {
		return f.startErr
	}
	if h, ok := f.handles[id]; ok {
		h.Status = docker.StatusRunning
		h.Endpoints = map[int]string{docker.WebPort: "127.0.0.1:4100"}
	} else {
		f.handles[id] = &docker.Handle{
			ID: id, Name: docker.NamePrefix + id, Status: docker.StatusRunning,
			Endpoints: map[int]string{docker.WebPort: "127.0.0.1:4100"},
		}
	}
	return nil
}

func (f *fakeRuntime) Stop(_ context.Context, id string) error {
	if f.stopErr != nil {
		return f.stopErr
	}
	if h, ok := f.handles[id]; ok {
		h.Status = docker.StatusStopped
		h.Endpoints = nil
	}
	return nil
}

func (f *fakeRuntime) Destroy(_ context.Context, id string) error {
	f.destroyed = append(f.destroyed, id)
	delete(f.handles, id)
	return nil
}

func (f *fakeRuntime) Recreate(ctx context.Context, spec docker.Spec) (*docker.Handle, error) {
	if err := f.Destroy(ctx, spec.ID); err != nil {
		return nil, err
	}
	return f.Create(ctx, spec)
}

func (f *fakeRuntime) Get(_ context.Context, id string) (*docker.Handle, error) {
	if h, ok := f.handles[id]; ok {
		return h, nil
	}
	return &docker.Handle{ID: id, Name: docker.NamePrefix + id, Status: docker.StatusNotFound}, nil
}

func (f *fakeRuntime) List(_ context.Context) ([]*docker.Handle, error) {
	var out []*docker.Handle
	for _, h := range f.handles {
		out = append(out, h)
	}
	return out, nil
}

func (f *fakeRuntime) Endpoints(_ context.Context, id string) (map[int]string, error) {
	if h, ok := f.handles[id]; ok {
		return h.Endpoints, nil
	}
	return nil, errors.New("missing")
}

func (f *fakeRuntime) Logs(_ context.Context, _ string, _ int) (string, error) {
	return f.logs, nil
}

func (f *fakeRuntime) ImageExists(_ context.Context, ref string) (bool, error) {
	return f.images[ref], nil
}

func (f *fakeRuntime) ImagePull(_ context.Context, ref string) error {
	f.pulled = append(f.pulled, ref)
	f.images[ref] = true
	return nil
}

func (f *fakeRuntime) ImageTag(_ context.Context, src, dst string) error {
	f.tagged = append(f.tagged, src+" "+dst)
	f.images[dst] = f.images[src]
	return nil
}

func testService(t *testing.T, fake *fakeRuntime) *Service {
	t.Helper()
	cfg := config.Default()
	cfg.Limits.MaxEnvs = 2
	cfg.Limits.IdleTTL = config.Duration(time.Hour)
	cfg.Dir = t.TempDir()
	s, err := NewService(cfg, fake, t.TempDir(), zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustCatalog(t *testing.T, s *Service, version string) {
	t.Helper()
	if _, err := s.UpsertImage(ImageConfig{Version: version}); err != nil {
		t.Fatal(err)
	}
}

func TestCreateRequiresConfiguredImage(t *testing.T) {
	s := testService(t, newFake())
	_, err := s.Create(context.Background(), CreateRequest{
		Name: "n", DSHVersion: "0.1.0-rc.7", APIKey: "sk-abcd1234",
		Provider: "deepseek-official", Model: "deepseek-v4-flash",
	})
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err=%v", err)
	}
}

func TestCreateRequiresPresentImage(t *testing.T) {
	s := testService(t, newFake())
	mustCatalog(t, s, "0.1.0-rc.7")
	_, err := s.Create(context.Background(), CreateRequest{
		Name: "n", DSHVersion: "0.1.0-rc.7", APIKey: "sk-abcd1234",
		Provider: "deepseek-official", Model: "deepseek-v4-flash",
	})
	if !errors.Is(err, ErrImageMissing) {
		t.Fatalf("err=%v", err)
	}
}

func TestCreateAndRedactKey(t *testing.T) {
	fake := newFake()
	fake.images["dsh-testsuite-runtime:0.1.0-rc.7"] = true
	var gotSpec docker.Spec
	fake.createFn = func(spec docker.Spec) (*docker.Handle, error) {
		gotSpec = spec
		h := &docker.Handle{
			ID: spec.ID, Name: docker.NamePrefix + spec.ID, Status: docker.StatusRunning,
			Endpoints: map[int]string{docker.WebPort: "127.0.0.1:4123"},
		}
		fake.handles[spec.ID] = h
		return h, nil
	}
	s := testService(t, fake)
	mustCatalog(t, s, "0.1.0-rc.7")
	v, err := s.Create(context.Background(), CreateRequest{
		Name: "skillhub", DSHVersion: "0.1.0-rc.7", APIKey: "sk-secret-key",
		Provider: "deepseek-official", Model: "deepseek-v4-flash",
		Plugins: []string{" github:cocofhu/skillhub#main "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.APIKeyHint != "****-key" {
		t.Fatalf("hint=%q", v.APIKeyHint)
	}
	if v.Status != StatusRunning || v.HostPort != 4123 {
		t.Fatalf("view=%+v", v)
	}
	if v.Health != HealthStarting {
		t.Fatalf("health=%q want starting while nothing listens on the published port", v.Health)
	}
	if gotSpec.Env["DSH_API_KEY"] != "sk-secret-key" {
		t.Fatalf("env not injected")
	}
	if !strings.Contains(strings.Join(gotSpec.Mounts, ","), "/bootstrap:ro") {
		t.Fatalf("mounts=%v", gotSpec.Mounts)
	}
	for _, m := range gotSpec.Mounts {
		host, _, ok := strings.Cut(m, ":")
		if !ok || !filepath.IsAbs(host) {
			t.Fatalf("mount host path must be absolute: %q", m)
		}
	}
	listed := s.List(context.Background())
	if len(listed) != 1 || listed[0].APIKeyHint != "****-key" {
		t.Fatalf("list=%+v", listed)
	}
	raw, _ := os.ReadFile(filepath.Join(s.envDir(v.ID), "bootstrap", "settings.yaml"))
	if !strings.Contains(string(raw), "deepseek-official") {
		t.Fatalf("settings=%s", raw)
	}
}

func TestCreateKubernetesOpenURL(t *testing.T) {
	fake := newFake()
	fake.images["dsh-testsuite-runtime:0.1.0-rc.7"] = true
	fake.createFn = func(spec docker.Spec) (*docker.Handle, error) {
		h := &docker.Handle{
			ID: spec.ID, Name: docker.NamePrefix + spec.ID, Status: docker.StatusRunning,
			OpenURL:   "http://env-" + spec.ID + ".example.com/",
			HealthURL: "http://dsh-ts-" + spec.ID + ":3080/",
		}
		fake.handles[spec.ID] = h
		if spec.Env["DSH_TRUSTED_HOST"] != "env-"+spec.ID+".example.com" {
			t.Fatalf("trusted host=%q", spec.Env["DSH_TRUSTED_HOST"])
		}
		return h, nil
	}
	s := testService(t, fake)
	s.cfg.Runtime = config.RuntimeKubernetes
	s.cfg.Kubernetes.EnvHostTemplate = "env-{id}.example.com"
	mustCatalog(t, s, "0.1.0-rc.7")
	v, err := s.Create(context.Background(), CreateRequest{
		Name: "k8senv", DSHVersion: "0.1.0-rc.7", APIKey: "sk-secret-key",
		Provider: "deepseek-official", Model: "deepseek-v4-flash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(v.OpenURL, "http://env-") || !strings.HasSuffix(v.OpenURL, ".example.com/") {
		t.Fatalf("OpenURL=%q", v.OpenURL)
	}
	if v.HostPort != 80 {
		t.Fatalf("HostPort=%d", v.HostPort)
	}
}

func TestCreateCustomProviderNeedsBaseURL(t *testing.T) {
	fake := newFake()
	fake.images["dsh-testsuite-runtime:0.1.0-rc.7"] = true
	s := testService(t, fake)
	_, err := s.Create(context.Background(), CreateRequest{
		Name: "n", DSHVersion: "0.1.0-rc.7", APIKey: "sk",
		Provider: "cpa", Model: "m",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMaxEnvs(t *testing.T) {
	fake := newFake()
	fake.images["dsh-testsuite-runtime:0.1.0-rc.7"] = true
	s := testService(t, fake)
	mustCatalog(t, s, "0.1.0-rc.7")
	req := CreateRequest{Name: "a", DSHVersion: "0.1.0-rc.7", APIKey: "sk", Provider: "deepseek-official", Model: "m"}
	if _, err := s.Create(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	req.Name = "b"
	if _, err := s.Create(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	req.Name = "c"
	_, err := s.Create(context.Background(), req)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err=%v", err)
	}
}

func TestStopStartDestroyLogs(t *testing.T) {
	fake := newFake()
	fake.images["dsh-testsuite-runtime:v1"] = true
	s := testService(t, fake)
	mustCatalog(t, s, "v1")
	v, err := s.Create(context.Background(), CreateRequest{
		Name: "n", DSHVersion: "v1", APIKey: "sk-xxxx", Provider: "deepseek-official", Model: "m",
	})
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := s.Stop(context.Background(), v.ID)
	if err != nil || stopped.Status != StatusStopped || stopped.HostPort != 0 {
		t.Fatalf("stop=%+v err=%v", stopped, err)
	}
	started, err := s.Start(context.Background(), v.ID)
	if err != nil || started.Status != StatusRunning {
		t.Fatalf("start=%+v err=%v", started, err)
	}
	restarted, err := s.Restart(context.Background(), v.ID)
	if err != nil || restarted.Status != StatusRunning {
		t.Fatalf("restart=%+v err=%v", restarted, err)
	}
	logs, err := s.Logs(context.Background(), v.ID, 10)
	if err != nil || logs != "boot ok" {
		t.Fatalf("logs=%q err=%v", logs, err)
	}
	if err := s.Destroy(context.Background(), v.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(context.Background(), v.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after destroy: %v", err)
	}
}

func TestReconcileMissingContainer(t *testing.T) {
	fake := newFake()
	s := testService(t, fake)
	rec := Record{ID: "dead", Name: "x", Status: StatusRunning, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := s.store.put(rec); err != nil {
		t.Fatal(err)
	}
	s.Reconcile(context.Background())
	got, _ := s.store.get("dead")
	if got.Status != StatusError {
		t.Fatalf("status=%s", got.Status)
	}
}

func TestReconcileOrphanGC(t *testing.T) {
	fake := newFake()
	fake.handles["orphan"] = &docker.Handle{ID: "orphan", Name: "dsh-ts-orphan", Status: docker.StatusRunning}
	s := testService(t, fake)
	s.Reconcile(context.Background())
	if len(fake.destroyed) != 1 || fake.destroyed[0] != "orphan" {
		t.Fatalf("destroyed=%v", fake.destroyed)
	}
}

func TestRenewExtendsDestroyAt(t *testing.T) {
	fake := newFake()
	fake.images["dsh-testsuite-runtime:v1"] = true
	s := testService(t, fake)
	mustCatalog(t, s, "v1")
	v, err := s.Create(context.Background(), CreateRequest{
		Name: "n", DSHVersion: "v1", APIKey: "sk", Provider: "deepseek-official", Model: "m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.DestroyAt == nil {
		t.Fatal("create missing destroyAt")
	}
	before := *v.DestroyAt
	renewed, err := s.Renew(context.Background(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.DestroyAt == nil {
		t.Fatal("renew missing destroyAt")
	}
	got := renewed.DestroyAt.Sub(before)
	if got < RenewDuration-time.Second || got > RenewDuration+time.Second {
		t.Fatalf("extended by %s want %s", got, RenewDuration)
	}

	past := time.Now().Add(-time.Minute)
	rec, _ := s.store.get(v.ID)
	rec.DestroyAt = &past
	_ = s.store.put(*rec)
	fromPast, err := s.Renew(context.Background(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	remain := time.Until(*fromPast.DestroyAt)
	if remain < RenewDuration-2*time.Second || remain > RenewDuration+2*time.Second {
		t.Fatalf("from past remain=%s want ~%s", remain, RenewDuration)
	}

	if _, err := s.Renew(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestSweepIdle(t *testing.T) {
	fake := newFake()
	fake.images["dsh-testsuite-runtime:v1"] = true
	s := testService(t, fake)
	mustCatalog(t, s, "v1")
	v, err := s.Create(context.Background(), CreateRequest{
		Name: "n", DSHVersion: "v1", APIKey: "sk", Provider: "deepseek-official", Model: "m",
	})
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	rec, _ := s.store.get(v.ID)
	rec.DestroyAt = &past
	_ = s.store.put(*rec)
	s.SweepIdle(context.Background())
	if _, err := s.Get(context.Background(), v.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("still present: %v", err)
	}
}

func TestImageCatalog(t *testing.T) {
	fake := newFake()
	fake.images["registry.example/dsh:rc7"] = true
	s := testService(t, fake)
	got, err := s.UpsertImage(ImageConfig{Version: "0.1.0-rc.7", Ref: "registry.example/dsh:rc7"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Present || got.Ref != "registry.example/dsh:rc7" {
		t.Fatalf("%+v", got)
	}
	list, err := s.ListImages(context.Background())
	if err != nil || len(list) != 1 || !list[0].Present {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	if err := s.DeleteImage("0.1.0-rc.7"); err != nil {
		t.Fatal(err)
	}
	list, err = s.ListImages(context.Background())
	if err != nil || len(list) != 0 {
		t.Fatalf("after delete list=%+v err=%v", list, err)
	}
}

func TestRegisterImagePullsFromGHCR(t *testing.T) {
	fake := newFake()
	s := testService(t, fake)
	got, err := s.RegisterImage(context.Background(), ImageConfig{Version: "0.1.1-rc.2"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Present || got.Ref != "dsh-testsuite-runtime:0.1.1-rc.2" {
		t.Fatalf("%+v", got)
	}
	wantPull := PublicRuntimeRepo + ":0.1.1-rc.2"
	if len(fake.pulled) != 1 || fake.pulled[0] != wantPull {
		t.Fatalf("pulled=%v want %s", fake.pulled, wantPull)
	}
	if len(fake.tagged) != 1 || fake.tagged[0] != wantPull+" dsh-testsuite-runtime:0.1.1-rc.2" {
		t.Fatalf("tagged=%v", fake.tagged)
	}

	fake.pulled, fake.tagged = nil, nil
	if _, err := s.RegisterImage(context.Background(), ImageConfig{Version: "0.1.1-rc.2"}, true); err != nil {
		t.Fatal(err)
	}
	if len(fake.pulled) != 0 || len(fake.tagged) != 0 {
		t.Fatalf("already present should skip pull: pulled=%v tagged=%v", fake.pulled, fake.tagged)
	}
}

func TestRegisterImagePullsExplicitRegistryRef(t *testing.T) {
	fake := newFake()
	s := testService(t, fake)
	ref := "ghcr.io/cocofhu/dsh-testsuite-runtime:0.1.0-rc.8"
	got, err := s.RegisterImage(context.Background(), ImageConfig{Version: "0.1.0-rc.8", Ref: ref}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Present || got.Ref != ref {
		t.Fatalf("%+v", got)
	}
	if len(fake.pulled) != 1 || fake.pulled[0] != ref {
		t.Fatalf("pulled=%v", fake.pulled)
	}
	if len(fake.tagged) != 0 {
		t.Fatalf("tagged=%v", fake.tagged)
	}
}

func TestHealthBecomesHealthy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<!doctype html>"))
	}))
	t.Cleanup(ts.Close)
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}

	fake := newFake()
	fake.images["dsh-testsuite-runtime:0.1.0-rc.7"] = true
	fake.createFn = func(spec docker.Spec) (*docker.Handle, error) {
		h := &docker.Handle{
			ID: spec.ID, Name: docker.NamePrefix + spec.ID, Status: docker.StatusRunning,
			Endpoints: map[int]string{docker.WebPort: "127.0.0.1:" + strconv.Itoa(port)},
		}
		fake.handles[spec.ID] = h
		return h, nil
	}
	s := testService(t, fake)
	mustCatalog(t, s, "0.1.0-rc.7")
	v, err := s.Create(context.Background(), CreateRequest{
		Name: "n", DSHVersion: "0.1.0-rc.7", APIKey: "sk",
		Provider: "deepseek-official", Model: "m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.Health != HealthHealthy {
		t.Fatalf("create health=%q", v.Health)
	}
	listed := s.List(context.Background())
	if len(listed) != 1 || listed[0].Health != HealthHealthy {
		t.Fatalf("list=%+v", listed)
	}
}

func TestListOrderStable(t *testing.T) {
	fake := newFake()
	fake.images["dsh-testsuite-runtime:0.1.0-rc.7"] = true
	s := testService(t, fake)
	mustCatalog(t, s, "0.1.0-rc.7")
	first, err := s.Create(context.Background(), CreateRequest{
		Name: "alpha", DSHVersion: "0.1.0-rc.7", APIKey: "sk-aaaa1111",
		Provider: "deepseek-official", Model: "deepseek-v4-flash",
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	second, err := s.Create(context.Background(), CreateRequest{
		Name: "beta", DSHVersion: "0.1.0-rc.7", APIKey: "sk-bbbb2222",
		Provider: "deepseek-official", Model: "deepseek-v4-flash",
	})
	if err != nil {
		t.Fatal(err)
	}
	var prev []string
	for i := 0; i < 12; i++ {
		listed := s.List(context.Background())
		if len(listed) != 2 {
			t.Fatalf("len=%d", len(listed))
		}
		ids := []string{listed[0].ID, listed[1].ID}
		if listed[0].CreatedAt.Before(listed[1].CreatedAt) {
			t.Fatalf("expected newest first: %+v", listed)
		}
		if prev != nil && (ids[0] != prev[0] || ids[1] != prev[1]) {
			t.Fatalf("order jumped: %v -> %v", prev, ids)
		}
		prev = ids
	}
	if prev[0] != second.ID || prev[1] != first.ID {
		t.Fatalf("want newest %s then %s, got %v", second.ID, first.ID, prev)
	}
}

func TestImageSnapshotOrderStable(t *testing.T) {
	s := testService(t, newFake())
	mustCatalog(t, s, "0.1.0-rc.7")
	mustCatalog(t, s, "0.1.0-rc.8")
	var prev []string
	for i := 0; i < 12; i++ {
		imgs, err := s.ListImages(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(imgs) != 2 {
			t.Fatalf("len=%d", len(imgs))
		}
		vers := []string{imgs[0].Version, imgs[1].Version}
		if prev != nil && (vers[0] != prev[0] || vers[1] != prev[1]) {
			t.Fatalf("order jumped: %v -> %v", prev, vers)
		}
		prev = vers
	}
	if prev[0] != "0.1.0-rc.8" || prev[1] != "0.1.0-rc.7" {
		t.Fatalf("want rc.8 then rc.7, got %v", prev)
	}
}

func TestPresetCRUDAndCreateEnv(t *testing.T) {
	fake := newFake()
	fake.images["dsh-testsuite-runtime:v1"] = true
	s := testService(t, fake)
	mustCatalog(t, s, "v1")

	view, err := s.CreatePreset(PresetInput{
		Name: "flash", Provider: "deepseek-official", Model: "deepseek-v4-flash",
		APIKey: "sk-secret-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.APIKeyHint != "****-key" || view.Name != "flash" {
		t.Fatalf("view=%+v", view)
	}
	listed := s.ListPresets()
	if len(listed) != 1 || listed[0].APIKeyHint != "****-key" {
		t.Fatalf("list=%+v", listed)
	}

	updated, err := s.UpdatePreset(view.ID, PresetInput{
		Name: "flash2", Provider: "deepseek-official", Model: "deepseek-v4-pro",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "flash2" || updated.Model != "deepseek-v4-pro" || updated.APIKeyHint != "****-key" {
		t.Fatalf("update=%+v", updated)
	}

	created, err := s.Create(context.Background(), CreateRequest{
		Name: "from-preset", DSHVersion: "v1", PresetID: view.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Provider != "deepseek-official" || created.Model != "deepseek-v4-pro" || created.APIKeyHint != "****-key" {
		t.Fatalf("create=%+v", created)
	}

	if _, err := s.Create(context.Background(), CreateRequest{
		Name: "missing", DSHVersion: "v1", PresetID: "nope",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing preset: %v", err)
	}

	if err := s.DeletePreset(view.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.ListPresets()) != 0 {
		t.Fatal("expected empty")
	}
}

func TestHintKey(t *testing.T) {
	if hintKey("ab") != "****" {
		t.Fatal(hintKey("ab"))
	}
	if hintKey("abcdefgh") != "****efgh" {
		t.Fatal(hintKey("abcdefgh"))
	}
}
