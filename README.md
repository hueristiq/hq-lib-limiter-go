# hq-lib-limiter-go

![made with go](https://img.shields.io/badge/made%20with-Go-1E90FF.svg) [![go reference](https://pkg.go.dev/badge/github.com/hueristiq/hq-lib-limiter-go.svg)](https://pkg.go.dev/github.com/hueristiq/hq-lib-limiter-go) [![license](https://img.shields.io/badge/license-MIT-gray.svg?color=1E90FF)](https://github.com/hueristiq/hq-lib-limiter-go/blob/main/LICENSE) ![maintenance](https://img.shields.io/badge/maintained%3F-yes-1E90FF.svg) [![open issues](https://img.shields.io/github/issues-raw/hueristiq/hq-lib-limiter-go.svg?style=flat&color=1E90FF)](https://github.com/hueristiq/hq-lib-limiter-go/issues?q=is:issue+is:open) [![closed issues](https://img.shields.io/github/issues-closed-raw/hueristiq/hq-lib-limiter-go.svg?style=flat&color=1E90FF)](https://github.com/hueristiq/hq-lib-limiter-go/issues?q=is:issue+is:closed) [![contribution](https://img.shields.io/badge/contributions-welcome-1E90FF.svg)](https://github.com/hueristiq/hq-lib-limiter-go/blob/main/CONTRIBUTING.md)

`hq-lib-limiter-go` is a [Go](https://golang.org/) package for throttling outgoing work to one or more rate limits.

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Usage](#usage)
	- [Wait](#wait)
	- [Allow](#allow)
	- [Inspect](#inspect)
	- [Every](#every)
	- [Multiple Rate Windows](#multiple-rate-windows)
- [Contributing](#contributing)
- [Licensing](#licensing)

## Features

- **Multiple Rate Windows:** `WithRate` is repeatable; every window is enforced simultaneously, so "5 per second _and_ 100 per minute" just works.
- **Token-Bucket Core:** One lazily refilled token bucket per window — no background goroutines or tickers.
- **Burst Control:** `WithBurst` caps how many operations may fire at once before pacing applies.
- **Three Acquisition Styles:** Non-blocking `Allow`, probing `Inspect` (reports how long a caller would wait), and blocking, context-aware `Wait`.
- **Fair Queuing:** `Wait` reserves tokens ahead of time, so concurrent waiters are paced in the order reservations are taken; a wait canceled before its reservation matures refunds it.
- **Zero Value Ready:** A zero-value `Limiter` enforces the default window (`60` per minute) — no constructor required for the default policy.
- **Concurrency-Safe:** A mutex guards the bookkeeping — it is never held while sleeping.

## Installation

```bash
go get github.com/hueristiq/hq-lib-limiter-go@latest
```

Requires Go 1.23 or later.

## Usage

The examples import the package under the `hqgolimiter` alias:

```go
import hqgolimiter "github.com/hueristiq/hq-lib-limiter-go"
```

### Wait

Use `Wait` to block until an operation may proceed, observing context cancellation and deadlines:

```go
l := hqgolimiter.New(
	hqgolimiter.WithRate(5, time.Second),
	hqgolimiter.WithBurst(2),
)

ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

defer cancel()

if err := l.Wait(ctx); err != nil {
	fmt.Printf("Stopped waiting: %v\n", err)

	return
}

fmt.Println("Request made")
```

### Allow

Use `Allow` when an operation should be skipped rather than queued:

```go
l := hqgolimiter.New(
	hqgolimiter.WithRequestsPerMinute(60),
	hqgolimiter.WithBurst(5),
)

if l.Allow() {
	fmt.Println("Request sent")
} else {
	fmt.Println("Request skipped: rate limited")
}
```

### Inspect

Use `Inspect` to probe availability — it reports whether an operation may proceed and how long a caller would otherwise wait, without consuming anything:

```go
l := hqgolimiter.New(
	hqgolimiter.WithRate(1, 100*time.Millisecond),
	hqgolimiter.WithBurst(1),
)

l.Allow() // consume the initial token

if allowed, delay := l.Inspect(); allowed {
	fmt.Println("A request may proceed right now")
} else {
	fmt.Printf("Rate limited; a caller would wait %v\n", delay)
}
```

### Every

Use `Every` for the common "one operation per interval" pacer — it is sugar for `New(WithRate(1, interval), WithBurst(1))`:

```go
l := hqgolimiter.Every(100 * time.Millisecond)

l.Wait(context.Background()) // immediate
l.Wait(context.Background()) // waits until the interval has passed
```

### Multiple Rate Windows

Supply `WithRate` repeatedly to enforce several windows simultaneously — an operation proceeds only when every window permits it:

```go
// At most 5 requests per second and 100 requests per minute.
l := hqgolimiter.New(
	hqgolimiter.WithRate(5, time.Second),
	hqgolimiter.WithRate(100, time.Minute),
	hqgolimiter.WithBurst(5),
)
```

## Contributing

Contributions are welcome and encouraged! Feel free to submit [Pull Requests](https://github.com/hueristiq/hq-lib-limiter-go/pulls) or report [Issues](https://github.com/hueristiq/hq-lib-limiter-go/issues). For more details, check out the [contribution guidelines](https://github.com/hueristiq/hq-lib-limiter-go/blob/main/CONTRIBUTING.md).

A big thank you to all the [contributors](https://github.com/hueristiq/hq-lib-limiter-go/graphs/contributors) for their ongoing support!

![contributors](https://contrib.rocks/image?repo=hueristiq/hq-lib-limiter-go&max=500)

## Licensing

This package is licensed under the [MIT license](https://opensource.org/license/mit). You are free to use, modify, and distribute it, as long as you follow the terms of the license. You can find the full license text in the repository - [Full MIT license text](https://github.com/hueristiq/hq-lib-limiter-go/blob/main/LICENSE).
