// Package httpserver is the HTTP shell every Fyniq service runs in: /healthz (liveness),
// /readyz (the database and other dependencies) and /version (NFR-IMG-06), plus the service's
// own routes. Cloud Run's start-up and liveness probes call the first two.
package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/cognizhi/fyniq/internal/version"
)

// Check reports whether one dependency is ready.
type Check func(ctx context.Context) error

// Server is a service's HTTP server.
type Server struct {
	Mux    *http.ServeMux
	info   version.Info
	mu     sync.Mutex
	checks map[string]Check
}

// New returns a server that already answers /healthz, /readyz and /version.
func New(info version.Info) *Server {
	s := &Server{Mux: http.NewServeMux(), info: info, checks: map[string]Check{}}
	s.Mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.Mux.HandleFunc("GET /readyz", s.readyz)
	s.Mux.HandleFunc("GET /version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.info)
	})
	return s
}

// AddReadiness registers a dependency that /readyz checks.
func (s *Server) AddReadiness(name string, c Check) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checks[name] = c
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	names := make([]string, 0, len(s.checks))
	for n := range s.checks {
		names = append(names, n)
	}
	s.mu.Unlock()
	sort.Strings(names)

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	failing := map[string]string{}
	for _, n := range names {
		s.mu.Lock()
		c := s.checks[n]
		s.mu.Unlock()
		if err := c(ctx); err != nil {
			failing[n] = err.Error()
		}
	}
	if len(failing) > 0 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "unavailable", "failing": failing})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "checks": names})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Serve runs the server on l until ctx is done, then shuts down gracefully.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	srv := &http.Server{Handler: s.Mux, ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(l) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			return err
		}
		if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
