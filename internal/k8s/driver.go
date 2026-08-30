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
	"time"

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
	StorageClass    string
	StorageSize     string
	NamePrefix      string
	CPUCores        float64
	MemoryMB        int64
	CPURequest      string
	MemoryRequest   string
}

// Driver creates one Deployment + Service + Ingress + bootstrap Secret per env.
type Driver struct {
	client          kubernetes.Interface
	namespace       string
	hostTemplate    string
	ingressClass    string
	imagePullPolicy corev1.PullPolicy
	storageClass    string
	storageSize     string
	namePrefix      string
	cpuCores        float64
	memoryMB        int64
	cpuRequest      *resource.Quantity
	memoryRequest   *resource.Quantity
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
	cpuReq, err := parseQuantityPtr(o.CPURequest)
	if err != nil {
		return nil, fmt.Errorf("kubernetes.cpuRequest: %w", err)
	}
	memReq, err := parseQuantityPtr(o.MemoryRequest)
	if err != nil {
		return nil, fmt.Errorf("kubernetes.memoryRequest: %w", err)
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
		storageClass:    strings.TrimSpace(o.StorageClass),
		storageSize:     strings.TrimSpace(o.StorageSize),
		namePrefix:      prefix,
		cpuCores:        o.CPUCores,
		memoryMB:        o.MemoryMB,
		cpuRequest:      cpuReq,
		memoryRequest:   memReq,
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

func (d *Driver) pvcName(id string) string { return d.resourceName(id) + "-data" }

func (d *Driver) usePVC() bool { return d.storageClass != "" }

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

	if err := d.ensurePVC(ctx, spec.ID, labels); err != nil {
		return nil, err
	}
	if _, err := d.client.CoreV1().Secrets(d.namespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("create secret: %w", err)
	}
	if _, err := d.client.AppsV1().Deployments(d.namespace).Create(ctx, deploy, metav1.CreateOptions{}); err != nil {
		_ = d.teardownWorkload(context.Background(), spec.ID)
		return nil, fmt.Errorf("create deployment: %w", err)
	}
	if _, err := d.client.CoreV1().Services(d.namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil {
		_ = d.teardownWorkload(context.Background(), spec.ID)
		return nil, fmt.Errorf("create service: %w", err)
	}
	if _, err := d.client.NetworkingV1().Ingresses(d.namespace).Create(ctx, ing, metav1.CreateOptions{}); err != nil {
		_ = d.teardownWorkload(context.Background(), spec.ID)
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

// SetDestroyAt writes the idle-destroy deadline onto the Deployment annotation.
func (d *Driver) SetDestroyAt(ctx context.Context, id string, at time.Time) error {
	name := d.resourceName(id)
	deploy, err := d.client.AppsV1().Deployments(d.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if deploy.Annotations == nil {
		deploy.Annotations = map[string]string{}
	}
	deploy.Annotations[docker.DestroyAtAnnotation] = at.UTC().Format(time.RFC3339)
	_, err = d.client.AppsV1().Deployments(d.namespace).Update(ctx, deploy, metav1.UpdateOptions{})
	return err
}

// GetDestroyAt reads the idle-destroy deadline from the Deployment annotation.
func (d *Driver) GetDestroyAt(ctx context.Context, id string) (*time.Time, error) {
	name := d.resourceName(id)
	deploy, err := d.client.AppsV1().Deployments(d.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if deploy.Annotations == nil {
		return nil, nil
	}
	raw := strings.TrimSpace(deploy.Annotations[docker.DestroyAtAnnotation])
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", docker.DestroyAtAnnotation, err)
	}
	t = t.UTC()
	return &t, nil
}

func (d *Driver) Destroy(ctx context.Context, id string) error {
	if err := d.teardownWorkload(ctx, id); err != nil {
		return err
	}
	return d.DestroyStorage(ctx, id)
}

func (d *Driver) teardownWorkload(ctx context.Context, id string) error {
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

// DestroyStorage deletes the environment PVC so the volume is released (no retain / named-LUN reuse).
func (d *Driver) DestroyStorage(ctx context.Context, id string) error {
	if !d.usePVC() {
		return nil
	}
	err := d.client.CoreV1().PersistentVolumeClaims(d.namespace).Delete(ctx, d.pvcName(id), metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (d *Driver) Recreate(ctx context.Context, spec docker.Spec) (*docker.Handle, error) {
	if err := d.teardownWorkload(ctx, spec.ID); err != nil {
		return nil, err
	}
	return d.Create(ctx, spec)
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
	resources := d.containerResources(cpu, memoryMB)
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
						Resources:    resources,
						VolumeMounts: d.volumeMounts(),
					}},
					Volumes: d.volumes(spec.ID),
				},
			},
		},
	}
}

func (d *Driver) volumeMounts() []corev1.VolumeMount {
	mounts := []corev1.VolumeMount{
		{Name: bootstrapVol, MountPath: "/bootstrap", ReadOnly: true},
	}
	if d.usePVC() {
		return append(mounts,
			corev1.VolumeMount{Name: "data", MountPath: "/data/dsh", SubPath: "home"},
			corev1.VolumeMount{Name: "data", MountPath: "/workspace", SubPath: "workspace"},
		)
	}
	return append(mounts,
		corev1.VolumeMount{Name: homeVol, MountPath: "/data/dsh"},
		corev1.VolumeMount{Name: workspaceVol, MountPath: "/workspace"},
	)
}

func (d *Driver) volumes(id string) []corev1.Volume {
	vols := []corev1.Volume{{
		Name: bootstrapVol,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{SecretName: d.secretName(id)},
		},
	}}
	if d.usePVC() {
		return append(vols, corev1.Volume{
			Name: "data",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: d.pvcName(id)},
			},
		})
	}
	return append(vols,
		corev1.Volume{Name: homeVol, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		corev1.Volume{Name: workspaceVol, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	)
}

func (d *Driver) ensurePVC(ctx context.Context, id string, labels map[string]string) error {
	if !d.usePVC() {
		return nil
	}
	size := d.storageSize
	if size == "" {
		size = "10Gi"
	}
	qty, err := resource.ParseQuantity(size)
	if err != nil {
		return fmt.Errorf("kubernetes.storageSize %q: %w", size, err)
	}
	sc := d.storageClass
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      d.pvcName(id),
			Namespace: d.namespace,
			Labels:    labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: &sc,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: qty},
			},
		},
	}
	_, err = d.client.CoreV1().PersistentVolumeClaims(d.namespace).Create(ctx, pvc, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create pvc: %w", err)
	}
	return nil
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

func (d *Driver) containerResources(cpu float64, memoryMB int64) corev1.ResourceRequirements {
	limits := resourceList(cpu, memoryMB)
	if len(limits) == 0 && d.cpuRequest == nil && d.memoryRequest == nil {
		return corev1.ResourceRequirements{}
	}
	requests := cloneResourceList(limits)
	if d.cpuRequest != nil {
		requests[corev1.ResourceCPU] = d.cpuRequest.DeepCopy()
	}
	if d.memoryRequest != nil {
		requests[corev1.ResourceMemory] = d.memoryRequest.DeepCopy()
	}
	return corev1.ResourceRequirements{Limits: limits, Requests: requests}
}

func cloneResourceList(in corev1.ResourceList) corev1.ResourceList {
	out := make(corev1.ResourceList, len(in))
	for k, v := range in {
		out[k] = v.DeepCopy()
	}
	return out
}

func parseQuantityPtr(s string) (*resource.Quantity, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return nil, err
	}
	return &q, nil
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
