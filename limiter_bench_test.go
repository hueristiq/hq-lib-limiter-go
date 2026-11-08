package limiter_test

import (
	"math"
	"sync/atomic"
	"testing"
	"time"

	hqgolimiter "github.com/hueristiq/hq-lib-limiter-go"
)

var (
	sinkAllowed bool
	sinkDelay   time.Duration
	sinkAtomic  atomic.Bool
)

func BenchmarkLimiter(b *testing.B) {
	l := hqgolimiter.New(
		hqgolimiter.WithRate(math.MaxInt, time.Second),
		hqgolimiter.WithBurst(math.MaxInt),
	)

	b.Run("Allow", func(b *testing.B) {
		b.ReportAllocs()

		for range b.N {
			sinkAllowed = l.Allow()
		}
	})

	b.Run("Inspect", func(b *testing.B) {
		b.ReportAllocs()

		for range b.N {
			sinkAllowed, sinkDelay = l.Inspect()
		}
	})

	b.Run("AllowContended", func(b *testing.B) {
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if !l.Allow() {
					sinkAtomic.Store(true)
				}
			}
		})
	})

	b.Run("InspectContended", func(b *testing.B) {
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if _, delay := l.Inspect(); delay < 0 {
					sinkAtomic.Store(true)
				}
			}
		})
	})
}
