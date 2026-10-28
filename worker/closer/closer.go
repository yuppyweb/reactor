// Package closer provides a worker that closes an io.Closer on Stop.
//
// Start blocks until Stop. Stop closes the resource once and unblocks
// Start with the error from Close. The concrete type is not exported.
// Callers receive reactor.Worker.
package closer

import (
	"errors"
	"io"
	"sync"

	"github.com/yuppyweb/reactor"
)

// ErrNilCloser is returned by New when resource is nil.
var ErrNilCloser = errors.New("closer is nil")

// closer closes one resource when Stop is called.
//
// Start blocks on stop. The first Stop closes resource, stores the
// error, and closes stop. A second Stop does nothing. Stop before Start
// is safe: Start then returns the stored error.
type closer struct {
	// resource is closed by the first Stop.
	resource io.Closer

	// stop is closed by the first Stop to unblock Start.
	stop chan struct{}

	// once makes Stop close the resource only once.
	once sync.Once

	// err is the error from Close. Start returns it after stop is closed.
	err error
}

// New returns a worker that keeps resource open until Stop.
//
// The concrete type is not exported. Callers receive reactor.Worker.
// A nil resource returns ErrNilCloser. The resource is not closed until
// Stop.
func New(resource io.Closer) (reactor.Worker, error) {
	if resource == nil {
		return nil, ErrNilCloser
	}

	return &closer{
		resource: resource,
		stop:     make(chan struct{}),
		once:     sync.Once{},
		err:      nil,
	}, nil
}

// Start blocks until Stop and returns the error from Close.
//
// Stop before Start is safe. Start returns as soon as stop is closed,
// including when Close returned an error.
func (cl *closer) Start() error {
	<-cl.stop

	return cl.err
}

// Stop closes the resource and unblocks Start.
//
// A second call does nothing. The error from Close is stored for Start.
// Stop does not wait for Start to read it. If Close panics, the panic
// propagates from Stop, Start is still unblocked and returns nil, and
// later calls do not call Close again.
func (cl *closer) Stop() {
	cl.once.Do(func() {
		defer close(cl.stop)

		cl.err = cl.resource.Close()
	})
}
