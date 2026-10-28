//go:build !windows

package trap_test

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/yuppyweb/reactor"
	"github.com/yuppyweb/reactor/worker/trap"
)

// TestStartReturnsOnSignal checks that Start returns nil when SIGUSR1 is
// delivered to this process. Other tests in this package trap
// os.Interrupt, so SIGUSR1 does not wake them.
func TestStartReturnsOnSignal(t *testing.T) {
	t.Parallel()

	worker, err := trap.New(syscall.SIGUSR1)
	if err != nil {
		t.Fatalf("new trap: %v", err)
	}

	signalUntilReturn(t, worker, syscall.SIGUSR1)
}

// TestNewCopiesSignals checks that changing the caller's slice after New
// does not change which signal Start waits for.
func TestNewCopiesSignals(t *testing.T) {
	t.Parallel()

	signals := []os.Signal{syscall.SIGUSR2}

	worker, err := trap.New(signals...)
	if err != nil {
		t.Fatalf("new trap: %v", err)
	}

	signals[0] = syscall.SIGHUP

	signalUntilReturn(t, worker, syscall.SIGUSR2)
}

// signalUntilReturn starts worker and sends sig to this process until
// Start returns. The test fails when Start does not return nil within a
// second.
func signalUntilReturn(t *testing.T, worker reactor.Worker, sig os.Signal) {
	t.Helper()

	// Register the signal before sending it. Otherwise the default action
	// terminates the process before the worker installs its own handler.
	ready := make(chan os.Signal, 1)

	signal.Notify(ready, sig)
	t.Cleanup(func() {
		signal.Stop(ready)
	})

	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("find process: %v", err)
	}

	if err = proc.Signal(sig); err != nil {
		t.Skip("sending a signal to the current process is not supported")
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
		if err = proc.Signal(sig); err != nil {
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
