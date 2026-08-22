// Package k8s runs dsh environments as in-cluster Deployment/Service/Ingress.
package k8s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/cocofhu/dsh-testsuite/internal/docker"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	bootstrapVol = "bootstrap"
	homeVol      = "dsh-home"
	workspaceVol = "workspace"
)

// Options configures the kubernetes driver.
type Options struct {
	Namespace       string
	Kubeconfig      string
	EnvHostTemplate string
	IngressClass    string
	ImagePullPolicy string
	NamePrefix      string
	CPUCores        float64
	MemoryMB        int64
}

// Driver creates one Deployment + Service + Ingress + bootstrap Secret per env.
type Driver struct {
	client          kubernetes.Interface
	namespace       string
	hostTemplate    string
	ingressClass    string
	imagePullPolicy corev1.PullPolicy
	namePrefix      string
	cpuCores        float64
	memoryMB        int64
}

// New talks to the cluster via kubeconfig, or in-cluster config when empty.
func New(o Options) (*Driver, error) {
	cfg, err := restConfig(o.Kubeconfig)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kubernetes client: %w", err)
	}
	return newWithClient(o, client)
}

func restConfig(kubeconfig string) (*rest.Config, error) {
	kubeconfig = strings.TrimSpace(kubeconfig)
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("kubernetes in-cluster config: %w", err)
	}
	return cfg, nil
}

func newWithClient(o Options, client kubernetes.Interface) (*Driver, error) {
	if strings.TrimSpace(o.EnvHostTemplate) == "" || !strings.Contains(o.EnvHostTemplate, "{id}") {
		return nil, fmt.Errorf("kubernetes.envHostTemplate is required and must contain {id}")
	}
	ns := strings.TrimSpace(o.Namespace)
	if ns == "" {
		ns = currentNamespace()
	}
	prefix := o.NamePrefix
	if prefix == "" {
		prefix = docker.NamePrefix
	}
	policy := corev1.PullIfNotPresent
	switch strings.ToLower(strings.TrimSpace(o.ImagePullPolicy)) {
	case "always":
		policy = corev1.PullAlways
	case "never":
		policy = corev1.PullNever
	case "", "ifnotpresent":
		policy = corev1.PullIfNotPresent
	default:
		policy = corev1.PullPolicy(o.ImagePullPolicy)
	}
	return &Driver{
		client:          client,
		namespace:       ns,
		hostTemplate:    strings.TrimSpace(o.EnvHostTemplate),
		ingressClass:    strings.TrimSpace(o.IngressClass),
		imagePullPolicy: policy,
		namePrefix:      prefix,
		cpuCores:        o.CPUCores,
		memoryMB:        o.MemoryMB,
	}, nil
}

func currentNamespace() string {
	raw, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err == nil {
		if ns := strings.TrimSpace(string(raw)); ns != "" {
			return ns
		}
	}
	return "default"
}

func (d *Driver) Name() string { return "kubernetes" }

func (d *Driver) resourceName(id string) string { return d.namePrefix + id }

func (d *Driver) secretName(id string) string { return d.resourceName(id) + "-bootstrap" }

func (d *Driver) envHost(id string) string {
	return strings.ReplaceAll(d.hostTemplate, "{id}", id)
}

func (d *Driver) handle(id string, st docker.Status) *docker.Handle {
	host := d.envHost(id)
	name := d.resourceName(id)
	return &docker.Handle{
		ID:        id,
		Name:      name,
		Status:    st,
		OpenURL:   "http://" + host + "/",
		HealthURL: fmt.Sprintf("http://%s:%d/", name, docker.WebPort),
		Endpoints: map[int]string{docker.WebPort: host + ":80"},
	}
}

// Create writes a bootstrap Secret, Deployment, Service, and Ingress.
func (d *Driver) Create(ctx context.Context, spec docker.Spec) (*docker.Handle, error) {
	if strings.TrimSpace(spec.ID) == "" {
		return nil, fmt.Errorf("spec.ID is required")
	}
	if strings.TrimSpace(spec.Image) == "" {
		return nil, fmt.Errorf("spec.Image is required")
	}
	files, err := readBootstrap(spec.Mounts)
	if err != nil {
		return nil, err
	}
	labels := map[string]string{
		docker.ManagedLabel: docker.ManagedValue,
		docker.IDLabel:      spec.ID,
		"app":               d.resourceName(spec.ID),
	}
	for k, v := range spec.Labels {
		labels[k] = v
	}
	cpu := spec.CPU
	if cpu <= 0 {
		cpu = d.cpuCores
	}
	mem := spec.MemoryMB
	if mem <= 0 {
		mem = d.memoryMB
	}

	secret := d.bootstrapSecret(spec.ID, labels, files)
	deploy := d.deployment(spec, labels, cpu, mem)
	svc := d.service(spec.ID, labels)
	ing := d.ingress(spec.ID, labels)

	if _, err := d.client.CoreV1().Secrets(d.namespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("create secret: %w", err)
	}
	if _, err := d.client.AppsV1().Deployments(d.namespace).Create(ctx, deploy, metav1.CreateOptions{}); err != nil {
		_ = d.Destroy(context.Background(), spec.ID)
		return nil, fmt.Errorf("create deployment: %w", err)
	}
	if _, err := d.client.CoreV1().Services(d.namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil {
		_ = d.Destroy(context.Background(), spec.ID)
		return nil, fmt.Errorf("create service: %w", err)
	}
	if _, err := d.client.NetworkingV1().Ingresses(d.namespace).Create(ctx, ing, metav1.CreateOptions{}); err != nil {
		_ = d.Destroy(context.Background(), spec.ID)
		return nil, fmt.Errorf("create ingress: %w", err)
	}
	return d.handle(spec.ID, docker.StatusRunning), nil
}

func (d *Driver) Start(ctx context.Context, id string) error {
	return d.scale(ctx, id, 1)
}

func (d *Driver) Stop(ctx context.Context, id string) error {
	return d.scale(ctx, id, 0)
}

func (d *Driver) scale(ctx context.Context, id string, replicas int32) error {
	name := d.resourceName(id)
	deploy, err := d.client.AppsV1().Deployments(d.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	deploy.Spec.Replicas = &replicas
	_, err = d.client.AppsV1().Deployments(d.namespace).Update(ctx, deploy, metav1.UpdateOptions{})
	return err
}

func (d *Driver) Destroy(ctx context.Context, id string) error {
	name := d.resourceName(id)
	prop := metav1.DeletePropagationBackground
	opts := metav1.DeleteOptions{PropagationPolicy: &prop}
	errs := []error{
		d.client.NetworkingV1().Ingresses(d.namespace).Delete(ctx, name, opts),
		d.client.CoreV1().Services(d.namespace).Delete(ctx, name, opts),
		d.client.AppsV1().Deployments(d.namespace).Delete(ctx, name, opts),
		d.client.CoreV1().Secrets(d.namespace).Delete(ctx, d.secretName(id), opts),
	}
	for _, err := range errs {
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (d *Driver) Get(ctx context.Context, id string) (*docker.Handle, error) {
	st, err := d.status(ctx, id)
	if err != nil {
		return nil, err
	}
	return d.handle(id, st), nil
}

func (d *Driver) List(ctx context.Context) ([]*docker.Handle, error) {
	list, err := d.client.AppsV1().Deployments(d.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: docker.ManagedLabel + "=" + docker.ManagedValue,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*docker.Handle, 0, len(list.Items))
	for i := range list.Items {
		id := list.Items[i].Labels[docker.IDLabel]
		if id == "" {
			id = strings.TrimPrefix(list.Items[i].Name, d.namePrefix)
		}
		h := d.handle(id, mapDeploy(&list.Items[i]))
		h.CreatedAt = list.Items[i].CreationTimestamp.Time
		out = append(out, h)
	}
	return out, nil
}

func (d *Driver) Endpoints(_ context.Context, id string) (map[int]string, error) {
	return d.handle(id, docker.StatusRunning).Endpoints, nil
}

func (d *Driver) Logs(ctx context.Context, id string, tail int) (string, error) {
	if tail <= 0 {
		tail = 5000
	}
	pods, err := d.client.CoreV1().Pods(d.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: docker.IDLabel + "=" + id,
	})
	if err != nil {
		return "", err
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("environment %s not found", id)
	}
	pod := pickPod(pods.Items)
	n := int64(tail)
	req := d.client.CoreV1().Pods(d.namespace).GetLogs(pod.Name, &corev1.PodLogOptions{TailLines: &n})
	raw, err := req.Do(ctx).Raw()
	if err != nil {
		return "", fmt.Errorf("pod logs: %w", err)
	}
	return string(raw), nil
}

func pickPod(pods []corev1.Pod) corev1.Pod {
	for _, p := range pods {
		if p.Status.Phase == corev1.PodRunning {
			return p
		}
	}
	return pods[0]
}

func (d *Driver) Status(ctx context.Context, id string) (docker.Status, error) {
	return d.status(ctx, id)
}

func (d *Driver) status(ctx context.Context, id string) (docker.Status, error) {
	deploy, err := d.client.AppsV1().Deployments(d.namespace).Get(ctx, d.resourceName(id), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return docker.StatusNotFound, nil
		}
		return docker.StatusNotFound, err
	}
	return mapDeploy(deploy), nil
}

func mapDeploy(d *appsv1.Deployment) docker.Status {
	if d.Spec.Replicas != nil && *d.Spec.Replicas == 0 {
		return docker.StatusStopped
	}
	if d.Status.AvailableReplicas > 0 {
		return docker.StatusRunning
	}
	return docker.StatusPending
}

func (d *Driver) ImageExists(context.Context, string) (bool, error) { return true, nil }

func (d *Driver) ImagePull(context.Context, string) error { return nil }

func (d *Driver) ImageTag(context.Context, string, string) error { return nil }

func (d *Driver) bootstrapSecret(id string, labels map[string]string, files map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      d.secretName(id),
			Namespace: d.namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: files,
	}
}

func (d *Driver) deployment(spec docker.Spec, labels map[string]string, cpu float64, memoryMB int64) *appsv1.Deployment {
	name := d.resourceName(spec.ID)
	replicas := int32(1)
	envVars := envList(spec.Env)
	resources := corev1.ResourceRequirements{}
	if req := resourceList(cpu, memoryMB); len(req) > 0 {
		resources.Limits = req
		resources.Requests = req
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: d.namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:            "dsh",
						Image:           spec.Image,
						ImagePullPolicy: d.imagePullPolicy,
						Env:             envVars,
						Ports: []corev1.ContainerPort{{
							Name:          "http",
							ContainerPort: int32(docker.WebPort),
						}},
						Resources: resources,
						VolumeMounts: []corev1.VolumeMount{
							{Name: bootstrapVol, MountPath: "/bootstrap", ReadOnly: true},
							{Name: homeVol, MountPath: "/data/dsh"},
							{Name: workspaceVol, MountPath: "/workspace"},
						},
					}},
					Volumes: []corev1.Volume{
						{
							Name: bootstrapVol,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{SecretName: d.secretName(spec.ID)},
							},
						},
						{Name: homeVol, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						{Name: workspaceVol, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					},
				},
			},
		},
	}
}

func (d *Driver) service(id string, labels map[string]string) *corev1.Service {
	name := d.resourceName(id)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: d.namespace, Labels: labels},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": name},
			Ports: []corev1.ServicePort{{
				Name:       "http",
				Port:       int32(docker.WebPort),
				TargetPort: intstr.FromString("http"),
			}},
		},
	}
}

func (d *Driver) ingress(id string, labels map[string]string) *netv1.Ingress {
	name := d.resourceName(id)
	pathType := netv1.PathTypePrefix
	ing := &netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: d.namespace, Labels: labels},
		Spec: netv1.IngressSpec{
			Rules: []netv1.IngressRule{{
				Host: d.envHost(id),
				IngressRuleValue: netv1.IngressRuleValue{
					HTTP: &netv1.HTTPIngressRuleValue{
						Paths: []netv1.HTTPIngressPath{{
							Path:     "/",
							PathType: &pathType,
							Backend: netv1.IngressBackend{
								Service: &netv1.IngressServiceBackend{
									Name: name,
									Port: netv1.ServiceBackendPort{Name: "http"},
								},
							},
						}},
					},
				},
			}},
		},
	}
	if d.ingressClass != "" {
		ing.Spec.IngressClassName = &d.ingressClass
	}
	return ing
}

func envList(env map[string]string) []corev1.EnvVar {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]corev1.EnvVar, 0, len(keys))
	for _, k := range keys {
		out = append(out, corev1.EnvVar{Name: k, Value: env[k]})
	}
	return out
}

func resourceList(cpu float64, memoryMB int64) corev1.ResourceList {
	rl := corev1.ResourceList{}
	if cpu > 0 {
		rl[corev1.ResourceCPU] = resource.MustParse(strconv.Itoa(int(cpu*1000)) + "m")
	}
	if memoryMB > 0 {
		rl[corev1.ResourceMemory] = resource.MustParse(fmt.Sprintf("%dMi", memoryMB))
	}
	return rl
}

func readBootstrap(mounts []string) (map[string][]byte, error) {
	files := map[string][]byte{}
	for _, m := range mounts {
		src, dest, ok := splitMount(m)
		if !ok || dest != "/bootstrap" {
			continue
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return nil, fmt.Errorf("read bootstrap %s: %w", src, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(src, e.Name()))
			if err != nil {
				return nil, err
			}
			files[e.Name()] = raw
		}
	}
	return files, nil
}

func splitMount(m string) (src, dest string, ok bool) {
	// hostPath:containerPath[:ro]
	parts := strings.Split(m, ":")
	if len(parts) < 2 {
		return "", "", false
	}
	src = parts[0]
	dest = parts[1]
	return src, dest, src != "" && dest != ""
}
