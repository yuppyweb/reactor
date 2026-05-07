package reactor_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/yuppyweb/reactor"
)

// mockLogger is a simple implementation of the reactor.Logger interface for testing purposes.
type mockDebugLog struct {
	ctx  context.Context
	msg  string
	args []any
}

// mockErrorLog is a simple struct to capture error log entries for testing purposes.
type mockErrorLog struct {
	ctx  context.Context
	err  error
	args []any
}

// mockLogger is a simple implementation of the reactor.Logger interface for testing purposes.
type mockLogger struct {
	mu       sync.Mutex
	debugLog []mockDebugLog
	errorLog []mockErrorLog
}

// Debug logs a debug message to the mockLogger.
func (m *mockLogger) Debug(ctx context.Context, msg string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.debugLog = append(m.debugLog, mockDebugLog{
		ctx:  ctx,
		msg:  msg,
		args: args,
	})
}

// Error captures error log entries in the mockLogger for testing purposes.
func (m *mockLogger) Error(ctx context.Context, err error, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.errorLog = append(m.errorLog, mockErrorLog{
		ctx:  ctx,
		err:  err,
		args: args,
	})
}

// Ensure mockLogger implements the reactor.Logger interface.
var _ reactor.Logger = (*mockLogger)(nil)

// TestNopLogger verifies that the NopLogger can be used without panicking and
// that it implements the Logger interface.
func TestNopLogger(t *testing.T) {
	t.Parallel()

	log := reactor.NewNopLogger()
	ctx := context.Background()

	// Test that NopLogger implements the Logger interface without panicking
	log.Debug(ctx, "this is a debug message", reactor.LogArgs{"key": "value"})
	log.Error(ctx, errors.New("error message"), reactor.LogArgs{"key": "value"})
}
