//go:build windows

package trap_test

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/yuppyweb/reactor/worker/trap"
)

// signalHelperVar selects the child process that waits for os.Interrupt.
const signalHelperVar = "REACTOR_TRAP_SIGNAL_HELPER"

// lockedBuffer is a bytes.Buffer that is safe to write from the exec
// copy goroutine while the test reads it.
type lockedBuffer struct {
	buf bytes.Buffer
	mu  sync.Mutex
}

// Write appends p to the buffer.
func (lb *lockedBuffer) Write(p []byte) (int, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	n, err := lb.buf.Write(p)
	if err != nil {
		return n, fmt.Errorf("write stderr: %w", err)
	}

	return n, nil
}

// String returns the buffered output.
func (lb *lockedBuffer) String() string {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	return lb.buf.String()
}

// TestMain runs the signal helper when signalHelperVar is set.
// Otherwise it runs the tests.
func TestMain(m *testing.M) {
	if os.Getenv(signalHelperVar) != "1" {
		os.Exit(m.Run())
	}

	if err := waitForInterrupt(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	os.Exit(0)
}

// TestStartReturnsOnSignal checks that Start in a child process returns
// when CTRL_BREAK is delivered to that process.
func TestStartReturnsOnSignal(t *testing.T) {
	t.Parallel()

	cmd, stderr, exited := startSignalHelper(t)

	// Repeat the event. The first CTRL_BREAK can arrive after ready is
	// written and before Start calls signal.Notify. The handler installed
	// before ready keeps that event from terminating the process.
	signalUntilExit(t, cmd, stderr, exited)
}

// startSignalHelper starts the helper process and waits until it prints
// ready.
func startSignalHelper(t *testing.T) (*exec.Cmd, *lockedBuffer, <-chan struct{}) {
	t.Helper()

	//nolint:gosec // The executable is this test binary.
	cmd := exec.CommandContext(t.Context(), os.Args[0])

	cmd.Env = append(os.Environ(), signalHelperVar+"=1")

	attr := new(syscall.SysProcAttr)
	attr.CreationFlags = syscall.CREATE_NEW_PROCESS_GROUP
	cmd.SysProcAttr = attr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout: %v", err)
	}

	stderr := new(lockedBuffer)
	cmd.Stderr = stderr

	if err = cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}

	// Wait closes stdout, so the ready line is read before Wait starts.
	ready, readErr := bufio.NewReader(stdout).ReadString('\n')

	exited := make(chan struct{})

	go func() {
		_ = cmd.Wait()

		close(exited)
	}()

	t.Cleanup(func() {
		_ = cmd.Process.Kill()

		<-exited
	})

	if readErr != nil {
		t.Fatalf("read ready: %v: %s", readErr, stderr.String())
	}

	if ready != "ready\n" {
		t.Fatalf("helper: %q: %s", ready, stderr.String())
	}

	return cmd, stderr, exited
}

// signalUntilExit sends CTRL_BREAK until the helper process exits.
func signalUntilExit(t *testing.T, cmd *exec.Cmd, stderr *lockedBuffer, exited <-chan struct{}) {
	t.Helper()

	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		sendErr := sendCtrlBreak(cmd.Process.Pid)

		select {
		case <-exited:
			requireHelperSuccess(t, cmd, stderr)

			return
		default:
		}

		if sendErr != nil {
			t.Skipf("sending a console control event is not supported: %v", sendErr)
		}

		select {
		case <-exited:
			requireHelperSuccess(t, cmd, stderr)

			return
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("start did not return on signal: %s", stderr.String())
		}
	}
}

// requireHelperSuccess fails the test unless the helper exits with
// status 0.
func requireHelperSuccess(t *testing.T, cmd *exec.Cmd, stderr *lockedBuffer) {
	t.Helper()

	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 0 {
		t.Fatalf("helper: %v: %s", cmd.ProcessState, stderr.String())
	}
}

// sendCtrlBreak sends CTRL_BREAK to the process group pid.
func sendCtrlBreak(pid int) error {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	generate := kernel32.NewProc("GenerateConsoleCtrlEvent")

	ret, _, callErr := generate.Call(syscall.CTRL_BREAK_EVENT, uintptr(pid))
	if ret != 0 {
		return nil
	}

	return fmt.Errorf("generate console ctrl event: %w", callErr)
}

// waitForInterrupt is the helper process. It notifies for os.Interrupt,
// prints ready, and returns when trap.Start returns.
func waitForInterrupt() error {
	// Notify before Start so an early CTRL_BREAK is delivered here and does
	// not run the default terminate action.
	ready := make(chan os.Signal, 1)

	signal.Notify(ready, os.Interrupt)
	defer signal.Stop(ready)

	if _, err := os.Stdout.WriteString("ready\n"); err != nil {
		return fmt.Errorf("write ready: %w", err)
	}

	worker, err := trap.New(os.Interrupt)
	if err != nil {
		return fmt.Errorf("new trap: %w", err)
	}

	if err = worker.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	return nil
}
