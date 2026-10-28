package integration_test

import (
	"errors"
	"os"
	"testing"
)

// TestStack_RunnerErrorClosesResource checks that a runner error stops
// the group, closes the resource once, and stops the other runner.
func TestStack_RunnerErrorClosesResource(t *testing.T) {
	t.Parallel()

	runErr := errors.New("run failed")
	resource := newRecordingCloser(nil)
	task, stopped := blockUntilStop()
	group := mustReactor(
		t,
		mustRunner(t, func(<-chan struct{}) error {
			return runErr
		}),
		mustRunner(t, task),
		mustCloser(t, resource),
	)
	t.Cleanup(group.Stop)

	err := awaitStart(t, group)
	if !errors.Is(err, runErr) {
		t.Fatalf("expected run error, got %v", err)
	}

	requireClosedOnce(t, resource)
	requireStopped(t, stopped)
}

// TestStack_StopClosesResourceAndUnblocksRunner checks that Stop closes
// the resource once and unblocks the runner. A second Stop does not close
// the resource again.
func TestStack_StopClosesResourceAndUnblocksRunner(t *testing.T) {
	t.Parallel()

	resource := newRecordingCloser(nil)
	task, stopped := blockUntilStop()
	group := mustReactor(
		t,
		mustRunner(t, task),
		mustCloser(t, resource),
	)
	t.Cleanup(group.Stop)

	done := startGroup(t, group)

	group.Stop()

	err := waitDone(t, done)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	requireClosedOnce(t, resource)

	group.Stop()

	requireClosedOnce(t, resource)
	requireStopped(t, stopped)
}

// TestStack_CloserErrorDoesNotReplaceNil checks that Start keeps the first
// result. The runner returns nil before Stop, so the closer error is drained
// and Start still returns nil.
func TestStack_CloserErrorDoesNotReplaceNil(t *testing.T) {
	t.Parallel()

	resource := newRecordingCloser(errors.New("close failed"))
	group := mustReactor(
		t,
		mustRunner(t, func(<-chan struct{}) error {
			return nil
		}),
		mustCloser(t, resource),
	)
	t.Cleanup(group.Stop)

	err := awaitStart(t, group)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}

	requireClosedOnce(t, resource)
}

// TestStack_PanicInRunnerStillCloses checks that a panic in a runner is
// raised again and the resource is still closed once.
func TestStack_PanicInRunnerStillCloses(t *testing.T) {
	t.Parallel()

	sentinel := "runner panicked"
	resource := newRecordingCloser(nil)
	group := mustReactor(
		t,
		mustRunner(t, func(<-chan struct{}) error {
			panic(sentinel)
		}),
		mustCloser(t, resource),
	)
	t.Cleanup(group.Stop)

	requirePanic(t, sentinel, func() {
		_ = group.Start()
	})

	requireClosedOnce(t, resource)
}

// TestStack_NestedReactorStopsInnerRunner checks that stopping the outer
// reactor stops an inner runner and closes the resource once.
func TestStack_NestedReactorStopsInnerRunner(t *testing.T) {
	t.Parallel()

	resource := newRecordingCloser(nil)
	task, stopped := blockUntilStop()
	inner := mustReactor(t, mustRunner(t, task))
	outer := mustReactor(t, inner, mustCloser(t, resource))
	t.Cleanup(outer.Stop)

	done := startGroup(t, outer)

	outer.Stop()

	err := waitDone(t, done)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	requireClosedOnce(t, resource)
	requireStopped(t, stopped)
}

// TestStack_StopBeforeStartClosesOnce checks that Stop before Start
// closes the resource once and unblocks the runner when Start runs.
func TestStack_StopBeforeStartClosesOnce(t *testing.T) {
	t.Parallel()

	resource := newRecordingCloser(nil)
	task, stopped := blockUntilStop()
	group := mustReactor(
		t,
		mustRunner(t, task),
		mustCloser(t, resource),
	)

	group.Stop()

	if err := group.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	requireClosedOnce(t, resource)
	requireStopped(t, stopped)
}

// TestStack_TrapStopShutsDownRunnerAndCloser checks that stopping the
// trap stops the runner and closes the resource once.
func TestStack_TrapStopShutsDownRunnerAndCloser(t *testing.T) {
	t.Parallel()

	resource := newRecordingCloser(nil)
	task, stopped := blockUntilStop()
	signals := mustTrap(t, os.Interrupt)
	group := mustReactor(
		t,
		signals,
		mustRunner(t, task),
		mustCloser(t, resource),
	)
	t.Cleanup(group.Stop)

	done := startGroup(t, group)

	signals.Stop()

	err := waitDone(t, done)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	requireClosedOnce(t, resource)
	requireStopped(t, stopped)
}
