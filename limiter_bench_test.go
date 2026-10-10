package limiter_test

import (
	"math"
	"sync/atomic"
	"testing"
	"time"

	hqgolimiter "github.com/hueristiq/hq-lib-limiter-go"
)

// sinkAllowed and sinkDelay keep serial benchmark results alive; sinkAtomic does the same
// for contended benchmarks, where a plain variable would be a data race.
var (
	sinkAllowed bool
	sinkDelay   time.Duration
	sinkAtomic  atomic.Bool
)

// BenchmarkLimiter measures [hqgolimiter.Limiter.Allow] and [hqgolimiter.Limiter.Take] on a
// limiter with an effectively unlimited rate, so every call exercises the bookkeeping fast
// path — serially, and under parallel contention where the mutex is genuinely shared.
func BenchmarkLimiter(b *testing.B) {
	l := hqgolimiter.New(
		hqgolimiter.WithRate(math.MaxInt, time.Second),
		hqgolimiter.WithBurst(math.MaxInt),
	)

	b.Run("Allow", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			sinkAllowed = l.Allow()
		}
	})

	b.Run("Take", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			sinkAllowed, sinkDelay = l.Take()
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

	b.Run("TakeContended", func(b *testing.B) {
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if _, delay := l.Take(); delay < 0 {
					sinkAtomic.Store(true)
				}
			}
		})
	})
}
