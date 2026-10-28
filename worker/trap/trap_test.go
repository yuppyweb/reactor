package trap_test

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/yuppyweb/reactor/worker/trap"
)

// TestNewNoSignals checks that New with no signals returns ErrNoSignals
// and a nil worker.
func TestNewNoSignals(t *testing.T) {
	t.Parallel()

	worker, err := trap.New()

	if !errors.Is(err, trap.ErrNoSignals) {
		t.Fatalf("expected ErrNoSignals, got %v", err)
	}

	if worker != nil {
		t.Fatal("expected nil worker")
	}
}

// TestNewNilSignal checks that New with a nil signal returns ErrNilSignal
// and a nil worker.
func TestNewNilSignal(t *testing.T) {
	t.Parallel()

	worker, err := trap.New(os.Interrupt, nil)

	if !errors.Is(err, trap.ErrNilSignal) {
		t.Fatalf("expected ErrNilSignal, got %v", err)
	}

	if worker != nil {
		t.Fatal("expected nil worker")
	}
}

// TestStopBeforeStart checks that Stop before Start makes Start return
// nil. A second Stop does nothing.
func TestStopBeforeStart(t *testing.T) {
	t.Parallel()

	worker, err := trap.New(os.Interrupt)
	if err != nil {
		t.Fatalf("new trap: %v", err)
	}

	worker.Stop()
	worker.Stop()

	if err = worker.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
}

// TestStopUnblocksStart checks that Stop unblocks Start and Start returns
// nil.
func TestStopUnblocksStart(t *testing.T) {
	t.Parallel()

	worker, err := trap.New(os.Interrupt)
	if err != nil {
		t.Fatalf("new trap: %v", err)
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
