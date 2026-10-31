package protocol

import (
	"fmt"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm/constants"
)

const (
	// rejectedMomentumTTL is how long the bridge remembers a side-chain
	// momentum it rejected on a consensus check. A peer that keeps
	// serving the same branch is refused before any rollback for this
	// long after the last time the branch was offered.
	rejectedMomentumTTL = time.Hour
	// rejectedMomentumCap bounds the memory; the oldest entry is evicted
	// when it is full.
	rejectedMomentumCap = 4096
)

// ErrSideChainKnownInvalid is the cause carried by SideChainRejected when a
// batch is refused because it contains a momentum this node already
// rejected, before any canonical momentum is removed.
var ErrSideChainKnownInvalid = errors.New("side-chain momentum was already rejected")

// SideChainRejected is returned by InsertChain when a side-chain candidate
// fails a consensus check: the static or state checks of the verifier, the
// producer election, or the VM while applying its content. Such a failure
// is deterministic for the state at the fork point, so the bridge remembers
// the momentum and refuses any later batch that carries it before rolling
// anything back, whichever peer serves it, and the protocol layer
// quarantines the peer that served it. Internal failures (store errors,
// verifier.ErrVerifierInternal, constants.ErrVmRunPanic, failed commits)
// are never classified this way.
type SideChainRejected struct {
	Momentum types.HashHeight
	Cause    error
}

func (e *SideChainRejected) Error() string {
	return fmt.Sprintf("side-chain momentum %v rejected: %v", e.Momentum, e.Cause)
}

func (e *SideChainRejected) Unwrap() error { return e.Cause }

// validationError marks an error raised while validating or applying a
// momentum's content, as opposed to one raised while committing it. It
// reports the wrapped error's text unchanged so logs do not change.
type validationError struct{ err error }

func (e *validationError) Error() string { return e.err.Error() }
func (e *validationError) Unwrap() error { return e.err }

// validationFailure tags err as a validation failure unless it reports an
// internal problem, which is not a verdict on the momentum.
func validationFailure(err error) error {
	if err == nil || errors.Is(err, verifier.ErrVerifierInternal) || errors.Is(err, constants.ErrVmRunPanic) {
		return err
	}
	return &validationError{err: err}
}

// rejectedMomentums remembers momentum hashes rejected on a consensus
// check, each until an expiry read from common.Clock.
type rejectedMomentums struct {
	mu    sync.Mutex
	ttl   time.Duration
	cap   int
	until map[types.Hash]time.Time
}

func newRejectedMomentums(ttl time.Duration, cap int) *rejectedMomentums {
	return &rejectedMomentums{ttl: ttl, cap: cap, until: make(map[types.Hash]time.Time)}
}

// add remembers hash for ttl from now, refreshing an existing entry.
func (r *rejectedMomentums) add(hash types.Hash) {
	now := common.Clock.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	for h, until := range r.until {
		if !now.Before(until) {
			delete(r.until, h)
		}
	}
	if _, known := r.until[hash]; !known && len(r.until) >= r.cap {
		var oldest types.Hash
		var oldestUntil time.Time
		for h, until := range r.until {
			if oldestUntil.IsZero() || until.Before(oldestUntil) {
				oldest, oldestUntil = h, until
			}
		}
		delete(r.until, oldest)
	}
	r.until[hash] = now.Add(r.ttl)
}

// has reports whether hash is remembered and not yet expired.
func (r *rejectedMomentums) has(hash types.Hash) bool {
	now := common.Clock.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	until, ok := r.until[hash]
	if !ok {
		return false
	}
	if !now.Before(until) {
		delete(r.until, hash)
		return false
	}
	return true
}

func (r *rejectedMomentums) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.until)
}

// reject records momentum as rejected for cause and returns the typed
// error callers classify on. cause must already be a consensus verdict.
func (c chainBridge) reject(momentum *nom.Momentum, cause error) error {
	c.rejected.add(momentum.Hash)
	return &SideChainRejected{Momentum: momentum.Identifier(), Cause: cause}
}

// rejectIfValidation returns err unchanged unless it is a validation
// failure, in which case the momentum is recorded and the error wrapped.
func (c chainBridge) rejectIfValidation(momentum *nom.Momentum, err error) error {
	var validation *validationError
	if !errors.As(err, &validation) {
		return err
	}
	return c.reject(momentum, err)
}
