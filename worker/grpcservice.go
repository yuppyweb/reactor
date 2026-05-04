package worker

import (
	"context"
	"fmt"
	"net"

	"github.com/yuppyweb/reactor"
)

// GRPCServer defines the interface for a gRPC server.
// Implementations are typically *grpc.Server from the google.golang.org/grpc package.
type GRPCServer interface {
	Serve(lis net.Listener) error
	GracefulStop()
}

// GRPCService is a worker implementation for managing gRPC server lifecycle.
// It implements the reactor.Worker interface.
type GRPCService struct {
	srv GRPCServer
	lis net.Listener
}

// NewGRPCService creates a new GRPCService worker with the provided gRPC server and listener.
func NewGRPCService(srv GRPCServer, lis net.Listener) *GRPCService {
	return &GRPCService{
		srv: srv,
		lis: lis,
	}
}

// Start begins serving gRPC requests on the listener.
func (s *GRPCService) Start(context.Context) error {
	if err := s.srv.Serve(s.lis); err != nil {
		return fmt.Errorf("failed to start gRPC server: %w", err)
	}

	return nil
}

// Shutdown gracefully stops the gRPC server.
func (s *GRPCService) Shutdown(context.Context) error {
	s.srv.GracefulStop()

	return nil
}

// Errors returns nil as gRPC server does not provide a built-in error channel.
func (s *GRPCService) Errors() <-chan error {
	return nil
}

// Ensure GRPCService implements the reactor.Worker interface.
var _ reactor.Worker = (*GRPCService)(nil)
