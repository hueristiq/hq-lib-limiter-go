package limiter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

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
