// Package reactor provides a lifecycle coordination framework for managing
// multiple concurrent workers. It allows workers to be started and gracefully
// shut down together while collecting errors from all workers into a single channel.
//
// The Reactor type coordinates the lifecycle of multiple Worker implementations,
// ensuring that all workers start and stop in a controlled manner with proper
// context propagation and error handling.
//
// Example:
//
//	type MyWorker struct {}
//
//	func (w *MyWorker) Start(ctx context.Context) error {
//		// Worker logic here
//		return nil
//	}
//
//	func (w *MyWorker) Shutdown(ctx context.Context) error {
//		// Cleanup logic here
//		return nil
//	}
//
//	func (w *MyWorker) Errors() <-chan error {
//		// Return error channel
//		return make(chan error)
//	}
//
//	reactor, err := reactor.New()
//	if err != nil {
//		// Handle error
//	}
//
//	if err := reactor.Add(&MyWorker{}); err != nil {
//		// Handle error
//	}
//
//	errCh := reactor.Errors()
//	go func() {
//		for err := range errCh {
//			// Handle error
//		}
//	}()
//
//	if err := reactor.Start(ctx); err != nil {
//		// Handle error
//	}
package reactor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

const (
	packageName = "reactor"

	// defaultErrorBufferSize is the default size of the error channel buffer for Reactor.
	defaultErrorBufferSize = 1000
)

var (
	ErrNilOption    = errors.New(packageName + ": option cannot be nil")
	ErrNilWorker    = errors.New(packageName + ": worker is nil")
	ErrNoWorkers    = errors.New(packageName + ": no workers added to reactor")
	ErrNotRestart   = errors.New(packageName + ": restart is not possible")
	ErrIsStarted    = errors.New(packageName + ": is already started")
	ErrNotStarted   = errors.New(packageName + ": is not started")
	ErrIsStopped    = errors.New(packageName + ": is stopped")
	ErrChannelFull  = errors.New(packageName + ": error channel is full")
	ErrFailedToSend = errors.New(packageName + ": failed to send error")
)

// Worker defines the lifecycle interface for a service component.
// Implementations must handle starting, shutting down, and reporting errors
// in a concurrent-safe manner.
//
// Start begins the operation asynchronously and returns an error immediately if startup fails.
// Any runtime errors should be sent to the Errors() channel.
// Shutdown gracefully stops the worker and returns an error immediately if shutdown fails.
// Any errors during shutdown should also be sent to the Errors() channel.
// Errors returns a buffered channel for error events; the channel must be closed
// when the worker stops, and implementations must support concurrent reads.
type Worker interface {
	// Start begins the worker's operation with the given context.
	// The implementation should respect context cancellation and deadlines.
	// An error is returned immediately if startup cannot begin (e.g., already started).
	// Runtime errors should be sent to the Errors() channel.
	Start(ctx context.Context) error

	// Shutdown gracefully stops the worker using the given context.
	// Implementations may be asynchronous and can launch shutdown in a separate goroutine.
	// The context deadline should be respected for cleanup operations.
	// Any errors during shutdown should be sent to the Errors() channel.
	Shutdown(ctx context.Context) error

	// Errors returns a read-only channel for error events from the worker.
	// May return nil if the worker does not report errors.
	// If a non-nil channel is returned, it should be closed when the worker stops.
	// Implementations must ensure the channel is safe for concurrent reads.
	Errors() <-chan error
}

// Option is a functional option type for configuring Reactor instances.
// Options are passed to the New function to customize Reactor behavior.
type Option func(*Reactor) error

// Reactor coordinates the lifecycle of multiple workers.
// It ensures all workers start and shut down in a controlled manner, collecting
// errors from all workers into a single channel for unified error handling.
//
// Reactor is safe for concurrent use and itself implements the Worker interface.
// The Start method can only be called once and blocks until all workers have
// started and completed (stopped). The Shutdown method must be called after Start
// and blocks until all workers have gracefully shut down. Both methods coordinate
// proper error collection and reporting.
type Reactor struct {
	workers   []Worker
	isStarted atomic.Bool
	isStopped atomic.Bool
	isSending atomic.Bool
	errCh     chan error
	errFullCh chan struct{}
	mu        sync.Mutex
	log       Logger
}

// New creates a new Reactor with no initial workers.
// Workers can be added later using the Add method.
func New(options ...Option) (*Reactor, error) {
	reactor := new(Reactor)
	reactor.workers = make([]Worker, 0)
	reactor.errCh = make(chan error, defaultErrorBufferSize)
	reactor.errFullCh = make(chan struct{})
	reactor.log = NewNopLogger()

	for _, option := range options {
		if option == nil {
			return nil, ErrNilOption
		}

		if err := option(reactor); err != nil {
			return nil, err
		}
	}

	return reactor, nil
}

// Add registers a new worker with the Reactor.
// Workers must be added before the Reactor is started; adding workers after Start returns ErrIsStarted.
// Adding a nil worker returns ErrNilWorker. Adding workers after the Reactor is stopped returns ErrIsStopped.
func (r *Reactor) Add(worker Worker) error {
	if worker == nil {
		return ErrNilWorker
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.isStopped.Load() {
		return ErrIsStopped
	}

	if r.isStarted.Load() {
		return ErrIsStarted
	}

	r.workers = append(r.workers, worker)

	return nil
}

// Start begins execution of all workers concurrently.
// It starts each worker in a separate goroutine and collects errors from all workers
// into the error channel. Start can be called only once per Reactor instance;
// subsequent calls return ErrNotRestart.
//
// The context parameter is propagated to all workers and controls the lifetime
// of the worker startup sequence. If the context is cancelled, no guarantee is made
// about which workers have started.
//
// This method blocks until all workers have started and completed (stopped reporting errors).
// This includes waiting for all error reporting goroutines to finish reading from worker
// error channels, which only happens when workers close their error channels (typically
// upon shutdown). The error channel is closed when this method returns.
//
// If the error channel buffer becomes full, no more errors can be sent and Start will
// return ErrChannelFull instead of blocking goroutines. The caller is responsible for
// reading errors concurrently and ensuring sufficient error channel buffer capacity.
func (r *Reactor) Start(ctx context.Context) error {
	if len(r.workers) == 0 {
		return ErrNoWorkers
	}

	if !r.isStarted.CompareAndSwap(false, true) {
		return ErrNotRestart
	}

	defer r.stop()

	r.isSending.Store(true)

	wg := sync.WaitGroup{}
	done := make(chan struct{})

	for _, worker := range r.workers {
		wg.Go(func() {
			r.log.Debug(ctx, packageName+": starting worker", LogArgs{"worker": worker})

			if err := worker.Start(ctx); err != nil {
				r.sendError(ctx, err)
			}
		})

		errCh := worker.Errors()

		// If the worker does not report errors, we skip it.
		if errCh == nil {
			continue
		}

		wg.Go(func() {
			for err := range errCh {
				r.sendError(ctx, err)
			}
		})
	}

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		r.log.Debug(ctx, packageName+": all workers completed")

	case <-ctx.Done():
		r.log.Debug(ctx, packageName+": workers startup cancelled by context")

	case <-r.errFullCh:
		return ErrChannelFull
	}

	return nil
}

// Shutdown gracefully stops all workers with the given context.
// Each worker's Shutdown is launched in a separate goroutine with the provided context.
// The method blocks until all workers have completed their shutdown, the context is
// cancelled, or an error occurs (such as ErrChannelFull if the error buffer becomes full).
// The context parameter is propagated to all workers to control their shutdown deadline.
func (r *Reactor) Shutdown(ctx context.Context) error {
	if !r.isStarted.Load() {
		return ErrNotStarted
	}

	if r.isStopped.Load() {
		return ErrIsStopped
	}

	wg := sync.WaitGroup{}
	done := make(chan struct{})

	for _, worker := range r.workers {
		wg.Go(func() {
			r.log.Debug(ctx, packageName+": shutting down worker", LogArgs{"worker": worker})

			if err := worker.Shutdown(ctx); err != nil {
				r.sendError(ctx, err)
			}
		})
	}

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		r.log.Debug(ctx, packageName+": all workers shut down")

	case <-ctx.Done():
		r.log.Debug(ctx, packageName+": workers shutdown cancelled by context")

	case <-r.errFullCh:
		return ErrChannelFull
	}

	return nil
}

// Errors returns a channel for receiving errors from all workers.
// The channel is closed when Start returns.
//
// The channel should be read concurrently with Start to receive all errors
// before the channel is closed. If the error channel buffer becomes full,
// ErrChannelFull is returned by Start or Shutdown instead of blocking goroutines,
// and the channel will be closed when Start returns.
func (r *Reactor) Errors() <-chan error {
	return r.errCh
}

// sendError safely sends an error to the error channel.
// If the reactor is stopped, the error is silently dropped.
// If the error channel is full, ErrChannelFull is sent to errDoneCh instead.
// This method is protected by a mutex to ensure thread-safe access to the channels.
func (r *Reactor) sendError(ctx context.Context, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.isSending.Load() {
		return
	}

	select {
	case r.errCh <- err:
	default:
		r.isSending.Store(false)
		close(r.errFullCh)

		r.log.Error(ctx, fmt.Errorf("%w: %w", ErrFailedToSend, err))
	}
}

// stop marks the reactor as stopped, stops error sending, and closes the error channel.
// This method is protected by a mutex to ensure thread-safe access to the channels.
func (r *Reactor) stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.isStopped.Store(true)
	r.isSending.Store(false)
	close(r.errCh)
}

// Ensure Reactor implements the Worker interface.
var _ Worker = (*Reactor)(nil)
