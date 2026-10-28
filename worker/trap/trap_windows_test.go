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

const (
	// signalHelperVar selects the child process that waits for os.Interrupt.
	signalHelperVar = "REACTOR_TRAP_SIGNAL_HELPER"

	// helperTimeout bounds each stage of the helper. Windows runners with
	// the race detector can take well over a second to deliver an event.
	helperTimeout = 10 * time.Second

	// readyLine is printed by the helper once it handles os.Interrupt.
	readyLine = "ready\n"

	// doneLine is printed by the helper after trap.Start returns.
	doneLine = "done\n"
)

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

// signalHelper is a running helper process.
type signalHelper struct {
	// cmd is the helper process.
	cmd *exec.Cmd

	// stderr collects the helper's error output.
	stderr *lockedBuffer

	// done receives the line printed after ready, or "" when stdout
	// closes first.
	done <-chan string

	// exited is closed after the helper process has exited.
	exited <-chan struct{}
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

	helper := startSignalHelper(t)

	// Repeat the event. The first CTRL_BREAK can arrive after ready is
	// written and before Start calls signal.Notify. Sending stops once the
	// helper reports done, so no event arrives while the process exits.
	signalUntilDone(t, helper)
	requireHelperSuccess(t, helper)
}

// startSignalHelper starts the helper process and waits until it prints
// ready.
func startSignalHelper(t *testing.T) *signalHelper {
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

	reader := bufio.NewReader(stdout)
	ready, readErr := reader.ReadString('\n')

	done := make(chan string, 1)
	exited := make(chan struct{})

	go func() {
		// Wait closes stdout, so every read finishes before Wait starts.
		line, _ := reader.ReadString('\n')
		done <- line

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

	if ready != readyLine {
		t.Fatalf("helper: %q: %s", ready, stderr.String())
	}

	return &signalHelper{
		cmd:    cmd,
		stderr: stderr,
		done:   done,
		exited: exited,
	}
}

// signalUntilDone sends CTRL_BREAK until the helper prints done.
func signalUntilDone(t *testing.T, helper *signalHelper) {
	t.Helper()

	deadline := time.NewTimer(helperTimeout)
	defer deadline.Stop()

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		sendErr := sendCtrlBreak(helper.cmd.Process.Pid)

		select {
		case line := <-helper.done:
			requireDoneLine(t, helper, line)

			return
		default:
		}

		if sendErr != nil {
			t.Skipf("sending a console control event is not supported: %v", sendErr)
		}

		select {
		case line := <-helper.done:
			requireDoneLine(t, helper, line)

			return
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("start did not return on signal: %s", helper.stderr.String())
		}
	}
}

// requireDoneLine fails the test unless line is the done line. On
// failure it waits for the helper to exit to report its status.
func requireDoneLine(t *testing.T, helper *signalHelper, line string) {
	t.Helper()

	if line == doneLine {
		return
	}

	select {
	case <-helper.exited:
		t.Fatalf(
			"helper stopped before done: %q: %v: %s",
			line,
			helper.cmd.ProcessState,
			helper.stderr.String(),
		)
	case <-time.After(helperTimeout):
		t.Fatalf("helper stopped before done: %q: %s", line, helper.stderr.String())
	}
}

// requireHelperSuccess fails the test unless the helper exits with
// status 0.
func requireHelperSuccess(t *testing.T, helper *signalHelper) {
	t.Helper()

	select {
	case <-helper.exited:
	case <-time.After(helperTimeout):
		t.Fatalf("helper did not exit after done: %s", helper.stderr.String())
	}

	if helper.cmd.ProcessState == nil || helper.cmd.ProcessState.ExitCode() != 0 {
		t.Fatalf("helper: %v: %s", helper.cmd.ProcessState, helper.stderr.String())
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
// prints ready, and prints done when trap.Start returns.
func waitForInterrupt() error {
	// Notify before Start so an early CTRL_BREAK is delivered here and does
	// not run the default terminate action. The channel is never stopped,
	// so an event that arrives after done is still handled here.
	ready := make(chan os.Signal, 1)

	signal.Notify(ready, os.Interrupt)

	if _, err := os.Stdout.WriteString(readyLine); err != nil {
		return fmt.Errorf("write ready: %w", err)
	}

	worker, err := trap.New(os.Interrupt)
	if err != nil {
		return fmt.Errorf("new trap: %w", err)
	}

	if err = worker.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	if _, err = os.Stdout.WriteString(doneLine); err != nil {
		return fmt.Errorf("write done: %w", err)
	}

	return nil
}
