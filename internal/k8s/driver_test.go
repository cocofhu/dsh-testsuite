package k8s

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cocofhu/dsh-testsuite/internal/docker"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func testDriver(t *testing.T) *Driver {
	t.Helper()
	d, err := newWithClient(Options{
		Namespace:       "demo",
		EnvHostTemplate: "env-{id}.example.com",
		IngressClass:    "nginx",
		NamePrefix:      docker.NamePrefix,
		CPUCores:        1,
		MemoryMB:        512,
	}, fake.NewSimpleClientset())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestName(t *testing.T) {
	if testDriver(t).Name() != "kubernetes" {
		t.Fatal(testDriver(t).Name())
	}
}

func TestCreateStopDestroy(t *testing.T) {
	d := testDriver(t)
	ctx := context.Background()
	boot := t.TempDir()
	if err := os.WriteFile(filepath.Join(boot, "settings.yaml"), []byte("provider: deepseek\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(boot, "plugins.txt"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := d.Create(ctx, docker.Spec{
		ID:    "abc12xyz",
		Image: "ghcr.io/cocofhu/dsh-testsuite-runtime:0.1.1-rc.2",
		Env:   map[string]string{"DSH_TRUSTED_HOST": "env-abc12xyz.example.com"},
		Mounts: []string{
			boot + ":/bootstrap:ro",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.OpenURL != "http://env-abc12xyz.example.com/" {
		t.Fatalf("OpenURL=%q", h.OpenURL)
	}
	if h.HealthURL != "http://dsh-ts-abc12xyz:3080/" {
		t.Fatalf("HealthURL=%q", h.HealthURL)
	}

	sec, err := d.client.CoreV1().Secrets("demo").Get(ctx, "dsh-ts-abc12xyz-bootstrap", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if string(sec.Data["settings.yaml"]) != "provider: deepseek\n" {
		t.Fatalf("secret=%q", sec.Data["settings.yaml"])
	}
	ing, err := d.client.NetworkingV1().Ingresses("demo").Get(ctx, "dsh-ts-abc12xyz", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ing.Spec.Rules[0].Host != "env-abc12xyz.example.com" {
		t.Fatalf("host=%q", ing.Spec.Rules[0].Host)
	}
	if ing.Spec.IngressClassName == nil || *ing.Spec.IngressClassName != "nginx" {
		t.Fatalf("class=%v", ing.Spec.IngressClassName)
	}

	if err := d.Stop(ctx, "abc12xyz"); err != nil {
		t.Fatal(err)
	}
	st, err := d.Status(ctx, "abc12xyz")
	if err != nil {
		t.Fatal(err)
	}
	if st != docker.StatusStopped {
		t.Fatalf("status=%s", st)
	}

	listed, err := d.List(ctx)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list %v %d", err, len(listed))
	}

	if err := d.Destroy(ctx, "abc12xyz"); err != nil {
		t.Fatal(err)
	}
	st, err = d.Status(ctx, "abc12xyz")
	if err != nil {
		t.Fatal(err)
	}
	if st != docker.StatusNotFound {
		t.Fatalf("after destroy %s", st)
	}
	ok, err := d.ImageExists(ctx, "anything")
	if err != nil || !ok {
		t.Fatalf("ImageExists %v %v", ok, err)
	}
}

func TestCreatePVCAndDestroyReleases(t *testing.T) {
	d, err := newWithClient(Options{
		Namespace:       "demo",
		EnvHostTemplate: "env-{id}.example.com",
		StorageClass:    "fast-ssd",
		StorageSize:     "5Gi",
		NamePrefix:      docker.NamePrefix,
	}, fake.NewSimpleClientset())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	boot := t.TempDir()
	if err := os.WriteFile(filepath.Join(boot, "settings.yaml"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Create(ctx, docker.Spec{
		ID:     "pvcenv01",
		Image:  "ghcr.io/cocofhu/dsh-testsuite-runtime:0.1.1-rc.2",
		Mounts: []string{boot + ":/bootstrap:ro"},
	}); err != nil {
		t.Fatal(err)
	}
	pvc, err := d.client.CoreV1().PersistentVolumeClaims("demo").Get(ctx, "dsh-ts-pvcenv01-data", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != "fast-ssd" {
		t.Fatalf("sc=%v", pvc.Spec.StorageClassName)
	}
	deploy, err := d.client.AppsV1().Deployments("demo").Get(ctx, "dsh-ts-pvcenv01", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	foundPVC := false
	for _, v := range deploy.Spec.Template.Spec.Volumes {
		if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == "dsh-ts-pvcenv01-data" {
			foundPVC = true
		}
	}
	if !foundPVC {
		t.Fatal("deployment should mount the env PVC")
	}
	if _, err := d.Recreate(ctx, docker.Spec{
		ID:     "pvcenv01",
		Image:  "ghcr.io/cocofhu/dsh-testsuite-runtime:0.1.1-rc.2",
		Mounts: []string{boot + ":/bootstrap:ro"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.client.CoreV1().PersistentVolumeClaims("demo").Get(ctx, "dsh-ts-pvcenv01-data", metav1.GetOptions{}); err != nil {
		t.Fatalf("recreate must keep PVC: %v", err)
	}
	if err := d.Destroy(ctx, "pvcenv01"); err != nil {
		t.Fatal(err)
	}
	_, err = d.client.CoreV1().PersistentVolumeClaims("demo").Get(ctx, "dsh-ts-pvcenv01-data", metav1.GetOptions{})
	if err == nil {
		t.Fatal("destroy should delete PVC")
	}
}

func TestNewRejectsBadTemplate(t *testing.T) {
	_, err := newWithClient(Options{EnvHostTemplate: "no-id.example.com"}, fake.NewSimpleClientset())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCreateSeparateRequests(t *testing.T) {
	d, err := newWithClient(Options{
		Namespace:       "demo",
		EnvHostTemplate: "env-{id}.example.com",
		NamePrefix:      docker.NamePrefix,
		CPUCores:        1,
		MemoryMB:        2048,
		CPURequest:      "250m",
		MemoryRequest:   "512Mi",
	}, fake.NewSimpleClientset())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	boot := t.TempDir()
	if err := os.WriteFile(filepath.Join(boot, "settings.yaml"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Create(ctx, docker.Spec{
		ID:     "reqenv01",
		Image:  "ghcr.io/cocofhu/dsh-testsuite-runtime:0.1.1-rc.2",
		Mounts: []string{boot + ":/bootstrap:ro"},
	}); err != nil {
		t.Fatal(err)
	}
	deploy, err := d.client.AppsV1().Deployments("demo").Get(ctx, "dsh-ts-reqenv01", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	res := deploy.Spec.Template.Spec.Containers[0].Resources
	if got := res.Limits.Cpu().MilliValue(); got != 1000 {
		t.Fatalf("cpu limit=%d", got)
	}
	if got := res.Limits.Memory().Value(); got != 2048*1024*1024 {
		t.Fatalf("mem limit=%d", got)
	}
	if got := res.Requests.Cpu().MilliValue(); got != 250 {
		t.Fatalf("cpu request=%d", got)
	}
	if got := res.Requests.Memory().Value(); got != 512*1024*1024 {
		t.Fatalf("mem request=%d", got)
	}
}

func TestNewRejectsBadRequestQuantity(t *testing.T) {
	_, err := newWithClient(Options{
		EnvHostTemplate: "env-{id}.example.com",
		CPURequest:      "nope",
	}, fake.NewSimpleClientset())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDestroyAtAnnotation(t *testing.T) {
	d := testDriver(t)
	ctx := context.Background()
	boot := t.TempDir()
	if err := os.WriteFile(filepath.Join(boot, "settings.yaml"), []byte("provider: deepseek\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(boot, "plugins.txt"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Create(ctx, docker.Spec{
		ID:     "ttlann01",
		Image:  "ghcr.io/cocofhu/dsh-testsuite-runtime:0.1.1-rc.2",
		Mounts: []string{boot + ":/bootstrap:ro"},
	}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(6 * time.Hour).Truncate(time.Second)
	if err := d.SetDestroyAt(ctx, "ttlann01", at); err != nil {
		t.Fatal(err)
	}
	deploy, err := d.client.AppsV1().Deployments("demo").Get(ctx, "dsh-ts-ttlann01", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	raw := deploy.Annotations[docker.DestroyAtAnnotation]
	if raw != at.Format(time.RFC3339) {
		t.Fatalf("annotation=%q want %s", raw, at.Format(time.RFC3339))
	}
	got, err := d.GetDestroyAt(ctx, "ttlann01")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || !got.Equal(at) {
		t.Fatalf("GetDestroyAt=%v want %v", got, at)
	}
}
