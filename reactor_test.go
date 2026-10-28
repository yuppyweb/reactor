package reactor_test

import (
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/yuppyweb/reactor"
)

// stopPanicked is the value a stub Stop panics with.
const stopPanicked = "stop panicked"

// stub is a Worker that counts Start and Stop and calls optional hooks.
type stub struct {
	start  func() error
	stop   func()
	mu     sync.Mutex
	starts int
	stops  int
}

// newStub returns a stub that calls start from Start and stop from Stop.
// A nil function is skipped.
func newStub(start func() error, stop func()) *stub {
	return &stub{
		start:  start,
		stop:   stop,
		mu:     sync.Mutex{},
		starts: 0,
		stops:  0,
	}
}

// Start counts one start and calls the start function.
// A nil start function returns nil.
func (st *stub) Start() error {
	st.mu.Lock()
	st.starts++
	start := st.start
	st.mu.Unlock()

	if start == nil {
		return nil
	}

	return start()
}

// Stop counts one stop and calls the stop function.
// A nil stop function does nothing.
func (st *stub) Stop() {
	st.mu.Lock()
	st.stops++
	stop := st.stop
	st.mu.Unlock()

	if stop != nil {
		stop()
	}
}

// Starts returns the number of Start calls.
func (st *stub) Starts() int {
	st.mu.Lock()
	defer st.mu.Unlock()

	return st.starts
}

// Stops returns the number of Stop calls.
func (st *stub) Stops() int {
	st.mu.Lock()
	defer st.mu.Unlock()

	return st.stops
}

// mustNew returns a reactor for workers or fails the test.
func mustNew(t *testing.T, workers ...reactor.Worker) reactor.Worker {
	t.Helper()

	group, err := reactor.New(workers...)
	if err != nil {
		t.Fatalf("new reactor: %v", err)
	}

	return group
}

// awaitStart runs Start and returns its error.
// The test fails when Start does not return within a second.
func awaitStart(t *testing.T, group reactor.Worker) error {
	t.Helper()

	done := make(chan error, 1)

	go func() {
		done <- group.Start()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("start did not return")
	}

	return nil
}

// catch runs call and returns the recovered value.
// The boolean is true when that value is not nil.
func catch(call func()) (any, bool) {
	var (
		value  any
		caught bool
	)

	func() {
		defer func() {
			value = recover()
			caught = value != nil
		}()

		call()
	}()

	return value, caught
}

// requirePanic fails the test unless call panics with want.
func requirePanic(t *testing.T, want any, call func()) {
	t.Helper()

	got, caught := catch(call)
	if !caught {
		t.Fatal("expected panic")
	}

	if got != want {
		t.Fatalf("panic %v, want %v", got, want)
	}
}

// TestNewNoWorkers checks that New with no workers returns ErrNoWorkers
// and a nil worker.
func TestNewNoWorkers(t *testing.T) {
	t.Parallel()

	worker, err := reactor.New()

	if !errors.Is(err, reactor.ErrNoWorkers) {
		t.Fatalf("expected ErrNoWorkers, got %v", err)
	}

	if worker != nil {
		t.Fatal("expected nil worker")
	}
}

// TestNewNilWorker checks that New with a nil worker returns ErrNilWorker
// and a nil worker.
func TestNewNilWorker(t *testing.T) {
	t.Parallel()

	worker, err := reactor.New(nil)

	if !errors.Is(err, reactor.ErrNilWorker) {
		t.Fatalf("expected ErrNilWorker, got %v", err)
	}

	if worker != nil {
		t.Fatal("expected nil worker")
	}
}

// TestNewNilWorkerAfterValid checks that a nil worker after a valid one
// returns ErrNilWorker and does not start the valid worker.
func TestNewNilWorkerAfterValid(t *testing.T) {
	t.Parallel()

	worker := newStub(nil, nil)
	group, err := reactor.New(worker, nil)

	if !errors.Is(err, reactor.ErrNilWorker) {
		t.Fatalf("expected ErrNilWorker, got %v", err)
	}

	if group != nil {
		t.Fatal("expected nil worker")
	}

	if worker.Starts() != 0 {
		t.Fatalf("starts %d, want 0", worker.Starts())
	}
}

// TestNewCopiesWorkers checks that changing the caller's slice after New
// does not change which workers Stop calls.
func TestNewCopiesWorkers(t *testing.T) {
	t.Parallel()

	first := newStub(nil, nil)
	second := newStub(nil, nil)
	workers := []reactor.Worker{first, second}
	group := mustNew(t, workers...)
	replacement := newStub(nil, nil)
	workers[0] = replacement

	group.Stop()

	if first.Stops() != 1 || second.Stops() != 1 {
		t.Fatalf("stops %d and %d, want 1 and 1", first.Stops(), second.Stops())
	}

	if replacement.Stops() != 0 {
		t.Fatalf("replacement stops %d, want 0", replacement.Stops())
	}
}

// TestStartReturnsWorkerError checks that Start returns the worker error
// and still calls Stop.
func TestStartReturnsWorkerError(t *testing.T) {
	t.Parallel()

	startErr := errors.New("start failed")
	worker := newStub(func() error { return startErr }, nil)
	group := mustNew(t, worker)

	err := group.Start()
	if !errors.Is(err, startErr) {
		t.Fatalf("expected start error, got %v", err)
	}

	if worker.Stops() != 1 {
		t.Fatalf("stops %d, want 1", worker.Stops())
	}
}

// TestStartReturnsNil checks that Start returns nil when the worker
// returns nil and still calls Stop.
func TestStartReturnsNil(t *testing.T) {
	t.Parallel()

	worker := newStub(func() error { return nil }, nil)
	group := mustNew(t, worker)

	if err := group.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	if worker.Stops() != 1 {
		t.Fatalf("stops %d, want 1", worker.Stops())
	}
}

// TestStartReturnsEarliestError checks that Start returns the first
// worker error and stops a worker that is still running.
func TestStartReturnsEarliestError(t *testing.T) {
	t.Parallel()

	fastErr := errors.New("fast failed")
	slowErr := errors.New("slow failed")
	release := make(chan struct{})

	var once sync.Once

	fast := newStub(func() error { return fastErr }, nil)
	slow := newStub(func() error {
		<-release

		return slowErr
	}, func() {
		once.Do(func() {
			close(release)
		})
	})
	group := mustNew(t, fast, slow)

	t.Cleanup(group.Stop)

	err := awaitStart(t, group)
	if !errors.Is(err, fastErr) {
		t.Fatalf("expected fast error, got %v", err)
	}

	if slow.Stops() != 1 {
		t.Fatalf("stops %d, want 1", slow.Stops())
	}
}

// TestStartReturnsEarliestNil checks that a nil result is kept when it
// arrives before another worker's error.
func TestStartReturnsEarliestNil(t *testing.T) {
	t.Parallel()

	slowErr := errors.New("slow failed")
	release := make(chan struct{})

	var once sync.Once

	fast := newStub(func() error { return nil }, nil)
	slow := newStub(func() error {
		<-release

		return slowErr
	}, func() {
		once.Do(func() {
			close(release)
		})
	})
	group := mustNew(t, fast, slow)

	t.Cleanup(group.Stop)

	if err := awaitStart(t, group); err != nil {
		t.Fatalf("start: %v", err)
	}

	if slow.Stops() != 1 {
		t.Fatalf("stops %d, want 1", slow.Stops())
	}
}

// TestStartWaitsForRemainingWorkers checks that Start does not return
// until workers stopped by the first result have finished.
func TestStartWaitsForRemainingWorkers(t *testing.T) {
	t.Parallel()

	const slowCount = 2

	release := make(chan struct{})
	stopped := make(chan struct{})
	arrived := make(chan struct{}, slowCount)

	var once sync.Once

	var releaseOnce sync.Once

	closeRelease := func() {
		releaseOnce.Do(func() {
			close(release)
		})
	}
	slowStart := func() error {
		<-stopped

		arrived <- struct{}{}

		<-release

		return nil
	}
	slowStop := func() {
		once.Do(func() {
			close(stopped)
		})
	}

	fast := newStub(func() error { return nil }, nil)
	first := newStub(slowStart, slowStop)
	second := newStub(slowStart, slowStop)
	group := mustNew(t, fast, first, second)

	t.Cleanup(closeRelease)
	t.Cleanup(group.Stop)

	done := make(chan error, 1)

	go func() {
		done <- group.Start()
	}()

	timeout := time.NewTimer(time.Second)
	defer timeout.Stop()

	for range slowCount {
		select {
		case <-arrived:
		case <-timeout.C:
			t.Fatal("worker did not reach release")
		}
	}

	select {
	case err := <-done:
		t.Fatalf("start returned early: %v", err)
	default:
	}

	closeRelease()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("start: %v", err)
		}
	case <-timeout.C:
		t.Fatal("start did not return")
	}
}

// TestStartRepanics checks that a panic in Start is raised again after
// Stop.
func TestStartRepanics(t *testing.T) {
	t.Parallel()

	sentinel := "start panicked"
	worker := newStub(func() error {
		panic(sentinel)
	}, nil)
	group := mustNew(t, worker)

	requirePanic(t, sentinel, func() {
		_ = group.Start()
	})

	if worker.Stops() != 1 {
		t.Fatalf("stops %d, want 1", worker.Stops())
	}
}

// TestStartPanicOverridesWorkerError checks that a panic is raised again
// instead of returning another worker's error. Both workers are stopped.
func TestStartPanicOverridesWorkerError(t *testing.T) {
	t.Parallel()

	startErr := errors.New("start failed")
	sentinel := "worker panicked"
	failing := newStub(func() error { return startErr }, nil)
	panicking := newStub(func() error {
		panic(sentinel)
	}, nil)
	group := mustNew(t, failing, panicking)

	requirePanic(t, sentinel, func() {
		_ = group.Start()
	})

	if failing.Stops() != 1 || panicking.Stops() != 1 {
		t.Fatalf("stops %d and %d, want 1 and 1", failing.Stops(), panicking.Stops())
	}
}

// TestStartPanicsWhenStopPanics checks that a panic in Stop during Start
// is raised again.
func TestStartPanicsWhenStopPanics(t *testing.T) {
	t.Parallel()

	sentinel := stopPanicked
	worker := newStub(func() error { return nil }, func() {
		panic(sentinel)
	})
	group := mustNew(t, worker)

	requirePanic(t, sentinel, func() {
		_ = group.Start()
	})
}

// TestStartWaitsWhenStopPanics checks that a panic in Stop during Start
// is raised only after every worker has finished.
func TestStartWaitsWhenStopPanics(t *testing.T) {
	t.Parallel()

	sentinel := stopPanicked
	stopped := make(chan struct{})
	release := make(chan struct{})

	var stopOnce, releaseOnce sync.Once

	closeRelease := func() {
		releaseOnce.Do(func() {
			close(release)
		})
	}

	fast := newStub(func() error { return nil }, nil)
	slow := newStub(func() error {
		<-release

		return nil
	}, func() {
		stopOnce.Do(func() {
			close(stopped)
		})

		panic(sentinel)
	})
	group := mustNew(t, fast, slow)

	t.Cleanup(closeRelease)

	done := make(chan any, 1)

	go func() {
		got, _ := catch(func() {
			_ = group.Start()
		})

		done <- got
	}()

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("slow worker was not stopped")
	}

	select {
	case got := <-done:
		t.Fatalf("start returned before slow worker finished: %v", got)
	case <-time.After(30 * time.Millisecond):
	}

	closeRelease()

	select {
	case got := <-done:
		if got != sentinel {
			t.Fatalf("panic %v, want %v", got, sentinel)
		}
	case <-time.After(time.Second):
		t.Fatal("start did not return")
	}
}

// TestStartPanicOverridesStopPanic checks that a panic in a worker's Start
// is raised instead of a panic in another worker's Stop.
func TestStartPanicOverridesStopPanic(t *testing.T) {
	t.Parallel()

	startSentinel := "start panicked"
	stopSentinel := stopPanicked
	stopped := make(chan struct{})

	var once sync.Once

	panicking := newStub(func() error {
		panic(startSentinel)
	}, nil)
	blocked := newStub(func() error {
		<-stopped

		return nil
	}, func() {
		once.Do(func() {
			close(stopped)
		})

		panic(stopSentinel)
	})
	group := mustNew(t, panicking, blocked)

	requirePanic(t, startSentinel, func() {
		_ = group.Start()
	})

	if panicking.Stops() != 1 || blocked.Stops() != 1 {
		t.Fatalf("stops %d and %d, want 1 and 1", panicking.Stops(), blocked.Stops())
	}
}

// TestStartGoexitCountsAsNil checks that a worker that exits through
// runtime.Goexit gives a nil result and does not block Start.
func TestStartGoexitCountsAsNil(t *testing.T) {
	t.Parallel()

	worker := newStub(func() error {
		runtime.Goexit()

		return nil
	}, nil)
	group := mustNew(t, worker)

	if err := awaitStart(t, group); err != nil {
		t.Fatalf("start: %v", err)
	}

	if worker.Stops() != 1 {
		t.Fatalf("stops %d, want 1", worker.Stops())
	}
}

// TestStopCallsEveryWorker checks that Stop calls Stop on every worker.
func TestStopCallsEveryWorker(t *testing.T) {
	t.Parallel()

	first := newStub(nil, nil)
	second := newStub(nil, nil)
	group := mustNew(t, first, second)

	group.Stop()

	if first.Stops() != 1 || second.Stops() != 1 {
		t.Fatalf("stops %d and %d, want 1 and 1", first.Stops(), second.Stops())
	}
}

// TestStopRunsConcurrently checks that one worker's Stop does not block
// another, and that Stop waits for the slow call.
func TestStopRunsConcurrently(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	otherEntered := make(chan struct{})

	var once sync.Once

	closeRelease := func() {
		once.Do(func() {
			close(release)
		})
	}

	blocker := newStub(nil, func() {
		close(entered)

		<-release
	})
	other := newStub(nil, func() {
		close(otherEntered)
	})
	group := mustNew(t, blocker, other)

	t.Cleanup(closeRelease)

	done := make(chan struct{})

	go func() {
		group.Stop()

		close(done)
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("blocker did not enter stop")
	}

	select {
	case <-otherEntered:
	case <-time.After(time.Second):
		t.Fatal("other stop did not run")
	}

	select {
	case <-done:
		t.Fatal("stop returned while blocker held")
	default:
	}

	closeRelease()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stop did not return")
	}
}

// TestStopRepanics checks that a panic in Stop is raised again after the
// other workers are stopped.
func TestStopRepanics(t *testing.T) {
	t.Parallel()

	sentinel := stopPanicked
	panicking := newStub(nil, func() {
		panic(sentinel)
	})
	other := newStub(nil, nil)
	group := mustNew(t, panicking, other)

	requirePanic(t, sentinel, group.Stop)

	if other.Stops() != 1 {
		t.Fatalf("stops %d, want 1", other.Stops())
	}
}

// TestStopCallsWorkersAgain checks that a second Stop calls the worker
// again.
func TestStopCallsWorkersAgain(t *testing.T) {
	t.Parallel()

	worker := newStub(nil, nil)
	group := mustNew(t, worker)

	group.Stop()
	group.Stop()

	if worker.Stops() != 2 {
		t.Fatalf("stops %d, want 2", worker.Stops())
	}
}

// TestNestedReactor checks that stopping an outer reactor stops a worker
// inside an inner reactor.
func TestNestedReactor(t *testing.T) {
	t.Parallel()

	stopped := make(chan struct{})

	var once sync.Once

	worker := newStub(func() error {
		<-stopped

		return nil
	}, func() {
		once.Do(func() {
			close(stopped)
		})
	})
	inner := mustNew(t, worker)
	outer := mustNew(t, inner)

	t.Cleanup(outer.Stop)

	done := make(chan error, 1)

	go func() {
		done <- outer.Start()
	}()

	outer.Stop()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("start: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("start did not return")
	}

	if worker.Starts() != 1 {
		t.Fatalf("starts %d, want 1", worker.Starts())
	}

	if worker.Stops() == 0 {
		t.Fatal("expected stop")
	}
}
