# ⚛️ Reactor

[![Go Version](https://img.shields.io/github/go-mod/go-version/yuppyweb/reactor)](https://github.com/yuppyweb/reactor)
[![Go Report Card](https://goreportcard.com/badge/github.com/yuppyweb/reactor)](https://goreportcard.com/report/github.com/yuppyweb/reactor)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)

**Reactor** is a Go library that implements a lifecycle coordination pattern for managing multiple concurrent workers. It enables you to start and gracefully shut down multiple workers simultaneously with proper context propagation and unified error handling.

Perfect for microservice architectures where you need to manage multiple services (HTTP, gRPC, custom workers) on a single server.

## ✨ Features

✅ **Lifecycle Management** — Add, start, and gracefully stop workers in a controlled manner  
✅ **Concurrency Safety** — Thread-safe design using `sync.Mutex`, `sync.WaitGroup`, and `atomic`  
✅ **Unified Error Handling** — All worker errors are collected into a single channel with configurable buffer  
✅ **Context Propagation** — Full support for `context.Context` for timeouts and cancellation  
✅ **Composability** — Reactor itself implements the Worker interface, allowing nested reactors  
✅ **Custom Logging** — Pluggable logger interface for debug and error logging  
✅ **Built-in Helpers** — Includes `GRPCService` and `HTTPService` workers for common use cases  

## 📦 Installation

```bash
go get github.com/yuppyweb/reactor
```

## 🚀 Quick Start

```go
package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/yuppyweb/reactor"
	"github.com/yuppyweb/reactor/worker"
)

func main() {
	// Create reactor
	r, err := reactor.New()
	if err != nil {
		log.Fatal(err)
	}

	// Create and add HTTP server
	httpServer := &http.Server{Addr: ":8080"}
	r.Add(worker.NewHTTPService(httpServer))

	// Listen for errors in a separate goroutine
	go func() {
		for err := range r.Errors() {
			log.Printf("Worker error: %v", err)
		}
	}()

	// Start reactor in a separate goroutine
	ctx := context.Background()
	go func() {
		if err := r.Start(ctx); err != nil {
			log.Fatal(err)
		}
	}()

	// Do something useful...
	time.Sleep(5 * time.Second)

	// Gracefully shutdown the reactor with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	if err := r.Shutdown(shutdownCtx); err != nil {
		log.Fatal(err)
	}
	log.Println("Reactor stopped")
}
```

## 🔧 API Reference

### Creating a Reactor 🏗️

```go
// Create a new reactor with default settings
r, err := reactor.New()
if err != nil {
    log.Fatal(err)
}

// Create a reactor with custom options
r, err := reactor.New(
    reactor.WithLogger(customLogger),
    reactor.WithErrorBufferSize(500),
)
if err != nil {
    log.Fatal(err)
}
```

### Adding Workers ➕

```go
// Add any worker that implements the Worker interface
err := r.Add(myWorker)
if err != nil {
    log.Fatal(err) // Returns error if reactor already started
}
```

### Starting and Stopping ▶️

```go
// Start all workers (blocks until all workers complete)
ctx := context.Background()
go func() {
    if err := r.Start(ctx); err != nil {
        log.Fatal(err)
    }
}()

// Listen for errors
for err := range r.Errors() {
    log.Printf("Error: %v", err)
}

// Shutdown all workers
shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

if err := r.Shutdown(shutdownCtx); err != nil {
    log.Fatal(err)
}
```

### `Worker` Interface 🔌

Any type implementing the Worker interface can be managed by the Reactor:

```go
type Worker interface {
	// Start begins the worker's operation with context.
	// Must return an error immediately if startup fails.
	// Runtime errors should be sent to the Errors() channel.
	Start(ctx context.Context) error

	// Shutdown gracefully stops the worker.
	// Must return an error immediately if shutdown fails.
	// Cleanup errors should be sent to the Errors() channel.
	Shutdown(ctx context.Context) error

	// Errors returns a read-only channel for error events.
	// May return nil if the worker doesn't report errors.
	// The channel must be closed when the worker stops.
	Errors() <-chan error
}
```

### `Reactor` Type 🔄

#### `New(options ...Option) (*Reactor, error)`
Creates a new Reactor with optional configuration.
- **Parameters**: functional options for customization
- **Returns**: Reactor pointer or error
- **Default error buffer size**: 1000 (range: 1-10000)

```go
r, err := reactor.New(
    reactor.WithErrorBufferSize(500),
    reactor.WithLogger(myLogger),
)
```

#### `Add(worker Worker) error`
Registers a worker with the Reactor. Must be called before `Start()`.
- **Restrictions**: cannot add workers after Start is called
- **Returns**: `ErrNilWorker` if worker is nil, `ErrIsStarted` if reactor already running
- **Thread-safe**: safe for concurrent calls before Start

```go
err := r.Add(worker1)
err := r.Add(worker2)
```

#### `Start(ctx context.Context) error`
Starts all registered workers in parallel and blocks until completion.
- **Call count**: can only be called once per Reactor instance
- **Blocking**: this is a blocking call; typically run in a goroutine
- **Context**: propagated to all workers for lifecycle control
- **Worker startup**: each worker starts in a separate goroutine
- **Error collection**: errors from workers are collected into the Errors channel
- **Channel closure**: Errors channel is closed when Start returns
- **Returns**: error if reactor is already started, or if error buffer becomes full

```go
ctx := context.Background()
go func() {
	if err := r.Start(ctx); err != nil {
		log.Fatal(err)
	}
}()
```

#### `Shutdown(ctx context.Context) error`
Gracefully stops all workers.
- **Prerequisite**: must call Start before Shutdown
- **Concurrent shutdown**: each worker's Shutdown is called in a separate goroutine
- **Context deadline**: propagated to workers for controlling shutdown duration
- **Blocking behavior**: blocks until all workers shutdown, context is cancelled, or error buffer becomes full
- **Returns**: error if reactor hasn't started, if already stopped, or if error buffer becomes full

```go
shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

if err := r.Shutdown(shutdownCtx); err != nil {
	log.Fatal(err)
}
```

#### `Errors() <-chan error`
Returns a read-only channel for receiving all worker errors.
- **Buffering**: when buffer is full, further error sends cause `Start()` or `Shutdown()` to return `ErrChannelFull`
- **Channel closure**: automatically closed when Start returns
- **Concurrency**: safe to read from multiple goroutines
- **Reading pattern**: start reading from this channel in a concurrent goroutine before calling `Start()`, so errors are captured as they occur. The channel closes when `Start()` completes

```go
go func() {
	for err := range r.Errors() {
		log.Printf("Worker error: %v", err)
	}
}()
```

## ⚙️ Configuration Options

### `WithLogger(logger Logger)`
Sets a custom logger for debug and error message logging.
- **Default**: `NopLogger` (discards all messages)
- **Error**: returns `ErrNilLogger` if logger is nil

```go
r, err := reactor.New(reactor.WithLogger(myLogger))
```

### `WithErrorBufferSize(size int)`
Configures the error channel buffer size.
- **Valid range**: 1 to 10000
- **Default**: 1000
- **Error**: returns `ErrMinErrorBufferSize` or `ErrMaxErrorBufferSize` if out of range

```go
r, err := reactor.New(reactor.WithErrorBufferSize(500))
```

## 📋 Logger Interface

Implement custom logging by implementing the Logger interface:

```go
type Logger interface {
	// Debug logs debug-level messages
	Debug(ctx context.Context, msg string, args ...any)
	
	// Error logs error-level messages
	Error(ctx context.Context, err error, args ...any)
}
```

### Built-in: `NopLogger` 🚫
A no-operation logger that discards all messages.

```go
logger := reactor.NewNopLogger()
```

### Recommended Logger: `Cakelog` 🍰

For structured logging in production, consider using [**Cakelog**](https://github.com/yuppyweb/cakelog) — a flexible and efficient logger that implements the Reactor Logger interface:

```go
import (
	"context"
	"log/slog"
	"os"
	"github.com/yuppyweb/cakelog/adapter"
	"github.com/yuppyweb/reactor"
)

func main() {
	// Create Slog logger with JSON output
	slogLogger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	
	// Create Cakelog adapter
	logger := adapter.NewSlogLogger(slogLogger)
	
	// Use with Reactor
	r, err := reactor.New(reactor.WithLogger(logger))
	if err != nil {
		panic(err)
	}
	
	r.Start(context.Background())
}
```

Cakelog provides:
- ✅ Unified interface for multiple logging backends (Logrus, Slog, Zap, Zerolog)
- ✅ Structured logging with context support
- ✅ Decorators for adding features
- ✅ Production-ready performance and flexibility

Visit [yuppyweb/cakelog](https://github.com/yuppyweb/cakelog) for more information.

## 🚀 Built-in Workers

### `HTTPService` 🌐

Manages the lifecycle of an `http.Server`:

```go
import (
	"net/http"
	"github.com/yuppyweb/reactor/worker"
)

server := &http.Server{
	Addr:    ":8080",
	Handler: mux,
}

httpWorker := worker.NewHTTPService(server)
r.Add(httpWorker)
```

- **Start**: calls `ListenAndServe()`  
- **Shutdown**: calls `Shutdown(ctx)` with context
- **Errors**: returns nil (HTTP errors are not reported)

### `GRPCService` 📡

Manages the lifecycle of a gRPC server:

```go
import (
	"net"
	"google.golang.org/grpc"
	"github.com/yuppyweb/reactor/worker"
)

lis, _ := net.Listen("tcp", ":5000")
grpcServer := grpc.NewServer()
gpc.RegisterMyServiceServer(grpcServer, myServiceImpl)

grpcWorker := worker.NewGRPCService(grpcServer, lis)
r.Add(grpcWorker)
```

- **Start**: calls `Serve(listener)`
- **Shutdown**: calls `GracefulStop()`
- **Errors**: returns nil (gRPC errors are not reported)

### Custom Workers 🛠️

Create your own worker by implementing the Worker interface:

```go
type CustomWorker struct {
	errCh chan error
}

func (w *CustomWorker) Start(ctx context.Context) error {
	go func() {
		defer close(w.errCh)
		// Worker logic here
		// Send errors to w.errCh
	}()
	return nil
}

func (w *CustomWorker) Shutdown(ctx context.Context) error {
	// Cleanup logic
	return nil
}

func (w *CustomWorker) Errors() <-chan error {
	return w.errCh
}

// Use it
r.Add(&CustomWorker{errCh: make(chan error)})
```

## 📚 Usage Examples

### Example 1: HTTP + gRPC Services 💻

```go
package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"github.com/yuppyweb/reactor"
	"github.com/yuppyweb/reactor/worker"
)

func main() {
	// Create reactor
	r, err := reactor.New(reactor.WithErrorBufferSize(500))
	if err != nil {
		log.Fatal(err)
	}

	// Add HTTP service
	httpServer := &http.Server{Addr: ":8080"}
	r.Add(worker.NewHTTPService(httpServer))

	// Add gRPC service
	listener, _ := net.Listen("tcp", ":5000")
	grpcServer := grpc.NewServer()
	r.Add(worker.NewGRPCService(grpcServer, listener))

	// Listen for errors
	go func() {
		for err := range r.Errors() {
			log.Printf("Service error: %v", err)
		}
	}()

	// Start services
	ctx := context.Background()
	go r.Start(ctx)

	// Wait for interrupt and shutdown gracefully
	select {}
	
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r.Shutdown(shutdownCtx)
}
```

### Example 2: Custom Worker 🗂️

```go
type DatabasePool struct {
	errCh chan error
}

func (p *DatabasePool) Start(ctx context.Context) error {
	go func() {
		defer close(p.errCh)
		// Initialize connection pool
		for {
			select {
			case <-ctx.Done():
				return
			// Handle database work
			}
		}
	}()
	return nil
}

func (p *DatabasePool) Shutdown(ctx context.Context) error {
	// Close connections gracefully
	return nil
}

func (p *DatabasePool) Errors() <-chan error {
	return p.errCh
}

// Add to reactor
r.Add(&DatabasePool{errCh: make(chan error)})
```

### Example 3: Nested Reactors 🧩

```go
// Create service-specific reactors
apiReactor, _ := reactor.New()
apiReactor.Add(httpWorker)
apiReactor.Add(metricsWorker)

// Create data-layer reactor
dataReactor, _ := reactor.New()
dataReactor.Add(dbWorker)
dataReactor.Add(cacheWorker)

// Combine in main reactor
main, _ := reactor.New()
main.Add(apiReactor)
main.Add(dataReactor)

// Start and shutdown as single unit
go main.Start(context.Background())
main.Shutdown(shutdownCtx)
```

## ⚠️ Error Handling

The Reactor defines the following error types:

| Error | Meaning |
|-------|---------|
| `ErrNilOption` | An option passed to `New()` is nil |
| `ErrNilLogger` | Logger passed to `WithLogger()` is nil |
| `ErrMinErrorBufferSize` | Buffer size less than 1 |
| `ErrMaxErrorBufferSize` | Buffer size greater than 10000 |
| `ErrNilWorker` | Worker passed to `Add()` is nil |
| `ErrNoWorkers` | `Start()` called with no workers added |
| `ErrNotRestart` | `Start()` called more than once |
| `ErrIsStarted` | `Add()` called after `Start()` |
| `ErrNotStarted` | `Shutdown()` called before `Start()` |
| `ErrIsStopped` | `Add()` called after reactor is stopped |
| `ErrChannelFull` | Error channel buffer is full; `Start()` or `Shutdown()` unable to send errors |

### Best Practices 💡

- **Always read errors concurrently**: Start reading from `Errors()` in a goroutine before calling `Start()`
- **Add workers before starting**: Call `Add()` for all workers before calling `Start()`
- **Use context deadlines**: Set a reasonable timeout with `context.WithTimeout()` for `Shutdown()`
- **Handle channel fullness**: Monitor for `ErrChannelFull` to detect if error buffer is too small
- **Close error channels**: Worker implementations must close their error channels when done

```go
// Good: read errors concurrently
go func() {
	for err := range r.Errors() {
		log.Printf("Error: %v", err)
	}
}()

r.Start(ctx)
```

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## 📄 License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.

For more information about the MIT License, visit [opensource.org/licenses/MIT](https://opensource.org/licenses/MIT).
