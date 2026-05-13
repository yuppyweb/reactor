package reactor

import (
	"errors"
	"fmt"
)

const (
	// minErrorBufferSize defines the minimum allowed buffer size for the error channel.
	minErrorBufferSize = 1

	// maxErrorBufferSize defines the maximum allowed buffer size for the error channel.
	maxErrorBufferSize = 10000
)

var (
	ErrNilLogger = errors.New(packageName + ": logger cannot be nil")

	ErrMinErrorBufferSize = fmt.Errorf(
		"%s: error buffer size cannot be less than %d", packageName, minErrorBufferSize,
	)

	ErrMaxErrorBufferSize = fmt.Errorf(
		"%s: error buffer size cannot be greater than %d", packageName, maxErrorBufferSize,
	)
)

// WithLogger is an Option that sets a custom Logger for the Reactor.
// If the provided Logger is nil, it returns ErrNilLogger.
func WithLogger(log Logger) Option {
	return func(reactor *Reactor) error {
		if log == nil {
			return ErrNilLogger
		}

		reactor.log = log

		return nil
	}
}

// WithErrorBufferSize is an Option that sets the buffer size for the Reactor's error channel.
// If the provided size is less than minErrorBufferSize, it returns ErrMinErrorBufferSize.
// If the provided size is greater than maxErrorBufferSize, it returns ErrMaxErrorBufferSize.
func WithErrorBufferSize(size int) Option {
	return func(reactor *Reactor) error {
		if size < minErrorBufferSize {
			return ErrMinErrorBufferSize
		}

		if size > maxErrorBufferSize {
			return ErrMaxErrorBufferSize
		}

		reactor.errCh = make(chan error, size)

		return nil
	}
}
