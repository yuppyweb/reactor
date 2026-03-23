# ⚛️ Reactor

[![Go Version](https://img.shields.io/github/go-mod/go-version/yuppyweb/reactor)](https://github.com/yuppyweb/reactor)
[![Go Report Card](https://goreportcard.com/badge/github.com/yuppyweb/reactor)](https://goreportcard.com/report/github.com/yuppyweb/reactor)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

## 📌 Overview

**Reactor** is a Go library that implements a lifecycle coordination pattern for managing multiple concurrent workers. It enables you to start and gracefully shut down multiple workers simultaneously with proper context propagation and unified error handling.

Perfect for microservice architectures where you need to manage multiple services on a single server.

## ✨ Features

✅ **Lifecycle Management** — Start and stop workers in a controlled manner  
✅ **Concurrency Safety** — Thread-safe design using `sync.Once` and `atomic`  
✅ **Unified Error Handling** — All worker errors are collected into a single channel  
✅ **Context Propagation** — Full support for `context.Context` for timeouts and cancellation  
✅ **Idempotent Shutdown** — Call `Shutdown()` as many times as you want — same result  
✅ **Composability** — Reactor itself implements the Worker interface, allowing nested reactors  

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
	"time"

	"github.com/yuppyweb/reactor"
)

// Implement the Worker interface
type MyService struct {
	errCh chan error
}

func (s *MyService) Start(ctx context.Context) {
    defer close(s.errCh)
	// Service startup logic
	log.Println("Service started")
	// Listen for context cancellation
	<-ctx.Done()
	log.Println("Context cancelled")
}

func (s *MyService) Shutdown(ctx context.Context) {
	log.Println("Service shutting down")
	// Cleanup logic
}

func (s *MyService) Errors() <-chan error {
	return s.errCh
}

func main() {
	// Create service instance
	svc := &MyService{errCh: make(chan error)}

	// Create reactor
	r, err := reactor.New(svc)
	if err != nil {
		log.Fatal(err)
	}

	// Listen for errors in a separate goroutine
	go func() {
		for err := range r.Errors() {
			log.Printf("Error: %v", err)
		}
	}()

	// Start reactor in a separate goroutine (Start blocks waiting for all workers)
	ctx := context.Background()
	go r.Start(ctx)

	// Do something useful...
	time.Sleep(5 * time.Second)

	// Gracefully shutdown the reactor with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	r.Shutdown(shutdownCtx)
	log.Println("Reactor stopped")
}
```

## 🔧 API Reference

### `Worker` Interface

```go
type Worker interface {
	Start(ctx context.Context)           // Start the worker
	Shutdown(ctx context.Context)        // Gracefully stop the worker
	Errors() <-chan error               // Return error channel
}
```

Any type can implement this interface and be managed by the Reactor.

### `Reactor` Type

#### `New(workers ...Worker) (*Reactor, error)`
Creates a new Reactor with the given workers.
- **Parameters**: variable number of Worker implementations
- **Returns**: Reactor pointer or error
- **Errors**: `ErrWorkerIsNil` if any worker is nil

```go
reactor, err := reactor.New(worker1, worker2, worker3)
if err != nil {
	log.Fatal(err)
}
```

#### `Start(ctx context.Context)`
Starts all workers in parallel and **blocks** until they finish and the error channel closes. Can only be called once.
- **Behavior**: blocking call, waits for all workers to complete
- **Context**: propagated to all workers
- **Goroutines**: workers start in separate goroutines, recommended to call Start in a separate goroutine

```go
ctx := context.Background()
go r.Start(ctx)  // Run in a goroutine, otherwise it will wait for completion
```

#### `Shutdown(ctx context.Context)`
Gracefully stops all workers. Idempotent — can be called multiple times.
- **Context**: controls shutdown timeout
- **Blocking**: may block until all workers finish or context expires
- **Safety**: does nothing if reactor hasn't been started

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
r.Shutdown(ctx)
```

#### `Errors() <-chan error`
Returns a channel for receiving errors from all workers.
- **Buffering**: errors are dropped if buffer is full (non-blocking send)
- **Closure**: channel is closed after reactor fully shuts down
- **Concurrency**: safe to read from multiple goroutines

```go
go func() {
	for err := range r.Errors() {
		log.Printf("Worker error: %v", err)
	}
}()
```

## 📚 Usage Examples

### Example 1: HTTP Server with Database 🌐

```go
type HTTPServer struct {
	errCh chan error
}

func (s *HTTPServer) Start(ctx context.Context) {
    defer close(s.errCh)

    if err := s.serve(ctx); err != nil {
        select {
        case s.errCh <- err:
        default:
        }
    }
}

func (s *HTTPServer) Shutdown(ctx context.Context) {
	// Graceful shutdown logic
}

func (s *HTTPServer) Errors() <-chan error {
	return s.errCh
}
```

### Example 2: Nested Reactors 🪆

```go
// Create reactors for different components
apiReactor, _ := reactor.New(apiServer, apiWorker)
dbReactor, _ := reactor.New(dbConnPool, dbAutomigration)

// Combine them in a main reactor
main, _ := reactor.New(apiReactor, dbReactor)

// Start in a separate goroutine (Start blocks)
go main.Start(ctx)

// Stop on exit
shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
defer main.Shutdown(shutdownCtx)
```

## ⚠️ Error Handling

- **Nil Worker**: Reactor returns `ErrWorkerIsNil` when adding a nil worker
- **Lost Errors**: If error channel is full, new errors are dropped silently
- **Context Timeout**: If context expires, `Shutdown()` returns without waiting for full completion

Recommendations:
- Always listen to `Errors()` in a separate goroutine
- Use `context.WithTimeout` for Shutdown
- Log all errors for debugging

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## 📄 License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.

For more information about the MIT License, visit [opensource.org/licenses/MIT](https://opensource.org/licenses/MIT).
