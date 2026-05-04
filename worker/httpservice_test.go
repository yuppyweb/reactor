package worker_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yuppyweb/reactor"
	"github.com/yuppyweb/reactor/worker"
)

type mockHTTPServer struct {
	listenAndServeCalled bool
	listenAndServeErr    error
	listenAndServeTime   time.Duration
	shutdownCalled       bool
	shutdownCtx          context.Context
	shutdownErr          error
}

func (m *mockHTTPServer) ListenAndServe() error {
	m.listenAndServeCalled = true

	time.Sleep(m.listenAndServeTime)

	return m.listenAndServeErr
}

func (m *mockHTTPServer) Shutdown(ctx context.Context) error {
	m.shutdownCalled = true
	m.shutdownCtx = ctx

	return m.shutdownErr
}

func TestHTTPService_Start(t *testing.T) {
	t.Parallel()

	mockSrv := new(mockHTTPServer)
	httpService := worker.NewHTTPService(mockSrv)

	errCh := httpService.Errors()
	if errCh != nil {
		t.Fatalf("expected Errors to return nil, got non-nil channel")
	}

	err := httpService.Start(context.Background())
	if err != nil {
		t.Fatalf("expected Start to succeed, got error: %v", err)
	}

	if !mockSrv.listenAndServeCalled {
		t.Fatal("expected ListenAndServe to be called, but it was not")
	}
}

func TestHTTPService_StartWithListenAndServeError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("listen error")
	mockSrv := new(mockHTTPServer)
	mockSrv.listenAndServeErr = expectedErr

	httpService := worker.NewHTTPService(mockSrv)

	errCh := httpService.Errors()
	if errCh != nil {
		t.Fatalf("expected Errors to return nil, got non-nil channel")
	}

	err := httpService.Start(context.Background())
	if err == nil {
		t.Fatal("expected Start to return an error, got nil")
	}

	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}

	if !strings.Contains(err.Error(), "failed to start HTTP server") {
		t.Fatalf("expected error message to contain 'failed to start HTTP server', got %v", err)
	}

	if !mockSrv.listenAndServeCalled {
		t.Fatal("expected ListenAndServe to be called, but it was not")
	}
}

func TestHTTPService_Shutdown(t *testing.T) {
	t.Parallel()

	type ctxTestKey struct{}

	mockSrv := new(mockHTTPServer)
	ctx := context.WithValue(context.Background(), ctxTestKey{}, "test-value1")
	httpService := worker.NewHTTPService(mockSrv)

	errCh := httpService.Errors()
	if errCh != nil {
		t.Fatalf("expected Errors to return nil, got non-nil channel")
	}

	err := httpService.Shutdown(ctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !mockSrv.shutdownCalled {
		t.Fatal("expected Shutdown to be called, but it was not")
	}

	if mockSrv.shutdownCtx != ctx {
		t.Fatal("expected Shutdown to be called with the correct context, but it was not")
	}
}

func TestHTTPService_ShutdownWithError(t *testing.T) {
	t.Parallel()

	type ctxTestKey struct{}

	expectedErr := errors.New("shutdown error")
	mockSrv := new(mockHTTPServer)
	mockSrv.shutdownErr = expectedErr
	ctx := context.WithValue(context.Background(), ctxTestKey{}, "test-value2")
	httpService := worker.NewHTTPService(mockSrv)

	errCh := httpService.Errors()
	if errCh != nil {
		t.Fatalf("expected Errors to return nil, got non-nil channel")
	}

	err := httpService.Shutdown(ctx)
	if err == nil {
		t.Fatal("expected Shutdown to return an error, got nil")
	}

	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}

	if !strings.Contains(err.Error(), "failed to shutdown HTTP server") {
		t.Fatalf("expected error message to contain 'failed to shutdown HTTP server', got %v", err)
	}

	if !mockSrv.shutdownCalled {
		t.Fatal("expected Shutdown to be called, but it was not")
	}

	if mockSrv.shutdownCtx != ctx {
		t.Fatal("expected Shutdown to be called with the correct context, but it was not")
	}
}

func TestHTTPService_Errors(t *testing.T) {
	t.Parallel()

	mockSrv := new(mockHTTPServer)
	httpService := worker.NewHTTPService(mockSrv)

	errCh := httpService.Errors()
	if errCh != nil {
		t.Fatalf("expected Errors to return nil, got non-nil channel")
	}
}

func TestHTTPService_StartInReactor(t *testing.T) {
	t.Parallel()

	type ctxTestKey struct{}

	mockSrv := new(mockHTTPServer)
	mockSrv.listenAndServeTime = 200 * time.Millisecond
	ctx := context.WithValue(context.Background(), ctxTestKey{}, "test-value3")
	httpService := worker.NewHTTPService(mockSrv)

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("failed to create reactor: %v", err)
	}

	err = react.Add(httpService)
	if err != nil {
		t.Fatalf("failed to add HTTPService worker to reactor: %v", err)
	}

	wg := &sync.WaitGroup{}

	wg.Go(func() {
		if err := react.Start(ctx); err != nil {
			t.Fatalf("failed to start reactor: %v", err)
		}

		if !mockSrv.listenAndServeCalled {
			t.Fatal("expected ListenAndServe to be called, but it was not")
		}
	})

	wg.Go(func() {
		time.Sleep(100 * time.Millisecond) // Ensure the server has started

		if err := react.Shutdown(ctx); err != nil {
			t.Fatalf("failed to shutdown reactor: %v", err)
		}

		if !mockSrv.shutdownCalled {
			t.Fatal("expected Shutdown to be called, but it was not")
		}

		if mockSrv.shutdownCtx != ctx {
			t.Fatal("expected Shutdown to be called with the correct context, but it was not")
		}
	})

	wg.Wait()
}
