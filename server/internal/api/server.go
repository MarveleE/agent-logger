package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/agentlogger/agentlog/internal/store"
)

// Server wraps an HTTP server bound to a Store.
type Server struct {
	store    store.Store
	http     *http.Server
	mux      *chi.Mux
	addr     string
	shutdown context.CancelFunc // set by ListenAndServe so /internal/shutdown can trigger it
}

// New constructs an HTTP server. Addr should include host:port (e.g. "127.0.0.1:8765").
func New(s store.Store, addr string) *Server {
	srv := &Server{store: s, addr: addr}
	srv.mux = srv.routes()
	srv.http = &http.Server{
		Addr:              addr,
		Handler:           srv.mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return srv
}

// Handler exposes the wired router (useful for tests).
func (s *Server) Handler() http.Handler { return s.mux }

// Addr returns the server address.
func (s *Server) Addr() string { return s.addr }

func (s *Server) routes() *chi.Mux {
	r := chi.NewMux()
	r.Use(middleware.Recoverer)

	r.Route("/v1", func(r chi.Router) {
		// Public API surface — RealIP rewrites RemoteAddr based on proxy
		// headers, which is fine here (no privileged endpoints in this group).
		r.Group(func(r chi.Router) {
			r.Use(middleware.RealIP)
			r.Get("/health", s.handleHealth)
			r.Post("/sessions", s.handleCreateSession)
			r.Get("/sessions", s.handleListSessions)
			r.Get("/sessions/latest", s.handleLatestSession)
			r.Get("/sessions/{id}", s.handleGetSession)
			r.Put("/sessions/{id}/heartbeat", s.handleHeartbeat)
			r.Post("/sessions/{id}/logs", s.handleIngestLogs)
			r.Get("/logs", s.handleQueryLogs)
			r.Get("/logs/search", s.handleSearchLogs)
			r.Post("/db/prune", s.handlePrune)
			r.Post("/db/reset", s.handleReset)
		})
		// Internal control surface — RealIP is INTENTIONALLY not applied here
		// so X-Forwarded-For cannot spoof a loopback check.
		r.Post("/internal/shutdown", s.handleShutdown)
	})
	return r
}

// ListenAndServe starts the HTTP server. It returns nil on graceful shutdown.
func (s *Server) ListenAndServe(ctx context.Context) error {
	// Wrap ctx so the /internal/shutdown handler has a way to cancel us.
	ctx, cancel := context.WithCancel(ctx)
	s.shutdown = cancel
	defer cancel()

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	errCh := make(chan error, 1)
	go func() { errCh <- s.http.Serve(ln) }()
	select {
	case <-ctx.Done():
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = s.http.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// handleShutdown triggers a graceful shutdown when invoked from loopback.
// Used primarily on Windows (where there's no SIGTERM-equivalent for an
// unrelated console process) but available on every platform.
func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if !isLoopback(r) {
		writeError(w, http.StatusForbidden, "shutdown is restricted to loopback")
		return
	}
	w.WriteHeader(http.StatusAccepted)
	// Trigger after the response has been flushed so the caller gets a clean reply.
	go func() {
		time.Sleep(50 * time.Millisecond)
		if s.shutdown != nil {
			s.shutdown()
		}
	}()
}

func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host == "" {
		return false
	}
	if strings.HasPrefix(host, "127.") || host == "::1" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
