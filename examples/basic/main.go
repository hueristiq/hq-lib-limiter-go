package main

import (
	"context"
	"fmt"
	"time"

	hqgolimiter "github.com/hueristiq/hq-lib-limiter-go"
)

func main() {
	limiter := hqgolimiter.New(hqgolimiter.WithRate(2, time.Second))

	if err := limiter.Wait(context.Background()); err != nil {
		fmt.Println("wait:", err)

		return
	}

	fmt.Println("allowed:", limiter.Allow())
}
