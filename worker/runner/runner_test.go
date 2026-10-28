package runner_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yuppyweb/reactor/worker/runner"
)

// TestNewNilFunc checks that New with a nil function returns ErrNilFunc
// and a nil worker.
func TestNewNilFunc(t *testing.T) {
	t.Parallel()

	worker, err := runner.New(nil)

	if !errors.Is(err, runner.ErrNilFunc) {
		t.Fatalf("expected ErrNilFunc, got %v", err)
	}

	if worker != nil {
		t.Fatal("expected nil worker")
	}
}

// TestStopBeforeStart checks that Stop before Start closes the stop
// channel, so Start returns nil. A second Stop does nothing.
func TestStopBeforeStart(t *testing.T) {
	t.Parallel()

	notStopped := errors.New("stop was not closed")

	worker, err := runner.New(func(stop <-chan struct{}) error {
		select {
		case <-stop:
			return nil
		default:
			return notStopped
		}
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	worker.Stop()
	worker.Stop()

	if err = worker.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
}

// TestStartReturnsFuncError checks that Start returns the function error.
// Stop after Start does not change that result.
func TestStartReturnsFuncError(t *testing.T) {
	t.Parallel()

	runErr := errors.New("run failed")

	worker, err := runner.New(func(<-chan struct{}) error {
		return runErr
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	if err = worker.Start(); !errors.Is(err, runErr) {
		t.Fatalf("expected run error, got %v", err)
	}

	worker.Stop()
	worker.Stop()
}

// TestStopUnblocksStart checks that Stop unblocks a function waiting on
// the stop channel and Start returns nil.
func TestStopUnblocksStart(t *testing.T) {
	t.Parallel()

	worker, err := runner.New(func(stop <-chan struct{}) error {
		<-stop

		return nil
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	done := make(chan error, 1)

	go func() {
		done <- worker.Start()
	}()

	worker.Stop()

	select {
	case err = <-done:
		if err != nil {
			t.Fatalf("start: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("start did not return")
	}
}

// TestStopDoesNotWaitForFunc checks that Stop returns while the function
// is still running after the stop channel is closed.
func TestStopDoesNotWaitForFunc(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})

	worker, err := runner.New(func(stop <-chan struct{}) error {
		close(entered)

		<-stop
		<-release

		return nil
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	done := make(chan error, 1)

	go func() {
		done <- worker.Start()
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("function did not start")
	}

	stopped := make(chan struct{})

	go func() {
		worker.Stop()

		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stop waited for the function")
	}

	close(release)

	select {
	case err = <-done:
		if err != nil {
			t.Fatalf("start: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("start did not return")
	}
}

// TestConcurrentStop checks that Stop called from several goroutines at
// once closes the stop channel once without a panic.
func TestConcurrentStop(t *testing.T) {
	t.Parallel()

	const callers = 8

	worker, err := runner.New(func(stop <-chan struct{}) error {
		<-stop

		return nil
	})
	if err != nil {
		t.Fatalf("new runner: %v", err)
	}

	wg := sync.WaitGroup{}

	for range callers {
		wg.Go(worker.Stop)
	}

	wg.Wait()

	if err = worker.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
}
