package limiter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBucket_WaitClampsNegativeWait(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		bucket bucket
		target float64
		want   time.Duration
	}{
		{
			name: "tokens above the target report an immediate wait",
			bucket: bucket{
				limit:    1,
				per:      time.Second,
				capacity: 1,
				tokens:   1.5,
			},
			target: 1,
			want:   0,
		},
		{
			name: "tokens below the target report the refill wait",
			bucket: bucket{
				limit:    1,
				per:      500 * time.Millisecond,
				capacity: 1,
				tokens:   0,
			},
			target: 1,
			want:   500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			b := tt.bucket

			assert.Equal(t, tt.want, b.wait(tt.target), "Expected the wait duration")
		})
	}
}
