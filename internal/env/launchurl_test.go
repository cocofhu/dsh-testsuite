package env

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/cocofhu/dsh-testsuite/internal/docker"
)

func TestLaunchTokenFromLogsUsesLastLine(t *testing.T) {
	logs := "boot\ndsh web: http://127.0.0.1:3081/?token=old\nlater\ndsh web: http://127.0.0.1:3081/?token=new-token\n"
	if got := launchTokenFromLogs(logs); got != "new-token" {
		t.Fatalf("token=%q", got)
	}
	if launchTokenFromLogs("no url here") != "" {
		t.Fatal("expected empty token")
	}
}

func TestWithLaunchTokenKeepsPublishedOrigin(t *testing.T) {
	got := withLaunchToken("http://env.example:4123/", "launch token")
	if got != "http://env.example:4123/?token=launch+token" {
		t.Fatalf("url=%q", got)
	}
	if !sameOpenBase(got, "http://env.example:4123/") {
		t.Fatalf("base changed: %q", got)
	}
}

func TestListOpenURLIncludesLaunchToken(t *testing.T) {
	fake := newFake()
	fake.images["dsh-testsuite-runtime:0.2.1-rc.4"] = true
	fake.logs = "dsh web: http://127.0.0.1:3081/?token=launch-token\n"
	fake.createFn = func(spec docker.Spec) (*docker.Handle, error) {
		if spec.Env["DSH_PUBLIC_URL"] != "" {
			t.Fatalf("docker should wait for the published port, got %q", spec.Env["DSH_PUBLIC_URL"])
		}
		h := &docker.Handle{
			ID: spec.ID, Name: docker.NamePrefix + spec.ID, Status: docker.StatusRunning,
			Endpoints: map[int]string{docker.WebPort: "127.0.0.1:4123"},
		}
		fake.handles[spec.ID] = h
		return h, nil
	}
	s := testService(t, fake)
	s.cfg.Server.PublicHost = "env.example"
	mustCatalog(t, s, "0.2.1-rc.4")
	v, err := s.Create(context.Background(), CreateRequest{
		Name: "n", DSHVersion: "0.2.1-rc.4", APIKey: "sk-secret-key",
		Provider: "deepseek-official", Model: "deepseek-v4-flash",
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(s.envDir(v.ID), "bootstrap", "public-url"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "http://env.example:4123/\n" {
		t.Fatalf("public-url=%q", body)
	}
	listed := s.List(context.Background())
	if len(listed) != 1 || listed[0].OpenURL != "http://env.example:4123/?token=launch-token" {
		t.Fatalf("open=%q", listed[0].OpenURL)
	}
}

func TestAuthRequiredStaysStartingUntilToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(ts.Close)
	_, portStr, err := net.SplitHostPort(ts.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	nport, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}

	fake := newFake()
	fake.images["dsh-testsuite-runtime:0.2.1-rc.4"] = true
	fake.logs = "still booting"
	fake.createFn = func(spec docker.Spec) (*docker.Handle, error) {
		h := &docker.Handle{
			ID: spec.ID, Name: docker.NamePrefix + spec.ID, Status: docker.StatusRunning,
			Endpoints: map[int]string{docker.WebPort: "127.0.0.1:" + strconv.Itoa(nport)},
		}
		fake.handles[spec.ID] = h
		return h, nil
	}
	s := testService(t, fake)
	mustCatalog(t, s, "0.2.1-rc.4")
	if _, err := s.Create(context.Background(), CreateRequest{
		Name: "n", DSHVersion: "0.2.1-rc.4", APIKey: "sk",
		Provider: "deepseek-official", Model: "m",
	}); err != nil {
		t.Fatal(err)
	}
	listed := s.List(context.Background())
	if listed[0].Health != HealthStarting {
		t.Fatalf("health=%q want starting before the launch URL", listed[0].Health)
	}
	fake.logs = "dsh web: http://127.0.0.1:3081/?token=ready\n"
	listed = s.List(context.Background())
	if listed[0].Health != HealthHealthy {
		t.Fatalf("health=%q", listed[0].Health)
	}
	if launchTokenFromURL(listed[0].OpenURL) != "ready" {
		t.Fatalf("open=%q", listed[0].OpenURL)
	}
}
