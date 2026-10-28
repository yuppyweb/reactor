// Package runner provides a worker that runs a function until Stop.
//
// Start calls the function and returns its error. Stop closes the channel
// passed to the function. The function must return after that channel is
// closed. Closing it does not by itself stop the function. The concrete
// type is not exported. Callers receive reactor.Worker.
package runner

import (
	"errors"
	"sync"

	"github.com/yuppyweb/reactor"
)

// ErrNilFunc is returned by New when run is nil.
var ErrNilFunc = errors.New("function is nil")

// Run is the function Start calls.
//
// stop is closed by the first Stop. Run must return after stop is closed.
// The returned error is the result of Start.
type Run func(stop <-chan struct{}) error

// runner runs one function until that function returns.
//
// The first Stop closes stop. It does not wait for the function.
type runner struct {
	// run is the function passed to New.
	run Run

	// stop is closed by the first Stop and passed to run.
	stop chan struct{}

	// once makes Stop close stop only once.
	once sync.Once
}

// New returns a worker that runs run.
//
// The concrete type is not exported. Callers receive reactor.Worker.
// A nil run returns ErrNilFunc. The function is not called until Start.
// Stop before Start is safe: the channel is already closed when run
// begins.
func New(run Run) (reactor.Worker, error) {
	if run == nil {
		return nil, ErrNilFunc
	}

	return &runner{
		run:  run,
		stop: make(chan struct{}),
		once: sync.Once{},
	}, nil
}

// Start calls run with the stop channel and returns its error.
//
// Stop may already have closed the channel. The caller stops the
// function by calling Stop.
func (rn *runner) Start() error {
	return rn.run(rn.stop)
}

// Stop closes the channel passed to the function.
//
// A second call does nothing. Stop does not wait for the function to
// return.
func (rn *runner) Stop() {
	rn.once.Do(func() {
		close(rn.stop)
	})
}
