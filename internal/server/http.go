package server

import (
	"context"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// Pinger is the readiness dependency (the pgx pool in production).
type Pinger interface {
	Ping(ctx context.Context) error
}

// HTTPServer serves only health endpoints; all real traffic is gRPC.
type HTTPServer struct {
	server *http.Server
	pinger Pinger
	addr   string
}

// NewHTTPServer creates the health HTTP server
func NewHTTPServer(pinger Pinger, addr string) *HTTPServer {
	s := &HTTPServer{pinger: pinger, addr: addr}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)

	s.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	return s
}

// Start starts the HTTP server
func (s *HTTPServer) Start() error {
	log.Info().Str("addr", s.addr).Msg("starting HTTP server")
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the HTTP server
func (s *HTTPServer) Shutdown(ctx context.Context) error {
	log.Info().Msg("shutting down HTTP server")
	return s.server.Shutdown(ctx)
}

func (s *HTTPServer) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *HTTPServer) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	w.Header().Set("Content-Type", "application/json")
	if s.pinger == nil {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
		return
	}
	if err := s.pinger.Ping(ctx); err != nil {
		log.Warn().Err(err).Msg("readiness check failed")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"not ready","error":"database unavailable"}`))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ready"}`))
}
