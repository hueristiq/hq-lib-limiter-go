package limiter_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hqgolimiter "github.com/hueristiq/hq-lib-limiter-go"
)

var errStopRequested = errors.New("stop requested")

func TestNew_Configuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ofs  []hqgolimiter.OptionFunc
		want []bool
	}{
		{
			name: "default options pace evenly",
			ofs:  nil,
			want: []bool{true, false},
		},
		{
			name: "explicit rate without burst paces evenly",
			ofs:  []hqgolimiter.OptionFunc{hqgolimiter.WithRate(5, time.Second)},
			want: []bool{true, false},
		},
		{
			name: "zero rate falls back to the default window",
			ofs:  []hqgolimiter.OptionFunc{hqgolimiter.WithRate(0, 0)},
			want: []bool{true, false},
		},
		{
			name: "negative rate falls back to the default window",
			ofs:  []hqgolimiter.OptionFunc{hqgolimiter.WithRate(-5, -time.Second)},
			want: []bool{true, false},
		},
		{
			name: "requests per minute sugar",
			ofs:  []hqgolimiter.OptionFunc{hqgolimiter.WithRequestsPerMinute(120)},
			want: []bool{true, false},
		},
		{
			name: "invalid requests per minute falls back to the default window",
			ofs:  []hqgolimiter.OptionFunc{hqgolimiter.WithRequestsPerMinute(0)},
			want: []bool{true, false},
		},
		{
			name: "burst permits a burst then denies",
			ofs: []hqgolimiter.OptionFunc{
				hqgolimiter.WithRate(5, time.Second),
				hqgolimiter.WithBurst(3),
			},
			want: []bool{true, true, true, false},
		},
		{
			name: "zero burst falls back to the default burst",
			ofs: []hqgolimiter.OptionFunc{
				hqgolimiter.WithRate(5, time.Second),
				hqgolimiter.WithBurst(0),
			},
			want: []bool{true, false},
		},
		{
			name: "negative burst falls back to the default burst",
			ofs: []hqgolimiter.OptionFunc{
				hqgolimiter.WithRate(5, time.Second),
				hqgolimiter.WithBurst(-2),
			},
			want: []bool{true, false},
		},
		{
			name: "burst above the window limit is clamped to it",
			ofs: []hqgolimiter.OptionFunc{
				hqgolimiter.WithRate(2, time.Second),
				hqgolimiter.WithBurst(10),
			},
			want: []bool{true, true, false},
		},
		{
			name: "repeatable rates enforce every window",
			ofs: []hqgolimiter.OptionFunc{
				hqgolimiter.WithRate(3, time.Second),
				hqgolimiter.WithRate(2, time.Minute),
				hqgolimiter.WithBurst(5),
			},
			want: []bool{true, true, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			l := hqgolimiter.New(tt.ofs...)

			gots := make([]bool, 0, len(tt.want))

			for range len(tt.want) {
				gots = append(gots, l.Allow())
			}

			assert.Equal(t, tt.want, gots, "Expected this sequence of Allow results")
		})
	}
}

func TestLimiter_Allow_Refill(t *testing.T) {
	t.Parallel()

	l := hqgolimiter.New(
		hqgolimiter.WithRate(4, 40*time.Millisecond),
		hqgolimiter.WithBurst(1),
	)

	assert.True(t, l.Allow(), "Expected the first call to consume the burst token")
	assert.False(t, l.Allow(), "Expected denial while the bucket is empty")

	time.Sleep(30 * time.Millisecond)

	assert.True(t, l.Allow(), "Expected a token to refill after enough time")
	assert.False(t, l.Allow(), "Expected denial again once the refilled token is spent")
}

func TestLimiter_Take(t *testing.T) {
	t.Parallel()

	l := hqgolimiter.New(
		hqgolimiter.WithRate(2, 20*time.Millisecond),
		hqgolimiter.WithBurst(2),
	)

	allowed, delay := l.Take()
	assert.True(t, allowed, "Expected availability on a fresh limiter")
	assert.Zero(t, delay, "Expected no wait on a fresh limiter")

	allowed, delay = l.Take()
	assert.True(t, allowed, "Expected Take to probe without consuming")
	assert.Zero(t, delay, "Expected no wait while tokens remain")

	require.True(t, l.Allow(), "Expected the burst to be consumable")
	require.True(t, l.Allow(), "Expected the burst to be consumable")
	require.False(t, l.Allow(), "Expected denial once the burst is spent")

	allowed, delay = l.Take()
	assert.False(t, allowed, "Expected no availability once the burst is spent")
	assert.Positive(t, delay, "Expected a positive wait once the burst is spent")
	assert.LessOrEqual(t, delay, 20*time.Millisecond, "Expected the wait to stay within one refill")

	time.Sleep(delay + 20*time.Millisecond)

	allowed, delay = l.Take()
	assert.True(t, allowed, "Expected availability after the reported wait elapsed")
	assert.Zero(t, delay, "Expected no wait after the reported wait elapsed")
}

func TestLimiter_Wait_Immediate(t *testing.T) {
	t.Parallel()

	l := hqgolimiter.New(hqgolimiter.WithRequestsPerMinute(60))

	err := l.Wait(t.Context())

	require.NoError(t, err, "Expected an immediate token on a fresh limiter")
}

func TestLimiter_Wait_Blocks(t *testing.T) {
	t.Parallel()

	l := hqgolimiter.New(hqgolimiter.WithRate(1, 30*time.Millisecond))

	require.True(t, l.Allow(), "Expected the burst token to drain the bucket")

	start := time.Now()

	err := l.Wait(t.Context())
	elapsed := time.Since(start)

	require.NoError(t, err, "Expected the wait to acquire the refilled token")
	assert.GreaterOrEqual(t, elapsed, 15*time.Millisecond, "Expected the wait to block for the refill")
}

func TestLimiter_Wait_Paced(t *testing.T) {
	t.Parallel()

	l := hqgolimiter.New(hqgolimiter.WithRate(1, 20*time.Millisecond))

	require.True(t, l.Allow(), "Expected the burst token to drain the bucket")

	start := time.Now()

	var wg sync.WaitGroup

	errs := make(chan error, 3)

	for range 3 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			errs <- l.Wait(t.Context())
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err, "Expected every queued wait to acquire its token")
	}

	// Queued reservations mature one refill interval apart, so the last of three
	// waits cannot complete before two intervals have passed.
	assert.GreaterOrEqual(t, time.Since(start), 40*time.Millisecond, "Expected queued waits to be paced")
}

func TestLimiter_Wait_ContextCancellation(t *testing.T) {
	t.Parallel()

	t.Run("already canceled context", func(t *testing.T) {
		t.Parallel()

		l := hqgolimiter.New()

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err := l.Wait(ctx)

		require.ErrorIs(t, err, context.Canceled, "Expected the context's cancellation")
		assert.ErrorContains(t, err, "limiter.Wait", "Expected the producing function's name")
	})

	t.Run("deadline before maturity fails immediately", func(t *testing.T) {
		t.Parallel()

		l := hqgolimiter.New(
			hqgolimiter.WithRate(1, time.Hour),
			hqgolimiter.WithBurst(1),
		)

		require.True(t, l.Allow(), "Expected the burst token to drain the bucket")

		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()

		start := time.Now()

		err := l.Wait(ctx)
		elapsed := time.Since(start)

		require.ErrorIs(t, err, context.DeadlineExceeded, "Expected the context's deadline")
		assert.Less(t, elapsed, 2*time.Second, "Expected an immediate failure, not a wait for the window")
	})

	t.Run("canceled while waiting releases the reservation", func(t *testing.T) {
		t.Parallel()

		l := hqgolimiter.New(hqgolimiter.WithRate(1, 60*time.Millisecond))

		require.True(t, l.Allow(), "Expected the burst token to drain the bucket")

		ctx, cancel := context.WithCancel(t.Context())

		errs := make(chan error, 1)

		go func() {
			errs <- l.Wait(ctx)
		}()

		time.Sleep(10 * time.Millisecond)
		cancel()

		err := <-errs

		require.ErrorIs(t, err, context.Canceled, "Expected the context's cancellation")

		// The canceled reservation was refunded, so after one refill window the token
		// is available again; without the refund it would still be owed to the queue.
		time.Sleep(70 * time.Millisecond)

		assert.True(t, l.Allow(), "Expected the canceled reservation's token to be refunded")
	})

	t.Run("cancel cause is returned", func(t *testing.T) {
		t.Parallel()

		l := hqgolimiter.New(
			hqgolimiter.WithRate(1, time.Hour),
			hqgolimiter.WithBurst(1),
		)

		require.True(t, l.Allow(), "Expected the burst token to drain the bucket")

		ctx, cancel := context.WithCancelCause(t.Context())

		errs := make(chan error, 1)

		go func() {
			errs <- l.Wait(ctx)
		}()

		time.Sleep(10 * time.Millisecond)
		cancel(errStopRequested)

		err := <-errs

		require.ErrorIs(t, err, errStopRequested, "Expected the context's custom cause")
		assert.ErrorContains(t, err, "limiter.Wait", "Expected the producing function's name")
	})
}

func TestLimiter_MultiRate(t *testing.T) {
	t.Parallel()

	l := hqgolimiter.New(
		hqgolimiter.WithRate(2, 50*time.Millisecond),
		hqgolimiter.WithRate(3, 300*time.Millisecond),
		hqgolimiter.WithBurst(3),
	)

	// The 50 ms window caps the burst at its own limit of 2.
	assert.True(t, l.Allow(), "Expected the first operation within both windows")
	assert.True(t, l.Allow(), "Expected the second operation within both windows")
	assert.False(t, l.Allow(), "Expected the fast window to cap the burst")

	time.Sleep(60 * time.Millisecond)

	// The fast window refilled fully, but the 300 ms window gates further operations.
	assert.True(t, l.Allow(), "Expected the slow window's refilled token")
	assert.False(t, l.Allow(), "Expected the slow window to gate further operations")
}

func TestLimiter_Concurrency(t *testing.T) {
	t.Parallel()

	l := hqgolimiter.New(
		hqgolimiter.WithRate(100000, time.Second),
		hqgolimiter.WithBurst(64),
	)

	const goroutines = 32

	var (
		wg      sync.WaitGroup
		allowed atomic.Int64
		waited  atomic.Int64
	)

	ctx := t.Context()

	for range goroutines {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range 100 {
				_, _ = l.Take()

				if l.Allow() {
					allowed.Add(1)
				}

				if err := l.Wait(ctx); err == nil {
					waited.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	assert.Positive(t, allowed.Load(), "Expected some calls to consume the burst")
	assert.Equal(t, int64(goroutines*100), waited.Load(), "Expected every wait to acquire a token")
}
