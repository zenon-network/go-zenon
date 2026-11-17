package definition

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"sort"
	"strings"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/abi"
	"github.com/zenon-network/go-zenon/vm/constants"
)

// MultisigPolicy is the mutable authority of a multisig account: the threshold-signers set plus
// the locked flag. Signers must be in canonical form (ascending byte-lexicographic, deduplicated,
// each exactly ed25519.PublicKeySize bytes) before they are accepted into a record — see
// CanonicalizeSigners / ValidPolicy.
type MultisigPolicy struct {
	Threshold uint8
	Signers   []ed25519.PublicKey
	Locked    bool
}

// MultisigRecord is the registry's per-account record: the active policy plus at most one
// pending (staged, not-yet-matured) policy change.
type MultisigRecord struct {
	Active        MultisigPolicy
	Pending       *MultisigPolicy
	PendingHeight uint64
}

const jsonMultisig = `
[
	{"type":"function","name":"CreateMultisig","inputs":[
		{"name":"nonce","type":"uint64"},
		{"name":"threshold","type":"uint8"},
		{"name":"signers","type":"bytes[]"}
	]},
	{"type":"function","name":"ChangePolicy","inputs":[
		{"name":"threshold","type":"uint8"},
		{"name":"signers","type":"bytes[]"},
		{"name":"lock","type":"bool"}
	]},

	{"type":"variable", "name":"multisigRecord", "inputs":[
		{"name":"activeThreshold", "type":"uint8"},
		{"name":"activeSigners", "type":"bytes"},
		{"name":"activeLocked", "type":"bool"},
		{"name":"hasPending", "type":"bool"},
		{"name":"pendingThreshold", "type":"uint8"},
		{"name":"pendingSigners", "type":"bytes"},
		{"name":"pendingLocked", "type":"bool"},
		{"name":"pendingHeight", "type":"uint64"}
	]}
]`

const (
	// CreateMultisigMethodName creates a new mutable multisig account.
	CreateMultisigMethodName = "CreateMultisig"
	// ChangePolicyMethodName stages a policy change on an existing multisig account.
	ChangePolicyMethodName = "ChangePolicy"

	multisigRecordVariableName = "multisigRecord"
)

// CreateMultisigParam is the unpacked CreateMultisig call data.
type CreateMultisigParam struct {
	Nonce     uint64
	Threshold uint8
	Signers   [][]byte
}

// ChangePolicyParam is the unpacked ChangePolicy call data.
type ChangePolicyParam struct {
	Threshold uint8
	Signers   [][]byte
	Lock      bool
}

// ABIMultisig is the single canonical ABI codec for MultisigPolicy/MultisigRecord. Both the
// registry contract (vm/embedded/implementation) and the account verifier import and call the
// helpers in this file directly; neither re-implements parsing.
var ABIMultisig = abi.JSONToABIContract(strings.NewReader(jsonMultisig))

const (
	_ byte = iota
	multisigPolicyKeyPrefix
)

var (
	ErrMultisigInvalidThreshold  = errors.New("multisig: threshold out of bounds")
	ErrMultisigInvalidSignerSize = errors.New("multisig: invalid signer count")
	ErrMultisigInvalidSigner     = errors.New("multisig: signer is not a valid ed25519 public key")
	ErrMultisigDuplicateSigner   = errors.New("multisig: duplicate signer")
	ErrMultisigCorruptSigners    = errors.New("multisig: stored signer blob is not a multiple of the public key size")
)

// multisigRecordEncoding is the flat, ABI-mappable shape of MultisigRecord. It exists only to
// drive ABIMultisig.PackVariablePanic/UnpackVariablePanic; callers use MultisigRecord.
type multisigRecordEncoding struct {
	ActiveThreshold  uint8
	ActiveSigners    []byte
	ActiveLocked     bool
	HasPending       bool
	PendingThreshold uint8
	PendingSigners   []byte
	PendingLocked    bool
	PendingHeight    uint64
}

// CanonicalizeSigners sorts signers ascending byte-lexicographically and rejects malformed or
// duplicate entries. This is the single canonicalisation routine used both when validating a
// proposed policy and when decoding one from storage.
func CanonicalizeSigners(signers []ed25519.PublicKey) ([]ed25519.PublicKey, error) {
	out := make([]ed25519.PublicKey, len(signers))
	for i, s := range signers {
		if len(s) != ed25519.PublicKeySize {
			return nil, ErrMultisigInvalidSigner
		}
		cp := make(ed25519.PublicKey, ed25519.PublicKeySize)
		copy(cp, s)
		out[i] = cp
	}
	sort.Slice(out, func(i, j int) bool {
		return bytes.Compare(out[i], out[j]) < 0
	})
	for i := 1; i < len(out); i++ {
		if bytes.Equal(out[i-1], out[i]) {
			return nil, ErrMultisigDuplicateSigner
		}
	}
	return out, nil
}

// ValidPolicy bounds-checks and canonicalises p in place. It enforces MinSigners <= len(Signers)
// <= MaxSigners, 2 <= Threshold <= len(Signers), and canonical-form signers. Exported so the
// registry contract (vm/embedded/implementation) validates proposed policies through the same
// single canonical codec the verifier reads.
func ValidPolicy(p *MultisigPolicy) error {
	if len(p.Signers) < constants.MinSigners || len(p.Signers) > constants.MaxSigners {
		return ErrMultisigInvalidSignerSize
	}
	canonical, err := CanonicalizeSigners(p.Signers)
	if err != nil {
		return err
	}
	p.Signers = canonical
	if p.Threshold < 2 || int(p.Threshold) > len(p.Signers) {
		return ErrMultisigInvalidThreshold
	}
	return nil
}

func packSigners(signers []ed25519.PublicKey) []byte {
	out := make([]byte, 0, len(signers)*ed25519.PublicKeySize)
	for _, s := range signers {
		out = append(out, s...)
	}
	return out
}

func unpackSigners(data []byte) []ed25519.PublicKey {
	if len(data) == 0 {
		return nil
	}
	if len(data)%ed25519.PublicKeySize != 0 {
		common.DealWithErr(ErrMultisigCorruptSigners)
	}
	count := len(data) / ed25519.PublicKeySize
	out := make([]ed25519.PublicKey, count)
	for i := 0; i < count; i++ {
		pk := make(ed25519.PublicKey, ed25519.PublicKeySize)
		copy(pk, data[i*ed25519.PublicKeySize:(i+1)*ed25519.PublicKeySize])
		out[i] = pk
	}
	return out
}

// Data ABI-encodes rec using the single canonical codec.
func (rec *MultisigRecord) Data() []byte {
	enc := multisigRecordEncoding{
		ActiveThreshold: rec.Active.Threshold,
		ActiveSigners:   packSigners(rec.Active.Signers),
		ActiveLocked:    rec.Active.Locked,
	}
	if rec.Pending != nil {
		enc.HasPending = true
		enc.PendingThreshold = rec.Pending.Threshold
		enc.PendingSigners = packSigners(rec.Pending.Signers)
		enc.PendingLocked = rec.Pending.Locked
		enc.PendingHeight = rec.PendingHeight
	}
	return ABIMultisig.PackVariablePanic(
		multisigRecordVariableName,
		enc.ActiveThreshold,
		enc.ActiveSigners,
		enc.ActiveLocked,
		enc.HasPending,
		enc.PendingThreshold,
		enc.PendingSigners,
		enc.PendingLocked,
		enc.PendingHeight)
}

// multisigKey returns the registry storage key for the multisig account at multisigAddr.
func multisigKey(multisigAddr types.Address) []byte {
	return common.JoinBytes([]byte{multisigPolicyKeyPrefix}, multisigAddr.Bytes())
}

// SaveMultisigRecord ABI-encodes and persists rec under multisigAddr's key in the registry's
// storage.
func SaveMultisigRecord(context db.DB, multisigAddr types.Address, rec *MultisigRecord) error {
	return context.Put(multisigKey(multisigAddr), rec.Data())
}

// ParseMultisigRecord decodes data using the single canonical codec.
func ParseMultisigRecord(data []byte) *MultisigRecord {
	enc := new(multisigRecordEncoding)
	ABIMultisig.UnpackVariablePanic(enc, multisigRecordVariableName, data)

	rec := &MultisigRecord{
		Active: MultisigPolicy{
			Threshold: enc.ActiveThreshold,
			Signers:   unpackSigners(enc.ActiveSigners),
			Locked:    enc.ActiveLocked,
		},
	}
	if enc.HasPending {
		rec.Pending = &MultisigPolicy{
			Threshold: enc.PendingThreshold,
			Signers:   unpackSigners(enc.PendingSigners),
			Locked:    enc.PendingLocked,
		}
		rec.PendingHeight = enc.PendingHeight
	}
	return rec
}

// GetMultisigRecord reads and decodes the record for multisigAddr from storage, or nil if none
// exists yet.
func GetMultisigRecord(context db.DB, multisigAddr types.Address) (*MultisigRecord, error) {
	data, err := context.Get(multisigKey(multisigAddr))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	return ParseMultisigRecord(data), nil
}

// Promote returns the effective record at reading height H, materialising a matured pending
// change. matured == true iff a pending change existed and has matured (Pending was promoted to
// Active and cleared). Pure: identical output on every node for the same (rec, H). This is the
// SINGLE source of truth for "which policy is active at height H" — called unchanged by both
// ChangePolicy.ReceiveBlock (write path) and the verifier's signature() (read path).
func Promote(rec *MultisigRecord, H uint64) (effective MultisigRecord, matured bool) {
	if rec == nil {
		return MultisigRecord{}, false // caller treats as "no policy"
	}
	if rec.Pending != nil && rec.PendingHeight+constants.MultisigPolicyMaturityDelay <= H {
		return MultisigRecord{Active: *rec.Pending, Pending: nil, PendingHeight: 0}, true
	}
	return *rec, false
}

// ActivePolicyAtHeight is the read-only view the verifier uses: the effective Active policy at
// height H. Returns nil iff rec == nil (no policy yet).
func ActivePolicyAtHeight(rec *MultisigRecord, H uint64) *MultisigPolicy {
	if rec == nil {
		return nil
	}
	eff, _ := Promote(rec, H)
	return &eff.Active
}

// VerifyMultisigAuth reports whether signatures authorise hash for multisigAddr under the policy
// active at height H, read from storage. Deterministic in (storage, addr, H, hash, signatures):
// identical on every node. Returns false when no record exists yet. This is the inclusion-time
// authorisation primitive shared by momentum content verification and the producer-side content
// filter.
func VerifyMultisigAuth(storage db.DB, addr types.Address, H uint64, hash []byte, signatures [][]byte) bool {
	rec, err := GetMultisigRecord(storage, addr)
	common.DealWithErr(err)
	if rec == nil {
		return false
	}
	eff, _ := Promote(rec, H)
	return VerifyThresholdSignatures(&eff.Active, hash, signatures)
}

// IsMultisigMAStale reports whether a multisig block whose MomentumAcknowledged height is
// maHeight lags referenceHeight by more than MultisigMaxMaLag. Additive arithmetic only, to avoid
// uint underflow at low reference heights.
func IsMultisigMAStale(maHeight, referenceHeight uint64) bool {
	return maHeight+constants.MultisigMaxMaLag < referenceHeight
}

// VerifyThresholdSignatures reports whether signatures satisfy policy: exactly Threshold
// signatures, each a well-formed ed25519 signature over hash by a distinct active signer
// (trial-match; no signer index is carried on the wire). This is the single primitive used by
// both the account verifier (read path) and ChangePolicy.ReceiveBlock (write-path re-auth).
func VerifyThresholdSignatures(policy *MultisigPolicy, hash []byte, signatures [][]byte) bool {
	if policy == nil || len(signatures) != int(policy.Threshold) {
		return false
	}
	used := make([]bool, len(policy.Signers))
	for _, sig := range signatures {
		if len(sig) != ed25519.SignatureSize {
			return false
		}
		matched := false
		for j, pk := range policy.Signers {
			if used[j] {
				continue
			}
			if len(pk) != ed25519.PublicKeySize {
				continue
			}
			if ed25519.Verify(pk, hash, sig) {
				used[j] = true
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
