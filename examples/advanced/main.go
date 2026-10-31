package main

import (
	"fmt"
	"time"

	hqgolimiter "github.com/hueristiq/hq-lib-limiter-go"
)

func main() {
	limiter := hqgolimiter.New(
		hqgolimiter.WithRequestsPer(2, time.Second),
		hqgolimiter.WithBurst(1),
	)

	allowed, delay := limiter.Inspect()

	fmt.Println(allowed, delay >= 0)
}
