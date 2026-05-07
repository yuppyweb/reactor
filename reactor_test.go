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

type mockFnWorker struct {
	errFn func(chan error)
	errCh chan error
}

var _ reactor.Worker = (*mockFnWorker)(nil)

func (w *mockFnWorker) Start(context.Context) error {
	defer func() {
		if w.errCh != nil {
			close(w.errCh)
		}
	}()

	if w.errFn != nil {
		w.errFn(w.errCh)
	}

	return nil
}

func (w *mockFnWorker) Shutdown(context.Context) error {
	return nil
}

func (w *mockFnWorker) Errors() <-chan error {
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

	if err = react.Add(new(mockWorker)); err != nil {
		t.Fatalf("expected no error when adding worker, got %v", err)
	}

	if err := react.Start(ctx); err != nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	if len(mockLogger.debugLog) != 2 {
		t.Fatalf("expected exactly two debug log entries, got %d", len(mockLogger.debugLog))
	}

	if !strings.Contains(mockLogger.debugLog[1].msg, "reactor: all workers completed") {
		t.Fatalf(
			"expected debug log message to contain 'reactor: all workers completed', got '%s'",
			mockLogger.debugLog[1].msg,
		)
	}

	if mockLogger.debugLog[1].ctx != ctx {
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

func TestReactor_Add_NilWorker(t *testing.T) {
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

func TestReactor_Add_IsStarted(t *testing.T) {
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

func TestReactor_Add_IsStopped(t *testing.T) {
	t.Parallel()

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if react == nil {
		t.Fatal("expected non-nil reactor, got nil")
	}

	if err = react.Add(new(mockWorker)); err != nil {
		t.Fatalf("expected no error when adding worker, got %v", err)
	}

	if err = react.Start(context.Background()); err != nil {
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

func TestReactor_Add_Concurrent(t *testing.T) {
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

func TestReactor_Start_Restart(t *testing.T) {
	t.Parallel()

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if react == nil {
		t.Fatal("expected non-nil reactor, got nil")
	}

	if err = react.Add(new(mockWorker)); err != nil {
		t.Fatalf("expected no error when adding worker, got %v", err)
	}

	if err = react.Start(context.Background()); err != nil {
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

func TestReactor_Start_SingleWorkerNilErrors(t *testing.T) {
	t.Parallel()

	mockWorker := new(mockWorker)
	ctx := context.Background()

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if err := react.Add(mockWorker); err != nil {
		t.Fatalf("expected no error when adding worker, got %v", err)
	}

	errCh := react.Errors()

	if err := react.Start(ctx); err != nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	if mockWorker.startCtx.Load() != ctx {
		t.Fatal("expected worker Start context to match the context passed to Start")
	}

	if mockWorker.countStartCalls.Load() != 1 {
		t.Fatalf(
			"expected worker Start to be called once, got %d",
			mockWorker.countStartCalls.Load(),
		)
	}

	if len(errCh) != 0 {
		t.Fatalf("expected error channel to be empty, got %d entries", len(errCh))
	}
}

func TestReactor_Start_SingleWorkerReturnError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("worker error")
	mockWorker := new(mockWorker)
	mockWorker.startErr = expectedErr
	ctx := context.Background()

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if err := react.Add(mockWorker); err != nil {
		t.Fatalf("expected no error when adding worker, got %v", err)
	}

	errCh := react.Errors()

	if err := react.Start(ctx); err != nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	if mockWorker.startCtx.Load() != ctx {
		t.Fatal("expected worker Start context to match the context passed to Start")
	}

	if mockWorker.countStartCalls.Load() != 1 {
		t.Fatalf(
			"expected worker Start to be called once, got %d",
			mockWorker.countStartCalls.Load(),
		)
	}

	if len(errCh) != 1 {
		t.Fatalf("expected error channel to have exactly one entry, got %d", len(errCh))
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, expectedErr) {
			t.Fatalf("expected error %v, got %v", expectedErr, err)
		}
	default:
		t.Fatal("expected an error in the error channel, but it was empty")
	}
}

func TestReactor_Start_SingleWorkerRuntimeErrors(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("worker error")
	mockWorker := new(mockWorker)

	mockWorker.errCh = make(chan error, 1)
	mockWorker.errCh <- expectedErr

	ctx := context.Background()

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if err := react.Add(mockWorker); err != nil {
		t.Fatalf("expected no error when adding worker, got %v", err)
	}

	errCh := react.Errors()

	if err := react.Start(ctx); err != nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	if mockWorker.startCtx.Load() != ctx {
		t.Fatal("expected worker Start context to match the context passed to Start")
	}

	if mockWorker.countStartCalls.Load() != 1 {
		t.Fatalf(
			"expected worker Start to be called once, got %d",
			mockWorker.countStartCalls.Load(),
		)
	}

	if len(errCh) != 1 {
		t.Fatalf("expected error channel to have exactly one entry, got %d", len(errCh))
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, expectedErr) {
			t.Fatalf("expected error %v, got %v", expectedErr, err)
		}
	default:
		t.Fatal("expected an error in the error channel, but it was empty")
	}
}

func TestReactor_Start_SingleWorkerMixedErrors(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("worker error")
	mockWorker := new(mockWorker)
	mockWorker.startErr = expectedErr

	mockWorker.errCh = make(chan error, 1)
	mockWorker.errCh <- expectedErr

	ctx := context.Background()

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if err := react.Add(mockWorker); err != nil {
		t.Fatalf("expected no error when adding worker, got %v", err)
	}

	errCh := react.Errors()

	if err := react.Start(ctx); err != nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	if mockWorker.startCtx.Load() != ctx {
		t.Fatal("expected worker Start context to match the context passed to Start")
	}

	if mockWorker.countStartCalls.Load() != 1 {
		t.Fatalf(
			"expected worker Start to be called once, got %d",
			mockWorker.countStartCalls.Load(),
		)
	}

	if len(errCh) != 2 {
		t.Fatalf("expected error channel to have exactly two entries, got %d", len(errCh))
	}

	for range 2 {
		select {
		case err := <-errCh:
			if !errors.Is(err, expectedErr) {
				t.Fatalf("expected error %v, got %v", expectedErr, err)
			}
		default:
			t.Fatal("expected an error in the error channel, but it was empty")
		}
	}
}

func TestReactor_Start_MultipleWorkersNilErrors(t *testing.T) {
	t.Parallel()

	workers := make([]*mockWorker, 1000)
	ctx := context.Background()

	for idx := range workers {
		workers[idx] = new(mockWorker)
	}

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for idx := range workers {
		if err := react.Add(workers[idx]); err != nil {
			t.Fatalf("expected no error when adding worker %d, got %v", idx, err)
		}
	}

	errCh := react.Errors()

	if err := react.Start(ctx); err != nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	for idx := range workers {
		if workers[idx].startCtx.Load() != ctx {
			t.Fatalf("expected worker %d Start context to match the context passed to Start", idx)
		}

		if workers[idx].countStartCalls.Load() != 1 {
			t.Fatalf(
				"expected worker %d Start to be called once, got %d",
				idx,
				workers[idx].countStartCalls.Load(),
			)
		}
	}

	if len(errCh) != 0 {
		t.Fatalf("expected error channel to be empty, got %d entries", len(errCh))
	}
}

func TestReactor_Start_MultipleWorkersReturnErrors(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("worker error")
	workers := make([]*mockWorker, 1000)
	ctx := context.Background()

	for idx := range workers {
		workers[idx] = new(mockWorker)
		workers[idx].startErr = expectedErr
	}

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for idx := range workers {
		if err := react.Add(workers[idx]); err != nil {
			t.Fatalf("expected no error when adding worker %d, got %v", idx, err)
		}
	}

	errCh := react.Errors()

	if err := react.Start(ctx); err != nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	for idx := range workers {
		if workers[idx].startCtx.Load() != ctx {
			t.Fatalf("expected worker %d Start context to match the context passed to Start", idx)
		}

		if workers[idx].countStartCalls.Load() != 1 {
			t.Fatalf(
				"expected worker %d Start to be called once, got %d",
				idx,
				workers[idx].countStartCalls.Load(),
			)
		}
	}

	if len(errCh) != len(workers) {
		t.Fatalf("expected error channel to have %d entries, got %d", len(workers), len(errCh))
	}

	for idx := range workers {
		select {
		case err := <-errCh:
			if !errors.Is(err, expectedErr) {
				t.Fatalf("expected error %v from worker %d, got %v", expectedErr, idx, err)
			}
		default:
			t.Fatalf("expected an error from worker %d in the error channel, but it was empty", idx)
		}
	}
}

func TestReactor_Start_MultipleWorkersRuntimeErrors(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("worker error")
	workers := make([]*mockWorker, 1000)
	ctx := context.Background()

	for idx := range workers {
		workers[idx] = new(mockWorker)

		workers[idx].errCh = make(chan error, 1)
		workers[idx].errCh <- expectedErr
	}

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for idx := range workers {
		if err := react.Add(workers[idx]); err != nil {
			t.Fatalf("expected no error when adding worker %d, got %v", idx, err)
		}
	}

	errCh := react.Errors()

	if err := react.Start(ctx); err != nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	for idx := range workers {
		if workers[idx].startCtx.Load() != ctx {
			t.Fatalf("expected worker %d Start context to match the context passed to Start", idx)
		}

		if workers[idx].countStartCalls.Load() != 1 {
			t.Fatalf(
				"expected worker %d Start to be called once, got %d",
				idx,
				workers[idx].countStartCalls.Load(),
			)
		}
	}

	if len(errCh) != len(workers) {
		t.Fatalf("expected error channel to have %d entries, got %d", len(workers), len(errCh))
	}

	for idx := range workers {
		select {
		case err := <-errCh:
			if !errors.Is(err, expectedErr) {
				t.Fatalf("expected error %v from worker %d, got %v", expectedErr, idx, err)
			}
		default:
			t.Fatalf("expected an error from worker %d in the error channel, but it was empty", idx)
		}
	}
}

func TestReactor_Start_MultipleWorkersMixedErrors(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("worker error")
	workers := make([]*mockWorker, 500)
	ctx := context.Background()

	for idx := range workers {
		workers[idx] = new(mockWorker)
		workers[idx].startErr = expectedErr

		workers[idx].errCh = make(chan error, 1)
		workers[idx].errCh <- expectedErr
	}

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for idx := range workers {
		if err := react.Add(workers[idx]); err != nil {
			t.Fatalf("expected no error when adding worker %d, got %v", idx, err)
		}
	}

	errCh := react.Errors()

	if err := react.Start(ctx); err != nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	for idx := range workers {
		if workers[idx].startCtx.Load() != ctx {
			t.Fatalf("expected worker %d Start context to match the context passed to Start", idx)
		}

		if workers[idx].countStartCalls.Load() != 1 {
			t.Fatalf(
				"expected worker %d Start to be called once, got %d",
				idx,
				workers[idx].countStartCalls.Load(),
			)
		}
	}

	if len(errCh) != len(workers)*2 {
		t.Fatalf("expected error channel to have %d entries, got %d", len(workers)*2, len(errCh))
	}

	for idx := range workers {
		for range 2 {
			select {
			case err := <-errCh:
				if !errors.Is(err, expectedErr) {
					t.Fatalf("expected error %v from worker %d, got %v", expectedErr, idx, err)
				}
			default:
				t.Fatalf(
					"expected an error from worker %d in the error channel, but it was empty",
					idx,
				)
			}
		}
	}
}

func TestReactor_Start_MultipleWorkersCancelContext(t *testing.T) {
	t.Parallel()

	workers := make([]*mockWorker, 1000)
	ctx, cancel := context.WithCancel(context.Background())

	for idx := range workers {
		workers[idx] = new(mockWorker)
		workers[idx].timeLifeStarted = 200 * time.Millisecond
	}

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for idx := range workers {
		if err := react.Add(workers[idx]); err != nil {
			t.Fatalf("expected no error when adding worker %d, got %v", idx, err)
		}
	}

	errCh := react.Errors()
	wg := &sync.WaitGroup{}

	wg.Go(func() {
		if err := react.Start(ctx); err != nil {
			t.Fatalf("expected no error from Start, got %v", err)
		}
	})

	time.Sleep(100 * time.Millisecond)
	cancel()

	wg.Wait()

	for idx := range workers {
		if workers[idx].startCtx.Load() != ctx {
			t.Fatalf("expected worker %d Start context to match the context passed to Start", idx)
		}

		if workers[idx].countStartCalls.Load() != 1 {
			t.Fatalf(
				"expected worker %d Start to be called once, got %d",
				idx,
				workers[idx].countStartCalls.Load(),
			)
		}
	}

	if len(errCh) != 0 {
		t.Fatalf("expected error channel to be empty, got %d entries", len(errCh))
	}
}

func TestReactor_Start_MultipleWorkersFullErrorsChannel(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("worker error")
	workers := make([]*mockWorker, 1000)
	ctx := context.Background()

	for idx := range workers {
		workers[idx] = new(mockWorker)
		workers[idx].startErr = expectedErr
	}

	react, err := reactor.New(reactor.WithErrorBufferSize(10))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for idx := range workers {
		if err := react.Add(workers[idx]); err != nil {
			t.Fatalf("expected no error when adding worker %d, got %v", idx, err)
		}
	}

	errCh := react.Errors()

	err = react.Start(ctx)
	if err == nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	if !errors.Is(err, reactor.ErrChannelFull) {
		t.Fatalf("expected error %v, got %v", reactor.ErrChannelFull, err)
	}

	if len(errCh) != 10 {
		t.Fatalf("expected error channel to have 10 entries (buffer size), got %d", len(errCh))
	}

	for idx := range errCh {
		select {
		case err := <-errCh:
			if !errors.Is(err, expectedErr) {
				t.Fatalf("expected error %v from worker %d, got %v", expectedErr, idx, err)
			}
		default:
			t.Fatalf("expected an error from worker %d in the error channel, but it was empty", idx)
		}
	}
}

func TestReactor_Start_NoWorkers(t *testing.T) {
	t.Parallel()

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	errCh := react.Errors()

	err = react.Start(context.Background())
	if err == nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	if !errors.Is(err, reactor.ErrNoWorkers) {
		t.Fatalf("expected error %v, got %v", reactor.ErrNoWorkers, err)
	}

	if len(errCh) != 0 {
		t.Fatalf("expected error channel to be empty, got %d entries", len(errCh))
	}
}

func TestReactor_Start_WithLogger(t *testing.T) {
	t.Parallel()

	mockLogger := new(mockLogger)
	mockWork := new(mockWorker)
	ctx := context.Background()

	react, err := reactor.New(reactor.WithLogger(mockLogger))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if err := react.Add(mockWork); err != nil {
		t.Fatalf("expected no error when adding worker, got %v", err)
	}

	if err := react.Start(ctx); err != nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	if len(mockLogger.debugLog) != 2 {
		t.Fatalf("expected exactly two debug log entries, got %d", len(mockLogger.debugLog))
	}

	if !strings.Contains(mockLogger.debugLog[0].msg, "reactor: starting worker") {
		t.Fatalf(
			"expected first debug log message to contain 'reactor: starting worker', got '%s'",
			mockLogger.debugLog[0].msg,
		)
	}

	if len(mockLogger.debugLog[0].args) != 1 {
		t.Fatalf(
			"expected first debug log to have exactly one argument, got %d",
			len(mockLogger.debugLog[0].args),
		)
	}

	args, ok := mockLogger.debugLog[0].args[0].(reactor.LogArgs)
	if !ok {
		t.Fatal("expected first debug log argument to be of type reactor.LogArgs")
	}

	worker, ok := args["worker"]
	if !ok {
		t.Fatal("expected first debug log argument to have key 'worker'")
	}

	mWorker, ok := worker.(*mockWorker)
	if !ok {
		t.Fatal("expected 'worker' argument to be of type *mockWorker")
	}

	if mWorker != mockWork {
		t.Fatal(
			"expected 'worker' argument to be the mockWorker instance added to the reactor",
		)
	}

	if mockLogger.debugLog[0].ctx != ctx {
		t.Fatal("expected first debug log context to match the context passed to Start")
	}

	if !strings.Contains(mockLogger.debugLog[1].msg, "reactor: all workers completed") {
		t.Fatalf(
			"expected second debug log message to contain 'reactor: all workers completed', got '%s'",
			mockLogger.debugLog[1].msg,
		)
	}

	if len(mockLogger.debugLog[1].args) != 0 {
		t.Fatalf(
			"expected second debug log to have no arguments, got %d",
			len(mockLogger.debugLog[1].args),
		)
	}

	if mockLogger.debugLog[1].ctx != ctx {
		t.Fatal("expected second debug log context to match the context passed to Start")
	}

	if len(mockLogger.errorLog) != 0 {
		t.Fatalf("expected no error log entries, got %d", len(mockLogger.errorLog))
	}
}

func TestReactor_Start_WithLoggerCancelContext(t *testing.T) {
	t.Parallel()

	mockLogger := new(mockLogger)
	mockWork := new(mockWorker)
	mockWork.timeLifeStarted = 200 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())

	react, err := reactor.New(reactor.WithLogger(mockLogger))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if err := react.Add(mockWork); err != nil {
		t.Fatalf("expected no error when adding worker, got %v", err)
	}

	wg := &sync.WaitGroup{}

	wg.Go(func() {
		if err := react.Start(ctx); err != nil {
			t.Fatalf("expected no error from Start, got %v", err)
		}
	})

	time.Sleep(100 * time.Millisecond)
	cancel()

	wg.Wait()

	if len(mockLogger.debugLog) != 2 {
		t.Fatalf("expected exactly two debug log entries, got %d", len(mockLogger.debugLog))
	}

	if !strings.Contains(mockLogger.debugLog[0].msg, "reactor: starting worker") {
		t.Fatalf(
			"expected first debug log message to contain 'reactor: starting worker', got '%s'",
			mockLogger.debugLog[0].msg,
		)
	}

	if len(mockLogger.debugLog[0].args) != 1 {
		t.Fatalf(
			"expected first debug log to have exactly one argument, got %d",
			len(mockLogger.debugLog[0].args),
		)
	}

	args, ok := mockLogger.debugLog[0].args[0].(reactor.LogArgs)
	if !ok {
		t.Fatal("expected first debug log argument to be of type reactor.LogArgs")
	}

	worker, ok := args["worker"]
	if !ok {
		t.Fatal("expected first debug log argument to have key 'worker'")
	}

	mWorker, ok := worker.(*mockWorker)
	if !ok {
		t.Fatal("expected 'worker' argument to be of type *mockWorker")
	}

	if mWorker != mockWork {
		t.Fatal(
			"expected 'worker' argument to be the mockWorker instance added to the reactor",
		)
	}

	if mockLogger.debugLog[0].ctx != ctx {
		t.Fatal("expected first debug log context to match the context passed to Start")
	}

	if !strings.Contains(
		mockLogger.debugLog[1].msg,
		"reactor: workers startup cancelled by context",
	) {
		t.Fatalf(
			"expected second debug log message to contain 'reactor: workers startup cancelled by context', got '%s'",
			mockLogger.debugLog[1].msg,
		)
	}

	if len(mockLogger.debugLog[1].args) != 0 {
		t.Fatalf(
			"expected second debug log to have no arguments, got %d",
			len(mockLogger.debugLog[1].args),
		)
	}

	if mockLogger.debugLog[1].ctx != ctx {
		t.Fatal("expected second debug log context to match the context passed to Start")
	}

	if len(mockLogger.errorLog) != 0 {
		t.Fatalf("expected no error log entries, got %d", len(mockLogger.errorLog))
	}
}

func TestReactor_Start_WithLoggerFullErrorsChannel(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("worker error")
	mockLogger := new(mockLogger)
	workers := make([]*mockWorker, 2)
	ctx := context.Background()

	for idx := range workers {
		workers[idx] = new(mockWorker)
		workers[idx].startErr = expectedErr
		workers[idx].timeLifeStarted = 100 * time.Millisecond
	}

	react, err := reactor.New(reactor.WithLogger(mockLogger), reactor.WithErrorBufferSize(1))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for idx := range workers {
		if err := react.Add(workers[idx]); err != nil {
			t.Fatalf("expected no error when adding worker %d, got %v", idx, err)
		}
	}

	err = react.Start(ctx)
	if err == nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	if !errors.Is(err, reactor.ErrChannelFull) {
		t.Fatalf("expected error %v, got %v", reactor.ErrChannelFull, err)
	}

	if len(mockLogger.debugLog) != 2 {
		t.Fatalf("expected exactly two debug log entries, got %d", len(mockLogger.debugLog))
	}

	for idx := range mockLogger.debugLog {
		if !strings.Contains(mockLogger.debugLog[idx].msg, "reactor: starting worker") {
			t.Fatalf(
				"expected debug log message to contain 'reactor: starting worker', got '%s'",
				mockLogger.debugLog[idx].msg,
			)
		}

		if len(mockLogger.debugLog[idx].args) != 1 {
			t.Fatalf(
				"expected debug log to have exactly one argument, got %d",
				len(mockLogger.debugLog[idx].args),
			)
		}

		args, ok := mockLogger.debugLog[idx].args[0].(reactor.LogArgs)
		if !ok {
			t.Fatal("expected debug log argument to be of type reactor.LogArgs")
		}

		worker, ok := args["worker"]
		if !ok {
			t.Fatal("expected debug log argument to have key 'worker'")
		}

		if _, ok := worker.(*mockWorker); !ok {
			t.Fatal("expected 'worker' argument to be of type *mockWorker")
		}

		if mockLogger.debugLog[idx].ctx != ctx {
			t.Fatal("expected debug log context to match the context passed to Start")
		}
	}

	if len(mockLogger.errorLog) != 1 {
		t.Fatalf("expected exactly one error log entry, got %d", len(mockLogger.errorLog))
	}

	if !errors.Is(mockLogger.errorLog[0].err, reactor.ErrFailedToSend) {
		t.Fatalf(
			"expected error log to contain error %v, got %v",
			reactor.ErrFailedToSend,
			mockLogger.errorLog[0].err,
		)
	}

	if !errors.Is(mockLogger.errorLog[0].err, expectedErr) {
		t.Fatalf(
			"expected error log to contain error %v, got %v",
			expectedErr,
			mockLogger.errorLog[0].err,
		)
	}

	if mockLogger.errorLog[0].ctx != ctx {
		t.Fatal("expected error log context to match the context passed to Start")
	}

	if len(mockLogger.errorLog[0].args) != 0 {
		t.Fatalf(
			"expected error log to have no arguments, got %d",
			len(mockLogger.errorLog[0].args),
		)
	}
}

func TestReactor_Shutdown_NotStarted(t *testing.T) {
	t.Parallel()

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if react == nil {
		t.Fatal("expected non-nil reactor, got nil")
	}

	err = react.Shutdown(context.Background())
	if err == nil {
		t.Fatal("expected an error when shutting down a not started reactor, got nil")
	}

	if !errors.Is(err, reactor.ErrNotStarted) {
		t.Fatalf("expected error %v, got %v", reactor.ErrNotStarted, err)
	}
}

func TestReactor_Shutdown_IsStopped(t *testing.T) {
	t.Parallel()

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if react == nil {
		t.Fatal("expected non-nil reactor, got nil")
	}

	if err := react.Add(new(mockWorker)); err != nil {
		t.Fatalf("expected no error when adding worker, got %v", err)
	}

	err = react.Start(context.Background())
	if err != nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	err = react.Shutdown(context.Background())
	if err == nil {
		t.Fatal("expected an error when shutting down an already stopped reactor, got nil")
	}

	if !errors.Is(err, reactor.ErrIsStopped) {
		t.Fatalf("expected error %v, got %v", reactor.ErrIsStopped, err)
	}
}

func TestReactor_Shutdown_SingleWorkerNilErrors(t *testing.T) {
	t.Parallel()

	mockWorker := new(mockWorker)
	mockWorker.timeLifeStarted = 200 * time.Millisecond
	ctx := context.Background()

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if err := react.Add(mockWorker); err != nil {
		t.Fatalf("expected no error when adding worker, got %v", err)
	}

	errCh := react.Errors()
	wg := &sync.WaitGroup{}

	wg.Go(func() {
		if err := react.Start(ctx); err != nil {
			t.Fatalf("expected no error from Start, got %v", err)
		}
	})

	time.Sleep(100 * time.Millisecond)

	if err := react.Shutdown(ctx); err != nil {
		t.Fatalf("expected no error from Shutdown, got %v", err)
	}

	if mockWorker.shutdownCtx.Load() != ctx {
		t.Fatal("expected worker Shutdown context to match the context passed to Shutdown")
	}

	if mockWorker.countShutdownCalls.Load() != 1 {
		t.Fatalf(
			"expected worker Shutdown to be called once, got %d",
			mockWorker.countShutdownCalls.Load(),
		)
	}

	if len(errCh) != 0 {
		t.Fatalf("expected error channel to be empty, got %d entries", len(errCh))
	}

	wg.Wait()
}

func TestReactor_Shutdown_SingleWorkerReturnError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("worker error")
	mockWorker := new(mockWorker)
	mockWorker.shutdownErr = expectedErr
	mockWorker.timeLifeStarted = 200 * time.Millisecond
	ctx := context.Background()

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if err := react.Add(mockWorker); err != nil {
		t.Fatalf("expected no error when adding worker, got %v", err)
	}

	errCh := react.Errors()
	wg := &sync.WaitGroup{}

	wg.Go(func() {
		if err := react.Start(ctx); err != nil {
			t.Fatalf("expected no error from Start, got %v", err)
		}
	})

	time.Sleep(100 * time.Millisecond)

	if err := react.Shutdown(ctx); err != nil {
		t.Fatalf("expected no error from Shutdown, got %v", err)
	}

	if mockWorker.shutdownCtx.Load() != ctx {
		t.Fatal("expected worker Shutdown context to match the context passed to Shutdown")
	}

	if mockWorker.countShutdownCalls.Load() != 1 {
		t.Fatalf(
			"expected worker Shutdown to be called once, got %d",
			mockWorker.countShutdownCalls.Load(),
		)
	}

	if len(errCh) != 1 {
		t.Fatalf("expected error channel to have exactly one entry, got %d", len(errCh))
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, expectedErr) {
			t.Fatalf("expected error %v, got %v", expectedErr, err)
		}
	default:
		t.Fatal("expected an error in the error channel, but it was empty")
	}

	wg.Wait()
}

func TestReactor_Shutdown_MultipleWorkersNilErrors(t *testing.T) {
	t.Parallel()

	workers := make([]*mockWorker, 1000)
	ctx := context.Background()

	for idx := range workers {
		workers[idx] = new(mockWorker)
		workers[idx].timeLifeStarted = 200 * time.Millisecond
	}

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for idx := range workers {
		if err := react.Add(workers[idx]); err != nil {
			t.Fatalf("expected no error when adding worker %d, got %v", idx, err)
		}
	}

	errCh := react.Errors()
	wg := &sync.WaitGroup{}

	wg.Go(func() {
		if err := react.Start(ctx); err != nil {
			t.Fatalf("expected no error from Start, got %v", err)
		}
	})

	time.Sleep(100 * time.Millisecond)

	if err := react.Shutdown(ctx); err != nil {
		t.Fatalf("expected no error from Shutdown, got %v", err)
	}

	for idx := range workers {
		if workers[idx].shutdownCtx.Load() != ctx {
			t.Fatalf(
				"expected worker %d Shutdown context to match the context passed to Shutdown",
				idx,
			)
		}

		if workers[idx].countShutdownCalls.Load() != 1 {
			t.Fatalf(
				"expected worker %d Shutdown to be called once, got %d",
				idx,
				workers[idx].countShutdownCalls.Load(),
			)
		}
	}

	if len(errCh) != 0 {
		t.Fatalf("expected error channel to be empty, got %d entries", len(errCh))
	}

	wg.Wait()
}

func TestReactor_Shutdown_MultipleWorkersReturnErrors(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("worker error")
	workers := make([]*mockWorker, 1000)
	ctx := context.Background()

	for idx := range workers {
		workers[idx] = new(mockWorker)
		workers[idx].shutdownErr = expectedErr
		workers[idx].timeLifeStarted = 200 * time.Millisecond
	}

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for idx := range workers {
		if err := react.Add(workers[idx]); err != nil {
			t.Fatalf("expected no error when adding worker %d, got %v", idx, err)
		}
	}

	errCh := react.Errors()
	wg := &sync.WaitGroup{}

	wg.Go(func() {
		if err := react.Start(ctx); err != nil {
			t.Fatalf("expected no error from Start, got %v", err)
		}
	})

	time.Sleep(100 * time.Millisecond)

	if err := react.Shutdown(ctx); err != nil {
		t.Fatalf("expected no error from Shutdown, got %v", err)
	}

	for idx := range workers {
		if workers[idx].shutdownCtx.Load() != ctx {
			t.Fatalf(
				"expected worker %d Shutdown context to match the context passed to Shutdown",
				idx,
			)
		}

		if workers[idx].countShutdownCalls.Load() != 1 {
			t.Fatalf(
				"expected worker %d Shutdown to be called once, got %d",
				idx,
				workers[idx].countShutdownCalls.Load(),
			)
		}
	}

	if len(errCh) != len(workers) {
		t.Fatalf("expected error channel to have %d entries, got %d", len(workers), len(errCh))
	}

	for idx := range workers {
		select {
		case err := <-errCh:
			if !errors.Is(err, expectedErr) {
				t.Fatalf("expected error %v from worker %d, got %v", expectedErr, idx, err)
			}
		default:
			t.Fatalf("expected an error from worker %d in the error channel, but it was empty", idx)
		}
	}

	wg.Wait()
}

func TestReactor_Shutdown_MultipleWorkersCancelContext(t *testing.T) {
	t.Parallel()

	workers := make([]*mockWorker, 1000)
	ctx, cancel := context.WithCancel(context.Background())

	for idx := range workers {
		workers[idx] = new(mockWorker)
		workers[idx].timeLifeStarted = 200 * time.Millisecond
	}

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for idx := range workers {
		if err := react.Add(workers[idx]); err != nil {
			t.Fatalf("expected no error when adding worker %d, got %v", idx, err)
		}
	}

	errCh := react.Errors()
	wg := &sync.WaitGroup{}

	wg.Go(func() {
		if err := react.Start(context.Background()); err != nil {
			t.Fatalf("expected no error from Start, got %v", err)
		}
	})

	time.Sleep(100 * time.Millisecond)
	cancel()

	wg.Go(func() {
		if err := react.Shutdown(ctx); err != nil {
			t.Fatalf("expected no error from Shutdown, got %v", err)
		}
	})

	wg.Wait()

	for idx := range workers {
		if workers[idx].shutdownCtx.Load() != ctx {
			t.Fatalf(
				"expected worker %d Shutdown context to match the context passed to Shutdown",
				idx,
			)
		}

		if workers[idx].countShutdownCalls.Load() != 1 {
			t.Fatalf(
				"expected worker %d Shutdown to be called once, got %d",
				idx,
				workers[idx].countShutdownCalls.Load(),
			)
		}
	}

	if len(errCh) != 0 {
		t.Fatalf("expected error channel to be empty, got %d entries", len(errCh))
	}
}

func TestReactor_Shutdown_MultipleWorkersFullErrorsChannel(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("worker error")
	workers := make([]*mockWorker, 1000)
	ctx := context.Background()

	for idx := range workers {
		workers[idx] = new(mockWorker)
		workers[idx].shutdownErr = expectedErr
		workers[idx].timeLifeStarted = 200 * time.Millisecond
		workers[idx].timeLifeShutdown = time.Duration(idx) * time.Microsecond
	}

	react, err := reactor.New(reactor.WithErrorBufferSize(10))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for idx := range workers {
		if err := react.Add(workers[idx]); err != nil {
			t.Fatalf("expected no error when adding worker %d, got %v", idx, err)
		}
	}

	errCh := react.Errors()
	wg := &sync.WaitGroup{}

	wg.Go(func() {
		err := react.Start(ctx)
		if err == nil {
			t.Fatalf("expected error from Start, got %v", err)
		}

		if !errors.Is(err, reactor.ErrChannelFull) {
			t.Fatalf("expected error %v from Start, got %v", reactor.ErrChannelFull, err)
		}
	})

	time.Sleep(100 * time.Millisecond)

	err = react.Shutdown(ctx)
	if err == nil {
		t.Fatalf("expected error from Shutdown, got %v", err)
	}

	if !errors.Is(err, reactor.ErrChannelFull) {
		t.Fatalf("expected error %v, got %v", reactor.ErrChannelFull, err)
	}

	for idx := range errCh {
		select {
		case err := <-errCh:
			if !errors.Is(err, expectedErr) {
				t.Fatalf("expected error %v from worker %d, got %v", expectedErr, idx, err)
			}
		default:
			t.Fatalf("expected an error from worker %d in the error channel, but it was empty", idx)
		}
	}

	wg.Wait()
}

func TestReactor_Shutdown_WithLogger(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("worker error")
	mockLogger := new(mockLogger)
	mockWork := new(mockWorker)
	mockWork.shutdownErr = expectedErr
	mockWork.timeLifeStarted = 200 * time.Millisecond
	ctx := context.Background()

	react, err := reactor.New(reactor.WithLogger(mockLogger))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if err := react.Add(mockWork); err != nil {
		t.Fatalf("expected no error when adding worker, got %v", err)
	}

	errCh := react.Errors()
	wg := &sync.WaitGroup{}

	wg.Go(func() {
		if err := react.Start(ctx); err != nil {
			t.Fatalf("expected no error from Start, got %v", err)
		}
	})

	time.Sleep(100 * time.Millisecond)

	if err := react.Shutdown(ctx); err != nil {
		t.Fatalf("expected no error from Shutdown, got %v", err)
	}

	wg.Wait()

	if mockWork.shutdownCtx.Load() != ctx {
		t.Fatal("expected worker Shutdown context to match the context passed to Shutdown")
	}

	if mockWork.countShutdownCalls.Load() != 1 {
		t.Fatalf(
			"expected worker Shutdown to be called once, got %d",
			mockWork.countShutdownCalls.Load(),
		)
	}

	if len(errCh) != 1 {
		t.Fatalf("expected error channel to have exactly one entry, got %d", len(errCh))
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, expectedErr) {
			t.Fatalf("expected error %v, got %v", expectedErr, err)
		}
	default:
		t.Fatal("expected an error in the error channel, but it was empty")
	}

	if len(mockLogger.debugLog) != 4 {
		t.Fatalf("expected exactly four debug log entries, got %d", len(mockLogger.debugLog))
	}

	if !strings.Contains(
		mockLogger.debugLog[1].msg,
		"reactor: shutting down worker",
	) {
		t.Fatalf(
			"expected first debug log message to contain 'reactor: shutting down worker', got '%s'",
			mockLogger.debugLog[1].msg,
		)
	}

	if len(mockLogger.debugLog[1].args) != 1 {
		t.Fatalf(
			"expected first debug log to have exactly one argument, got %d",
			len(mockLogger.debugLog[1].args),
		)
	}

	args, ok := mockLogger.debugLog[1].args[0].(reactor.LogArgs)
	if !ok {
		t.Fatal("expected first debug log argument to be of type reactor.LogArgs")
	}

	worker, ok := args["worker"]
	if !ok {
		t.Fatal("expected first debug log argument to have key 'worker'")
	}

	mWorker, ok := worker.(*mockWorker)
	if !ok {
		t.Fatal("expected 'worker' argument to be of type *mockWorker")
	}

	if mWorker != mockWork {
		t.Fatal(
			"expected 'worker' argument to be the mockWorker instance added to the reactor",
		)
	}

	if mockLogger.debugLog[1].ctx != ctx {
		t.Fatal("expected first debug log context to match the context passed to Shutdown")
	}

	if !strings.Contains(
		mockLogger.debugLog[2].msg,
		"reactor: all workers shut down",
	) {
		t.Fatalf(
			"expected second debug log message to contain 'reactor: all workers shut down', got '%s'",
			mockLogger.debugLog[2].msg,
		)
	}

	if len(mockLogger.debugLog[2].args) != 0 {
		t.Fatalf(
			"expected second debug log to have no arguments, got %d",
			len(mockLogger.debugLog[2].args),
		)
	}

	if mockLogger.debugLog[2].ctx != ctx {
		t.Fatal("expected second debug log context to match the context passed to Shutdown")
	}

	if len(mockLogger.errorLog) != 0 {
		t.Fatalf("expected no error log entries, got %d", len(mockLogger.errorLog))
	}
}

func TestReactor_Shutdown_WithLoggerCancelContext(t *testing.T) {
	t.Parallel()

	mockLogger := new(mockLogger)
	mockWork := new(mockWorker)
	mockWork.timeLifeStarted = 300 * time.Millisecond
	mockWork.timeLifeShutdown = 200 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())

	react, err := reactor.New(reactor.WithLogger(mockLogger))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if err := react.Add(mockWork); err != nil {
		t.Fatalf("expected no error when adding worker, got %v", err)
	}

	errCh := react.Errors()
	wg := &sync.WaitGroup{}

	wg.Go(func() {
		if err := react.Start(context.Background()); err != nil {
			t.Fatalf("expected no error from Start, got %v", err)
		}
	})

	time.Sleep(100 * time.Millisecond)

	wg.Go(func() {
		if err := react.Shutdown(ctx); err != nil {
			t.Fatalf("expected no error from Shutdown, got %v", err)
		}
	})

	time.Sleep(100 * time.Millisecond)
	cancel()

	wg.Wait()

	if mockWork.shutdownCtx.Load() != ctx {
		t.Fatal("expected worker Shutdown context to match the context passed to Shutdown")
	}

	if mockWork.countShutdownCalls.Load() != 1 {
		t.Fatalf(
			"expected worker Shutdown to be called once, got %d",
			mockWork.countShutdownCalls.Load(),
		)
	}

	if len(errCh) != 0 {
		t.Fatalf("expected error channel to be empty, got %d entries", len(errCh))
	}

	if len(mockLogger.debugLog) != 4 {
		t.Fatalf("expected exactly four debug log entries, got %d", len(mockLogger.debugLog))
	}

	if !strings.Contains(
		mockLogger.debugLog[1].msg,
		"reactor: shutting down worker",
	) {
		t.Fatalf(
			"expected first debug log message to contain 'reactor: shutting down worker', got '%s'",
			mockLogger.debugLog[1].msg,
		)
	}

	if len(mockLogger.debugLog[1].args) != 1 {
		t.Fatalf(
			"expected first debug log to have exactly one argument, got %d",
			len(mockLogger.debugLog[1].args),
		)
	}

	args, ok := mockLogger.debugLog[1].args[0].(reactor.LogArgs)
	if !ok {
		t.Fatal("expected first debug log argument to be of type reactor.LogArgs")
	}

	worker, ok := args["worker"]
	if !ok {
		t.Fatal("expected first debug log argument to have key 'worker'")
	}

	mWorker, ok := worker.(*mockWorker)
	if !ok {
		t.Fatal("expected 'worker' argument to be of type *mockWorker")
	}

	if mWorker != mockWork {
		t.Fatal(
			"expected 'worker' argument to be the mockWorker instance added to the reactor",
		)
	}

	if mockLogger.debugLog[1].ctx != ctx {
		t.Fatal("expected first debug log context to match the context passed to Shutdown")
	}

	if !strings.Contains(
		mockLogger.debugLog[2].msg,
		"reactor: workers shutdown cancelled by context",
	) {
		t.Fatalf(
			"expected second debug log message to contain 'reactor: workers shutdown cancelled by context', got '%s'",
			mockLogger.debugLog[2].msg,
		)
	}

	if len(mockLogger.debugLog[2].args) != 0 {
		t.Fatalf(
			"expected second debug log to have no arguments, got %d",
			len(mockLogger.debugLog[2].args),
		)
	}

	if mockLogger.debugLog[2].ctx != ctx {
		t.Fatal("expected second debug log context to match the context passed to Shutdown")
	}

	if len(mockLogger.errorLog) != 0 {
		t.Fatalf("expected no error log entries, got %d", len(mockLogger.errorLog))
	}
}

func TestReactor_Shutdown_WithLoggerFullErrorsChannel(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("worker error")
	mockLogger := new(mockLogger)
	workers := make([]*mockWorker, 2)
	ctx := context.Background()

	for idx := range workers {
		workers[idx] = new(mockWorker)
		workers[idx].shutdownErr = expectedErr
		workers[idx].timeLifeStarted = 200 * time.Millisecond
	}

	react, err := reactor.New(reactor.WithLogger(mockLogger), reactor.WithErrorBufferSize(1))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	for idx := range workers {
		if err := react.Add(workers[idx]); err != nil {
			t.Fatalf("expected no error when adding worker %d, got %v", idx, err)
		}
	}

	wg := &sync.WaitGroup{}

	wg.Go(func() {
		err := react.Start(ctx)
		if err == nil {
			t.Fatalf("expected no error from Start, got %v", err)
		}

		if !errors.Is(err, reactor.ErrChannelFull) {
			t.Fatalf("expected error %v from Start, got %v", reactor.ErrChannelFull, err)
		}
	})

	time.Sleep(100 * time.Millisecond)

	err = react.Shutdown(ctx)
	if err == nil {
		t.Fatalf("expected no error from Shutdown, got %v", err)
	}

	if !errors.Is(err, reactor.ErrChannelFull) {
		t.Fatalf("expected error %v from Shutdown, got %v", reactor.ErrChannelFull, err)
	}

	wg.Wait()

	if len(mockLogger.debugLog) != 4 {
		t.Fatalf("expected exactly four debug log entries, got %d", len(mockLogger.debugLog))
	}

	if !strings.Contains(
		mockLogger.debugLog[2].msg,
		"reactor: shutting down worker",
	) {
		t.Fatalf(
			"expected debug log message to contain 'reactor: shutting down worker', got '%s'",
			mockLogger.debugLog[2].msg,
		)
	}

	if len(mockLogger.debugLog[2].args) != 1 {
		t.Fatalf(
			"expected debug log to have exactly one argument, got %d",
			len(mockLogger.debugLog[2].args),
		)
	}

	args, ok := mockLogger.debugLog[2].args[0].(reactor.LogArgs)
	if !ok {
		t.Fatal("expected debug log argument to be of type reactor.LogArgs")
	}

	worker, ok := args["worker"]
	if !ok {
		t.Fatal("expected debug log argument to have key 'worker'")
	}

	if _, ok := worker.(*mockWorker); !ok {
		t.Fatal("expected 'worker' argument to be of type *mockWorker")
	}

	if mockLogger.debugLog[2].ctx != ctx {
		t.Fatal("expected debug log context to match the context passed to Shutdown")
	}

	if len(mockLogger.errorLog) != 1 {
		t.Fatalf("expected exactly one error log entry, got %d", len(mockLogger.errorLog))
	}

	if !errors.Is(mockLogger.errorLog[0].err, reactor.ErrFailedToSend) {
		t.Fatalf(
			"expected error log to contain error %v, got %v",
			reactor.ErrFailedToSend,
			mockLogger.errorLog[0].err,
		)
	}

	if !errors.Is(mockLogger.errorLog[0].err, expectedErr) {
		t.Fatalf(
			"expected error log to contain error %v, got %v",
			expectedErr,
			mockLogger.errorLog[0].err,
		)
	}

	if mockLogger.errorLog[0].ctx != ctx {
		t.Fatal("expected error log context to match the context passed to Shutdown")
	}

	if len(mockLogger.errorLog[0].args) != 0 {
		t.Fatalf(
			"expected error log to have no arguments, got %d",
			len(mockLogger.errorLog[0].args),
		)
	}
}

func TestReactor_Errors_ChannelClosed(t *testing.T) {
	t.Parallel()

	mockWorker := new(mockWorker)
	mockWorker.startErr = errors.New("start error")

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	errCh := react.Errors()

	if err := react.Add(mockWorker); err != nil {
		t.Fatalf("expected no error when adding worker, got %v", err)
	}

	if err := react.Start(context.Background()); err != nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	if _, ok := <-errCh; !ok {
		t.Fatal("expected error channel to be open, but it was closed")
	}

	if _, ok := <-errCh; ok {
		t.Fatal("expected error channel to be closed, but it was open")
	}
}

func TestReactor_Errors_MultipleWorkers(t *testing.T) {
	t.Parallel()

	workers := make([]*mockWorker, 2)

	for idx := range workers {
		workers[idx] = new(mockWorker)
		workers[idx].startErr = errors.New("start error")
	}

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	errCh1 := react.Errors()
	errCh2 := react.Errors()

	for _, worker := range workers {
		if err := react.Add(worker); err != nil {
			t.Fatalf("expected no error when adding worker, got %v", err)
		}
	}

	if err := react.Start(context.Background()); err != nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	if _, ok := <-errCh1; !ok {
		t.Fatal("expected first error channel to be open, but it was closed")
	}

	if _, ok := <-errCh2; !ok {
		t.Fatal("expected second error channel to be open, but it was closed")
	}

	if _, ok := <-errCh1; ok {
		t.Fatal("expected first error channel to be closed, but it was open")
	}

	if _, ok := <-errCh2; ok {
		t.Fatal("expected second error channel to be closed, but it was open")
	}
}

func TestReactor_Errors_ConcurrentWorkers(t *testing.T) {
	t.Parallel()

	var errCounter atomic.Int64

	numWorkers := 1000
	workers := make([]*mockWorker, numWorkers)

	for idx := range workers {
		workers[idx] = new(mockWorker)
		workers[idx].startErr = errors.New("start error")
	}

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	wg := &sync.WaitGroup{}

	for range 100 {
		wg.Go(func() {
			errCh := react.Errors()

			for {
				_, ok := <-errCh
				if !ok {
					break
				}

				errCounter.Add(1)
			}
		})
	}

	for _, worker := range workers {
		if err := react.Add(worker); err != nil {
			t.Fatalf("expected no error when adding worker, got %v", err)
		}
	}

	if err := react.Start(context.Background()); err != nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	wg.Wait()

	if errCounter.Load() != int64(numWorkers) {
		t.Fatalf("expected %d errors, got %d", numWorkers, errCounter.Load())
	}
}

func TestReactor_Errors_ConcurrentDynamicWorkers(t *testing.T) {
	t.Parallel()

	var errCounter atomic.Int64

	workers := make([]*mockFnWorker, 10)

	for idx := range workers {
		workers[idx] = new(mockFnWorker)
		workers[idx].errCh = make(chan error, 1)
		workers[idx].errFn = func(errCh chan error) {
			for range 100 {
				errCh <- errors.New("worker error")
			}
		}
	}

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	wg := &sync.WaitGroup{}

	for range 100 {
		wg.Go(func() {
			errCh := react.Errors()

			for {
				_, ok := <-errCh
				if !ok {
					break
				}

				errCounter.Add(1)
			}
		})
	}

	for _, worker := range workers {
		if err := react.Add(worker); err != nil {
			t.Fatalf("expected no error when adding worker, got %v", err)
		}
	}

	if err := react.Start(context.Background()); err != nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	wg.Wait()

	expectedErrCount := int64(len(workers) * 100)
	if errCounter.Load() != expectedErrCount {
		t.Fatalf("expected %d errors, got %d", expectedErrCount, errCounter.Load())
	}
}

func TestReactor_Errors_NestedReactors(t *testing.T) {
	t.Parallel()

	var errCounter atomic.Int64

	reactors := make([]*reactor.Reactor, 10)

	for idx := range reactors {
		workers := make([]*mockFnWorker, 10)

		for idx := range workers {
			workers[idx] = &mockFnWorker{
				errCh: make(chan error, 1),
				errFn: func(errCh chan error) {
					for range 10 {
						errCh <- errors.New("worker error")
					}
				},
			}
		}

		react, err := reactor.New()
		if err != nil {
			t.Fatalf("expected no error when creating reactor %d, got %v", idx, err)
		}

		if react == nil {
			t.Fatalf("expected non-nil reactor %d, got nil", idx)
		}

		for idx := range workers {
			if err := react.Add(workers[idx]); err != nil {
				t.Fatalf(
					"expected no error when adding worker %d to reactor %d, got %v",
					idx,
					idx,
					err,
				)
			}
		}

		reactors[idx] = react
	}

	react, err := reactor.New()
	if err != nil {
		t.Fatalf("expected no error when creating main reactor, got %v", err)
	}

	if react == nil {
		t.Fatal("expected non-nil main reactor, got nil")
	}

	wg := &sync.WaitGroup{}

	for range 100 {
		wg.Go(func() {
			errCh := react.Errors()

			for {
				_, ok := <-errCh
				if !ok {
					break
				}

				errCounter.Add(1)
			}
		})
	}

	for idx := range reactors {
		if err := react.Add(reactors[idx]); err != nil {
			t.Fatalf("expected no error when adding reactor %d to main reactor, got %v", idx, err)
		}
	}

	if err := react.Start(context.Background()); err != nil {
		t.Fatalf("expected no error from Start, got %v", err)
	}

	wg.Wait()

	expectedErrCount := int64(len(reactors) * 10 * 10)
	if errCounter.Load() != expectedErrCount {
		t.Fatalf("expected %d errors, got %d", expectedErrCount, errCounter.Load())
	}
}
