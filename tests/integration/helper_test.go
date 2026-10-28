package integration_test

import (
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/yuppyweb/reactor"
	"github.com/yuppyweb/reactor/worker/closer"
	"github.com/yuppyweb/reactor/worker/runner"
	"github.com/yuppyweb/reactor/worker/trap"
)

// recordingCloser counts Close calls and returns a fixed error.
type recordingCloser struct {
	err    error
	closes int
	mu     sync.Mutex
}

// newRecordingCloser returns a recordingCloser whose Close returns err.
func newRecordingCloser(err error) *recordingCloser {
	return &recordingCloser{
		err:    err,
		closes: 0,
		mu:     sync.Mutex{},
	}
}

// Close counts one close and returns the fixed error.
func (rc *recordingCloser) Close() error {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	rc.closes++

	return rc.err
}

// Closes returns the number of Close calls.
func (rc *recordingCloser) Closes() int {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	return rc.closes
}

// panicCloser panics with a fixed value from Close.
type panicCloser struct {
	value any
}

// Close panics with the fixed value.
func (pc panicCloser) Close() error {
	panic(pc.value)
}

// mustRunner returns a runner for task or fails the test.
func mustRunner(t *testing.T, task runner.Run) reactor.Worker {
	t.Helper()

	worker, err := runner.New(task)
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	return worker
}

// mustCloser returns a closer for resource or fails the test.
func mustCloser(t *testing.T, resource io.Closer) reactor.Worker {
	t.Helper()

	worker, err := closer.New(resource)
	if err != nil {
		t.Fatalf("new closer: %v", err)
	}

	return worker
}

// mustTrap returns a trap for signals or fails the test.
func mustTrap(t *testing.T, signals ...os.Signal) reactor.Worker {
	t.Helper()

	worker, err := trap.New(signals...)
	if err != nil {
		t.Fatalf("new trap: %v", err)
	}

	return worker
}

// mustReactor returns a reactor for workers or fails the test.
func mustReactor(t *testing.T, workers ...reactor.Worker) reactor.Worker {
	t.Helper()

	group, err := reactor.New(workers...)
	if err != nil {
		t.Fatalf("new reactor: %v", err)
	}

	return group
}

// blockUntilStop returns a task that blocks until stop is closed and a
// channel that is closed when that task returns.
func blockUntilStop() (runner.Run, <-chan struct{}) {
	stopped := make(chan struct{})

	task := func(stop <-chan struct{}) error {
		<-stop
		close(stopped)

		return nil
	}

	return task, stopped
}

// startGroup runs Start in the background and returns its result channel.
func startGroup(t *testing.T, group reactor.Worker) <-chan error {
	t.Helper()

	done := make(chan error, 1)

	go func() {
		done <- group.Start()
	}()

	return done
}

// waitDone returns the result from done.
// The test fails when done does not arrive within a second.
func waitDone(t *testing.T, done <-chan error) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("start did not return")
	}

	return nil
}

// awaitStart runs Start and returns its error.
// The test fails when Start does not return within a second.
func awaitStart(t *testing.T, group reactor.Worker) error {
	t.Helper()

	return waitDone(t, startGroup(t, group))
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

// requireClosedOnce fails the test unless resource was closed once.
func requireClosedOnce(t *testing.T, resource *recordingCloser) {
	t.Helper()

	if resource.Closes() != 1 {
		t.Fatalf("closes %d, want 1", resource.Closes())
	}
}

// requireStopped fails the test unless the runner has stopped.
func requireStopped(t *testing.T, stopped <-chan struct{}) {
	t.Helper()

	select {
	case <-stopped:
	default:
		t.Fatal("expected runner to stop")
	}
}
