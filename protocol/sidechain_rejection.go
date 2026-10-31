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
	// rejectedCandidateTTL is how long the bridge remembers a side-chain
	// candidate it rejected on a consensus check. A peer that keeps serving
	// the same bytes is refused before any rollback for this long after the
	// last time they were offered.
	rejectedCandidateTTL = time.Hour
	// rejectedCandidateCap bounds the memory; the oldest entry is evicted
	// when it is full.
	rejectedCandidateCap = 4096
)

// ErrSideChainKnownInvalid is the cause carried by SideChainRejected when a
// batch is refused because it contains a candidate this node already
// rejected, before any canonical momentum is removed.
var ErrSideChainKnownInvalid = errors.New("side-chain momentum was already rejected")

// SideChainRejected is returned by InsertChain when a side-chain candidate
// fails a consensus check: the static or state checks of the verifier, the
// producer election, or the VM while applying its content. The protocol
// layer quarantines the peer that served it. When the verdict is one the
// candidate's own bytes reproduce, the bridge also remembers those bytes
// and refuses any later batch that carries them before rolling anything
// back, whichever peer serves them. Only consensus verdicts are classified
// this way (see validationFailure); store errors, internal failures, failed
// commits and clock-dependent checks are not.
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

// validationFailure tags err when it is a consensus verdict on the
// momentum, one a later offer of the same momentum reproduces: a verifier
// validation error (verifier.IsValidationError), a contract-receive whose
// changes-hash differs from the locally generated one, or a plasma bound.
// Everything else is returned unchanged and is never remembered or held
// against the peer: store errors, verifier.ErrVerifierInternal,
// constants.ErrVmRunPanic, and verifier.ErrMTimestampInTheFuture, which
// depends on the local clock. A verdict missing from the list costs only
// the memory, never a wrong rejection.
func validationFailure(err error) error {
	if err == nil || errors.Is(err, verifier.ErrVerifierInternal) || errors.Is(err, constants.ErrVmRunPanic) || errors.Is(err, verifier.ErrMTimestampInTheFuture) {
		return err
	}
	if verifier.IsValidationError(err) ||
		errors.Is(err, constants.ErrAutoReceiveChangesHashMismatch) ||
		errors.Is(err, constants.ErrBlockPlasmaLimitReached) ||
		errors.Is(err, constants.ErrNotEnoughPlasma) ||
		errors.Is(err, constants.ErrNotEnoughTotalPlasma) {
		return &validationError{err: err}
	}
	return err
}

// contextVerdicts are verdicts that depend on what else the batch or the
// state holds rather than on the candidate's own bytes: the same candidate
// can be valid in a complete batch. They still reject the batch and hold
// the serving peer to account, but the candidate is not remembered.
var contextVerdicts = []error{
	verifier.ErrMPreviousMissing,
	verifier.ErrABPreviousMissing,
	verifier.ErrABMAMissing,
	verifier.ErrABFromBlockMissing,
}

// rememberable reports whether cause is a verdict the candidate's own bytes
// reproduce, so that remembering those bytes is sound.
func rememberable(cause error) bool {
	for _, verdict := range contextVerdicts {
		if errors.Is(cause, verdict) {
			return false
		}
	}
	return true
}

// candidateKey identifies a candidate by every byte the peer served for it:
// the momentum and its account blocks as serialised, which includes the
// fields the hashes do not cover (signatures, public keys, a block's
// changes-hash). A tampered copy and the genuine momentum therefore get
// different keys, so a verdict on one is never held against the other. The
// second result is false when the candidate cannot be serialised.
func candidateKey(detailed *nom.DetailedMomentum) (types.Hash, bool) {
	parts := make([][]byte, 0, 1+len(detailed.AccountBlocks))
	momentum, err := detailed.Momentum.Serialize()
	if err != nil {
		return types.Hash{}, false
	}
	parts = append(parts, momentum)
	for _, block := range detailed.AccountBlocks {
		serialized, err := block.Serialize()
		if err != nil {
			return types.Hash{}, false
		}
		parts = append(parts, serialized)
	}
	return types.NewHash(common.JoinBytes(parts...)), true
}

// rejectedCandidates remembers candidate keys rejected on a consensus
// check, each until an expiry read from common.Clock.
type rejectedCandidates struct {
	mu    sync.Mutex
	ttl   time.Duration
	cap   int
	until map[types.Hash]time.Time
}

func newRejectedCandidates(ttl time.Duration, cap int) *rejectedCandidates {
	return &rejectedCandidates{ttl: ttl, cap: cap, until: make(map[types.Hash]time.Time)}
}

// add remembers key for ttl from now, refreshing an existing entry.
func (r *rejectedCandidates) add(key types.Hash) {
	now := common.Clock.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, until := range r.until {
		if !now.Before(until) {
			delete(r.until, k)
		}
	}
	if _, known := r.until[key]; !known && len(r.until) >= r.cap {
		var oldest types.Hash
		var oldestUntil time.Time
		for k, until := range r.until {
			if oldestUntil.IsZero() || until.Before(oldestUntil) {
				oldest, oldestUntil = k, until
			}
		}
		delete(r.until, oldest)
	}
	r.until[key] = now.Add(r.ttl)
}

// has reports whether key is remembered and not yet expired.
func (r *rejectedCandidates) has(key types.Hash) bool {
	now := common.Clock.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	until, ok := r.until[key]
	if !ok {
		return false
	}
	if !now.Before(until) {
		delete(r.until, key)
		return false
	}
	return true
}

func (r *rejectedCandidates) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.until)
}

// remembered reports whether the bytes served for detailed were rejected
// earlier and are still remembered.
func (c chainBridge) remembered(detailed *nom.DetailedMomentum) bool {
	key, ok := candidateKey(detailed)
	return ok && c.rejected.has(key)
}

// reject returns the typed error for a consensus verdict on detailed and,
// when the verdict is one its bytes reproduce, remembers those bytes.
func (c chainBridge) reject(detailed *nom.DetailedMomentum, cause error) error {
	if rememberable(cause) {
		if key, ok := candidateKey(detailed); ok {
			c.rejected.add(key)
		}
	}
	return &SideChainRejected{Momentum: detailed.Momentum.Identifier(), Cause: cause}
}

// rejectIfValidation returns err unchanged unless it is a validation
// failure, in which case the candidate is rejected (see reject).
func (c chainBridge) rejectIfValidation(detailed *nom.DetailedMomentum, err error) error {
	var validation *validationError
	if !errors.As(err, &validation) {
		return err
	}
	return c.reject(detailed, err)
}
