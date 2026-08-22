package env

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/cocofhu/dsh-testsuite/internal/docker"
)

const (
	HealthHealthy  = "healthy"
	HealthStarting = "starting"
)

const healthProbeTimeout = 800 * time.Millisecond

var healthClient = &http.Client{
	Timeout: healthProbeTimeout,
	Transport: &http.Transport{
		DisableKeepAlives: true,
		DialContext:       (&net.Dialer{Timeout: 400 * time.Millisecond}).DialContext,
	},
}

func (s *Service) withHealth(ctx context.Context, rec Record) View {
	v := rec.ToView()
	v.Health = s.probeHealth(ctx, rec)
	return v
}

func (s *Service) probeHealth(ctx context.Context, rec Record) string {
	if rec.Status != StatusRunning {
		return ""
	}
	target := s.healthTarget(rec)
	if target == "" {
		return HealthStarting
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return HealthStarting
	}
	resp, err := healthClient.Do(req)
	if err != nil {
		return HealthStarting
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return HealthHealthy
	}
	return HealthStarting
}

func (s *Service) healthTarget(rec Record) string {
	if s.cfg.IsKubernetes() {
		return fmt.Sprintf("http://%s%s:%d/", docker.NamePrefix, rec.ID, docker.WebPort)
	}
	if rec.HostPort <= 0 {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d/", rec.HostPort)
}

func (s *Service) attachHealth(ctx context.Context, views []View, recs []Record) {
	var wg sync.WaitGroup
	for i := range views {
		if recs[i].Status != StatusRunning {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			views[i].Health = s.probeHealth(ctx, recs[i])
		}(i)
	}
	wg.Wait()
}
