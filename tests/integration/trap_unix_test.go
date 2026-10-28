//go:build !windows

package integration_test

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// TestStack_SignalShutsDownRunnerAndCloser checks that SIGUSR1 stops the
// trap and the runner, and closes the resource once.
func TestStack_SignalShutsDownRunnerAndCloser(t *testing.T) {
	t.Parallel()

	// Register the signal before sending it. Otherwise the default action
	// terminates the process before the worker installs its own handler.
	ready := make(chan os.Signal, 1)

	signal.Notify(ready, syscall.SIGUSR1)
	t.Cleanup(func() {
		signal.Stop(ready)
	})

	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("find process: %v", err)
	}

	if err = proc.Signal(syscall.SIGUSR1); err != nil {
		t.Skip("sending a signal to the current process is not supported")
	}

	resource := newRecordingCloser(nil)
	task, stopped := blockUntilStop()
	group := mustReactor(
		t,
		mustTrap(t, syscall.SIGUSR1),
		mustRunner(t, task),
		mustCloser(t, resource),
	)
	t.Cleanup(group.Stop)

	done := startGroup(t, group)

	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err = proc.Signal(syscall.SIGUSR1); err != nil {
			t.Fatalf("send signal: %v", err)
		}

		select {
		case err = <-done:
			if err != nil {
				t.Fatalf("start: %v", err)
			}

			requireClosedOnce(t, resource)
			requireStopped(t, stopped)

			return
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("start did not return on signal")
		}
	}
}
