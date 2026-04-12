package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const defaultShutdownTimeout = 30 * time.Second

// Server wraps an HTTP server with a graceful shutdown mechanism.
type Server struct {
	srv             *http.Server
	logger          *slog.Logger
	shutdownTimeout time.Duration
}

// New creates a Server with the given mux and port.
func New(mux http.Handler, port string, logger *slog.Logger) *Server {
	return &Server{
		srv: &http.Server{
			Addr:              net.JoinHostPort("", port),
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
		},
		logger:          logger,
		shutdownTimeout: defaultShutdownTimeout,
	}
}

// Run starts the HTTP server and blocks until SIGINT or SIGTERM is received,
// then performs a graceful shutdown with a 30-second timeout.
func (s *Server) Run() error {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("server starting", slog.String("addr", s.srv.Addr))
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case sig := <-quit:
		s.logger.Info("shutdown signal received", slog.String("signal", sig.String()))
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	defer cancel()

	s.logger.Info("shutting down server gracefully")
	if err := s.srv.Shutdown(ctx); err != nil {
		s.logger.Error("server shutdown error", slog.String("error", err.Error()))
		return fmt.Errorf("shutting down server: %w", err)
	}
	s.logger.Info("server stopped")
	return nil
}

// NewMux builds the HTTP mux with the webhook endpoint.
func NewMux(webhookHandler http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("POST /webhooks/github", webhookHandler)
	return mux
}
