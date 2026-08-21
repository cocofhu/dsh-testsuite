// Package docker runs dsh environments as containers via the host docker CLI.
package docker

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	// ManagedLabel / ManagedValue tag every environment this control plane creates.
	ManagedLabel = "dsh-testsuite.managed"
	ManagedValue = "1"
	// IDLabel stores the environment id on the container.
	IDLabel = "dsh-testsuite.id"
	// RuntimeLabel marks baked dsh runtime images.
	RuntimeLabel = "dsh-testsuite.runtime"
	// VersionLabel is the dsh npm version baked into a runtime image.
	VersionLabel = "dsh-testsuite.dsh-version"
	// WebPort is the in-container dsh web listen port.
	WebPort = 3080
	// NamePrefix is prepended to the environment id to form the container name.
	NamePrefix = "dsh-ts-"
)

// Status is a container lifecycle state.
type Status string

const (
	StatusRunning  Status = "running"
	StatusStopped  Status = "stopped"
	StatusPending  Status = "pending"
	StatusNotFound Status = "not_found"
)

// cmdRunner executes docker CLI commands (overridable in unit tests).
type cmdRunner func(ctx context.Context, timeout time.Duration, args ...string) (string, error)

// Driver publishes one host port per environment and labels containers for reconcile.
type Driver struct {
	bindIP     string
	network    string
	namePrefix string
	cpuCores   float64
	memoryMB   int64
	run        cmdRunner
}

// Options configures the docker driver.
type Options struct {
	BindIP     string
	Network    string
	NamePrefix string
	CPUCores   float64
	MemoryMB   int64
}

// Spec describes one environment container to create.
type Spec struct {
	ID       string
	Image    string
	Env      map[string]string
	Mounts   []string
	Labels   map[string]string
	CPU      float64
	MemoryMB int64
}

// Handle is the driver's view of a live container.
type Handle struct {
	ID        string
	Name      string
	Status    Status
	Endpoints map[int]string
	CreatedAt time.Time
}

// Image is one locally baked runtime image.
type Image struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	ID         string `json:"id"`
	Version    string `json:"version"`
	CreatedAt  string `json:"createdAt"`
	Size       string `json:"size"`
}

// New builds a docker driver.
func New(o Options) *Driver {
	if o.BindIP == "" {
		o.BindIP = "127.0.0.1"
	}
	if o.NamePrefix == "" {
		o.NamePrefix = NamePrefix
	}
	return &Driver{
		bindIP:     o.BindIP,
		network:    o.Network,
		namePrefix: o.NamePrefix,
		cpuCores:   o.CPUCores,
		memoryMB:   o.MemoryMB,
		run:        run,
	}
}

func (d *Driver) Name() string { return "docker" }

func (d *Driver) containerName(id string) string { return d.namePrefix + id }

// Create starts a container and returns once docker run succeeds and the web
// port is published.
func (d *Driver) Create(ctx context.Context, spec Spec) (*Handle, error) {
	if strings.TrimSpace(spec.ID) == "" {
		return nil, fmt.Errorf("spec.ID is required")
	}
	if strings.TrimSpace(spec.Image) == "" {
		return nil, fmt.Errorf("spec.Image is required")
	}
	name := d.containerName(spec.ID)
	args := []string{
		"run", "-d", "--name", name,
		"--add-host", "host.docker.internal:host-gateway",
		"--label", ManagedLabel + "=" + ManagedValue,
		"--label", IDLabel + "=" + spec.ID,
	}
	cpu := spec.CPU
	if cpu <= 0 {
		cpu = d.cpuCores
	}
	mem := spec.MemoryMB
	if mem <= 0 {
		mem = d.memoryMB
	}
	if cpu > 0 {
		args = append(args, "--cpus", fmt.Sprintf("%.2f", cpu))
	}
	if mem > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", mem))
	}
	args = append(args, "-p", fmt.Sprintf("%s::%d", d.bindIP, WebPort))
	if d.network != "" {
		args = append(args, "--network", d.network)
	}
	for k, v := range spec.Labels {
		args = append(args, "--label", k+"="+v)
	}
	for k, v := range spec.Env {
		args = append(args, "-e", k+"="+v)
	}
	for _, mt := range spec.Mounts {
		args = append(args, "-v", mt)
	}
	args = append(args, spec.Image)

	if _, err := d.run(ctx, 90*time.Second, args...); err != nil {
		return nil, fmt.Errorf("docker run: %w", err)
	}
	eps, err := d.endpoints(ctx, name)
	if err != nil {
		_ = d.destroyByName(context.Background(), name)
		return nil, err
	}
	return &Handle{
		ID:        spec.ID,
		Name:      name,
		Status:    StatusRunning,
		Endpoints: eps,
	}, nil
}

// Start resumes a stopped container.
func (d *Driver) Start(ctx context.Context, id string) error {
	_, err := d.run(ctx, 30*time.Second, "start", d.containerName(id))
	return err
}

// Stop stops a container but keeps it.
func (d *Driver) Stop(ctx context.Context, id string) error {
	_, err := d.run(ctx, 30*time.Second, "stop", d.containerName(id))
	return err
}

// Destroy removes the container and its anonymous volumes.
func (d *Driver) Destroy(ctx context.Context, id string) error {
	return d.destroyByName(ctx, d.containerName(id))
}

func (d *Driver) destroyByName(ctx context.Context, name string) error {
	_, err := d.run(ctx, 30*time.Second, "rm", "-f", "-v", name)
	if isNoSuchContainer(err) {
		return nil
	}
	return err
}

// Get refreshes status and endpoints.
func (d *Driver) Get(ctx context.Context, id string) (*Handle, error) {
	name := d.containerName(id)
	st, err := d.status(ctx, name)
	if err != nil {
		return nil, err
	}
	h := &Handle{ID: id, Name: name, Status: st}
	if st == StatusRunning {
		if eps, err := d.endpoints(ctx, name); err == nil {
			h.Endpoints = eps
		}
	}
	return h, nil
}

// List returns every container this driver manages.
func (d *Driver) List(ctx context.Context) ([]*Handle, error) {
	out, err := d.run(ctx, 15*time.Second, "ps", "-a",
		"--filter", "label="+ManagedLabel+"="+ManagedValue,
		"--format", "{{.Names}}\t{{.State}}\t{{.Label \""+IDLabel+"\"}}\t{{.CreatedAt}}")
	if err != nil {
		return nil, err
	}
	var handles []*Handle
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		id := ""
		if len(parts) >= 3 {
			id = strings.TrimSpace(parts[2])
		}
		if id == "" {
			id = strings.TrimPrefix(name, d.namePrefix)
		}
		h := &Handle{
			ID:     id,
			Name:   name,
			Status: mapState(strings.TrimSpace(parts[1])),
		}
		if len(parts) >= 4 {
			h.CreatedAt = parseDockerCreatedAt(parts[3])
		}
		handles = append(handles, h)
	}
	return handles, nil
}

// Status returns just the lifecycle status.
func (d *Driver) Status(ctx context.Context, id string) (Status, error) {
	return d.status(ctx, d.containerName(id))
}

func (d *Driver) status(ctx context.Context, name string) (Status, error) {
	out, err := d.run(ctx, 10*time.Second, "inspect", "--format", "{{.State.Status}}", name)
	if err != nil {
		if isNoSuchContainer(err) {
			return StatusNotFound, nil
		}
		return StatusNotFound, err
	}
	return mapState(strings.TrimSpace(out)), nil
}

// Endpoints returns container-port -> "host:port".
func (d *Driver) Endpoints(ctx context.Context, id string) (map[int]string, error) {
	return d.endpoints(ctx, d.containerName(id))
}

// Logs returns combined PID1 stdout/stderr.
func (d *Driver) Logs(ctx context.Context, id string, tail int) (string, error) {
	if tail <= 0 {
		tail = 5000
	}
	name := d.containerName(id)
	out, err := d.run(ctx, 30*time.Second, "logs", "--tail", strconv.Itoa(tail), name)
	if err != nil {
		if isNoSuchContainer(err) {
			return "", fmt.Errorf("environment %s not found", id)
		}
		return "", fmt.Errorf("docker logs: %w", err)
	}
	return out, nil
}

func (d *Driver) endpoints(ctx context.Context, name string) (map[int]string, error) {
	hp, err := d.hostPort(ctx, name, WebPort)
	if err != nil {
		return nil, err
	}
	return map[int]string{WebPort: fmt.Sprintf("%s:%d", d.bindIP, hp)}, nil
}

func (d *Driver) hostPort(ctx context.Context, name string, containerPort int) (int, error) {
	format := fmt.Sprintf(`{{(index (index .NetworkSettings.Ports "%d/tcp") 0).HostPort}}`, containerPort)
	out, err := d.run(ctx, 10*time.Second, "inspect", "--format", format, name)
	if err != nil {
		return 0, fmt.Errorf("docker inspect port %d: %w", containerPort, err)
	}
	var port int
	if _, err := fmt.Sscanf(strings.TrimSpace(out), "%d", &port); err != nil || port == 0 {
		return 0, fmt.Errorf("parse host port from %q", out)
	}
	return port, nil
}

// ImageExists reports whether repository:tag is present locally.
func (d *Driver) ImageExists(ctx context.Context, ref string) (bool, error) {
	_, err := d.run(ctx, 15*time.Second, "image", "inspect", ref)
	if err != nil {
		if isNoSuchImage(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ImagePull fetches a registry image onto the host.
func (d *Driver) ImagePull(ctx context.Context, ref string) error {
	_, err := d.run(ctx, 15*time.Minute, "pull", ref)
	if err != nil {
		return fmt.Errorf("docker pull %s: %w", ref, err)
	}
	return nil
}

// ImageTag adds dst as another name for an image already present as src.
func (d *Driver) ImageTag(ctx context.Context, src, dst string) error {
	if src == "" || dst == "" {
		return fmt.Errorf("docker tag: src and dst are required")
	}
	if src == dst {
		return nil
	}
	_, err := d.run(ctx, 30*time.Second, "tag", src, dst)
	if err != nil {
		return fmt.Errorf("docker tag %s %s: %w", src, dst, err)
	}
	return nil
}

// ListImages returns baked runtime images (label dsh-testsuite.runtime=1).
func (d *Driver) ListImages(ctx context.Context) ([]Image, error) {
	out, err := d.run(ctx, 20*time.Second, "images",
		"--filter", "label="+RuntimeLabel+"=1",
		"--format", "{{.Repository}}\t{{.Tag}}\t{{.ID}}\t{{.CreatedAt}}\t{{.Size}}")
	if err != nil {
		return nil, err
	}
	var images []Image
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		tag := strings.TrimSpace(parts[1])
		if tag == "<none>" {
			continue
		}
		img := Image{
			Repository: strings.TrimSpace(parts[0]),
			Tag:        tag,
			ID:         strings.TrimSpace(parts[2]),
			Version:    tag,
		}
		if len(parts) >= 4 {
			img.CreatedAt = strings.TrimSpace(parts[3])
		}
		if len(parts) >= 5 {
			img.Size = strings.TrimSpace(parts[4])
		}
		images = append(images, img)
	}
	return images, nil
}

func parseDockerCreatedAt(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05 -0700 MST",
		time.RFC3339Nano,
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func mapState(s string) Status {
	switch s {
	case "running":
		return StatusRunning
	case "created", "restarting", "paused":
		return StatusPending
	case "exited", "dead", "stopped":
		return StatusStopped
	case "", "not_found":
		return StatusNotFound
	default:
		return StatusStopped
	}
}

func isNoSuchContainer(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such container")
}

func isNoSuchImage(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such image") || strings.Contains(msg, "not found")
}

func run(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return strings.TrimSpace(stdout.String() + "\n" + stderr.String()), fmt.Errorf("%w: %s", err, msg)
	}
	return strings.TrimSpace(stdout.String() + "\n" + stderr.String()), nil
}

// HostPortFromEndpoints extracts the published web port from a handle.
func HostPortFromEndpoints(eps map[int]string) int {
	if eps == nil {
		return 0
	}
	addr, ok := eps[WebPort]
	if !ok {
		return 0
	}
	_, port, ok := strings.Cut(addr, ":")
	if !ok {
		return 0
	}
	n, _ := strconv.Atoi(port)
	return n
}
