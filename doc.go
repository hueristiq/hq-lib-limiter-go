// Package limiter throttles outgoing work to one or more rate windows, enforcing policies
// such as "at most 5 operations per second and 100 per minute" with a token bucket per
// window.
//
// It suits clients that must stay within a remote service's quotas — API consumers,
// crawlers, scanners, batch jobs — where pacing outgoing calls is politer and safer than
// being cut off for exceeding them.
//
// # Entry points
//
// [New] builds a [Limiter] from functional options. The limiter then offers three ways to
// acquire a token: [Limiter.Allow] reports immediately whether an operation may proceed,
// [Limiter.Take] additionally reports how long a caller would have to wait, and
// [Limiter.Wait] blocks until a token is available or the supplied context is done.
//
// # Configuration
//
// Behavior is set through functional options passed to [New]:
//
//   - [WithRate] adds a rate window of limit operations per duration. It is repeatable:
//     every supplied window is enforced simultaneously, and an operation proceeds only when
//     all windows permit it.
//   - [WithRequestsPerMinute] is sugar for a per-minute window.
//   - [WithBurst] caps how many operations may fire at once before pacing applies.
//
// Unset options fall back to defaults: a single window of [DefaultLimit] operations per
// [DefaultPer] and a burst of [DefaultBurst]. Invalid values are normalized the same way —
// a non-positive limit, window duration, or burst falls back to its default — so the
// limiter always runs with a valid policy.
//
// # Concurrency
//
// A [Limiter] is safe for concurrent use: all state is guarded by a mutex that is held for
// bookkeeping only, never while sleeping. Waiters queue by reserving future tokens, so
// concurrent [Limiter.Wait] calls are paced in arrival order without holding the lock.
//
// # Errors
//
// [Limiter.Wait] is the only operation that fails: it returns the context's cause —
// cancellation, deadline, or a custom cause — wrapped with the producing function's name.
// A wait canceled before its reservation matures releases the reservation, so canceled
// waiters do not consume quota.
//
// The implementation relies on the Go standard library only.
package limiter
