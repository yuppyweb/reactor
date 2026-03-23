package reactor_test

import (
	"context"
	"errors"
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
	errCh              chan error
}

var _ reactor.Worker = (*mockWorker)(nil)

func (w *mockWorker) Start(ctx context.Context) {
	w.startCtx.Store(ctx)
	w.countStartCalls.Add(1)
	time.Sleep(w.timeLifeStarted)
	close(w.errCh)
}

func (w *mockWorker) Shutdown(ctx context.Context) {
	w.shutdownCtx.Store(ctx)
	w.countShutdownCalls.Add(1)
	time.Sleep(w.timeLifeShutdown)
}

func (w *mockWorker) Errors() <-chan error {
	return w.errCh
}

type testKey struct{}

func TestReactor_NewWithNilWorker(t *testing.T) {
	t.Parallel()

	_, err := reactor.New(nil)
	if !errors.Is(err, reactor.ErrWorkerIsNil) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReactor_StartOnce(t *testing.T) {
	t.Parallel()

	expectErr := errors.New("reactor already started")
	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	work := new(mockWorker)

	work.errCh = make(chan error, 1)
	work.errCh <- expectErr

	rc, err := reactor.New(work)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 1 {
		t.Fatalf("expected error channel capacity to be 1, got %d", cap(rc.Errors()))
	}

	rc.Start(ctx)

	err, ok := <-rc.Errors()
	if !errors.Is(err, expectErr) {
		t.Fatalf("unexpected error: %v", err)
	}

	if !ok {
		t.Fatal("error channel closed unexpectedly")
	}

	rc.Start(ctx)

	err, ok = <-rc.Errors()
	if err != nil {
		t.Fatalf("unexpected error on second start: %v", err)
	}

	if ok {
		t.Fatal("expected error channel to be closed on second start, but it was open")
	}

	if work.countStartCalls.Load() != 1 {
		t.Fatalf(
			"expected Start to be called once, but it was called %d times",
			work.countStartCalls.Load(),
		)
	}

	if work.startCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Start")
	}
}

func TestReactor_StartIsStarted(t *testing.T) {
	t.Parallel()

	expectErr := errors.New("reactor already started")
	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	work := new(mockWorker)
	work.timeLifeStarted = 300 * time.Millisecond

	work.errCh = make(chan error, 1)
	work.errCh <- expectErr

	rc, err := reactor.New(work)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 1 {
		t.Fatalf("expected error channel capacity to be 1, got %d", cap(rc.Errors()))
	}

	go rc.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	rc.Start(ctx)

	time.Sleep(300 * time.Millisecond)

	err, ok := <-rc.Errors()
	if !errors.Is(err, expectErr) {
		t.Fatalf("unexpected error: %v", err)
	}

	if !ok {
		t.Fatal("error channel closed unexpectedly")
	}

	if work.countStartCalls.Load() != 1 {
		t.Fatalf(
			"expected Start to be called once, but it was called %d times",
			work.countStartCalls.Load(),
		)
	}

	if work.startCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Start")
	}
}

func TestReactor_StartWithoutWorkers(t *testing.T) {
	t.Parallel()

	rc, err := reactor.New()
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 0 {
		t.Fatalf("expected error channel capacity to be 0, got %d", cap(rc.Errors()))
	}

	rc.Start(context.Background())

	err, ok := <-rc.Errors()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ok {
		t.Fatal("expected error channel to be closed, but it was open")
	}
}

func TestReactor_StartSingleShortWorker(t *testing.T) {
	t.Parallel()

	work := new(mockWorker)
	work.errCh = make(chan error, 1)
	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	rc, err := reactor.New(work)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 1 {
		t.Fatalf("expected error channel capacity to be 1, got %d", cap(rc.Errors()))
	}

	rc.Start(ctx)

	err, ok := <-rc.Errors()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ok {
		t.Fatal("expected error channel to be closed, but it was open")
	}

	if work.startCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Start")
	}
}

func TestReactor_StartSingleShortWorkerWithError(t *testing.T) {
	t.Parallel()

	countErr := 0
	expectedErr := errors.New("test error")
	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	work := new(mockWorker)

	work.errCh = make(chan error, 1)
	work.errCh <- expectedErr

	rc, err := reactor.New(work)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 1 {
		t.Fatalf("expected error channel capacity to be 1, got %d", cap(rc.Errors()))
	}

	rc.Start(ctx)

	for err := range rc.Errors() {
		if !errors.Is(err, expectedErr) {
			t.Fatalf("unexpected error: %v", err)
		}

		countErr++
	}

	if countErr != 1 {
		t.Fatalf("expected 1 error, got %d", countErr)
	}

	if work.startCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Start")
	}
}

func TestReactor_StartSingleShortWorkerWithManyErrors(t *testing.T) {
	t.Parallel()

	countErr := 0
	expectedErr := errors.New("test error")
	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	work := new(mockWorker)
	work.errCh = make(chan error, 50)

	for range 50 {
		work.errCh <- expectedErr
	}

	rc, err := reactor.New(work)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 50 {
		t.Fatalf("expected error channel capacity to be 50, got %d", cap(rc.Errors()))
	}

	rc.Start(ctx)

	for err := range rc.Errors() {
		if !errors.Is(err, expectedErr) {
			t.Fatalf("unexpected error: %v", err)
		}

		countErr++
	}

	if countErr != 50 {
		t.Fatalf("expected 50 errors, got %d", countErr)
	}

	if work.startCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Start")
	}
}

func TestReactor_StartMultipleShortWorkers(t *testing.T) {
	t.Parallel()

	work1 := new(mockWorker)
	work1.errCh = make(chan error, 1)

	work2 := new(mockWorker)
	work2.errCh = make(chan error, 1)
	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	rc, err := reactor.New(work1, work2)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 2 {
		t.Fatalf("expected error channel capacity to be 2, got %d", cap(rc.Errors()))
	}

	rc.Start(ctx)

	err, ok := <-rc.Errors()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ok {
		t.Fatal("expected error channel to be closed, but it was open")
	}

	if work1.startCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Start for worker 1")
	}

	if work2.startCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Start for worker 2")
	}
}

func TestReactor_StartMultipleShortWorkersWithErrors(t *testing.T) {
	t.Parallel()

	countErr := 0
	expectedErr1 := errors.New("test error 1")
	expectedErr2 := errors.New("test error 2")
	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	work1 := new(mockWorker)

	work1.errCh = make(chan error, 1)
	work1.errCh <- expectedErr1

	work2 := new(mockWorker)

	work2.errCh = make(chan error, 1)
	work2.errCh <- expectedErr2

	rc, err := reactor.New(work1, work2)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 2 {
		t.Fatalf("expected error channel capacity to be 2, got %d", cap(rc.Errors()))
	}

	rc.Start(ctx)

	for err := range rc.Errors() {
		if !errors.Is(err, expectedErr1) && !errors.Is(err, expectedErr2) {
			t.Fatalf("unexpected error: %v", err)
		}

		countErr++
	}

	if countErr != 2 {
		t.Fatalf("expected 2 errors, got %d", countErr)
	}

	if work1.startCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Start for worker 1")
	}

	if work2.startCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Start for worker 2")
	}
}

func TestReactor_StartMultipleShortWorkersWithManyErrors(t *testing.T) {
	t.Parallel()

	countErr := 0
	expectedErr1 := errors.New("test error 1")
	expectedErr2 := errors.New("test error 2")
	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	work1 := new(mockWorker)
	work1.errCh = make(chan error, 50)

	work2 := new(mockWorker)
	work2.errCh = make(chan error, 50)

	for range 50 {
		work1.errCh <- expectedErr1

		work2.errCh <- expectedErr2
	}

	rc, err := reactor.New(work1, work2)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 100 {
		t.Fatalf("expected error channel capacity to be 100, got %d", cap(rc.Errors()))
	}

	rc.Start(ctx)

	for err := range rc.Errors() {
		if !errors.Is(err, expectedErr1) && !errors.Is(err, expectedErr2) {
			t.Fatalf("unexpected error: %v", err)
		}

		countErr++
	}

	if countErr != 100 {
		t.Fatalf("expected 100 errors, got %d", countErr)
	}

	if work1.startCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Start for worker 1")
	}
}

func TestReactor_StartSingleLongWorker(t *testing.T) {
	t.Parallel()

	work := new(mockWorker)
	work.errCh = make(chan error, 1)
	work.timeLifeStarted = 500 * time.Millisecond
	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	rc, err := reactor.New(work)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 1 {
		t.Fatalf("expected error channel capacity to be 1, got %d", cap(rc.Errors()))
	}

	go rc.Start(ctx)

	select {
	case err := <-rc.Errors():
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for worker to start")
	}

	err, ok := <-rc.Errors()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ok {
		t.Fatal("expected error channel to be closed, but it was open")
	}

	if work.startCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Start")
	}
}

func TestReactor_StartSingleLongWorkerWithError(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("test error")
	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	work := new(mockWorker)
	work.errCh = make(chan error, 1)

	work.timeLifeStarted = 500 * time.Millisecond
	work.errCh <- expectedErr

	rc, err := reactor.New(work)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 1 {
		t.Fatalf("expected error channel capacity to be 1, got %d", cap(rc.Errors()))
	}

	go rc.Start(ctx)

	select {
	case err := <-rc.Errors():
		if !errors.Is(err, expectedErr) {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for worker to start")
	}

	err, ok := <-rc.Errors()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ok {
		t.Fatal("expected error channel to be closed, but it was open")
	}

	if work.startCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Start")
	}
}

func TestReactor_StartSingleLongWorkerWithManyErrors(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("test error")
	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()

	work := new(mockWorker)
	work.errCh = make(chan error, 1)
	work.timeLifeStarted = 500 * time.Millisecond

	rc, err := reactor.New(work)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 1 {
		t.Fatalf("expected error channel capacity to be 1, got %d", cap(rc.Errors()))
	}

	var countWriteErr atomic.Int32
	var countReadErr atomic.Int32

	go rc.Start(ctx)

	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			select {
			case <-ctx.Done():
				return
			default:
				work.errCh <- expectedErr

				countWriteErr.Add(1)
			}
		}
	}()

	for err := range rc.Errors() {
		if !errors.Is(err, expectedErr) {
			t.Fatalf("unexpected error: %v", err)
		}

		countReadErr.Add(1)
	}

	if countWriteErr.Load() == 0 {
		t.Fatal(
			"expected at least one error to be written to the worker's error channel, but none were written",
		)
	}

	if countWriteErr.Load() != countReadErr.Load() {
		t.Fatalf(
			"expected number of errors read from reactor to match number of errors written to worker, "+
				"but got %d read and %d written",
			countReadErr.Load(),
			countWriteErr.Load(),
		)
	}

	if work.startCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Start")
	}
}

func TestReactor_StartMultipleLongWorkers(t *testing.T) {
	t.Parallel()

	work1 := new(mockWorker)
	work1.errCh = make(chan error, 1)
	work1.timeLifeStarted = 300 * time.Millisecond

	work2 := new(mockWorker)
	work2.errCh = make(chan error, 1)
	work2.timeLifeStarted = 500 * time.Millisecond
	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	rc, err := reactor.New(work1, work2)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 2 {
		t.Fatalf("expected error channel capacity to be 2, got %d", cap(rc.Errors()))
	}

	go rc.Start(ctx)

	select {
	case err := <-rc.Errors():
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for workers to start")
	}

	err, ok := <-rc.Errors()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ok {
		t.Fatal("expected error channel to be closed, but it was open")
	}

	if work1.startCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Start for worker 1")
	}

	if work2.startCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Start for worker 2")
	}
}

func TestReactor_StartMultipleLongWorkersWithErrors(t *testing.T) {
	t.Parallel()

	expectedErr1 := errors.New("test error 1")
	expectedErr2 := errors.New("test error 2")
	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	work1 := new(mockWorker)
	work1.errCh = make(chan error, 1)

	work1.timeLifeStarted = 300 * time.Millisecond
	work1.errCh <- expectedErr1

	work2 := new(mockWorker)
	work2.errCh = make(chan error, 1)

	work2.timeLifeStarted = 500 * time.Millisecond
	work2.errCh <- expectedErr2

	rc, err := reactor.New(work1, work2)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 2 {
		t.Fatalf("expected error channel capacity to be 2, got %d", cap(rc.Errors()))
	}

	go rc.Start(ctx)

	isExpectedErr1Received := false
	isExpectedErr2Received := false
	countErr := 0
	finishTime := time.Now().Add(time.Second)

	for err := range rc.Errors() {
		if time.Now().After(finishTime) {
			t.Fatal("timeout waiting for errors from workers")
		}

		if !errors.Is(err, expectedErr1) && !errors.Is(err, expectedErr2) {
			t.Fatalf("unexpected error: %v", err)
		}

		if errors.Is(err, expectedErr1) {
			isExpectedErr1Received = true
		}

		if errors.Is(err, expectedErr2) {
			isExpectedErr2Received = true
		}

		countErr++
	}

	if !isExpectedErr1Received {
		t.Fatal("expected error 1 not received")
	}

	if !isExpectedErr2Received {
		t.Fatal("expected error 2 not received")
	}

	if countErr != 2 {
		t.Fatalf("expected 2 errors, got %d", countErr)
	}

	if work1.startCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Start for worker 1")
	}

	if work2.startCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Start for worker 2")
	}
}

func TestReactor_StartMultipleLongWorkersWithManyErrors(t *testing.T) {
	t.Parallel()

	expectedErr1 := errors.New("test error 1")
	expectedErr2 := errors.New("test error 2")
	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()

	work1 := new(mockWorker)
	work1.errCh = make(chan error, 1)
	work1.timeLifeStarted = 500 * time.Millisecond

	work2 := new(mockWorker)
	work2.errCh = make(chan error, 1)
	work2.timeLifeStarted = 500 * time.Millisecond

	rc, err := reactor.New(work1, work2)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 2 {
		t.Fatalf("expected error channel capacity to be 2, got %d", cap(rc.Errors()))
	}

	var (
		countWriteErr atomic.Int32
		countReadErr  atomic.Int32
	)

	go rc.Start(ctx)

	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			select {
			case <-ctx.Done():
				return
			case work1.errCh <- expectedErr1:
				countWriteErr.Add(1)
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			select {
			case <-ctx.Done():
				return
			case work2.errCh <- expectedErr2:
				countWriteErr.Add(1)
			}
		}
	}()

	finishTime := time.Now().Add(time.Second)

	for err := range rc.Errors() {
		if time.Now().After(finishTime) {
			t.Fatal("timeout waiting for errors from workers")
		}

		if !errors.Is(err, expectedErr1) && !errors.Is(err, expectedErr2) {
			t.Fatalf("unexpected error: %v", err)
		}

		countReadErr.Add(1)
	}

	if countWriteErr.Load() == 0 {
		t.Fatal(
			"expected at least one error to be written to the workers' error channels, but none were written",
		)
	}

	if countWriteErr.Load() != countReadErr.Load() {
		t.Fatalf(
			"expected number of errors read from reactor to match number of errors written to workers, "+
				"but got %d read and %d written",
			countReadErr.Load(),
			countWriteErr.Load(),
		)
	}

	if work1.startCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Start for worker 1")
	}
}

func TestReactor_ShutdownWithoutStart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	work := new(mockWorker)
	work.errCh = make(chan error, 1)

	rc, err := reactor.New(work)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 1 {
		t.Fatalf("expected error channel capacity to be 1, got %d", cap(rc.Errors()))
	}

	rc.Shutdown(ctx)

	if work.countShutdownCalls.Load() != 0 {
		t.Fatalf(
			"expected Shutdown to not be called, but it was called %d times",
			work.countShutdownCalls.Load(),
		)
	}
}

func TestReactor_ShutdownAfterStart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	work := new(mockWorker)
	work.errCh = make(chan error, 1)

	rc, err := reactor.New(work)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 1 {
		t.Fatalf("expected error channel capacity to be 1, got %d", cap(rc.Errors()))
	}

	rc.Start(ctx)
	rc.Shutdown(ctx)

	if work.countStartCalls.Load() != 1 {
		t.Fatalf(
			"expected Start to be called once, but it was called %d times",
			work.countStartCalls.Load(),
		)
	}

	if work.countShutdownCalls.Load() != 0 {
		t.Fatalf(
			"expected Shutdown to not be called, but it was called %d times",
			work.countShutdownCalls.Load(),
		)
	}
}

func TestReactor_ShutdownOnce(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	work := new(mockWorker)
	work.errCh = make(chan error, 1)
	work.timeLifeStarted = 500 * time.Millisecond

	rc, err := reactor.New(work)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 1 {
		t.Fatalf("expected error channel capacity to be 1, got %d", cap(rc.Errors()))
	}

	go rc.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	rc.Shutdown(ctx)
	rc.Shutdown(ctx)

	if work.countShutdownCalls.Load() != 1 {
		t.Fatalf(
			"expected Shutdown to be called once, but it was called %d times",
			work.countShutdownCalls.Load(),
		)
	}

	if work.shutdownCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Shutdown")
	}
}

func TestReactor_ShutdownOnceConcurrent(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	work := new(mockWorker)
	work.errCh = make(chan error, 1)
	work.timeLifeStarted = 500 * time.Millisecond

	rc, err := reactor.New(work)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 1 {
		t.Fatalf("expected error channel capacity to be 1, got %d", cap(rc.Errors()))
	}

	go rc.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	wg := sync.WaitGroup{}

	wg.Go(func() {
		rc.Shutdown(ctx)
	})

	wg.Go(func() {
		rc.Shutdown(ctx)
	})

	wg.Go(func() {
		rc.Shutdown(ctx)
	})

	wg.Wait()

	if work.countShutdownCalls.Load() != 1 {
		t.Fatalf(
			"expected Shutdown to be called once, but it was called %d times",
			work.countShutdownCalls.Load(),
		)
	}

	if work.shutdownCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Shutdown")
	}
}

func TestReactor_ShutdownSingleLongWorker(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	work := new(mockWorker)
	work.errCh = make(chan error, 1)
	work.timeLifeStarted = 500 * time.Millisecond
	work.timeLifeShutdown = 100 * time.Millisecond

	rc, err := reactor.New(work)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 1 {
		t.Fatalf("expected error channel capacity to be 1, got %d", cap(rc.Errors()))
	}

	go rc.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	start := time.Now()

	rc.Shutdown(ctx)

	end := time.Since(start)

	if end > 300*time.Millisecond {
		t.Fatalf("shutdown took too long: %v", end)
	}

	if work.countShutdownCalls.Load() != 1 {
		t.Fatal("expected Shutdown to be called once, but it was called multiple times")
	}

	if work.shutdownCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Shutdown")
	}
}

func TestReactor_ShutdownMultipleLongWorkers(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	work1 := new(mockWorker)
	work1.errCh = make(chan error, 1)
	work1.timeLifeStarted = 300 * time.Millisecond
	work1.timeLifeShutdown = 100 * time.Millisecond

	work2 := new(mockWorker)
	work2.errCh = make(chan error, 1)
	work2.timeLifeStarted = 500 * time.Millisecond
	work2.timeLifeShutdown = 200 * time.Millisecond

	rc, err := reactor.New(work1, work2)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 2 {
		t.Fatalf("expected error channel capacity to be 2, got %d", cap(rc.Errors()))
	}

	go rc.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	start := time.Now()

	rc.Shutdown(ctx)

	end := time.Since(start)

	if end > 500*time.Millisecond {
		t.Fatalf("shutdown took too long: %v", end)
	}

	if work1.countShutdownCalls.Load() != 1 {
		t.Fatal("expected Shutdown to be called once for worker 1, but it was not called")
	}

	if work2.countShutdownCalls.Load() != 1 {
		t.Fatal("expected Shutdown to be called once for worker 2, but it was not called")
	}

	if work1.shutdownCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Shutdown for worker 1")
	}

	if work2.shutdownCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Shutdown for worker 2")
	}
}

func TestReactor_ShutdownWithCancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	work := new(mockWorker)
	work.errCh = make(chan error, 1)
	work.timeLifeStarted = 500 * time.Millisecond
	work.timeLifeShutdown = 500 * time.Millisecond

	rc, err := reactor.New(work)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 1 {
		t.Fatalf("expected error channel capacity to be 1, got %d", cap(rc.Errors()))
	}

	go rc.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	go rc.Shutdown(ctx)

	select {
	case <-ctx.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for context to be cancelled")
	}
}

func TestReactor_NestedReactors(t *testing.T) {
	t.Parallel()

	expectErr1 := errors.New("test error1")
	expectErr2 := errors.New("test error2")
	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	work1 := new(mockWorker)
	work1.errCh = make(chan error, 1)
	work1.timeLifeStarted = 300 * time.Millisecond

	work1.timeLifeShutdown = 100 * time.Millisecond
	work1.errCh <- expectErr1

	work2 := new(mockWorker)
	work2.errCh = make(chan error, 1)
	work2.timeLifeStarted = 500 * time.Millisecond

	work2.timeLifeShutdown = 200 * time.Millisecond
	work2.errCh <- expectErr2

	rc1, err := reactor.New(work1)
	if err != nil {
		t.Fatalf("unexpected error creating reactor 1: %v", err)
	}

	if cap(rc1.Errors()) != 1 {
		t.Fatalf("expected error channel capacity to be 1 for reactor 1, got %d", cap(rc1.Errors()))
	}

	rc2, err := reactor.New(work2)
	if err != nil {
		t.Fatalf("unexpected error creating reactor 2: %v", err)
	}

	if cap(rc2.Errors()) != 1 {
		t.Fatalf("expected error channel capacity to be 1 for reactor 2, got %d", cap(rc2.Errors()))
	}

	rcMain, err := reactor.New(rc1, rc2)
	if err != nil {
		t.Fatalf("unexpected error creating main reactor: %v", err)
	}

	if cap(rcMain.Errors()) != 2 {
		t.Fatalf("expected error channel capacity to be 2, got %d", cap(rcMain.Errors()))
	}

	go rcMain.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	go rcMain.Shutdown(ctx)

	isExpectedErr1Received := false
	isExpectedErr2Received := false
	countErr := 0
	finishTime := time.Now().Add(time.Second)

	for err := range rcMain.Errors() {
		if time.Now().After(finishTime) {
			t.Fatal("timeout waiting for errors from workers")
		}

		if !errors.Is(err, expectErr1) && !errors.Is(err, expectErr2) {
			t.Fatalf("unexpected error: %v", err)
		}

		if errors.Is(err, expectErr1) {
			isExpectedErr1Received = true
		}

		if errors.Is(err, expectErr2) {
			isExpectedErr2Received = true
		}

		countErr++
	}

	if !isExpectedErr1Received {
		t.Fatal("expected error 1 not received")
	}

	if !isExpectedErr2Received {
		t.Fatal("expected error 2 not received")
	}

	if countErr != 2 {
		t.Fatalf("expected 2 errors, got %d", countErr)
	}

	if work1.countStartCalls.Load() != 1 {
		t.Fatalf(
			"expected Start to be called once for worker 1, but it was called %d times",
			work1.countStartCalls.Load(),
		)
	}

	if work2.countStartCalls.Load() != 1 {
		t.Fatalf(
			"expected Start to be called once for worker 2, but it was called %d times",
			work2.countStartCalls.Load(),
		)
	}

	if work1.countShutdownCalls.Load() != 1 {
		t.Fatalf(
			"expected Shutdown to be called once for worker 1, but it was called %d times",
			work1.countShutdownCalls.Load(),
		)
	}

	if work2.countShutdownCalls.Load() != 1 {
		t.Fatalf(
			"expected Shutdown to be called once for worker 2, but it was called %d times",
			work2.countShutdownCalls.Load(),
		)
	}

	if work1.startCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Start for worker 1")
	}

	if work2.startCtx.Load() != ctx {
		t.Fatal("unexpected context passed to Start for worker 2")
	}
}

func TestReactor_ManyWorkers(t *testing.T) {
	t.Parallel()

	expectErr := errors.New("test error")
	ctx := context.WithValue(context.Background(), testKey{}, "test-value")

	workers := make([]reactor.Worker, 100)

	for idx := range workers {
		work := new(mockWorker)
		work.errCh = make(chan error, 1)
		work.timeLifeStarted = 300 * time.Millisecond

		work.timeLifeShutdown = 100 * time.Millisecond
		work.errCh <- expectErr

		workers[idx] = work
	}

	rc, err := reactor.New(workers...)
	if err != nil {
		t.Fatalf("unexpected error creating reactor: %v", err)
	}

	if cap(rc.Errors()) != 100 {
		t.Fatalf("expected error channel capacity to be 100, got %d", cap(rc.Errors()))
	}

	go rc.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	go rc.Shutdown(ctx)

	countErr := 0
	finishTime := time.Now().Add(2 * time.Second)

	for err := range rc.Errors() {
		if time.Now().After(finishTime) {
			t.Fatal("timeout waiting for errors from workers")
		}

		if !errors.Is(err, expectErr) {
			t.Fatalf("unexpected error: %v", err)
		}

		countErr++
	}

	if countErr != 100 {
		t.Fatalf("expected 100 errors, got %d", countErr)
	}

	for idx, worker := range workers {
		mockWorker, ok := worker.(*mockWorker)
		if !ok {
			t.Fatalf("unexpected worker type at index %d: %T", idx, worker)
		}

		if mockWorker.countStartCalls.Load() != 1 {
			t.Fatalf(
				"expected Start to be called once for worker %d, but it was called %d times",
				idx,
				mockWorker.countStartCalls.Load(),
			)
		}

		if mockWorker.countShutdownCalls.Load() != 1 {
			t.Fatalf(
				"expected Shutdown to be called once for worker %d, but it was called %d times",
				idx,
				mockWorker.countShutdownCalls.Load(),
			)
		}

		if mockWorker.startCtx.Load() != ctx {
			t.Fatalf("unexpected context passed to Start for worker %d", idx)
		}
	}
}
