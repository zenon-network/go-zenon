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
	// when it is full. The bound is on memory, not on what nodes can
	// record: entries are keyed on the bytes served for a candidate, so
	// every distinct candidate a node serves can be a new entry. A batch
	// records at most one (the first candidate it rejects), and the node
	// that served it is then quarantined, which refuses its reconnection
	// while that quarantine entry lasts. The quarantine holds at most
	// peerQuarantineCap ids and evicts the one expiring soonest, so it
	// bounds neither how many ids can add entries nor, once an id is
	// evicted, how soon that id can add another: enough rotating ids can
	// fill the memory within rejectedCandidateTTL.
	// Eviction only forgets a verdict: the candidate's next offer takes
	// the ordinary validation path again, with the rollback and restore
	// that path makes where it reaches them, as every candidate did before
	// the memory existed.
	rejectedCandidateCap = 4096
)

// ErrSideChainKnownInvalid is the cause carried by SideChainRejected when a
// batch is refused because it contains a candidate this node already
// rejected, before any canonical momentum is removed and before any
// content is applied.
var ErrSideChainKnownInvalid = errors.New("side-chain momentum was already rejected")

// SideChainRejected is returned by InsertChain when a candidate fails a
// consensus check: the static or state checks of the verifier, the
// producer election, or the VM while applying its content. Despite the
// name it covers both insert paths, a side-chain that would replace part
// of the local chain and a batch that extends the local frontier, since
// the verdict and the peer's part in it are the same on both. The protocol
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

// BatchRejected is returned by InsertChain when a batch is refused for its
// shape and every candidate in it is the momentum its hash names: the
// candidates do not chain, or the batch does not attach to the local chain,
// attaches more than the rollback limit below the frontier, or is not
// longer than the local chain. It is a verdict on the batch and not on the
// candidate at the returned index or on the peer that delivered it. Nothing
// is remembered and nobody is quarantined for it. It reports its cause's
// text unchanged.
//
// Since every candidate is what its hash names, the shape of the batch is
// the shape of the list of hashes it was requested by. Verdict is the
// downloader's sentinel for what else the refusal depends on, which is what
// decides whom the downloader holds it against (see the sentinels, and
// refuseBatch):
//
//   - downloader.ErrBatchRejected: nothing else. Whether the candidates
//     chain is fixed by the list.
//   - downloader.ErrBatchMisplaced: the local chain. Where the batch
//     attaches and how far below the frontier is fixed by the list only
//     for the chain it was requested against, and the frontier can have
//     moved since.
//   - downloader.ErrBatchInconclusive: how much of the list had arrived
//     when the batch was handed over. A batch that is not longer than the
//     local chain may be the beginning of one that is.
type BatchRejected struct {
	Cause   error
	Verdict error
}

func (e *BatchRejected) Error() string { return e.Cause.Error() }

func (e *BatchRejected) Unwrap() error { return e.Cause }

// Is makes errors.Is hold for Verdict.
func (e *BatchRejected) Is(target error) bool { return target == e.Verdict }

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
// Everything else is returned unchanged, so it is never remembered and
// never quarantines the peer (the caller's ordinary import-failure
// handling, a disconnect, still applies): store errors,
// verifier.ErrVerifierInternal, constants.ErrVmRunPanic, and
// verifier.ErrMTimestampInTheFuture, which depends on the local clock. A
// verdict missing from the list costs only the memory, never a wrong
// rejection.
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
// can be valid in a complete batch. They reject the batch as an ordinary
// import failure: the candidate is not remembered, and the peer that
// served it is not quarantined, since the batch's composition, not the
// candidate or its supplier, is what failed.
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

// reject returns the typed error for a consensus verdict on detailed and
// remembers the bytes served for it. A verdict that depends on the batch
// or the state rather than on those bytes (contextVerdicts) is returned
// as it is: neither remembered nor typed, so the caller's ordinary
// import-failure handling applies and no peer is quarantined for it.
func (c chainBridge) reject(detailed *nom.DetailedMomentum, cause error) error {
	if !rememberable(cause) {
		return cause
	}
	if key, ok := candidateKey(detailed); ok {
		c.rejected.add(key)
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
