// Package trap provides a worker that waits for operating system signals.
//
// Start blocks until one configured signal arrives or Stop is called.
// Both outcomes return nil. signal.Stop runs when Start returns. The
// concrete type is not exported. Callers receive reactor.Worker.
package trap

import (
	"errors"
	"os"
	"os/signal"
	"sync"

	"github.com/yuppyweb/reactor"
)

var (
	// ErrNoSignals is returned by New when no signals are provided.
	ErrNoSignals = errors.New("no signals provided")

	// ErrNilSignal is returned by New when a signal is nil.
	ErrNilSignal = errors.New("signal is nil")
)

// trap waits until one configured signal arrives or Stop is called.
//
// The first Stop closes stop. Start then returns nil. A second Stop
// does nothing.
type trap struct {
	// signals is the copied list passed to New.
	signals []os.Signal

	// stop is closed by the first Stop to unblock Start.
	stop chan struct{}

	// once makes Stop close stop only once.
	once sync.Once
}

// New returns a worker that waits for any of the given signals.
//
// The signals slice is copied. Later changes to the caller's slice do
// not affect the trap. The concrete type is not exported. Callers
// receive reactor.Worker.
//
// An empty list returns ErrNoSignals. A nil signal returns ErrNilSignal.
// No signal handler is installed until Start.
func New(signals ...os.Signal) (reactor.Worker, error) {
	if len(signals) == 0 {
		return nil, ErrNoSignals
	}

	copied := make([]os.Signal, 0, len(signals))

	for _, sig := range signals {
		if sig == nil {
			return nil, ErrNilSignal
		}

		copied = append(copied, sig)
	}

	return &trap{
		signals: copied,
		stop:    make(chan struct{}),
		once:    sync.Once{},
	}, nil
}

// Start waits until a signal arrives or Stop is called.
//
// Both outcomes return nil. signal.Stop is called when Start returns,
// so the process no longer delivers those signals to this worker. When
// no other channel is registered for a signal, its default action applies
// again: a second interrupt during shutdown ends the process. Stop before
// Start is safe: Start returns nil without waiting.
func (tr *trap) Start() error {
	signalCh := make(chan os.Signal, 1)

	signal.Notify(signalCh, tr.signals...)
	defer signal.Stop(signalCh)

	select {
	case <-signalCh:
		return nil
	case <-tr.stop:
		return nil
	}
}

// Stop unblocks Start.
//
// A second call does nothing. Stop does not wait for Start to return.
func (tr *trap) Stop() {
	tr.once.Do(func() {
		close(tr.stop)
	})
}
