package env

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// dsh 0.2.1 prints "dsh web: http://…/?token=…" once the UI can accept browsers.
var dshWebURLRe = regexp.MustCompile(`dsh web:\s+(https?://\S+)`)

func launchTokenFromLogs(logs string) string {
	matches := dshWebURLRe.FindAllStringSubmatch(logs, -1)
	if len(matches) == 0 {
		return ""
	}
	return launchTokenFromURL(matches[len(matches)-1][1])
}

func launchTokenFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return u.Query().Get("token")
}

func openBase(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	u.RawQuery = ""
	u.Fragment = ""
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String()
}

func sameOpenBase(existing, base string) bool {
	if existing == "" || base == "" {
		return existing == base
	}
	return openBase(existing) == openBase(base)
}

func withLaunchToken(base, token string) string {
	if token == "" || base == "" {
		return base
	}
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *Service) attachLaunchToken(ctx context.Context, rec *Record) bool {
	if rec.Status != StatusRunning || launchTokenFromURL(rec.OpenURL) != "" {
		return false
	}
	base := openBase(rec.OpenURL)
	if base == "" {
		return false
	}
	logs, err := s.drv.Logs(ctx, rec.ID, 4000)
	if err != nil {
		return false
	}
	token := launchTokenFromLogs(logs)
	if token == "" {
		return false
	}
	rec.OpenURL = withLaunchToken(base, token)
	return true
}

func (s *Service) clearPublicURL(id string) {
	_ = os.Remove(filepath.Join(s.envDir(id), "bootstrap", "public-url"))
}

func (s *Service) writePublicURL(rec Record) {
	if s.cfg.IsKubernetes() {
		return
	}
	base := openBase(rec.OpenURL)
	if base == "" {
		return
	}
	path := filepath.Join(s.envDir(rec.ID), "bootstrap", "public-url")
	body := []byte(base + "\n")
	if existing, err := os.ReadFile(path); err == nil && string(existing) == string(body) {
		return
	}
	_ = os.WriteFile(path, body, 0o644)
}
