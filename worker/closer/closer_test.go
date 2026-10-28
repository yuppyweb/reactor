package closer_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yuppyweb/reactor/worker/closer"
)

// mockResource counts Close calls and returns a fixed error.
type mockResource struct {
	err    error
	closes int
	mu     sync.Mutex
}

// Close counts one close and returns the fixed error.
func (m *mockResource) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closes++

	return m.err
}

// Closes returns the number of Close calls.
func (m *mockResource) Closes() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.closes
}

// panicResource counts Close calls and panics with a fixed value.
type panicResource struct {
	value  any
	closes int
	mu     sync.Mutex
}

// Close counts one close and panics with the fixed value.
func (p *panicResource) Close() error {
	p.mu.Lock()
	p.closes++
	p.mu.Unlock()

	panic(p.value)
}

// Closes returns the number of Close calls.
func (p *panicResource) Closes() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.closes
}

// TestNewNilCloser checks that New with a nil resource returns
// ErrNilCloser and a nil worker.
func TestNewNilCloser(t *testing.T) {
	t.Parallel()

	worker, err := closer.New(nil)

	if !errors.Is(err, closer.ErrNilCloser) {
		t.Fatalf("expected ErrNilCloser, got %v", err)
	}

	if worker != nil {
		t.Fatal("expected nil worker")
	}
}

// TestStopBeforeStart checks that Stop before Start closes the resource
// once and Start returns nil. A second Stop does not close it again.
func TestStopBeforeStart(t *testing.T) {
	t.Parallel()

	resource := &mockResource{
		err:    nil,
		closes: 0,
		mu:     sync.Mutex{},
	}

	worker, err := closer.New(resource)
	if err != nil {
		t.Fatalf("new closer: %v", err)
	}

	worker.Stop()
	worker.Stop()

	if err = worker.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	if resource.Closes() != 1 {
		t.Fatalf("expected one close, got %d", resource.Closes())
	}
}

// TestStopUnblocksStart checks that Stop unblocks Start and Start returns
// the error from Close. The resource is closed once.
func TestStopUnblocksStart(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("close failed")
	resource := &mockResource{
		err:    closeErr,
		closes: 0,
		mu:     sync.Mutex{},
	}

	worker, err := closer.New(resource)
	if err != nil {
		t.Fatalf("new closer: %v", err)
	}

	done := make(chan error, 1)

	go func() {
		done <- worker.Start()
	}()

	worker.Stop()

	select {
	case err = <-done:
		if !errors.Is(err, closeErr) {
			t.Fatalf("expected close error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("start did not return")
	}

	if resource.Closes() != 1 {
		t.Fatalf("expected one close, got %d", resource.Closes())
	}
}

// TestStopBeforeStartReturnsCloseError checks that Start called after Stop
// returns the error stored from Close.
func TestStopBeforeStartReturnsCloseError(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("close failed")
	resource := &mockResource{
		err:    closeErr,
		closes: 0,
		mu:     sync.Mutex{},
	}

	worker, err := closer.New(resource)
	if err != nil {
		t.Fatalf("new closer: %v", err)
	}

	worker.Stop()

	if err = worker.Start(); !errors.Is(err, closeErr) {
		t.Fatalf("expected close error, got %v", err)
	}
}

// TestConcurrentStopClosesOnce checks that Stop called from several
// goroutines at once closes the resource once.
func TestConcurrentStopClosesOnce(t *testing.T) {
	t.Parallel()

	const callers = 8

	resource := &mockResource{
		err:    nil,
		closes: 0,
		mu:     sync.Mutex{},
	}

	worker, err := closer.New(resource)
	if err != nil {
		t.Fatalf("new closer: %v", err)
	}

	wg := sync.WaitGroup{}

	for range callers {
		wg.Go(worker.Stop)
	}

	wg.Wait()

	if err = worker.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	if resource.Closes() != 1 {
		t.Fatalf("expected one close, got %d", resource.Closes())
	}
}

// TestClosePanicUnblocksStart checks that a panic in Close is raised from
// Stop, Start still returns nil, and a second Stop does not close again.
func TestClosePanicUnblocksStart(t *testing.T) {
	t.Parallel()

	sentinel := "close panicked"
	resource := &panicResource{
		value:  sentinel,
		closes: 0,
		mu:     sync.Mutex{},
	}

	worker, err := closer.New(resource)
	if err != nil {
		t.Fatalf("new closer: %v", err)
	}

	done := make(chan error, 1)

	go func() {
		done <- worker.Start()
	}()

	got := catchPanic(worker.Stop)
	if got != sentinel {
		t.Fatalf("panic %v, want %v", got, sentinel)
	}

	select {
	case err = <-done:
		if err != nil {
			t.Fatalf("start: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("start did not return")
	}

	if got = catchPanic(worker.Stop); got != nil {
		t.Fatalf("second stop panicked: %v", got)
	}

	if resource.Closes() != 1 {
		t.Fatalf("expected one close, got %d", resource.Closes())
	}
}

// catchPanic runs call and returns the recovered value, or nil.
func catchPanic(call func()) any {
	var value any

	func() {
		defer func() {
			value = recover()
		}()

		call()
	}()

	return value
}
