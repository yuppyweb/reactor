package worker_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yuppyweb/reactor"
	"github.com/yuppyweb/reactor/worker"
)

// mockGRPCServer is a simple implementation of the GRPCServer interface for testing purposes.
type mockGRPCServer struct {
	serveCalled        bool
	serveLis           net.Listener
	serveErr           error
	serveTime          time.Duration
	gracefulStopCalled bool
}

// Serve simulates serving gRPC requests and can be configured to return an error or take a certain amount of time.
func (m *mockGRPCServer) Serve(lis net.Listener) error {
	m.serveLis = lis
	m.serveCalled = true

	time.Sleep(m.serveTime)

	return m.serveErr
}

// GracefulStop simulates gracefully stopping the gRPC server and records that it was called.
func (m *mockGRPCServer) GracefulStop() {
	m.gracefulStopCalled = true
}

// Ensure mockGRPCServer implements the GRPCServer interface.
var _ worker.GRPCServer = (*mockGRPCServer)(nil)

// Ensure mockGRPCServer implements the GRPCServer interface.
func TestGRPCService_Start(t *testing.T) {
	t.Parallel()

	mockSrv := new(mockGRPCServer)
	lisCfg := new(net.ListenConfig)

	lis, err := lisCfg.Listen(context.Background(), "tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	grpcService := worker.NewGRPCService(mockSrv, lis)

	errCh := grpcService.Errors()
	if errCh != nil {
		t.Fatalf("expected Errors to return nil, got non-nil channel")
	}

	err = grpcService.Start(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if mockSrv.serveLis != lis {
		t.Fatalf("expected Serve to be called with the correct listener")
	}

	if !mockSrv.serveCalled {
		t.Fatalf("expected Serve to be called")
	}
}

// TestGRPCService_Start_WithServeError verifies that if the Serve method returns an error,
// it is properly wrapped and returned by the Start method.
func TestGRPCService_Start_WithServeError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("serve error")
	mockSrv := new(mockGRPCServer)
	mockSrv.serveErr = expectedErr
	lisCfg := new(net.ListenConfig)

	lis, err := lisCfg.Listen(context.Background(), "tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	grpcService := worker.NewGRPCService(mockSrv, lis)

	errCh := grpcService.Errors()
	if errCh != nil {
		t.Fatalf("expected Errors to return nil, got non-nil channel")
	}

	err = grpcService.Start(context.Background())
	if err == nil {
		t.Fatal("expected Start to return an error, got nil")
	}

	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}

	if !strings.Contains(err.Error(), "failed to start gRPC server") {
		t.Fatalf("expected error message to contain 'failed to start gRPC server', got %v", err)
	}

	if mockSrv.serveLis != lis {
		t.Fatalf("expected Serve to be called with the correct listener")
	}

	if !mockSrv.serveCalled {
		t.Fatalf("expected Serve to be called")
	}
}

// TestGRPCService_Shutdown verifies that the Shutdown method calls GracefulStop
// on the gRPC server and returns no error.
func TestGRPCService_Shutdown(t *testing.T) {
	t.Parallel()

	mockSrv := new(mockGRPCServer)
	lisCfg := new(net.ListenConfig)

	lis, err := lisCfg.Listen(context.Background(), "tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	grpcService := worker.NewGRPCService(mockSrv, lis)

	errCh := grpcService.Errors()
	if errCh != nil {
		t.Fatalf("expected Errors to return nil, got non-nil channel")
	}

	err = grpcService.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !mockSrv.gracefulStopCalled {
		t.Fatalf("expected GracefulStop to be called")
	}
}

// TestGRPCService_Errors verifies that the Errors method returns nil,
// as the GRPCService does not produce errors through a channel.
func TestGRPCService_Errors(t *testing.T) {
	t.Parallel()

	mockSrv := new(mockGRPCServer)
	lisCfg := new(net.ListenConfig)

	lis, err := lisCfg.Listen(context.Background(), "tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	grpcService := worker.NewGRPCService(mockSrv, lis)

	errCh := grpcService.Errors()
	if errCh != nil {
		t.Fatalf("expected Errors to return nil, got non-nil channel")
	}
}

// TestGRPCService_Start_InReactor verifies that the GRPCService can be started and stopped within a reactor,
// and that the Serve and GracefulStop methods are called appropriately.
func TestGRPCService_Start_InReactor(t *testing.T) {
	t.Parallel()

	mockSrv := new(mockGRPCServer)
	mockSrv.serveTime = 200 * time.Millisecond
	lisCfg := new(net.ListenConfig)

	lis, err := lisCfg.Listen(context.Background(), "tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	grpcService := worker.NewGRPCService(mockSrv, lis)

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("failed to create reactor: %v", err)
	}

	if err := react.Add(grpcService); err != nil {
		t.Fatalf("failed to add GRPCService to reactor: %v", err)
	}

	wg := sync.WaitGroup{}

	wg.Go(func() {
		if err := react.Start(context.Background()); err != nil {
			t.Fatalf("failed to start reactor: %v", err)
		}

		if mockSrv.serveLis != lis {
			t.Fatalf("expected Serve to be called with the correct listener")
		}

		if !mockSrv.serveCalled {
			t.Fatalf("expected Serve to be called")
		}
	})

	wg.Go(func() {
		time.Sleep(100 * time.Millisecond) // Ensure the server has started

		if err := react.Shutdown(context.Background()); err != nil {
			t.Fatalf("failed to shutdown reactor: %v", err)
		}

		if !mockSrv.gracefulStopCalled {
			t.Fatalf("expected GracefulStop to be called")
		}
	})

	wg.Wait()
}
