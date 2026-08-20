package docker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type mockRunner struct {
	mu       sync.Mutex
	calls    [][]string
	byCmd    map[string]mockResp
	fallback mockResp
}

type mockResp struct {
	out string
	err error
}

func newMock() *mockRunner {
	return &mockRunner{byCmd: map[string]mockResp{}}
}

func (m *mockRunner) on(cmd string, out string, err error) {
	m.byCmd[cmd] = mockResp{out: out, err: err}
}

func (m *mockRunner) run(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	_ = ctx
	_ = timeout
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := append([]string(nil), args...)
	m.calls = append(m.calls, cp)
	if len(args) == 0 {
		return m.fallback.out, m.fallback.err
	}
	if r, ok := m.byCmd[args[0]]; ok {
		return r.out, r.err
	}
	return m.fallback.out, m.fallback.err
}

func (m *mockRunner) lastCall() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return nil
	}
	return m.calls[len(m.calls)-1]
}

func (m *mockRunner) callsWith(first string) [][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out [][]string
	for _, c := range m.calls {
		if len(c) > 0 && c[0] == first {
			out = append(out, c)
		}
	}
	return out
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func containsPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func TestNewDefaults(t *testing.T) {
	d := New(Options{})
	if d.bindIP != "127.0.0.1" {
		t.Fatalf("bindIP=%q", d.bindIP)
	}
	if d.namePrefix != NamePrefix {
		t.Fatalf("prefix=%q", d.namePrefix)
	}
	if d.Name() != "docker" {
		t.Fatalf("Name=%q", d.Name())
	}
}

func TestCreateArgs(t *testing.T) {
	m := newMock()
	m.on("run", "cid", nil)
	m.on("inspect", "34567", nil)
	d := New(Options{BindIP: "192.168.1.1", Network: "sbx-net", CPUCores: 1.5, MemoryMB: 2048})
	d.run = m.run

	h, err := d.Create(context.Background(), Spec{
		ID:    "abc123",
		Image: "dsh-testsuite-runtime:0.1.0-rc.7",
		Env:   map[string]string{"DSH_API_KEY": "sk-test"},
		Mounts: []string{
			"/data/envs/abc123/bootstrap:/bootstrap:ro",
			"/data/envs/abc123/home:/data/dsh",
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if h.Name != "dsh-ts-abc123" || h.Status != StatusRunning {
		t.Fatalf("handle=%+v", h)
	}
	if h.Endpoints[WebPort] != "192.168.1.1:34567" {
		t.Fatalf("endpoints=%v", h.Endpoints)
	}

	run := m.callsWith("run")
	if len(run) != 1 {
		t.Fatalf("run calls=%d", len(run))
	}
	args := run[0]
	if !containsArg(args, "dsh-testsuite-runtime:0.1.0-rc.7") {
		t.Fatalf("missing image: %v", args)
	}
	if !containsPair(args, "--label", ManagedLabel+"="+ManagedValue) {
		t.Fatalf("missing managed label: %v", args)
	}
	if !containsPair(args, "--label", IDLabel+"=abc123") {
		t.Fatalf("missing id label: %v", args)
	}
	if !containsArg(args, "-p") || !containsArg(args, "192.168.1.1::3080") {
		t.Fatalf("missing publish: %v", args)
	}
	if !containsPair(args, "--network", "sbx-net") {
		t.Fatalf("missing network: %v", args)
	}
	if !containsPair(args, "--cpus", "1.50") || !containsPair(args, "--memory", "2048m") {
		t.Fatalf("missing resources: %v", args)
	}
	if !containsPair(args, "-e", "DSH_API_KEY=sk-test") {
		t.Fatalf("missing env: %v", args)
	}
	if !containsArg(args, "/data/envs/abc123/bootstrap:/bootstrap:ro") {
		t.Fatalf("missing bootstrap mount: %v", args)
	}
}

func TestCreateRollbackOnInspectError(t *testing.T) {
	m := newMock()
	m.on("run", "cid", nil)
	m.on("inspect", "", errors.New("inspect failed"))
	m.on("rm", "", nil)
	d := New(Options{})
	d.run = m.run
	_, err := d.Create(context.Background(), Spec{ID: "x", Image: "img:tag"})
	if err == nil {
		t.Fatal("expected error")
	}
	if len(m.callsWith("rm")) == 0 {
		t.Fatal("expected rollback rm")
	}
}

func TestStartStopDestroyLogs(t *testing.T) {
	m := newMock()
	m.on("start", "", nil)
	m.on("stop", "", nil)
	m.on("rm", "", nil)
	m.on("logs", "hello", nil)
	d := New(Options{})
	d.run = m.run
	ctx := context.Background()
	if err := d.Start(ctx, "id1"); err != nil {
		t.Fatal(err)
	}
	if err := d.Stop(ctx, "id1"); err != nil {
		t.Fatal(err)
	}
	if err := d.Destroy(ctx, "id1"); err != nil {
		t.Fatal(err)
	}
	out, err := d.Logs(ctx, "id1", 100)
	if err != nil || out != "hello" {
		t.Fatalf("logs=%q err=%v", out, err)
	}
	if got := m.callsWith("start")[0]; !containsArg(got, "dsh-ts-id1") {
		t.Fatalf("start args=%v", got)
	}
}

func TestDestroyNoSuchContainer(t *testing.T) {
	m := newMock()
	m.on("rm", "", errors.New("Error: No such container: dsh-ts-gone"))
	d := New(Options{})
	d.run = m.run
	if err := d.Destroy(context.Background(), "gone"); err != nil {
		t.Fatal(err)
	}
}

func TestListAndStatus(t *testing.T) {
	m := newMock()
	m.on("ps", "dsh-ts-a\trunning\ta\t2026-01-02 03:04:05 +0000 UTC\ndsh-ts-b\texited\tb\t", nil)
	m.on("inspect", "running", nil)
	d := New(Options{})
	d.run = m.run
	list, err := d.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Status != StatusRunning || list[1].Status != StatusStopped {
		t.Fatalf("list=%+v", list)
	}
	st, err := d.Status(context.Background(), "a")
	if err != nil || st != StatusRunning {
		t.Fatalf("status=%s err=%v", st, err)
	}
}

func TestListImagesAndExists(t *testing.T) {
	m := newMock()
	m.on("images", "dsh-testsuite-runtime\t0.1.0-rc.7\tsha256:abc\t2026-01-01\t1.2GB\nother\t<none>\tsha256:x\t\t", nil)
	m.on("image", "", nil)
	d := New(Options{})
	d.run = m.run
	imgs, err := d.ListImages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != 1 || imgs[0].Tag != "0.1.0-rc.7" || imgs[0].Version != "0.1.0-rc.7" {
		t.Fatalf("images=%+v", imgs)
	}
	ok, err := d.ImageExists(context.Background(), "dsh-testsuite-runtime:0.1.0-rc.7")
	if err != nil || !ok {
		t.Fatalf("exists=%v err=%v", ok, err)
	}
	args := m.callsWith("images")[0]
	if !containsArg(args, "label="+RuntimeLabel+"=1") {
		t.Fatalf("missing filter: %v", args)
	}
}

func TestImageExistsMissing(t *testing.T) {
	m := newMock()
	m.on("image", "", errors.New("Error: No such image: foo:bar"))
	d := New(Options{})
	d.run = m.run
	ok, err := d.ImageExists(context.Background(), "foo:bar")
	if err != nil || ok {
		t.Fatalf("exists=%v err=%v", ok, err)
	}
}

func TestImagePull(t *testing.T) {
	m := newMock()
	m.on("pull", "", nil)
	d := New(Options{})
	d.run = m.run
	if err := d.ImagePull(context.Background(), "ghcr.io/cocofhu/dsh-testsuite-runtime:0.1.0-rc.8"); err != nil {
		t.Fatal(err)
	}
	args := m.callsWith("pull")[0]
	if args[1] != "ghcr.io/cocofhu/dsh-testsuite-runtime:0.1.0-rc.8" {
		t.Fatalf("args=%v", args)
	}
}

func TestHostPortFromEndpoints(t *testing.T) {
	if HostPortFromEndpoints(map[int]string{WebPort: "127.0.0.1:4123"}) != 4123 {
		t.Fatal("parse failed")
	}
	if HostPortFromEndpoints(nil) != 0 {
		t.Fatal("nil should be 0")
	}
}
