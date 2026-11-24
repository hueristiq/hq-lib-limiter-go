# hq-lib-limiter-go

![made with go](https://img.shields.io/badge/made%20with-Go-1E90FF.svg) [![go reference](https://pkg.go.dev/badge/github.com/hueristiq/hq-lib-limiter-go.svg)](https://pkg.go.dev/github.com/hueristiq/hq-lib-limiter-go) [![license](https://img.shields.io/badge/license-MIT-gray.svg?color=1E90FF)](https://github.com/hueristiq/hq-lib-limiter-go/blob/main/LICENSE) ![maintenance](https://img.shields.io/badge/maintained%3F-yes-1E90FF.svg) [![open issues](https://img.shields.io/github/issues-raw/hueristiq/hq-lib-limiter-go.svg?style=flat&color=1E90FF)](https://github.com/hueristiq/hq-lib-limiter-go/issues?q=is:issue+is:open) [![closed issues](https://img.shields.io/github/issues-closed-raw/hueristiq/hq-lib-limiter-go.svg?style=flat&color=1E90FF)](https://github.com/hueristiq/hq-lib-limiter-go/issues?q=is:issue+is:closed)

`hq-lib-limiter-go` is a [Go](https://golang.org/) package for throttling outgoing work to one or more rate limits.

## Resources

- [Features](#features)
- [Installation](#installation)
- [Usage](#usage)
	- [Wait, allow, and inspect](#wait-allow-and-inspect)
	- [Every](#every)
	- [Multiple rate windows](#multiple-rate-windows)
- [Contributing](#contributing)
- [Licensing](#licensing)

## Features

- **Rate windows:** `WithRate` adds a window; repeating it enforces several at once.
- **Acquisition:** non-blocking `Allow`, non-consuming `Reserve`, and context-aware `Wait`.
- **Reservations:** `Wait` books tokens in order and refunds canceled waits.
- **Zero value:** a zero-value `Limiter` enforces 60 operations per minute.

## Installation

To install `hq-lib-limiter-go`, run the following command in your Go project:

```bash
go get github.com/hueristiq/hq-lib-limiter-go@latest
```

Requires Go 1.26.0 or later.

## Usage

The examples import the package under the `hqgolimiter` alias.

### Wait, allow, and inspect

```go
package main

import (
	"context"
	"fmt"
	"log"
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

	if err := l.Wait(ctx); err != nil {
		log.Fatal(err)
	}

	if l.Allow() {
		fmt.Println("request sent")
	} else {
		fmt.Println("request skipped: rate limited")
	}

	if allowed, delay := l.Reserve(); allowed {
		fmt.Println("a request may proceed now")
	} else {
		fmt.Printf("a caller would wait %s\n", delay)
	}
}
```

Use `Wait` to block until an operation may proceed. It observes context cancellation and deadlines.

Use `Allow` when an operation should be skipped rather than queued. It consumes a token from every window when it can, and never partially consumes.

Use `Reserve` to probe availability without consuming anything. It reports whether an operation may proceed and how long a caller would otherwise wait. Another goroutine may claim the token before you act, so pair `Reserve` with `Allow` or `Wait`.

### Every

`Every` builds the common "one operation per interval" pacer. It is sugar for `New(WithRate(1, interval), WithBurst(1))`.

```go
l := hqgolimiter.Every(100 * time.Millisecond)

if err := l.Wait(context.Background()); err != nil {
	log.Fatal(err)
}

if err := l.Wait(context.Background()); err != nil {
	log.Fatal(err)
}
```

### Multiple rate windows

Supply `WithRate` repeatedly to enforce several windows at the same time. An operation proceeds only when every window permits it.

```go
// At most 5 requests per second and 100 requests per minute.
l := hqgolimiter.New(
	hqgolimiter.WithRate(5, time.Second),
	hqgolimiter.WithRate(100, time.Minute),
	hqgolimiter.WithBurst(5),
)
```

### Zero value

A zero-value `Limiter` is ready to use and enforces the default 60-per-minute window.

```go
var l hqgolimiter.Limiter

if err := l.Wait(context.Background()); err != nil {
	log.Fatal(err)
}
```

## Contributing

Contributions are welcome and encouraged! Feel free to submit [Pull Requests](https://github.com/hueristiq/hq-lib-limiter-go/pulls) or report [Issues](https://github.com/hueristiq/hq-lib-limiter-go/issues).

A big thank you to all the [contributors](https://github.com/hueristiq/hq-lib-limiter-go/graphs/contributors) for your ongoing support!

![contributors](https://contrib.rocks/image?repo=hueristiq/hq-lib-limiter-go&max=500)

## Licensing

This package is licensed under the [MIT license](https://opensource.org/license/mit). You are free to use, modify, and distribute it, as long as you follow the terms of the license. You can find the full license text in the repository - [Full MIT license text](https://github.com/hueristiq/hq-lib-limiter-go/blob/main/LICENSE).
