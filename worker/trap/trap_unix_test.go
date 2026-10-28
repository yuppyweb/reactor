//go:build !windows

package trap_test

import (
	"os"
	"os/signal"
	"testing"
	"time"

	"github.com/yuppyweb/reactor/worker/trap"
)

// TestStartReturnsOnSignal checks that Start returns nil when os.Interrupt
// is delivered to this process.
func TestStartReturnsOnSignal(t *testing.T) {
	t.Parallel()

	// Register the signal before sending it. Otherwise the default action
	// terminates the process before the worker installs its own handler.
	ready := make(chan os.Signal, 1)

	signal.Notify(ready, os.Interrupt)
	t.Cleanup(func() {
		signal.Stop(ready)
	})

	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("find process: %v", err)
	}

	if err = proc.Signal(os.Interrupt); err != nil {
		t.Skip("sending a signal to the current process is not supported")
	}

	worker, err := trap.New(os.Interrupt)
	if err != nil {
		t.Fatalf("new trap: %v", err)
	}

	t.Cleanup(worker.Stop)

	done := make(chan error, 1)

	go func() {
		done <- worker.Start()
	}()

	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err = proc.Signal(os.Interrupt); err != nil {
			t.Fatalf("send signal: %v", err)
		}

		select {
		case err = <-done:
			if err != nil {
				t.Fatalf("start: %v", err)
			}

			return
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("start did not return on signal")
		}
	}
}
