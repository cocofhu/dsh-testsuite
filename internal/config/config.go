// Package config loads the control-plane YAML configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration wraps time.Duration so YAML can carry values like "2h".
type Duration time.Duration

// UnmarshalYAML parses a Go duration string.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// D returns the wrapped duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

// Config is the whole control-plane configuration.
type Config struct {
	Server Server `yaml:"server"`
	Docker Docker `yaml:"docker"`
	Limits Limits `yaml:"limits"`

	// Dir is the directory the config file lived in. Relative paths resolve
	// against it (or the process cwd when the config was not loaded from a file).
	Dir string `yaml:"-"`
}

// Server is the HTTP listener and how env Open URLs are advertised.
type Server struct {
	Listen     string `yaml:"listen"`
	PublicHost string `yaml:"publicHost"`
	WebDir     string `yaml:"webDir"`
}

// Docker selects the runtime image repository and how containers are published.
type Docker struct {
	BindIP          string  `yaml:"bindIP"`
	Network         string  `yaml:"network"`
	ImageRepository string  `yaml:"imageRepository"`
	CPUCores        float64 `yaml:"cpuCores"`
	MemoryMB        int64   `yaml:"memoryMB"`
}

// Limits cap concurrent live environments and idle lifetime.
type Limits struct {
	MaxEnvs int      `yaml:"maxEnvs"`
	IdleTTL Duration `yaml:"idleTTL"`
}

// Default returns a config that publishes on loopback and looks for ./image.
func Default() *Config {
	return &Config{
		Server: Server{
			Listen: ":8090",
			WebDir: "web",
		},
		Docker: Docker{
			BindIP:          "127.0.0.1",
			ImageRepository: "dsh-testsuite-runtime",
		},
		Limits: Limits{
			MaxEnvs: 8,
			IdleTTL: Duration(2 * time.Hour),
		},
	}
}

// Load reads path (a file, or a directory containing config.yaml). Empty path
// returns Default.
func Load(path string) (*Config, error) {
	cfg := Default()
	path = strings.TrimSpace(path)
	if path == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		cfg.Dir = cwd
		cfg.resolve()
		return cfg, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	file := path
	dir := filepath.Dir(path)
	if info.IsDir() {
		dir = path
		file = filepath.Join(path, "config.yaml")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err)
	}
	cfg.Dir = dir
	cfg.applyDefaults()
	cfg.resolve()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) applyDefaults() {
	def := Default()
	if strings.TrimSpace(c.Server.Listen) == "" {
		c.Server.Listen = def.Server.Listen
	}
	if strings.TrimSpace(c.Server.WebDir) == "" {
		c.Server.WebDir = def.Server.WebDir
	}
	if strings.TrimSpace(c.Docker.BindIP) == "" {
		c.Docker.BindIP = def.Docker.BindIP
	}
	if strings.TrimSpace(c.Docker.ImageRepository) == "" {
		c.Docker.ImageRepository = def.Docker.ImageRepository
	}
	if c.Limits.MaxEnvs <= 0 {
		c.Limits.MaxEnvs = def.Limits.MaxEnvs
	}
	if c.Limits.IdleTTL <= 0 {
		c.Limits.IdleTTL = def.Limits.IdleTTL
	}
}

func (c *Config) resolve() {
	c.Server.WebDir = c.abs(c.Server.WebDir)
}

func (c *Config) abs(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	base := c.Dir
	if base == "" {
		base, _ = os.Getwd()
	}
	return filepath.Clean(filepath.Join(base, p))
}

// Validate checks required fields after defaults are applied.
func (c *Config) Validate() error {
	if c == nil {
		return fmt.Errorf("nil config")
	}
	if strings.TrimSpace(c.Docker.ImageRepository) == "" {
		return fmt.Errorf("docker.imageRepository is required")
	}
	if strings.ContainsAny(c.Docker.ImageRepository, " \t\n") {
		return fmt.Errorf("docker.imageRepository must not contain whitespace")
	}
	if c.Limits.MaxEnvs <= 0 {
		return fmt.Errorf("limits.maxEnvs must be positive")
	}
	return nil
}

// PublicHost returns the host used in Open URLs. 0.0.0.0 / :: bind addresses
// are advertised as 127.0.0.1 unless publicHost is set.
func (c *Config) PublicHost() string {
	if h := strings.TrimSpace(c.Server.PublicHost); h != "" {
		return h
	}
	ip := strings.TrimSpace(c.Docker.BindIP)
	switch ip {
	case "", "0.0.0.0", "::", "[::]":
		return "127.0.0.1"
	default:
		return ip
	}
}

// ImageRef returns repository:version for a baked dsh runtime image.
func (c *Config) ImageRef(version string) string {
	return c.Docker.ImageRepository + ":" + strings.TrimSpace(version)
}
