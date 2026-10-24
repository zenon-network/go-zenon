package protocol

import (
	"fmt"
	"time"
)

// Request budget constants. GetBlocksMsg, GetBlockHashesMsg and
// GetBlockHashesFromNumberMsg are each bounded per message (MaxBlocksRequest,
// downloader.MaxHashFetch, SoftResponseLimit), but nothing bounded how much
// work one peer could have the receiver do over time, so a peer could keep
// the receiver's store busy indefinitely for a few bytes per lookup. Every
// connection now carries two token buckets, shared by the three request
// codes. A charge the buckets cannot cover right away is served after the
// peer's handler has waited for the refill, so the work one peer causes is
// bounded by the refill rate; a charge that could never be covered (more
// than a bucket holds) is refused and disconnects the peer.
//
// Pacing rather than disconnecting is deliberate. An honest node syncing
// from a single peer on a fast link runs the downloader flat out, one
// request of each kind in flight and the next sent as soon as a reply
// lands, so there is no request rate an honest peer "stays under": the
// rate is whatever the receiver can serve. Pacing slows such a peer to the
// budget without cutting it off. The delay a request sees is its own cost
// at the refill rate plus that of the requests the same peer has queued
// ahead of it on the connection; an honest peer has at most a handful
// outstanding (one hash request, one block request, a few fetcher
// batches), so the total stays far inside the downloader's timeouts
// (hashTTL 5 s, blockHardTTL 9 s). TestHandleRequests_MixedFlatOutTraffic
// pins that bound for the defaults.
//
// Lookup units: every request costs one unit, every store read the
// handler makes costs one unit, every hash a walk visits costs one unit,
// and in GetBlocksMsg a hash the receiver does not have costs
// RequestMissCost units in total. Honest peers ask for hashes they learned
// from this node's own announcements and hash replies, so misses are rare
// for them and the signature of a scan; after a reorganisation an honest
// peer may miss a few and is paced, not dropped. Reply bytes have their own
// bucket, charged by approximate encoded size before the reply is sent.
const (
	// RequestLookupRate is the sustained number of lookup units one peer
	// may spend per second across the three request codes. A full
	// GetBlocksMsg of downloader.MaxBlockFetch hits costs 129 units and a
	// full hash request 513 (514 for the from-number variant, which also
	// pays its height lookup), so the rate sustains about 63 block requests
	// or 16 hash requests per second per peer; a scanner naming
	// MaxBlocksRequest unknown hashes per message is held to four such
	// messages per second once its burst is spent.
	RequestLookupRate = 8192.0

	// RequestLookupBurst is the number of lookup units a peer holds when
	// idle: the most it can spend before pacing applies. It also caps what
	// one charge may ask for; a charge above it is refused. No request
	// code can issue a charge that large, so in practice it bounds pacing
	// only.
	RequestLookupBurst = 32768.0

	// RequestMissCost is what a GetBlocksMsg lookup for a hash the receiver
	// does not have costs in lookup units, including the unit every lookup
	// costs.
	RequestMissCost = 8.0

	// RequestReplyByteRate is the sustained number of reply bytes one peer
	// may be sent per second in answer to the three request codes.
	RequestReplyByteRate = 16 * 1024 * 1024.0

	// RequestReplyByteBurst is the number of reply bytes a peer holds when
	// idle. It covers many SoftResponseLimit-sized replies at once and caps
	// the size of one reply; a reply above it is refused and not sent.
	RequestReplyByteBurst = 64 * 1024 * 1024.0
)

// tokenBucket is a lazily refilled token bucket whose balance may go
// negative: reserve deducts first and reports how long the deficit takes to
// refill. It is not safe for concurrent use; a peer's messages are handled
// by one goroutine.
type tokenBucket struct {
	rate   float64 // units per second
	burst  float64 // capacity, and the most one reserve may ask for
	tokens float64
	last   time.Time
	now    func() time.Time
}

func newTokenBucket(rate, burst float64, now func() time.Time) *tokenBucket {
	if now == nil {
		now = time.Now
	}
	return &tokenBucket{rate: rate, burst: burst, tokens: burst, last: now(), now: now}
}

// refill credits the time elapsed since the last refill, capped at the
// burst. A clock that moved backwards credits nothing and is not recorded,
// so it cannot manufacture a refill later.
func (b *tokenBucket) refill() {
	t := b.now()
	if elapsed := t.Sub(b.last); elapsed > 0 {
		b.tokens += elapsed.Seconds() * b.rate
		if b.tokens > b.burst {
			b.tokens = b.burst
		}
		b.last = t
	}
}

// reserve deducts n units and returns how long the caller must wait before
// the deduction is covered by the refill: zero when the bucket held n, else
// the deficit at the refill rate. It reports false, deducting nothing, when
// n can never be covered: n exceeds the burst, or there is a deficit and no
// refill rate.
func (b *tokenBucket) reserve(n float64) (time.Duration, bool) {
	if n > b.burst {
		return 0, false
	}
	b.refill()
	if n <= b.tokens {
		b.tokens -= n
		return 0, true
	}
	if b.rate <= 0 {
		return 0, false
	}
	deficit := n - b.tokens
	b.tokens -= n
	return time.Duration(deficit / b.rate * float64(time.Second)), true
}

// requestBudget is one peer's budget for the three request codes.
type requestBudget struct {
	id         string
	lookups    *tokenBucket
	replyBytes *tokenBucket
	// wait blocks for d, or returns an error when the peer is going away
	// and the wait should be abandoned.
	wait func(d time.Duration) error
}

func newRequestBudget(id string, lookupRate, lookupBurst, byteRate, byteBurst float64, now func() time.Time, wait func(time.Duration) error) *requestBudget {
	return &requestBudget{
		id:         id,
		lookups:    newTokenBucket(lookupRate, lookupBurst, now),
		replyBytes: newTokenBucket(byteRate, byteBurst, now),
		wait:       wait,
	}
}

// newDefaultRequestBudget returns the default budget for a connection whose
// close signal is closed (nil when the transport exposes none): a wait ends
// early, with an error, when the peer goes away.
func newDefaultRequestBudget(id string, closed <-chan struct{}) *requestBudget {
	return newRequestBudget(id, RequestLookupRate, RequestLookupBurst, RequestReplyByteRate, RequestReplyByteBurst, nil, sleepUnlessClosed(closed))
}

// sleepUnlessClosed returns a wait that sleeps for the given duration unless
// closed fires first.
func sleepUnlessClosed(closed <-chan struct{}) func(time.Duration) error {
	return func(d time.Duration) error {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-timer.C:
			return nil
		case <-closed:
			return fmt.Errorf("request budget: peer closed while paced")
		}
	}
}

func (b *requestBudget) admit(bucket *tokenBucket, what string, n float64) error {
	d, ok := bucket.reserve(n)
	if !ok {
		return errResp(ErrRequestBudgetExceeded, "%s: %.0f requested, at most %.0f per charge", what, n, bucket.burst)
	}
	if d <= 0 {
		return nil
	}
	log.Debug("pacing peer request", "peer-id", b.id, "budget", what, "wait", d)
	return b.wait(d)
}

// admitLookups charges n lookup units, waiting for the refill when the
// bucket is short. It returns the disconnecting error when n can never be
// covered, or the wait's error when the peer goes away meanwhile.
func (b *requestBudget) admitLookups(n float64) error {
	return b.admit(b.lookups, "lookups", n)
}

// chargeMiss charges the remainder of RequestMissCost after the one unit the
// lookup itself cost.
func (b *requestBudget) chargeMiss() error {
	return b.admitLookups(RequestMissCost - 1)
}

// admitReplyBytes charges n reply bytes under the same rules as
// admitLookups.
func (b *requestBudget) admitReplyBytes(n int) error {
	return b.admit(b.replyBytes, "reply bytes", float64(n))
}
