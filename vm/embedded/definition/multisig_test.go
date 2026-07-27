package definition

import (
	"bytes"
	"crypto/ed25519"
	"testing"

	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
)

func genSigner(t *testing.T) ed25519.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

func genSigners(t *testing.T, n int) []ed25519.PublicKey {
	t.Helper()
	out := make([]ed25519.PublicKey, n)
	for i := 0; i < n; i++ {
		out[i] = genSigner(t)
	}
	return out
}

func policiesEqual(a, b MultisigPolicy) bool {
	if a.Threshold != b.Threshold || a.Locked != b.Locked {
		return false
	}
	if len(a.Signers) != len(b.Signers) {
		return false
	}
	for i := range a.Signers {
		if !bytes.Equal(a.Signers[i], b.Signers[i]) {
			return false
		}
	}
	return true
}

func recordsEqual(a, b *MultisigRecord) bool {
	if (a.Pending == nil) != (b.Pending == nil) {
		return false
	}
	if a.PendingHeight != b.PendingHeight {
		return false
	}
	if a.Pending != nil && !policiesEqual(*a.Pending, *b.Pending) {
		return false
	}
	return policiesEqual(a.Active, b.Active)
}

// --- round-trip tests ---

func TestMultisigRecord_RoundTrip_NoPending(t *testing.T) {
	rec := &MultisigRecord{
		Active: MultisigPolicy{
			Threshold: 2,
			Signers:   genSigners(t, 3),
			Locked:    false,
		},
	}

	data := rec.Data()
	decoded := ParseMultisigRecord(data)

	if !recordsEqual(rec, decoded) {
		t.Fatalf("expected round-tripped record to equal original.\noriginal: %+v\ndecoded:  %+v", rec, decoded)
	}

	data2 := decoded.Data()
	if !bytes.Equal(data, data2) {
		t.Fatalf("expected byte-identical re-encoding")
	}
}

func TestMultisigRecord_RoundTrip_WithPending(t *testing.T) {
	pending := MultisigPolicy{
		Threshold: 3,
		Signers:   genSigners(t, 4),
		Locked:    true,
	}
	rec := &MultisigRecord{
		Active: MultisigPolicy{
			Threshold: 2,
			Signers:   genSigners(t, 2),
			Locked:    false,
		},
		Pending:       &pending,
		PendingHeight: 12345,
	}

	data := rec.Data()
	decoded := ParseMultisigRecord(data)

	if !recordsEqual(rec, decoded) {
		t.Fatalf("expected round-tripped record to equal original.\noriginal: %+v\ndecoded:  %+v", rec, decoded)
	}
	if decoded.Pending == nil {
		t.Fatalf("expected decoded Pending to be non-nil")
	}

	data2 := decoded.Data()
	if !bytes.Equal(data, data2) {
		t.Fatalf("expected byte-identical re-encoding")
	}
}

func TestGetSaveMultisigRecord(t *testing.T) {
	storage := db.DisableNotFound(db.NewMemDB())
	addr := types.MultisigCreationToAddress(genSigner(t), 0)

	if rec, err := GetMultisigRecord(storage, addr); err != nil {
		t.Fatal(err)
	} else if rec != nil {
		t.Fatalf("expected nil record before any Save, got %+v", rec)
	}

	rec := &MultisigRecord{
		Active: MultisigPolicy{
			Threshold: 2,
			Signers:   genSigners(t, 2),
		},
	}
	if err := SaveMultisigRecord(storage, addr, rec); err != nil {
		t.Fatal(err)
	}

	got, err := GetMultisigRecord(storage, addr)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatalf("expected non-nil record after Save")
	}
	if !recordsEqual(rec, got) {
		t.Fatalf("expected stored record to equal saved record.\nsaved: %+v\ngot:   %+v", rec, got)
	}

	// a different address must not see this record
	otherAddr := types.MultisigCreationToAddress(genSigner(t), 0)
	if other, err := GetMultisigRecord(storage, otherAddr); err != nil {
		t.Fatal(err)
	} else if other != nil {
		t.Fatalf("expected nil record for a different address, got %+v", other)
	}
}

// --- canonicalisation tests ---

func TestCanonicalizeSigners_SortsAndIsDeterministic(t *testing.T) {
	signers := genSigners(t, 5)
	canonical1, err := CanonicalizeSigners(signers)
	if err != nil {
		t.Fatal(err)
	}

	// reverse the input order; canonicalisation must produce the same sorted output
	reversed := make([]ed25519.PublicKey, len(signers))
	for i, s := range signers {
		reversed[len(signers)-1-i] = s
	}
	canonical2, err := CanonicalizeSigners(reversed)
	if err != nil {
		t.Fatal(err)
	}

	if len(canonical1) != len(canonical2) {
		t.Fatalf("expected equal length canonical sets")
	}
	for i := range canonical1 {
		if !bytes.Equal(canonical1[i], canonical2[i]) {
			t.Fatalf("expected canonicalisation to be order-independent at index %d", i)
		}
	}

	for i := 1; i < len(canonical1); i++ {
		if bytes.Compare(canonical1[i-1], canonical1[i]) >= 0 {
			t.Fatalf("expected ascending byte-lexicographic order at index %d", i)
		}
	}
}

func TestCanonicalizeSigners_RejectsDuplicate(t *testing.T) {
	s := genSigner(t)
	_, err := CanonicalizeSigners([]ed25519.PublicKey{s, genSigner(t), s})
	if err != ErrMultisigDuplicateSigner {
		t.Fatalf("expected ErrMultisigDuplicateSigner, got %v", err)
	}
}

func TestCanonicalizeSigners_RejectsWrongLength(t *testing.T) {
	_, err := CanonicalizeSigners([]ed25519.PublicKey{genSigner(t)[:31]})
	if err != ErrMultisigInvalidSigner {
		t.Fatalf("expected ErrMultisigInvalidSigner, got %v", err)
	}
}

// --- validPolicy tests ---

func TestValidPolicy_Accepts(t *testing.T) {
	p := MultisigPolicy{Threshold: 2, Signers: genSigners(t, 3)}
	if err := ValidPolicy(&p); err != nil {
		t.Fatalf("expected valid policy, got %v", err)
	}
}

func TestValidPolicy_RejectsThresholdTooLow(t *testing.T) {
	p := MultisigPolicy{Threshold: 1, Signers: genSigners(t, 3)}
	if err := ValidPolicy(&p); err != ErrMultisigInvalidThreshold {
		t.Fatalf("expected ErrMultisigInvalidThreshold, got %v", err)
	}
}

func TestValidPolicy_RejectsThresholdTooHigh(t *testing.T) {
	p := MultisigPolicy{Threshold: 4, Signers: genSigners(t, 3)}
	if err := ValidPolicy(&p); err != ErrMultisigInvalidThreshold {
		t.Fatalf("expected ErrMultisigInvalidThreshold, got %v", err)
	}
}

func TestValidPolicy_RejectsTooFewSigners(t *testing.T) {
	p := MultisigPolicy{Threshold: 2, Signers: genSigners(t, 1)}
	if err := ValidPolicy(&p); err != ErrMultisigInvalidSignerSize {
		t.Fatalf("expected ErrMultisigInvalidSignerSize, got %v", err)
	}
}

func TestValidPolicy_RejectsTooManySigners(t *testing.T) {
	p := MultisigPolicy{Threshold: 2, Signers: genSigners(t, constants.MaxSigners+1)}
	if err := ValidPolicy(&p); err != ErrMultisigInvalidSignerSize {
		t.Fatalf("expected ErrMultisigInvalidSignerSize, got %v", err)
	}
}

func TestValidPolicy_RejectsDuplicateSigners(t *testing.T) {
	s := genSigner(t)
	p := MultisigPolicy{Threshold: 2, Signers: []ed25519.PublicKey{s, genSigner(t), s}}
	if err := ValidPolicy(&p); err != ErrMultisigDuplicateSigner {
		t.Fatalf("expected ErrMultisigDuplicateSigner, got %v", err)
	}
}

func TestValidPolicy_RejectsMalformedSigner(t *testing.T) {
	p := MultisigPolicy{Threshold: 2, Signers: []ed25519.PublicKey{genSigner(t), genSigner(t)[:10]}}
	if err := ValidPolicy(&p); err != ErrMultisigInvalidSigner {
		t.Fatalf("expected ErrMultisigInvalidSigner, got %v", err)
	}
}

// --- Promote / ActivePolicyAtHeight table tests ---

func TestPromote_NilRecord(t *testing.T) {
	eff, matured := Promote(nil, 1000)
	if matured {
		t.Fatalf("expected matured == false for nil record")
	}
	if !policiesEqual(eff.Active, MultisigPolicy{}) || eff.Pending != nil {
		t.Fatalf("expected empty record for nil input, got %+v", eff)
	}
}

func TestPromote_NoPending(t *testing.T) {
	active := MultisigPolicy{Threshold: 2, Signers: genSigners(t, 2)}
	rec := &MultisigRecord{Active: active}

	eff, matured := Promote(rec, 999999)
	if matured {
		t.Fatalf("expected matured == false when there is no pending change")
	}
	if !policiesEqual(eff.Active, active) || eff.Pending != nil {
		t.Fatalf("expected unchanged record, got %+v", eff)
	}
}

func TestPromote_BelowMaturity(t *testing.T) {
	oldPolicy := MultisigPolicy{Threshold: 2, Signers: genSigners(t, 2)}
	newPolicy := MultisigPolicy{Threshold: 3, Signers: genSigners(t, 4)}
	rec := &MultisigRecord{
		Active:        oldPolicy,
		Pending:       &newPolicy,
		PendingHeight: 100,
	}

	H := 100 + constants.MultisigPolicyMaturityDelay - 1
	eff, matured := Promote(rec, H)
	if matured {
		t.Fatalf("expected matured == false below maturity")
	}
	if !policiesEqual(eff.Active, oldPolicy) {
		t.Fatalf("expected Active to remain the old policy below maturity")
	}
	if eff.Pending == nil || !policiesEqual(*eff.Pending, newPolicy) {
		t.Fatalf("expected Pending to remain set below maturity")
	}
}

func TestPromote_ExactlyAtMaturity(t *testing.T) {
	oldPolicy := MultisigPolicy{Threshold: 2, Signers: genSigners(t, 2)}
	newPolicy := MultisigPolicy{Threshold: 3, Signers: genSigners(t, 4)}
	rec := &MultisigRecord{
		Active:        oldPolicy,
		Pending:       &newPolicy,
		PendingHeight: 100,
	}

	H := 100 + constants.MultisigPolicyMaturityDelay
	eff, matured := Promote(rec, H)
	if !matured {
		t.Fatalf("expected matured == true at exactly PendingHeight+delay")
	}
	if !policiesEqual(eff.Active, newPolicy) {
		t.Fatalf("expected Active to be promoted to the new policy")
	}
	if eff.Pending != nil {
		t.Fatalf("expected Pending to be cleared after maturity")
	}
}

func TestPromote_AboveMaturity(t *testing.T) {
	oldPolicy := MultisigPolicy{Threshold: 2, Signers: genSigners(t, 2)}
	newPolicy := MultisigPolicy{Threshold: 3, Signers: genSigners(t, 4)}
	rec := &MultisigRecord{
		Active:        oldPolicy,
		Pending:       &newPolicy,
		PendingHeight: 100,
	}

	H := 100 + constants.MultisigPolicyMaturityDelay + 1000
	eff, matured := Promote(rec, H)
	if !matured {
		t.Fatalf("expected matured == true above maturity")
	}
	if !policiesEqual(eff.Active, newPolicy) {
		t.Fatalf("expected Active to be promoted to the new policy")
	}
	if eff.Pending != nil {
		t.Fatalf("expected Pending to be cleared after maturity")
	}
}

func TestActivePolicyAtHeight_NilRecord(t *testing.T) {
	if p := ActivePolicyAtHeight(nil, 1000); p != nil {
		t.Fatalf("expected nil policy for nil record, got %+v", p)
	}
}

func TestActivePolicyAtHeight_TracksPromote(t *testing.T) {
	oldPolicy := MultisigPolicy{Threshold: 2, Signers: genSigners(t, 2)}
	newPolicy := MultisigPolicy{Threshold: 3, Signers: genSigners(t, 4)}
	rec := &MultisigRecord{
		Active:        oldPolicy,
		Pending:       &newPolicy,
		PendingHeight: 100,
	}

	before := ActivePolicyAtHeight(rec, 100+constants.MultisigPolicyMaturityDelay-1)
	if before == nil || !policiesEqual(*before, oldPolicy) {
		t.Fatalf("expected old policy before maturity, got %+v", before)
	}

	after := ActivePolicyAtHeight(rec, 100+constants.MultisigPolicyMaturityDelay)
	if after == nil || !policiesEqual(*after, newPolicy) {
		t.Fatalf("expected new policy at maturity, got %+v", after)
	}
}

// --- VerifyMultisigAuth table tests ---

func TestVerifyMultisigAuth(t *testing.T) {
	addr := types.MultisigCreationToAddress(genSigner(t), 0)
	hash := []byte("some block hash..............32")

	oldPub, oldPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	newPub, newPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	oldPolicy := MultisigPolicy{Threshold: 1, Signers: []ed25519.PublicKey{oldPub}}
	newPolicy := MultisigPolicy{Threshold: 1, Signers: []ed25519.PublicKey{newPub}}
	oldSig := ed25519.Sign(oldPriv, hash)
	newSig := ed25519.Sign(newPriv, hash)

	t.Run("NilRecord", func(t *testing.T) {
		storage := db.DisableNotFound(db.NewMemDB())
		if VerifyMultisigAuth(storage, addr, 1000, hash, [][]byte{oldSig}) {
			t.Fatalf("expected false for an address with no registry record")
		}
	})

	t.Run("ValidSigsUnderActivePolicy", func(t *testing.T) {
		storage := db.DisableNotFound(db.NewMemDB())
		if err := SaveMultisigRecord(storage, addr, &MultisigRecord{Active: oldPolicy}); err != nil {
			t.Fatal(err)
		}
		if !VerifyMultisigAuth(storage, addr, 1000, hash, [][]byte{oldSig}) {
			t.Fatalf("expected true for signatures satisfying the active policy")
		}
	})

	t.Run("BeforeAndAfterMaturity", func(t *testing.T) {
		storage := db.DisableNotFound(db.NewMemDB())
		const pendingHeight = uint64(100)
		if err := SaveMultisigRecord(storage, addr, &MultisigRecord{
			Active:        oldPolicy,
			Pending:       &newPolicy,
			PendingHeight: pendingHeight,
		}); err != nil {
			t.Fatal(err)
		}

		beforeH := pendingHeight + constants.MultisigPolicyMaturityDelay - 1
		if !VerifyMultisigAuth(storage, addr, beforeH, hash, [][]byte{oldSig}) {
			t.Fatalf("expected old-set signatures to authorise before maturity")
		}
		if VerifyMultisigAuth(storage, addr, beforeH, hash, [][]byte{newSig}) {
			t.Fatalf("expected new-set signatures to fail before maturity")
		}

		atH := pendingHeight + constants.MultisigPolicyMaturityDelay
		if !VerifyMultisigAuth(storage, addr, atH, hash, [][]byte{newSig}) {
			t.Fatalf("expected new-set signatures to authorise at maturity")
		}
		if VerifyMultisigAuth(storage, addr, atH, hash, [][]byte{oldSig}) {
			t.Fatalf("expected old-set signatures to fail at maturity")
		}
	})

	t.Run("WrongCount", func(t *testing.T) {
		storage := db.DisableNotFound(db.NewMemDB())
		if err := SaveMultisigRecord(storage, addr, &MultisigRecord{Active: oldPolicy}); err != nil {
			t.Fatal(err)
		}
		if VerifyMultisigAuth(storage, addr, 1000, hash, [][]byte{oldSig, oldSig}) {
			t.Fatalf("expected false for a signature count mismatching threshold")
		}
	})

	t.Run("NilSignatures", func(t *testing.T) {
		storage := db.DisableNotFound(db.NewMemDB())
		if err := SaveMultisigRecord(storage, addr, &MultisigRecord{Active: oldPolicy}); err != nil {
			t.Fatal(err)
		}
		if VerifyMultisigAuth(storage, addr, 1000, hash, nil) {
			t.Fatalf("expected false for nil signatures")
		}
	})

	t.Run("ThresholdAndSignerSetChangeTogether", func(t *testing.T) {
		// 2-of-3 rotates to 3-of-4: a block carrying the old policy's 2-signature count must fail
		// against the new policy once matured, even though 2 signatures were sufficient before.
		storage := db.DisableNotFound(db.NewMemDB())
		oldPub1, oldPriv1, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		twoOfTwo := MultisigPolicy{Threshold: 2, Signers: []ed25519.PublicKey{oldPub, oldPub1}}
		threeOfFour := MultisigPolicy{Threshold: 3, Signers: []ed25519.PublicKey{newPub, oldPub, oldPub1, genSigner(t)}}
		const pendingHeight = uint64(200)
		if err := SaveMultisigRecord(storage, addr, &MultisigRecord{
			Active:        twoOfTwo,
			Pending:       &threeOfFour,
			PendingHeight: pendingHeight,
		}); err != nil {
			t.Fatal(err)
		}
		oldSigs := [][]byte{oldSig, ed25519.Sign(oldPriv1, hash)}

		beforeH := pendingHeight + constants.MultisigPolicyMaturityDelay - 1
		if !VerifyMultisigAuth(storage, addr, beforeH, hash, oldSigs) {
			t.Fatalf("expected the old policy's 2-signature count to authorise before maturity")
		}

		atH := pendingHeight + constants.MultisigPolicyMaturityDelay
		if VerifyMultisigAuth(storage, addr, atH, hash, oldSigs) {
			t.Fatalf("expected the old policy's 2-signature count to fail once the 3-of-4 policy matures")
		}
	})
}

// --- unpackSigners corruption guard ---

func TestUnpackSigners_CorruptBlob_Panics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected unpackSigners to panic on a blob whose length is not a multiple of the public key size")
		}
		if err, ok := r.(error); !ok || err != ErrMultisigCorruptSigners {
			t.Fatalf("expected panic value ErrMultisigCorruptSigners, got %v", r)
		}
	}()
	unpackSigners(make([]byte, ed25519.PublicKeySize+1))
}

func TestUnpackSigners_EmptyBlob_ReturnsNil(t *testing.T) {
	if signers := unpackSigners(nil); signers != nil {
		t.Fatalf("expected nil signers for an empty blob, got %v", signers)
	}
}

func TestUnpackSigners_WellFormedBlob_Unchanged(t *testing.T) {
	signers := genSigners(t, 3)
	packed := make([]byte, 0, len(signers)*ed25519.PublicKeySize)
	for _, s := range signers {
		packed = append(packed, s...)
	}
	unpacked := unpackSigners(packed)
	if len(unpacked) != len(signers) {
		t.Fatalf("expected %d signers, got %d", len(signers), len(unpacked))
	}
	for i := range signers {
		if !bytes.Equal(signers[i], unpacked[i]) {
			t.Fatalf("expected signer %d to round-trip unchanged", i)
		}
	}
}

// --- VerifyThresholdSignatures malformed-key guard ---

// TestVerifyThresholdSignatures_MalformedSignerKeyLength: a policy signer whose stored key is
// not ed25519.PublicKeySize bytes must be skipped rather than crash ed25519.Verify (which panics
// on a mismatched key length), while signatures from the policy's well-formed signers still
// authorise as normal.
func TestVerifyThresholdSignatures_MalformedSignerKeyLength(t *testing.T) {
	hash := []byte("some block hash..............32")
	goodPub, goodPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	malformed := ed25519.PublicKey(make([]byte, 10))

	policy := &MultisigPolicy{
		Threshold: 1,
		Signers:   []ed25519.PublicKey{malformed, goodPub},
	}
	sig := ed25519.Sign(goodPriv, hash)

	if !VerifyThresholdSignatures(policy, hash, [][]byte{sig}) {
		t.Fatalf("expected a valid signature from a well-formed signer to authorise despite a malformed signer sharing the policy")
	}

	// no signature can match the malformed-key-only signer; must reject cleanly, not panic.
	onlyMalformed := &MultisigPolicy{Threshold: 1, Signers: []ed25519.PublicKey{malformed}}
	if VerifyThresholdSignatures(onlyMalformed, hash, [][]byte{sig}) {
		t.Fatalf("expected rejection when the only signer has a malformed key length")
	}
}
