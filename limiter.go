package limiter

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	// DefaultLimit is the default number of operations allowed per DefaultPer window.
	//
	// It applies when no rate window is configured, and when a configured limit less than
	// or equal to 0 is normalized back to the default.
	DefaultLimit = 60

	// DefaultPer is the default duration of a rate window.
	//
	// It applies when no rate window is configured, and when a configured window duration
	// less than or equal to 0 is normalized back to the default.
	DefaultPer = time.Minute

	// DefaultBurst is the default number of operations that may fire at once before pacing
	// applies.
	//
	// It applies when WithBurst is not supplied, and when a supplied value less than or
	// equal to 0 is normalized back to the default. A burst of 1 paces operations evenly,
	// with no bursting.
	DefaultBurst = 1
)

// rate is a single rate window: at most limit operations per duration, enforced by one
// token bucket.
//
// Fields:
//   - limit (int): The number of operations the window refills per per.
//   - per (time.Duration): The duration over which limit operations refill.
type rate struct {
	limit int
	per   time.Duration
}

// options holds the settings for a Limiter, defining the windows it enforces and the burst
// it tolerates.
//
// Fields:
//   - rates ([]rate): The rate windows to enforce simultaneously, one per WithRate call.
//   - burst (int): The maximum number of operations that may fire at once, capping every
//     window's token capacity.
type options struct {
	rates []rate
	burst int
}

// OptionFunc is a function type used to modify the limiter options in a declarative manner.
//
// It allows users to customize limiter behavior by setting fields in the options struct,
// such as the rate windows and the burst size. Multiple options can be combined to create
// a tailored policy; WithRate is repeatable, adding one window per call. Options can only
// be created by this package's With* helpers.
//
// Parameters:
//   - opts (*options): A pointer to the options struct to be modified.
type OptionFunc func(opts *options)

// WithRate returns an OptionFunc that adds a rate window of limit operations per duration.
//
// It is repeatable: every call adds another window, and all windows are enforced
// simultaneously — an operation proceeds only when every window has a token available.
// For example, WithRate(5, time.Second) together with WithRate(100, time.Minute) allows
// short bursts of 5 operations per second while capping sustained throughput at 100
// operations per minute.
//
// Parameters:
//   - limit (int): The number of operations allowed per window. Values less than or equal
//     to 0 fall back to DefaultLimit.
//   - per (time.Duration): The window duration. Values less than or equal to 0 fall back
//     to DefaultPer.
//
// Returns:
//   - f (OptionFunc): A functional option that appends the window to the options.
func WithRate(limit int, per time.Duration) (f OptionFunc) {
	return func(opts *options) {
		opts.rates = append(opts.rates, rate{limit: limit, per: per})
	}
}

// WithRequestsPerMinute returns an OptionFunc that adds a per-minute rate window.
//
// It is sugar for WithRate(rpm, time.Minute) and shares its normalization: a value less
// than or equal to 0 falls back to DefaultLimit.
//
// Parameters:
//   - rpm (int): The number of requests allowed per minute.
//
// Returns:
//   - f (OptionFunc): A functional option that appends the window to the options.
func WithRequestsPerMinute(rpm int) (f OptionFunc) {
	return WithRate(rpm, time.Minute)
}

// WithBurst returns an OptionFunc that sets the maximum number of operations that may fire
// at once.
//
// Each window's token capacity is capped at burst (and never exceeds the window's own
// limit), so a burst of 1 paces operations evenly while a burst equal to a window's limit
// lets the whole window fire immediately. Unset, the burst falls back to DefaultBurst.
//
// Parameters:
//   - burst (int): The maximum burst size. Values less than or equal to 0 fall back to
//     DefaultBurst.
//
// Returns:
//   - f (OptionFunc): A functional option that sets the burst field in the options.
func WithBurst(burst int) (f OptionFunc) {
	return func(opts *options) {
		opts.burst = burst
	}
}

// bucket is a token bucket tracking one rate window.
//
// The bucket refills lazily: advance folds the time elapsed since the last update into the
// token count instead of running a background ticker. The token count may go negative when
// Wait reserves tokens ahead of time; a negative count represents operations already
// promised to future timestamps.
//
// Fields:
//   - limit (int): The number of tokens refilled per window.
//   - per (time.Duration): The window duration over which limit tokens refill.
//   - capacity (float64): The maximum number of tokens the bucket holds.
//   - tokens (float64): The current token count; negative while reservations are queued.
//   - last (time.Time): The time of the last advance.
type bucket struct {
	limit    int
	per      time.Duration
	capacity float64
	tokens   float64
	last     time.Time
}

// advance folds the time elapsed since the last update into the token count, clamping the
// result to the bucket's capacity.
//
// Parameters:
//   - now (time.Time): The current time.
func (b *bucket) advance(now time.Time) {
	elapsed := now.Sub(b.last)
	if elapsed <= 0 {
		return
	}

	b.tokens = min(b.capacity, b.tokens+float64(elapsed)*float64(b.limit)/float64(b.per))
	b.last = now
}

// wait returns the time until the token count reaches target, or 0 when the count is
// already there.
//
// The returned duration is clamped at 0, so a bucket ahead of its target (a negative
// reservation balance with no elapsed time) reports an immediate wait rather than one in
// the past.
//
// Parameters:
//   - target (float64): The token count to wait for.
//
// Returns:
//   - d (time.Duration): The wait at the bucket's refill rate.
func (b *bucket) wait(target float64) (d time.Duration) {
	if b.tokens >= target {
		return 0
	}

	d = max(0, time.Duration((target-b.tokens)*float64(b.per)/float64(b.limit)))

	return
}

// Limiter throttles operations to one or more rate windows.
//
// A Limiter holds one token bucket per configured window; an operation may proceed only
// when every bucket can spend a token. All methods are safe for concurrent use: the mutex
// is held for bookkeeping only, never while sleeping.
//
// The zero value is a ready-to-use limiter enforcing the default window of DefaultLimit
// operations per DefaultPer, exactly like New() with no options.
//
// Fields:
//   - mu (sync.Mutex): Guards the buckets' state.
//   - buckets ([]*bucket): The token buckets, one per rate window.
type Limiter struct {
	mu      sync.Mutex
	buckets []*bucket
}

// New creates a Limiter enforcing the configured rate windows.
//
// After the options are applied, invalid values are normalized: a non-positive limit or
// window duration falls back to its default, a non-positive burst falls back to
// DefaultBurst, and a limiter configured with no window enforces DefaultLimit operations
// per DefaultPer. Each window's token capacity is min(burst, window limit), and every
// bucket starts full, so the first burst of operations proceeds immediately.
//
// Parameters:
//   - ofs (...OptionFunc): Variadic functional options to customize limiter behavior, such
//     as rate windows and burst size.
//
// Returns:
//   - l (*Limiter): A new Limiter enforcing the configured windows.
func New(ofs ...OptionFunc) (l *Limiter) {
	opts := &options{
		burst: DefaultBurst,
	}

	for _, f := range ofs {
		f(opts)
	}

	if opts.burst <= 0 {
		opts.burst = DefaultBurst
	}

	if len(opts.rates) == 0 {
		opts.rates = []rate{{limit: DefaultLimit, per: DefaultPer}}
	}

	now := time.Now()

	l = &Limiter{
		buckets: make([]*bucket, 0, len(opts.rates)),
	}

	for _, r := range opts.rates {
		if r.limit <= 0 {
			r.limit = DefaultLimit
		}

		if r.per <= 0 {
			r.per = DefaultPer
		}

		capacity := float64(min(opts.burst, r.limit))

		l.buckets = append(l.buckets, &bucket{
			limit:    r.limit,
			per:      r.per,
			capacity: capacity,
			tokens:   capacity,
			last:     now,
		})
	}

	return
}

// defaultBuckets returns the limiter's configured buckets, creating and installing the
// default window's bucket when the limiter has none — the zero value.
//
// It must be called with mu held, so installing the default bucket is safe; later calls
// reuse it, keeping its refill state.
//
// Parameters:
//   - now (time.Time): The current time, used to seed the default bucket's last update.
//
// Returns:
//   - buckets ([]*bucket): The limiter's buckets, never nil.
func (l *Limiter) defaultBuckets(now time.Time) (buckets []*bucket) {
	if l.buckets != nil {
		return l.buckets
	}

	capacity := float64(min(DefaultBurst, DefaultLimit))

	l.buckets = []*bucket{{
		limit:    DefaultLimit,
		per:      DefaultPer,
		capacity: capacity,
		tokens:   capacity,
		last:     now,
	}}

	return l.buckets
}

// Allow reports whether an operation may proceed immediately, consuming a token from every
// window when it can.
//
// It never blocks and never partially consumes: when any window lacks a token, no window
// is charged and false is returned.
//
// Returns:
//   - allowed (bool): true when a token was available in every window and consumed.
func (l *Limiter) Allow() (allowed bool) {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	buckets := l.defaultBuckets(now)
	allowed = true

	for _, b := range buckets {
		b.advance(now)

		if b.tokens < 1 {
			allowed = false
		}
	}

	if !allowed {
		return
	}

	for _, b := range buckets {
		b.tokens--
	}

	return
}

// Take reports whether an operation may proceed immediately, and how long a caller would
// have to wait when it may not.
//
// It is a probe and consumes nothing: no tokens are charged, so an availability it reports
// may be claimed by another goroutine before the caller acts on it. Pair it with Allow or
// Wait to actually acquire a token.
//
// Returns:
//   - allowed (bool): true when a token is available in every window.
//   - delay (time.Duration): The wait until every window holds a token, 0 when allowed.
func (l *Limiter) Take() (allowed bool, delay time.Duration) {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	buckets := l.defaultBuckets(now)
	allowed = true

	for _, b := range buckets {
		b.advance(now)

		if b.tokens >= 1 {
			continue
		}

		allowed = false

		if d := b.wait(1); d > delay {
			delay = d
		}
	}

	return
}

// Wait blocks until a token is available in every window or the context is done, consuming
// a token from every window when it succeeds.
//
// The wait is a reservation: the token is booked before sleeping, so concurrent waiters
// are paced in mutex-acquisition order and no token can be stolen while sleeping. When the
// context's deadline falls before the reservation would mature, Wait fails immediately
// without consuming. A wait canceled before its reservation matures releases the
// reservation, so canceled waiters do not consume quota; a cancellation racing the
// reservation's maturity may still consume it.
//
// Parameters:
//   - ctx (context.Context): The context controlling the wait. Cancellation or timeout
//     aborts the wait and returns the context's cause.
//
// Returns:
//   - err (error): nil when a token was acquired, or the context's cause wrapped with the
//     producing function's name.
func (l *Limiter) Wait(ctx context.Context) (err error) {
	if ctx.Err() != nil {
		err = fmt.Errorf("limiter.Wait: %w", context.Cause(ctx))

		return
	}

	now := time.Now()

	l.mu.Lock()
	matures := l.reserve(now)
	l.mu.Unlock()

	delay := matures.Sub(now)
	if delay <= 0 {
		return
	}

	if deadline, ok := ctx.Deadline(); ok && matures.After(deadline) {
		l.unreserve(matures)

		err = fmt.Errorf("limiter.Wait: %w", context.DeadlineExceeded)

		return
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
	case <-ctx.Done():
		l.unreserve(matures)

		err = fmt.Errorf("limiter.Wait: %w", context.Cause(ctx))
	}

	return
}

// reserve books one token in every bucket and returns the moment the reservation matures.
// Token counts may go negative, representing operations promised to future timestamps;
// later callers queue behind them.
//
// Parameters:
//   - now (time.Time): The current time.
//
// Returns:
//   - matures (time.Time): The moment every bucket's reservation matures; equal to now when
//     all buckets had a token immediately available.
func (l *Limiter) reserve(now time.Time) (matures time.Time) {
	var delay time.Duration

	buckets := l.defaultBuckets(now)

	for _, b := range buckets {
		b.advance(now)

		if d := b.wait(1); d > delay {
			delay = d
		}
	}

	for _, b := range buckets {
		b.tokens--
	}

	matures = now.Add(delay)

	return
}

// unreserve refunds one token to every bucket, releasing a reservation booked by reserve,
// unless the reservation has already matured — refunding a matured reservation would hand
// out a free token.
//
// Parameters:
//   - matures (time.Time): The moment the reservation matures.
func (l *Limiter) unreserve(matures time.Time) {
	now := time.Now()

	if !now.Before(matures) {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	for _, b := range l.buckets {
		b.advance(now)
		b.tokens = min(b.capacity, b.tokens+1)
	}
}
