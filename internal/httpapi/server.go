// Package httpapi serves the environment manager REST API and static UI.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cocofhu/dsh-testsuite/internal/env"
	"github.com/cocofhu/dsh-testsuite/internal/settings"
	"github.com/rs/zerolog"
)

// Server is the HTTP surface.
type Server struct {
	svc    *env.Service
	webDir string
	log    zerolog.Logger
	mux    *http.ServeMux
}

// New builds the mux. webDir is the static UI root (index.html).
func New(svc *env.Service, webDir string, log zerolog.Logger) *Server {
	s := &Server{svc: svc, webDir: webDir, log: log, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.healthz)
	s.mux.HandleFunc("GET /api/environments", s.listEnvs)
	s.mux.HandleFunc("POST /api/environments", s.createEnv)
	s.mux.HandleFunc("GET /api/environments/{id}", s.getEnv)
	s.mux.HandleFunc("POST /api/environments/{id}/start", s.startEnv)
	s.mux.HandleFunc("POST /api/environments/{id}/stop", s.stopEnv)
	s.mux.HandleFunc("POST /api/environments/{id}/restart", s.restartEnv)
	s.mux.HandleFunc("POST /api/environments/{id}/renew", s.renewEnv)
	s.mux.HandleFunc("DELETE /api/environments/{id}", s.deleteEnv)
	s.mux.HandleFunc("GET /api/environments/{id}/logs", s.envLogs)
	s.mux.HandleFunc("GET /api/images", s.listImages)
	s.mux.HandleFunc("GET /api/images/remote", s.listRemoteImages)
	s.mux.HandleFunc("POST /api/images", s.upsertImage)
	s.mux.HandleFunc("DELETE /api/images/{version}", s.deleteImage)
	s.mux.HandleFunc("GET /api/providers", s.listProviders)
	s.mux.HandleFunc("GET /api/presets", s.listPresets)
	s.mux.HandleFunc("POST /api/presets", s.createPreset)
	s.mux.HandleFunc("PUT /api/presets/{id}", s.updatePreset)
	s.mux.HandleFunc("DELETE /api/presets/{id}", s.deletePreset)
	s.mux.HandleFunc("/", s.static)
}

// Handler returns the root handler with access logs.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, status: 200}
		s.mux.ServeHTTP(rw, r)
		ev := s.log.Info()
		switch {
		case rw.status >= 500:
			ev = s.log.Error()
		case rw.status >= 400:
			ev = s.log.Warn()
		}
		ev.Str("method", r.Method).Str("path", r.URL.Path).
			Int("status", rw.status).
			Int64("cost_ms", time.Since(start).Milliseconds()).
			Msg("http")
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "driver": s.svc.DriverName()})
}

func (s *Server) listEnvs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.List(r.Context()))
}

func (s *Server) createEnv(w http.ResponseWriter, r *http.Request) {
	var req env.CreateRequest
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.svc.Create(r.Context(), req)
	if err != nil {
		writeEnvErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (s *Server) getEnv(w http.ResponseWriter, r *http.Request) {
	view, err := s.svc.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeEnvErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) startEnv(w http.ResponseWriter, r *http.Request) {
	view, err := s.svc.Start(r.Context(), r.PathValue("id"))
	if err != nil {
		writeEnvErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) stopEnv(w http.ResponseWriter, r *http.Request) {
	view, err := s.svc.Stop(r.Context(), r.PathValue("id"))
	if err != nil {
		writeEnvErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) restartEnv(w http.ResponseWriter, r *http.Request) {
	view, err := s.svc.Restart(r.Context(), r.PathValue("id"))
	if err != nil {
		writeEnvErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) renewEnv(w http.ResponseWriter, r *http.Request) {
	view, err := s.svc.Renew(r.Context(), r.PathValue("id"))
	if err != nil {
		writeEnvErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) deleteEnv(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.Destroy(r.Context(), r.PathValue("id")); err != nil {
		writeEnvErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) envLogs(w http.ResponseWriter, r *http.Request) {
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	out, err := s.svc.Logs(r.Context(), r.PathValue("id"), tail)
	if err != nil {
		writeEnvErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"logs": out})
}

func (s *Server) listProviders(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"providers": settings.ProviderOptions(),
	})
}

func (s *Server) listPresets(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.svc.ListPresets())
}

func (s *Server) createPreset(w http.ResponseWriter, r *http.Request) {
	var req env.PresetInput
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.svc.CreatePreset(req)
	if err != nil {
		writeEnvErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (s *Server) updatePreset(w http.ResponseWriter, r *http.Request) {
	var req env.PresetInput
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	view, err := s.svc.UpdatePreset(r.PathValue("id"), req)
	if err != nil {
		writeEnvErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) deletePreset(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.DeletePreset(r.PathValue("id")); err != nil {
		writeEnvErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listImages(w http.ResponseWriter, r *http.Request) {
	imgs, err := s.svc.ListImages(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, imgs)
}

func (s *Server) upsertImage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		env.ImageConfig
		Pull *bool `json:"pull"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	pull := true
	if body.Pull != nil {
		pull = *body.Pull
	}
	view, err := s.svc.RegisterImage(r.Context(), body.ImageConfig, pull)
	if err != nil {
		msg := err.Error()
		code := http.StatusBadRequest
		if strings.Contains(msg, "docker") {
			code = http.StatusBadGateway
		}
		writeErr(w, code, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) listRemoteImages(w http.ResponseWriter, r *http.Request) {
	imgs, err := s.svc.ListRemoteImages(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, imgs)
}

func (s *Server) deleteImage(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.DeleteImage(r.PathValue("version")); err != nil {
		writeEnvErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	if s.webDir == "" {
		http.Error(w, "web ui not configured", http.StatusNotFound)
		return
	}
	root := os.DirFS(s.webDir)
	p := strings.TrimPrefix(r.URL.Path, "/")
	if p == "" {
		p = "index.html"
	}
	if _, err := fs.Stat(root, p); err != nil {
		p = "index.html"
	}
	http.ServeFileFS(w, r, root, p)
}

func writeEnvErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, env.ErrNotFound):
		writeErr(w, http.StatusNotFound, err)
	case errors.Is(err, env.ErrNotConfigured):
		writeErr(w, http.StatusConflict, err)
	case errors.Is(err, env.ErrImageMissing):
		writeErr(w, http.StatusConflict, err)
	case errors.Is(err, env.ErrConflict):
		writeErr(w, http.StatusConflict, err)
	default:
		msg := err.Error()
		code := http.StatusBadRequest
		if strings.Contains(msg, "docker") {
			code = http.StatusBadGateway
		}
		writeErr(w, code, err)
	}
}

func readJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
