# hq-lib-limiter-go

![made with go](https://img.shields.io/badge/made%20with-Go-1E90FF.svg) [![go reference](https://pkg.go.dev/badge/github.com/hueristiq/hq-lib-limiter-go.svg)](https://pkg.go.dev/github.com/hueristiq/hq-lib-limiter-go) [![license](https://img.shields.io/badge/license-MIT-gray.svg?color=1E90FF)](https://github.com/hueristiq/hq-lib-limiter-go/blob/main/LICENSE) ![maintenance](https://img.shields.io/badge/maintained%3F-yes-1E90FF.svg) [![open issues](https://img.shields.io/github/issues-raw/hueristiq/hq-lib-limiter-go.svg?style=flat&color=1E90FF)](https://github.com/hueristiq/hq-lib-limiter-go/issues?q=is:issue+is:open) [![closed issues](https://img.shields.io/github/issues-closed-raw/hueristiq/hq-lib-limiter-go.svg?style=flat&color=1E90FF)](https://github.com/hueristiq/hq-lib-limiter-go/issues?q=is:issue+is:closed) [![contribution](https://img.shields.io/badge/contributions-welcome-1E90FF.svg)](https://github.com/hueristiq/hq-lib-limiter-go/blob/main/CONTRIBUTING.md)

`hq-lib-limiter-go` is a [Go](https://golang.org/) package for throttling outgoing work to one or more rate limits, such as "5 operations per second" or "100 requests per minute".

## Resources

- [Features](#features)
- [Installation](#installation)
- [Usage](#usage)
	- [Wait](#wait)
	- [Allow](#allow)
	- [Take](#take)
	- [Multiple Rate Windows](#multiple-rate-windows)
- [Configuration](#configuration)
- [Contributing](#contributing)
- [Licensing](#licensing)

## Features

- **Multiple Rate Windows:** `WithRate` is repeatable; every window is enforced simultaneously, so "5 per second _and_ 100 per minute" just works.
- **Token-Bucket Core:** One lazily refilled token bucket per window — no background goroutines or tickers.
- **Burst Control:** `WithBurst` caps how many operations may fire at once before pacing applies.
- **Three Acquisition Styles:** Non-blocking `Allow`, probing `Take` (reports how long a caller would wait), and blocking, context-aware `Wait`.
- **Fair Queuing:** `Wait` reserves tokens ahead of time, so concurrent waiters are paced in mutex-acquisition order; a wait canceled before its reservation matures refunds it.
- **Zero Value Ready:** A zero-value `Limiter` enforces the default window (`60` per minute) — no constructor required for the default policy.
- **Concurrency-Safe:** A mutex guards the bookkeeping — it is never held while sleeping.
- **Standard Library Only:** No third-party runtime dependencies.

## Installation

To install `hq-lib-limiter-go`, run the following command in your Go project:

```bash
go get -v -u github.com/hueristiq/hq-lib-limiter-go
```

## Usage

The examples below import the package under the `hqgolimiter` alias.

```go
import hqgolimiter "github.com/hueristiq/hq-lib-limiter-go"
```

### Wait

Use `Wait` to block until an operation may proceed, observing context cancellation and deadlines:

```go
package main

import (
	"context"
	"fmt"
	"time"

	hqgolimiter "github.com/hueristiq/hq-lib-limiter-go"
)

func main() {
	l := hqgolimiter.New(
		hqgolimiter.WithRate(5, time.Second),
		hqgolimiter.WithBurst(2),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	defer cancel()

	for i := 1; i <= 6; i++ {
		if err := l.Wait(ctx); err != nil {
			fmt.Printf("Stopped waiting: %v\n", err)

			break
		}

		fmt.Printf("Request %d made at %s\n", i, time.Now().Format(time.TimeOnly))
	}
}
```

### Allow

Use `Allow` when an operation should be skipped rather than queued:

```go
package main

import (
	"fmt"

	hqgolimiter "github.com/hueristiq/hq-lib-limiter-go"
)

func main() {
	l := hqgolimiter.New(
		hqgolimiter.WithRequestsPerMinute(60),
		hqgolimiter.WithBurst(5),
	)

	for i := 1; i <= 6; i++ {
		if l.Allow() {
			fmt.Printf("Request %d sent\n", i)
		} else {
			fmt.Printf("Request %d skipped: rate limited\n", i)
		}
	}
}
```

### Take

Use `Take` to probe availability — it reports whether an operation may proceed and how long a caller would otherwise wait, without consuming anything:

```go
package main

import (
	"fmt"
	"time"

	hqgolimiter "github.com/hueristiq/hq-lib-limiter-go"
)

func main() {
	l := hqgolimiter.New(hqgolimiter.WithRate(1, 100*time.Millisecond))

	if allowed, delay := l.Take(); allowed {
		fmt.Println("A request may proceed right now")
	} else {
		fmt.Printf("Rate limited; a caller would wait %v\n", delay)
	}
}
```

### Multiple Rate Windows

Supply `WithRate` repeatedly to enforce several windows simultaneously — an operation proceeds only when every window permits it:

```go
package main

import (
	"context"
	"fmt"
	"time"

	hqgolimiter "github.com/hueristiq/hq-lib-limiter-go"
)

func main() {
	// At most 5 requests per second and 100 requests per minute.
	l := hqgolimiter.New(
		hqgolimiter.WithRate(5, time.Second),
		hqgolimiter.WithRate(100, time.Minute),
		hqgolimiter.WithBurst(5),
	)

	ctx := context.Background()

	for i := 1; i <= 10; i++ {
		if err := l.Wait(ctx); err != nil {
			fmt.Printf("Stopped waiting: %v\n", err)

			break
		}

		fmt.Printf("Request %d made\n", i)
	}
}
```

## Configuration

Behavior is set through functional options passed to `New`. Any option left unset falls back to its default.

| Option | Default | Description |
| --- | --- | --- |
| `WithRate(limit, per)` | `DefaultLimit` per `DefaultPer` (`60` per minute) when no window is supplied | Adds a rate window of `limit` operations per `per`. Repeatable — every supplied window is enforced simultaneously. A non-positive `limit` or `per` falls back to its default. |
| `WithRequestsPerMinute(rpm)` | `DefaultLimit` (`60`) for non-positive values | Sugar for `WithRate(rpm, time.Minute)`. |
| `WithBurst(n)` | `DefaultBurst` (`1`) | Caps how many operations may fire at once; each window's token capacity is `min(n, window limit)`. A burst of `1` paces operations evenly. Non-positive values fall back to the default. |

Every bucket starts full, so the first burst of operations proceeds immediately. Buckets refill lazily as time passes — there is no background goroutine to stop.

A zero-value `Limiter` (`var l hqgolimiter.Limiter`) is ready to use and enforces the default window, exactly like `New()` with no options.

## Contributing

Contributions are welcome and encouraged! Feel free to submit [Pull Requests](https://github.com/hueristiq/hq-lib-limiter-go/pulls) or report [Issues](https://github.com/hueristiq/hq-lib-limiter-go/issues). For more details, check out the [contribution guidelines](https://github.com/hueristiq/hq-lib-limiter-go/blob/main/CONTRIBUTING.md).

A big thank you to all the [contributors](https://github.com/hueristiq/hq-lib-limiter-go/graphs/contributors) for your ongoing support!

![contributors](https://contrib.rocks/image?repo=hueristiq/hq-lib-limiter-go&max=500)

## Licensing

This package is licensed under the [MIT license](https://opensource.org/license/mit). You are free to use, modify, and distribute it, as long as you follow the terms of the license. You can find the full license text in the repository - [Full MIT license text](https://github.com/hueristiq/hq-lib-limiter-go/blob/main/LICENSE).
