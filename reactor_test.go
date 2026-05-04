package reactor_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuppyweb/reactor"
)

type mockWorker struct {
	startCtx           atomic.Value
	shutdownCtx        atomic.Value
	countStartCalls    atomic.Int32
	countShutdownCalls atomic.Int32
	timeLifeStarted    time.Duration
	timeLifeShutdown   time.Duration
	startErr           error
	shutdownErr        error
	errCh              chan error
}

var _ reactor.Worker = (*mockWorker)(nil)

func (w *mockWorker) Start(ctx context.Context) error {
	defer func() {
		if w.errCh != nil {
			close(w.errCh)
		}
	}()

	w.startCtx.Store(ctx)
	w.countStartCalls.Add(1)
	time.Sleep(w.timeLifeStarted)

	return w.startErr
}

func (w *mockWorker) Shutdown(ctx context.Context) error {
	w.shutdownCtx.Store(ctx)
	w.countShutdownCalls.Add(1)
	time.Sleep(w.timeLifeShutdown)

	return w.shutdownErr
}

func (w *mockWorker) Errors() <-chan error {
	return w.errCh
}

func TestNewReactor_WithDefaultErrorBufferSize(t *testing.T) {
	t.Parallel()

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if react == nil {
		t.Fatal("expected non-nil reactor, got nil")
	}

	if cap(react.Errors()) != 1000 {
		t.Fatalf("expected error channel buffer size of 1000, got %d", cap(react.Errors()))
	}
}

func TestNewReactor_WithCustomErrorBufferSize(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		bufferSize int
	}{
		{"BufferSize1", 1},
		{"BufferSize10", 10},
		{"BufferSize100", 100},
		{"BufferSize1000", 1000},
		{"BufferSize5000", 5000},
		{"BufferSize10000", 10000},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			react, err := reactor.New(reactor.WithErrorBufferSize(tc.bufferSize))
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			if react == nil {
				t.Fatal("expected non-nil reactor, got nil")
			}

			if cap(react.Errors()) != tc.bufferSize {
				t.Fatalf(
					"expected error channel buffer size of %d, got %d",
					tc.bufferSize,
					cap(react.Errors()),
				)
			}
		})
	}
}

func TestNewReactor_WithInvalidErrorBufferSize(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		bufferSize  int
		expectedErr error
	}{
		{"BufferSize0", 0, reactor.ErrMinErrorBufferSize},
		{"BufferSizeNegative", -1, reactor.ErrMinErrorBufferSize},
		{"BufferSizeTooLarge", 10001, reactor.ErrMaxErrorBufferSize},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := reactor.New(reactor.WithErrorBufferSize(tc.bufferSize))
			if err == nil {
				t.Fatal("expected an error, got nil")
			}

			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("expected error %v, got %v", tc.expectedErr, err)
			}
		})
	}
}

func TestNewReactor_WithNilOption(t *testing.T) {
	t.Parallel()

	_, err := reactor.New(nil)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	if !errors.Is(err, reactor.ErrNilOption) {
		t.Fatalf("expected error %v, got %v", reactor.ErrNilOption, err)
	}
}

func TestNewReactor_WithOptionError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("option error")
	opt := func(*reactor.Reactor) error {
		return expectedErr
	}

	_, err := reactor.New(opt)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
}

func TestNewReactor_AddNilWorker(t *testing.T) {
	t.Parallel()

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if react == nil {
		t.Fatal("expected non-nil reactor, got nil")
	}

	err = react.Add(nil)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	if !errors.Is(err, reactor.ErrNilWorker) {
		t.Fatalf("expected error %v, got %v", reactor.ErrNilWorker, err)
	}
}

func TestNewReactor_WithLogger(t *testing.T) {
	t.Parallel()

	mockLogger := new(mockLogger)
	ctx := context.Background()

	react, err := reactor.New(reactor.WithLogger(mockLogger))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if react == nil {
		t.Fatal("expected non-nil reactor, got nil")
	}

	if err := react.Start(ctx); err != nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	if len(mockLogger.debugLog) != 1 {
		t.Fatalf("expected exactly one debug log entry, got %d", len(mockLogger.debugLog))
	}

	if !strings.Contains(mockLogger.debugLog[0].msg, "reactor: all workers completed") {
		t.Fatalf(
			"expected debug log message to contain 'reactor: all workers completed', got '%s'",
			mockLogger.debugLog[0].msg,
		)
	}

	if mockLogger.debugLog[0].ctx != ctx {
		t.Fatal("expected debug log context to match the context passed to Start")
	}

	if len(mockLogger.errorLog) != 0 {
		t.Fatalf("expected no error log entries, got %d", len(mockLogger.errorLog))
	}
}

func TestNewReactor_WithNilLogger(t *testing.T) {
	t.Parallel()

	_, err := reactor.New(reactor.WithLogger(nil))
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	if !errors.Is(err, reactor.ErrNilLogger) {
		t.Fatalf("expected error %v, got %v", reactor.ErrNilLogger, err)
	}
}

func TestNewReactor_AddIsStarted(t *testing.T) {
	t.Parallel()

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if react == nil {
		t.Fatal("expected non-nil reactor, got nil")
	}

	wg := &sync.WaitGroup{}

	wg.Go(func() {
		mockWorker := new(mockWorker)
		mockWorker.timeLifeStarted = 200 * time.Millisecond

		if err = react.Add(mockWorker); err != nil {
			t.Fatal("expected an error when adding worker to started reactor, got nil")
		}

		err = react.Start(context.Background())
		if err != nil {
			t.Fatalf("expected no error from Start, got %v", err)
		}
	})

	time.Sleep(100 * time.Millisecond)

	err = react.Add(new(mockWorker))
	if err == nil {
		t.Fatal("expected an error when adding worker to started reactor, got nil")
	}

	if !errors.Is(err, reactor.ErrIsStarted) {
		t.Fatalf("expected error %v, got %v", reactor.ErrIsStarted, err)
	}

	wg.Wait()
}

func TestNewReactor_AddIsStopped(t *testing.T) {
	t.Parallel()

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if react == nil {
		t.Fatal("expected non-nil reactor, got nil")
	}

	err = react.Start(context.Background())
	if err != nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	err = react.Add(new(mockWorker))
	if err == nil {
		t.Fatal("expected an error when adding worker to stopped reactor, got nil")
	}

	if !errors.Is(err, reactor.ErrIsStopped) {
		t.Fatalf("expected error %v, got %v", reactor.ErrIsStopped, err)
	}
}

func TestNewReactor_AddConcurrent(t *testing.T) {
	t.Parallel()

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	wg := &sync.WaitGroup{}
	workers := make([]*mockWorker, 1000)

	for idx := range workers {
		wg.Go(func() {
			workers[idx] = new(mockWorker)

			if err := react.Add(workers[idx]); err != nil {
				t.Fatalf("expected no error when adding worker, got %v", err)
			}
		})
	}

	wg.Wait()

	if err := react.Start(context.Background()); err != nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	for idx := range workers {
		if workers[idx].countStartCalls.Load() != 1 {
			t.Fatalf(
				"expected worker %d to have Start called once, got %d",
				idx,
				workers[idx].countStartCalls.Load(),
			)
		}
	}
}

func TestNewReactor_StartRestart(t *testing.T) {
	t.Parallel()

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	err = react.Start(context.Background())
	if err != nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	err = react.Start(context.Background())
	if err == nil {
		t.Fatal("expected an error when starting an already started reactor, got nil")
	}

	if !errors.Is(err, reactor.ErrNotRestart) {
		t.Fatalf("expected error %v, got %v", reactor.ErrNotRestart, err)
	}
}
