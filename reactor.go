// Package reactor provides a lifecycle coordination framework for managing
// multiple concurrent workers. It implements the reactor pattern, allowing
// workers to be started and gracefully shut down together while collecting
// errors from all workers into a single channel.
//
// The Reactor type coordinates the lifecycle of multiple Worker implementations,
// ensuring that all workers start and stop in a controlled manner with proper
// context propagation and error handling.
//
// Example:
//
//	type MyWorker struct {}
//
//	func (w *MyWorker) Start(ctx context.Context) {
//		// Worker logic here
//	}
//
//	func (w *MyWorker) Shutdown(ctx context.Context) {
//		// Cleanup logic here
//	}
//
//	func (w *MyWorker) Errors() <-chan error {
//		// Return error channel
//	}
//
//	reactor := reactor.New(&MyWorker{})
//	reactor.Start(ctx)
//	defer reactor.Shutdown(ctx)
package reactor

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

var ErrWorkerIsNil = errors.New("worker is nil")

// Worker defines the lifecycle interface for a service component.
// Implementations must handle starting, shutting down, and reporting errors
// in a concurrent-safe manner.
type Worker interface {
	// Start begins the worker's operation with the given context.
	// The implementation should respect context cancellation and deadlines.
	// This call should return only after the worker has fully started.
	Start(ctx context.Context)

	// Shutdown gracefully stops the worker using the given context.
	// The implementation should respect the context deadline and perform
	// cleanup operations. This call should return only after the worker
	// has fully stopped.
	Shutdown(ctx context.Context)

	// Errors returns a read-only channel for error events from the worker.
	// The channel should be closed when the worker stops.
	// Implementations must ensure the channel is safe for concurrent reads.
	Errors() <-chan error
}

// Reactor coordinates the lifecycle of multiple workers.
// It ensures all workers start and shut down in a controlled manner, collecting
// errors from all workers into a single channel for unified error handling.
//
// Reactor is safe for concurrent use. The Start method can only be called once
// and returns immediately. The Shutdown method can be called multiple times
// and is idempotent.
type Reactor struct {
	workers      []Worker
	isStarted    atomic.Bool
	startOnce    sync.Once
	shutdownOnce sync.Once
	errCh        chan error
}

// New creates a new Reactor with the given workers.
// The workers will be started and shut down in the order they are provided.
// If no workers are provided, the Reactor will still function correctly.
func New(workers ...Worker) (*Reactor, error) {
	bufferSize := 0

	for _, worker := range workers {
		if worker == nil {
			return nil, ErrWorkerIsNil
		}

		bufferSize += cap(worker.Errors())
	}

	if bufferSize == 0 {
		bufferSize = len(workers)
	}

	return &Reactor{
		workers:      workers,
		isStarted:    atomic.Bool{},
		startOnce:    sync.Once{},
		shutdownOnce: sync.Once{},
		errCh:        make(chan error, bufferSize),
	}, nil
}

// Start begins execution of all workers concurrently.
// It starts each worker in a separate goroutine and collects errors from all workers
// into the error channel. Start can be called only once per Reactor instance;
// subsequent calls are no-ops.
//
// The context parameter is propagated to all workers and controls the lifetime
// of the worker startup sequence. If the context is cancelled, no guarantee is made
// about which workers have started.
//
// This method is non-blocking and returns immediately after starting all workers.
func (r *Reactor) Start(ctx context.Context) {
	if r.isStarted.Load() {
		return
	}

	r.startOnce.Do(func() {
		wg := sync.WaitGroup{}

		for _, worker := range r.workers {
			wg.Go(func() {
				for err := range worker.Errors() {
					select {
					case r.errCh <- err:
					default:
					}
				}
			})

			wg.Go(func() {
				worker.Start(ctx)
			})
		}

		r.isStarted.Store(true)
		wg.Wait()
		r.isStarted.Store(false)

		close(r.errCh)
	})
}

// Shutdown gracefully stops all workers with the given context.
// It can be called multiple times safely; only the first call performs the shutdown sequence.
// Subsequent calls are no-ops.
//
// The context parameter controls the deadline for the shutdown sequence.
// If the context is cancelled before all workers have shut down, the function returns
// immediately without waiting for all workers to complete shutdown.
//
// Shutdown does nothing if the reactor has not been started.
func (r *Reactor) Shutdown(ctx context.Context) {
	if !r.isStarted.Load() {
		return
	}

	r.shutdownOnce.Do(func() {
		wg := sync.WaitGroup{}
		done := make(chan struct{})

		for _, worker := range r.workers {
			wg.Go(func() {
				worker.Shutdown(ctx)
			})
		}

		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-ctx.Done():
		}
	})
}

// Errors returns a channel for receiving errors from all workers.
// The channel is closed after all workers have been shut down.
// Errors sent by workers are forwarded to this channel in a non-blocking manner;
// if the channel buffer is full, errors are dropped silently.
//
// The channel should be read concurrently with Start to avoid blocking workers
// that are trying to send errors.
func (r *Reactor) Errors() <-chan error {
	return r.errCh
}

// Ensure Reactor implements the Worker interface.
var _ Worker = (*Reactor)(nil)
