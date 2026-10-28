// Package reactor coordinates the lifecycle of concurrent workers.
//
// New groups workers that implement Worker. Start runs every worker and
// returns the first result. It then stops the group and waits until the
// remaining workers finish. Later results are discarded. Stop stops every
// worker. A panic in Start or Stop is recovered and raised again after the
// other workers have finished.
//
// Reactor implements Worker, so one reactor can be a worker of another.
package reactor

import (
	"errors"
	"sync"
)

var (
	// ErrNoWorkers is returned by New when no workers are provided.
	ErrNoWorkers = errors.New("no workers provided")

	// ErrNilWorker is returned by New when a worker is nil.
	ErrNilWorker = errors.New("worker is nil")
)

// Worker is one unit of work with a start and a stop.
//
// Start runs until the work finishes. Stop asks it to finish. Reactor
// calls Start once per Start. It calls Stop once per Stop and once more
// after Start receives the first result, so Stop is usually called more
// than once and may run at the same time as Start or another Stop.
// Reactor recovers a panic in either method and raises it again after the
// other workers have finished. Start must still return after Stop, even
// when Stop panicked, or the reactor's Start blocks.
type Worker interface {
	// Start runs the worker and returns when it finishes.
	Start() error

	// Stop asks the worker to finish.
	Stop()
}

// reactor runs a fixed list of workers together.
//
// workers is the copy taken by New. Later changes to the caller's slice
// do not change it.
type reactor struct {
	// workers is the copied list passed to New, in the same order.
	workers []Worker
}

// New returns a worker that runs every given worker together.
//
// The workers slice is copied. Later changes to the caller's slice do
// not affect the reactor. The concrete type is not exported. Callers
// receive Worker, and a reactor can itself be a worker of another
// reactor.
//
// An empty list returns ErrNoWorkers. A nil worker returns ErrNilWorker.
// No worker is started.
func New(workers ...Worker) (Worker, error) {
	if len(workers) == 0 {
		return nil, ErrNoWorkers
	}

	copied := make([]Worker, 0, len(workers))

	for _, worker := range workers {
		if worker == nil {
			return nil, ErrNilWorker
		}

		copied = append(copied, worker)
	}

	return &reactor{workers: copied}, nil
}

// Start runs every worker at the same time and returns the first result.
// It then calls Stop and waits until every worker has finished. Results
// that arrive after the first are discarded.
//
// A panic in a worker's Start is recovered. Stop still runs, and the
// panic is raised again instead of being returned. If more than one
// worker panics, one recovered value is raised. A panic in a worker's
// Stop is also recovered. Start still waits for every worker, then raises
// it again. A panic in Start takes precedence over a panic in Stop. A
// worker that returns through runtime.Goexit counts as a nil result.
func (r *reactor) Start() error {
	errCh := make(chan error, len(r.workers))
	panicCh := make(chan any, len(r.workers))

	for _, worker := range r.workers {
		go func() {
			var err error

			defer func() {
				if rec := recover(); rec != nil {
					panicCh <- rec
				}

				errCh <- err
			}()

			err = worker.Start()
		}()
	}

	err := <-errCh

	stopPanic := r.stop()

	for i := 1; i < cap(errCh); i++ {
		<-errCh
	}

	select {
	case rec := <-panicCh:
		panic(rec)
	default:
	}

	if stopPanic != nil {
		panic(stopPanic)
	}

	return err
}

// Stop calls Stop on every worker at the same time and waits until every
// call returns. A later Stop calls every worker again.
//
// A panic in a worker's Stop is recovered. The other Stop calls still
// finish, and one recovered value is raised again.
func (r *reactor) Stop() {
	if rec := r.stop(); rec != nil {
		panic(rec)
	}
}

// stop calls Stop on every worker at the same time and waits until every
// call returns. It returns one recovered panic value, or nil when no Stop
// panicked.
func (r *reactor) stop() any {
	wg := sync.WaitGroup{}
	panicCh := make(chan any, len(r.workers))

	for _, worker := range r.workers {
		wg.Go(func() {
			defer func() {
				if rec := recover(); rec != nil {
					panicCh <- rec
				}
			}()

			worker.Stop()
		})
	}

	wg.Wait()

	select {
	case rec := <-panicCh:
		return rec
	default:
	}

	return nil
}
