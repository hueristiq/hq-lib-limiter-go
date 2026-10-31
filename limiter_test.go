//nolint:testpackage // tests exercise unexported behavior.
package limiter

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errStopRequested = errors.New("stop requested")

func TestNew_Configuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ofs  []OptionFunc
		want []bool
	}{
		{
			name: "default options pace evenly",
			ofs:  nil,
			want: []bool{true, false},
		},
		{
			name: "explicit rate without burst paces evenly",
			ofs:  []OptionFunc{WithRate(5, time.Second)},
			want: []bool{true, false},
		},
		{
			name: "zero rate falls back to the default window",
			ofs:  []OptionFunc{WithRate(0, 0)},
			want: []bool{true, false},
		},
		{
			name: "negative rate falls back to the default window",
			ofs:  []OptionFunc{WithRate(-5, -time.Second)},
			want: []bool{true, false},
		},
		{
			name: "requests per minute sugar",
			ofs:  []OptionFunc{WithRequestsPerMinute(120)},
			want: []bool{true, false},
		},
		{
			name: "invalid requests per minute falls back to the default window",
			ofs:  []OptionFunc{WithRequestsPerMinute(0)},
			want: []bool{true, false},
		},
		{
			name: "requests per is equivalent to a rate window",
			ofs:  []OptionFunc{WithRequestsPer(5, time.Second)},
			want: []bool{true, false},
		},
		{
			name: "burst permits a burst then denies",
			ofs: []OptionFunc{
				WithRate(5, time.Second),
				WithBurst(3),
			},
			want: []bool{true, true, true, false},
		},
		{
			name: "zero burst falls back to the default burst",
			ofs: []OptionFunc{
				WithRate(5, time.Second),
				WithBurst(0),
			},
			want: []bool{true, false},
		},
		{
			name: "negative burst falls back to the default burst",
			ofs: []OptionFunc{
				WithRate(5, time.Second),
				WithBurst(-2),
			},
			want: []bool{true, false},
		},
		{
			name: "burst above the window limit is clamped to it",
			ofs: []OptionFunc{
				WithRate(2, time.Second),
				WithBurst(10),
			},
			want: []bool{true, true, false},
		},
		{
			name: "repeatable rates enforce every window",
			ofs: []OptionFunc{
				WithRate(3, time.Second),
				WithRate(2, time.Minute),
				WithBurst(5),
			},
			want: []bool{true, true, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			l := New(tt.ofs...)

			gots := make([]bool, 0, len(tt.want))

			for range len(tt.want) {
				gots = append(gots, l.Allow())
			}

			assert.Equal(t, tt.want, gots, "Expected this sequence of Allow results")
		})
	}
}

func TestEvery(t *testing.T) {
	t.Parallel()

	l := Every(40 * time.Millisecond)

	assert.True(t, l.Allow(), "Expected the first operation to proceed")
	assert.False(t, l.Allow(), "Expected the next operation to be paced")

	time.Sleep(50 * time.Millisecond)

	assert.True(t, l.Allow(), "Expected an operation to proceed after the interval elapsed")
}

func TestLimiter_Allow_Refill(t *testing.T) {
	t.Parallel()

	l := New(
		WithRate(4, 40*time.Millisecond),
		WithBurst(1),
	)

	assert.True(t, l.Allow(), "Expected the first call to consume the burst token")
	assert.False(t, l.Allow(), "Expected denial while the bucket is empty")

	time.Sleep(30 * time.Millisecond)

	assert.True(t, l.Allow(), "Expected a token to refill after enough time")
	assert.False(t, l.Allow(), "Expected denial again once the refilled token is spent")
}

func TestLimiter_Inspect(t *testing.T) {
	t.Parallel()

	l := New(
		WithRate(2, 20*time.Millisecond),
		WithBurst(2),
	)

	allowed, delay := l.Inspect()
	assert.True(t, allowed, "Expected availability on a fresh limiter")
	assert.Zero(t, delay, "Expected no wait on a fresh limiter")

	allowed, delay = l.Inspect()
	assert.True(t, allowed, "Expected Inspect to probe without consuming")
	assert.Zero(t, delay, "Expected no wait while tokens remain")

	require.True(t, l.Allow(), "Expected the burst to be consumable")
	require.True(t, l.Allow(), "Expected the burst to be consumable")
	require.False(t, l.Allow(), "Expected denial once the burst is spent")

	allowed, delay = l.Inspect()
	assert.False(t, allowed, "Expected no availability once the burst is spent")
	assert.Positive(t, delay, "Expected a positive wait once the burst is spent")
	assert.LessOrEqual(t, delay, 20*time.Millisecond, "Expected the wait to stay within one refill")

	time.Sleep(delay + 20*time.Millisecond)

	allowed, delay = l.Inspect()
	assert.True(t, allowed, "Expected availability after the reported wait elapsed")
	assert.Zero(t, delay, "Expected no wait after the reported wait elapsed")
}

func TestLimiter_Wait_Immediate(t *testing.T) {
	t.Parallel()

	l := New(WithRequestsPerMinute(60))

	err := l.Wait(context.Background())

	require.NoError(t, err, "Expected an immediate token on a fresh limiter")
}

func TestLimiter_Wait_Blocks(t *testing.T) {
	t.Parallel()

	l := New(WithRate(1, 30*time.Millisecond))

	require.True(t, l.Allow(), "Expected the burst token to drain the bucket")

	start := time.Now()

	err := l.Wait(context.Background())
	elapsed := time.Since(start)

	require.NoError(t, err, "Expected the wait to acquire the refilled token")
	assert.GreaterOrEqual(t, elapsed, 15*time.Millisecond, "Expected the wait to block for the refill")
}

func TestLimiter_Wait_Paced(t *testing.T) {
	t.Parallel()

	l := New(WithRate(1, 20*time.Millisecond))

	require.True(t, l.Allow(), "Expected the burst token to drain the bucket")

	start := time.Now()

	var wg sync.WaitGroup

	errs := make(chan error, 3)

	for range 3 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			errs <- l.Wait(context.Background())
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err, "Expected every queued wait to acquire its token")
	}

	// The Allow above drained the burst, so each of the three waits reserves one refill
	// interval ahead of the last: the final reservation matures after two intervals, and
	// the third wait cannot complete before that.
	assert.GreaterOrEqual(t, time.Since(start), 40*time.Millisecond, "Expected queued waits to be paced")
}

func TestLimiter_Wait_ContextCancellation(t *testing.T) {
	t.Parallel()

	t.Run("already canceled context", func(t *testing.T) {
		t.Parallel()

		l := New()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := l.Wait(ctx)

		require.ErrorIs(t, err, context.Canceled, "Expected the context's cancellation")
		assert.ErrorContains(t, err, "limiter.Wait", "Expected the producing function's name")
	})

	t.Run("deadline before maturity fails immediately", func(t *testing.T) {
		t.Parallel()

		l := New(
			WithRate(1, time.Hour),
			WithBurst(1),
		)

		require.True(t, l.Allow(), "Expected the burst token to drain the bucket")

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		start := time.Now()

		err := l.Wait(ctx)
		elapsed := time.Since(start)

		require.ErrorIs(t, err, context.DeadlineExceeded, "Expected the context's deadline")
		assert.Less(t, elapsed, 2*time.Second, "Expected an immediate failure, not a wait for the window")
	})

	t.Run("canceled while waiting releases the reservation", func(t *testing.T) {
		t.Parallel()

		l := New(WithRate(1, 60*time.Millisecond))

		require.True(t, l.Allow(), "Expected the burst token to drain the bucket")

		ctx, cancel := context.WithCancel(context.Background())

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

		l := New(
			WithRate(1, time.Hour),
			WithBurst(1),
		)

		require.True(t, l.Allow(), "Expected the burst token to drain the bucket")

		ctx, cancel := context.WithCancelCause(context.Background())

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

func TestLimiter_ZeroValue(t *testing.T) {
	t.Parallel()

	t.Run("Allow rate-limits at the default window", func(t *testing.T) {
		t.Parallel()

		var l Limiter

		assert.True(t, l.Allow(), "Expected the zero value's first call to consume the burst token")
		assert.False(t, l.Allow(), "Expected the zero value's second call to be denied within the default window")
	})

	t.Run("Inspect probes without consuming", func(t *testing.T) {
		t.Parallel()

		var l Limiter

		allowed, delay := l.Inspect()
		assert.True(t, allowed, "Expected availability on a fresh zero-value limiter")
		assert.Zero(t, delay, "Expected no wait on a fresh zero-value limiter")

		allowed, delay = l.Inspect()
		assert.True(t, allowed, "Expected Inspect to probe without consuming")
		assert.Zero(t, delay, "Expected no wait while the burst token remains")

		require.True(t, l.Allow(), "Expected the burst token to be consumable")

		allowed, delay = l.Inspect()
		assert.False(t, allowed, "Expected denial once the burst is spent")
		assert.Positive(t, delay, "Expected a positive wait until the default window refills")
		assert.LessOrEqual(t, delay, DefaultPer, "Expected the wait to stay within one refill")
	})

	t.Run("Wait reports an already canceled context", func(t *testing.T) {
		t.Parallel()

		var l Limiter

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := l.Wait(ctx)

		require.ErrorIs(t, err, context.Canceled, "Expected the context's cancellation")
		assert.ErrorContains(t, err, "limiter.Wait", "Expected the producing function's name")
	})
}

func TestLimiter_MultiRate(t *testing.T) {
	t.Parallel()

	l := New(
		WithRate(2, 50*time.Millisecond),
		WithRate(3, 300*time.Millisecond),
		WithBurst(3),
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

	l := New(
		WithRate(100000, time.Second),
		WithBurst(64),
	)

	const goroutines = 32

	var (
		wg      sync.WaitGroup
		allowed atomic.Int64
		waited  atomic.Int64
	)

	ctx := context.Background()

	for range goroutines {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range 100 {
				_, _ = l.Inspect()

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

func TestBucket_WaitClampsNegativeWait(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		limit    int
		per      time.Duration
		capacity float64
		tokens   float64
		target   float64
		want     time.Duration
	}{
		{
			name:     "tokens above the target report an immediate wait",
			limit:    1,
			per:      time.Second,
			capacity: 1,
			tokens:   1.5,
			target:   1,
			want:     0,
		},
		{
			name:     "tokens below the target report the refill wait",
			limit:    1,
			per:      500 * time.Millisecond,
			capacity: 1,
			tokens:   0,
			target:   1,
			want:     500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b := newBucket(tt.limit, tt.per, tt.capacity, time.Now())
			b.tokens = tt.tokens

			assert.Equal(t, tt.want, b.wait(tt.target), "Expected the wait duration")
		})
	}
}

func TestUnreserveMature(t *testing.T) {
	t.Parallel()

	limiter := New(WithRate(1, time.Second))
	limiter.unreserve(time.Now().Add(-time.Second))
}

// ExampleEvery demonstrates a pacer enforcing one operation per interval.
func ExampleEvery() {
	l := Every(time.Second)

	fmt.Println(l.Allow())
	fmt.Println(l.Allow())
	// Output:
	// true
	// false
}

// ExampleNew demonstrates a rate window with a burst.
func ExampleNew() {
	l := New(WithRate(5, time.Second), WithBurst(2))

	fmt.Println(l.Allow())
	fmt.Println(l.Allow())
	fmt.Println(l.Allow())
	// Output:
	// true
	// true
	// false
}

// FuzzAllow exercises Allow over arbitrary rate and burst configurations: the
// number of admitted calls must never exceed the normalized window capacity.
func FuzzAllow(f *testing.F) {
	f.Add(5, 2, 10)
	f.Add(0, 0, 3)
	f.Add(-7, -1, 8)

	f.Fuzz(func(t *testing.T, limit int, burst int, calls int) {
		if calls < 0 {
			calls = -calls
		}

		calls %= 100

		// An hour-long window makes refill negligible, so the admission count is
		// exactly min(calls, capacity).
		l := New(WithRate(limit, time.Hour), WithBurst(burst))

		capacity := int(l.buckets[0].capacity)

		allowed := 0

		for range calls {
			if l.Allow() {
				allowed++
			}
		}

		if allowed != min(calls, capacity) {
			t.Fatalf("Expected %d allowed calls out of %d with capacity %d, got %d", min(calls, capacity), calls, capacity, allowed)
		}
	})
}
