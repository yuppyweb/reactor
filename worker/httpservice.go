package worker

import (
	"context"
	"fmt"

	"github.com/yuppyweb/reactor"
)

// HTTPServer defines the interface for an HTTP server.
// Implementations are typically *http.Server from the net/http package.
type HTTPServer interface {
	ListenAndServe() error
	Shutdown(ctx context.Context) error
}

// HTTPService is a worker implementation for managing HTTP server lifecycle.
// It implements the reactor.Worker interface.
type HTTPService struct {
	srv HTTPServer
}

// NewHTTPService creates a new HTTPService worker with the provided HTTP server.
func NewHTTPService(srv HTTPServer) *HTTPService {
	return &HTTPService{srv: srv}
}

// Start begins serving HTTP requests.
func (w *HTTPService) Start(context.Context) error {
	if err := w.srv.ListenAndServe(); err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}

	return nil
}

// Shutdown gracefully stops the HTTP server.
func (w *HTTPService) Shutdown(ctx context.Context) error {
	if err := w.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown HTTP server: %w", err)
	}

	return nil
}

// Errors returns nil as HTTP server does not provide a built-in error channel.
func (w *HTTPService) Errors() <-chan error {
	return nil
}

// Ensure HTTPService implements the reactor.Worker interface.
var _ reactor.Worker = (*HTTPService)(nil)
