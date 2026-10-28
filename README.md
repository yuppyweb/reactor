# ⚛️ Reactor

[![Go Version](https://img.shields.io/github/go-mod/go-version/yuppyweb/reactor)](https://github.com/yuppyweb/reactor) 
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT) 

**Reactor** is a Go library that runs a group of workers together. `Start` runs every worker, returns the first result, and stops the rest. `Stop` stops every worker. A reactor is itself a worker, so groups nest.

## ✨ Features

✅ **One Worker Interface** — Start and stop any unit of work the same way  
✅ **First Result Wins** — `Start` returns the earliest result and discards the rest  
✅ **Concurrent Stop** — `Stop` runs every worker at the same time and waits  
✅ **Panic Recovery** — A panic is raised again after the other workers have stopped  
✅ **Nested Groups** — A reactor implements `Worker`, so one group can contain another  
✅ **Built-in Workers** — `runner`, `closer`, and `trap` for functions, resources, and signals  
✅ **Fixed Membership** — The worker list is copied in `New` and stays fixed  

## 🎯 Core Idea

Application code depends on `reactor.Worker`. `New` turns a list of workers into one worker with the same interface. Built-in workers cover a function, an `io.Closer`, and operating system signals. A custom type can implement `Worker` directly.

```
┌─ Your process
│
├─ reactor.Worker ◄──── one group, also a worker
│
├─ Start
│  • run every worker at once
│  • return the first result
│  • stop the rest and wait
│
└─ Workers
   • runner — a function until stop
   • closer — close an io.Closer on Stop
   • trap   — wait for an OS signal
   • your own Worker
```

## 📦 Installation

```bash
go get github.com/yuppyweb/reactor
```

Import the worker packages you use:

```go
import (
    "github.com/yuppyweb/reactor"
    "github.com/yuppyweb/reactor/worker/closer"
    "github.com/yuppyweb/reactor/worker/runner"
    "github.com/yuppyweb/reactor/worker/trap"
)
```

## 🚀 Quick Start

`Start` blocks until the signal arrives or the runner returns. The first of those results is what `Start` returns. `Stop` then closes the file and unblocks the runner.

```go
package main

import (
    "log"
    "os"

    "github.com/yuppyweb/reactor"
    "github.com/yuppyweb/reactor/worker/closer"
    "github.com/yuppyweb/reactor/worker/runner"
    "github.com/yuppyweb/reactor/worker/trap"
)

func main() {
    file, err := os.Create("app.log")
    if err != nil {
        log.Fatal(err)
    }

    signals, err := trap.New(os.Interrupt)
    if err != nil {
        log.Fatal(err)
    }

    work, err := runner.New(func(stop <-chan struct{}) error {
        <-stop
        return nil
    })
    if err != nil {
        log.Fatal(err)
    }

    shutdown, err := closer.New(file)
    if err != nil {
        log.Fatal(err)
    }

    app, err := reactor.New(signals, work, shutdown)
    if err != nil {
        log.Fatal(err)
    }

    if err := app.Start(); err != nil {
        log.Fatal(err)
    }
}
```

## 📖 Core Components

### Worker Interface 📋

```go
type Worker interface {
    Start() error
    Stop()
}
```

`Start` runs until the work finishes. `Stop` asks it to finish. A parent reactor calls `Start` once per `Start` and `Stop` once per `Stop`. The two calls may run at the same time, so an implementation has to allow `Stop` while `Start` is in progress.

The concrete reactor type stays inside the package. Callers receive `Worker`.

### New 🏗️

```go
app, err := reactor.New(signals, work, shutdown)
```

`New` copies the worker list. Later changes to the caller's slice leave the group unchanged. An empty list returns `ErrNoWorkers`. A nil worker returns `ErrNilWorker`. In both cases the returned worker is nil, and no worker has been started.

Membership is fixed after `New`. Add another worker by building a new group, or by nesting groups.

### Start ▶️

`Start` runs every worker at the same time and returns the first result. That result may be nil when a worker finishes successfully before another worker returns an error. `Start` then calls `Stop` and waits until every worker has finished. Results that arrive after the first are discarded.

```go
err := app.Start()
```

A panic in a worker's `Start` is recovered. `Stop` still runs, and the panic is raised again after the other workers have stopped. When more than one worker panics, one recovered value is raised. The raised panic takes the place of a returned error, including when another worker already finished with an error. A panic during the `Stop` that follows the first result is raised from that `Stop`, after the other `Stop` calls finish.

`Start` blocks for the whole lifetime of the group. Call `Stop` from another goroutine when the group should end before a worker returns on its own. `trap` is the usual way to do that from an operating system signal.

### Stop ⏹️

`Stop` calls `Stop` on every worker at the same time and waits until every call returns. A later `Stop` calls every worker again. `runner`, `closer`, and `trap` ignore that second call. A custom worker chooses what a second `Stop` means.

```go
app.Stop()
```

A panic in a worker's `Stop` is recovered. The other `Stop` calls still finish, and one recovered value is raised again.

`Stop` before `Start` is safe. It stops every worker, and a later `Start` still runs them. Built-in workers then see that they are already stopped.

## 🔌 Workers

Each worker lives in its own package and exposes `New`. A nil argument returns a package-specific error and a nil worker. The concrete type stays in the package. Callers receive `reactor.Worker`.

### Runner 🏃

`runner` runs one function. `Start` calls it and returns its error. The first `Stop` closes the channel passed to the function and returns. The function must return after that channel is closed. A second `Stop` does nothing.

`Stop` before `Start` is safe: the channel is already closed when the function begins.

A nil function returns `ErrNilFunc`.

Inside a group, a function that keeps running after the channel is closed keeps `Start` blocked. The group waits for every worker to finish after the first result.

Typical uses:

- Run a server or client loop until the group stops
- Block until shutdown when the work has no other completion
- Return the function error as the group's first result

```go
import "github.com/yuppyweb/reactor/worker/runner"

work, err := runner.New(func(stop <-chan struct{}) error {
    <-stop
    return nil
})
```

### Closer 🔒

`closer` keeps an `io.Closer` open until `Stop`. `Start` blocks until `Stop`. The first `Stop` closes the resource once, stores the error from `Close`, and unblocks `Start` with that error. A second `Stop` does nothing.

`Stop` before `Start` is safe. `Start` then returns the stored close error.

A nil resource returns `ErrNilCloser`.

Inside a group, that close error is the value `Start` returns when the closer finishes first. When another worker finishes first, the close error is discarded with every other later result. The resource is still closed once.

Typical uses:

- Close a listener, file, or connection pool when the group stops
- Keep a resource open for the life of `Start`
- Report `Close` when this worker is the one that finishes first

```go
import "github.com/yuppyweb/reactor/worker/closer"

shutdown, err := closer.New(listener)
```

### Trap 📡

`trap` waits until one configured signal arrives or `Stop` is called. Both outcomes return nil, so a trap alone does not fail the group. `signal.Stop` runs when `Start` returns, and the process stops delivering those signals to this worker.

The signal list is copied. Later changes to the caller's slice leave the trap unchanged. An empty list returns `ErrNoSignals`. A nil signal returns `ErrNilSignal`. The handler is installed in `Start`.

The first `Stop` unblocks `Start`. A second `Stop` does nothing. `Stop` before `Start` is safe: `Start` returns nil.

Typical uses:

- End the process on interrupt
- Unblock a group when the operating system asks it to stop
- Finish the group with nil when the signal is the first result

```go
import (
    "os"

    "github.com/yuppyweb/reactor/worker/trap"
)

signals, err := trap.New(os.Interrupt)
```

## 📝 Examples

### Custom Worker 🛠️

`Start` and `Stop` may run at the same time. This worker closes its channel once, so a second `Stop` from the parent reactor is safe.

```go
import "sync"

type gate struct {
    stop chan struct{}
    once sync.Once
}

func newGate() *gate {
    return &gate{stop: make(chan struct{})}
}

func (g *gate) Start() error {
    <-g.stop
    return nil
}

func (g *gate) Stop() {
    g.once.Do(func() {
        close(g.stop)
    })
}
```

```go
worker := newGate()
app, err := reactor.New(worker)
```

### Nested Reactors 🧩

Stopping the outer reactor stops the inner groups. Each inner `Stop` stops that group's workers.

```go
// httpWorker, metricsWorker, dbWorker, and cacheWorker implement reactor.Worker.
// signals is the trap from above.

api, err := reactor.New(httpWorker, metricsWorker)
if err != nil {
    log.Fatal(err)
}

data, err := reactor.New(dbWorker, cacheWorker)
if err != nil {
    log.Fatal(err)
}

app, err := reactor.New(signals, api, data)
if err != nil {
    log.Fatal(err)
}

if err := app.Start(); err != nil {
    log.Fatal(err)
}
```

### Signal, Serve, and Close 🌐

The trap, the serve loop, and the listener run as one group. `http.Serve` returns when the closer closes the listener. The runner returns after `stop` is closed, once `Serve` has returned.

`Start` returns the first result. An interrupt finishes the trap with nil, so `Start` returns nil and a later close error is discarded. A serve error that finishes first is the value `Start` returns. The listener is closed either way.

```go
package main

import (
    "errors"
    "log"
    "net"
    "net/http"
    "os"

    "github.com/yuppyweb/reactor"
    "github.com/yuppyweb/reactor/worker/closer"
    "github.com/yuppyweb/reactor/worker/runner"
    "github.com/yuppyweb/reactor/worker/trap"
)

func main() {
    listener, err := net.Listen("tcp", ":8080")
    if err != nil {
        log.Fatal(err)
    }

    signals, err := trap.New(os.Interrupt)
    if err != nil {
        log.Fatal(err)
    }

    serve, err := runner.New(func(stop <-chan struct{}) error {
        errCh := make(chan error, 1)

        go func() {
            errCh <- http.Serve(listener, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
        }()

        select {
        case <-stop:
            <-errCh
            return nil
        case err := <-errCh:
            if errors.Is(err, net.ErrClosed) {
                return nil
            }

            return err
        }
    })
    if err != nil {
        log.Fatal(err)
    }

    shutdown, err := closer.New(listener)
    if err != nil {
        log.Fatal(err)
    }

    app, err := reactor.New(signals, serve, shutdown)
    if err != nil {
        log.Fatal(err)
    }

    if err := app.Start(); err != nil {
        log.Fatal(err)
    }
}
```

## 🤝 Contributing

Contributions are welcome. Please open a pull request.

## 📄 License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
