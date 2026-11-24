//go:build e2e

package e2e

import (
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"

	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// TestPtlc covers the PTLC contract (TEST-PLAN.md section 6.4). It needs a
// chain with the PTLC spork enforced: the all-on or staged genesis variant.
func TestPtlc(t *testing.T) {
	group(t)
	needs(t, "ptlc")
	a := newAccount(t, 100, 30, 0, 2500) // creator, the time-locked side
	b := newAccount(t, 101, 0, 0, 1500)  // the paid side
	c := newAccount(t, 102, 0, 0, 1000)  // a third party
	ptlc := types.PtlcContract
	none := types.ZeroAddress

	// Deterministic test keys: they lock nothing outside a devnet.
	seed := func(label string) []byte {
		h := sha256.Sum256([]byte("e2e ptlc " + label + " " + runTag))
		return h[:]
	}
	edKey := ed25519.NewKeyFromSeed(seed("ed25519"))
	edLock := []byte(edKey.Public().(ed25519.PublicKey))
	schnorrKey, schnorrPub := btcec.PrivKeyFromBytes(seed("bip340"))
	schnorrLock := schnorr.SerializePubKey(schnorrPub)
	scalar := seed("scalar")
	_, scalarPub := btcec.PrivKeyFromBytes(scalar)
	scalarLock := scalarPub.SerializeCompressed()

	signEd := func(msg []byte) []byte { return ed25519.Sign(edKey, msg) }
	signSchnorr := func(t *testing.T, msg []byte) []byte {
		sig, err := schnorr.Sign(schnorrKey, msg)
		if err != nil {
			t.Fatal(err)
		}
		return sig.Serialize()
	}
	unlockMsg := func(pointType uint8, id types.Hash, dest types.Address) []byte {
		return definition.GetPtlcUnlockMessage(net.ChainID, pointType, id, dest)
	}

	create := func(expiry int64, pointType uint8, lock []byte, dest types.Address) []byte {
		return definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName, expiry, pointType, lock, dest)
	}
	unlock := func(id types.Hash, witness []byte) []byte {
		return definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName, id, witness)
	}
	proxy := func(id types.Hash, dest types.Address, witness []byte) []byte {
		return definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName, id, dest, witness)
	}
	reclaim := func(id types.Hash) []byte {
		return definition.ABIPtlc.PackMethodPanic(definition.ReclaimPtlcMethodName, id)
	}
	now := func(t *testing.T) int64 {
		m, err := net.Frontier()
		if err != nil {
			t.Fatal(err)
		}
		return int64(m.TimestampUnix)
	}
	waitUntil := func(t *testing.T, ts int64) {
		for now(t) < ts {
			time.Sleep(2 * time.Second)
		}
	}
	gone := func(t *testing.T, id types.Hash) {
		t.Helper()
		var got obj
		// Gone means the read fails because the entry does not exist, not
		// that the read failed for any reason.
		if err := net.Call(&got, "embedded.ptlc.getById", id); err == nil || err.Error() != constants.ErrDataNonExistent.Error() {
			t.Errorf("ptlc %s after it was closed: answer %v, error %v; want the error %q", id, got, err, constants.ErrDataNonExistent)
		}
	}

	var edID types.Hash

	t.Run("PT-1_create_with_an_ed25519_lock", func(t *testing.T) {
		cover(t, "E1", "contract-method", "ptlc.Create")
		before := bal(t, a.Address, znn)
		expiry := now(t) + 3600
		edID = callOK(t, a, ptlc, create(expiry, definition.PointTypeED25519, edLock, none), znn, coins(2)).Send.Hash
		wantBig(t, "ZNN locked", bal(t, a.Address, znn), sub(before, coins(2)))
		var got obj
		get(t, &got, "embedded.ptlc.getById", edID)
		if str(got["timeLocked"]) != a.Address.String() || str(got["amount"]) != coins(2).String() ||
			str(got["expirationTime"]) != fmt.Sprint(expiry) || str(got["pointType"]) != "0" {
			t.Errorf("ptlc read back: %v", got)
		}
	})

	t.Run("PT-2_create_refusals", func(t *testing.T) {
		cover(t, "E2", "contract-method", "ptlc.Create")
		cover(t, "E5", "contract-method", "ptlc.Create")
		cover(t, "E6", "contract-method", "ptlc.Create")
		cover(t, "E2", "error", "vm/constants.ErrInvalidPointType", "vm/constants.ErrInvalidPointLock", "vm/constants.ErrInvalidDestination")
		expiry := now(t) + 3600
		lock, typ, dest := constants.ErrInvalidPointLock.Error(), constants.ErrInvalidPointType.Error(), constants.ErrInvalidDestination.Error()
		callReject(t, typ, a, ptlc, create(expiry, 3, edLock, none), znn, coins(1))
		callReject(t, lock, a, ptlc, create(expiry, definition.PointTypeED25519, edLock[:31], none), znn, coins(1))
		callReject(t, lock, a, ptlc, create(expiry, definition.PointTypeED25519, append(append([]byte{}, edLock...), 0), none), znn, coins(1))
		identity := make([]byte, 32)
		identity[0] = 1 // the Ed25519 identity point: small order, nobody holds its key
		callReject(t, lock, a, ptlc, create(expiry, definition.PointTypeED25519, identity, none), znn, coins(1))
		callReject(t, lock, a, ptlc, create(expiry, definition.PointTypeBIP340, make([]byte, 32), none), znn, coins(1)) // x = 0 is not on the curve
		badPrefix := append([]byte{4}, scalarLock[1:]...)
		callReject(t, lock, a, ptlc, create(expiry, definition.PointTypeSecp256k1Point, badPrefix, b.Address), znn, coins(1))
		callReject(t, dest, a, ptlc, create(expiry, definition.PointTypeSecp256k1Point, scalarLock, none), znn, coins(1)) // a scalar lock must name who is paid
		callReject(t, dest, a, ptlc, create(expiry, definition.PointTypeED25519, edLock, types.TokenContract), znn, coins(1))
		callReject(t, constants.ErrInvalidTokenOrAmount.Error(), a, ptlc, create(expiry, definition.PointTypeED25519, edLock, none), znn, units(0))
	})

	t.Run("PT-3_create_already_expired_fails_and_refunds", func(t *testing.T) {
		cover(t, "E12", "contract-method", "ptlc.Create")
		before := bal(t, a.Address, znn)
		res := callFail(t, a, ptlc, create(now(t)-1, definition.PointTypeED25519, edLock, none), znn, coins(1))
		if len(res.Receive.DescendantBlocks) != 1 {
			t.Fatalf("failed create produced %d descendant blocks, want 1 refund", len(res.Receive.DescendantBlocks))
		}
		receiveAll(t, a, 1)
		wantBig(t, "ZNN after the refund", bal(t, a.Address, znn), before)
	})

	t.Run("PT-4_unlock_refusals", func(t *testing.T) {
		cover(t, "E5", "contract-method", "ptlc.Unlock")
		cover(t, "E6", "contract-method", "ptlc.Unlock")
		cover(t, "E3", "contract-method", "ptlc.Unlock")
		cover(t, "E6", "error", "vm/constants.ErrInvalidPointSignature")
		// A witness of a length no lock type accepts is refused at publish.
		callReject(t, constants.ErrInvalidPointSignature.Error(), b, ptlc, unlock(edID, make([]byte, 10)), znn, units(0))
		callReject(t, constants.ErrInvalidTokenOrAmount.Error(), b, ptlc, unlock(edID, signEd(unlockMsg(0, edID, b.Address))), znn, units(1))
		// A signature made for another receiver does not pay this caller.
		callFail(t, c, ptlc, unlock(edID, signEd(unlockMsg(0, edID, b.Address))), znn, units(0), watch("embedded.ptlc.getById", edID))
		// A signature for another entry, a scalar-sized witness and a signature under the wrong lock type all fail.
		callFail(t, b, ptlc, unlock(edID, signEd(unlockMsg(0, types.NewHash([]byte("other")), b.Address))), znn, units(0), watch("embedded.ptlc.getById", edID))
		callFail(t, b, ptlc, unlock(edID, make([]byte, 32)), znn, units(0), watch("embedded.ptlc.getById", edID))
		callFail(t, b, ptlc, unlock(edID, signEd(unlockMsg(1, edID, b.Address))), znn, units(0), watch("embedded.ptlc.getById", edID))
		callFail(t, b, ptlc, unlock(types.NewHash([]byte("no ptlc "+runTag)), signEd(unlockMsg(0, edID, b.Address))), znn, units(0), watch("embedded.ptlc.getById", edID))
		var got obj
		get(t, &got, "embedded.ptlc.getById", edID)
	})

	t.Run("PT-5_reclaim_before_expiry_or_by_the_wrong_account_fails", func(t *testing.T) {
		cover(t, "E12", "contract-method", "ptlc.Reclaim")
		cover(t, "E3", "contract-method", "ptlc.Reclaim")
		callFail(t, a, ptlc, reclaim(edID), znn, units(0), watch("embedded.ptlc.getById", edID))
		callFail(t, b, ptlc, reclaim(edID), znn, units(0), watch("embedded.ptlc.getById", edID))
	})

	t.Run("PT-6_unlock_with_an_ed25519_signature_pays_the_caller", func(t *testing.T) {
		cover(t, "E1", "contract-method", "ptlc.Unlock")
		cover(t, "E4", "contract-method", "ptlc.Unlock")
		before := bal(t, b.Address, znn)
		res := callOK(t, b, ptlc, unlock(edID, signEd(unlockMsg(0, edID, b.Address))), znn, units(0))
		receiveAll(t, b, 1)
		wantBig(t, "ZNN paid out", bal(t, b.Address, znn), add(before, coins(2)))
		gone(t, edID)
		if err := net.Agreement(res.Receive.Hash, wait); err != nil {
			t.Error(err)
		}
		callFail(t, b, ptlc, unlock(edID, signEd(unlockMsg(0, edID, b.Address))), znn, units(0)) // the entry is gone
	})

	t.Run("PT-7_proxy_unlock_with_a_bip340_signature", func(t *testing.T) {
		cover(t, "E1", "contract-method", "ptlc.ProxyUnlock", "ptlc.Create")
		cover(t, "E3", "contract-method", "ptlc.ProxyUnlock")
		cover(t, "E5", "contract-method", "ptlc.ProxyUnlock")
		id := callOK(t, a, ptlc, create(now(t)+3600, definition.PointTypeBIP340, schnorrLock, none), znn, coins(3)).Send.Hash
		sig := signSchnorr(t, unlockMsg(1, id, b.Address))
		// The signature names b: a proxy cannot redirect the payment to itself.
		callFail(t, c, ptlc, proxy(id, c.Address, sig), znn, units(0), watch("embedded.ptlc.getById", id))
		callReject(t, constants.ErrInvalidDestination.Error(), c, ptlc, proxy(id, types.TokenContract, sig), znn, units(0))
		before := bal(t, b.Address, znn)
		callOK(t, c, ptlc, proxy(id, b.Address, sig), znn, units(0))
		receiveAll(t, b, 1)
		wantBig(t, "ZNN paid to the signed destination", bal(t, b.Address, znn), add(before, coins(3)))
		gone(t, id)
	})

	t.Run("PT-8_secp256k1_point_unlocked_by_its_scalar", func(t *testing.T) {
		cover(t, "E1", "contract-method", "ptlc.Create", "ptlc.Unlock")
		cover(t, "E3", "contract-method", "ptlc.Unlock")
		cover(t, "E5", "error", "vm/constants.ErrInvalidPointScalar")
		id := callOK(t, a, ptlc, create(now(t)+3600, definition.PointTypeSecp256k1Point, scalarLock, b.Address), znn, coins(1)).Send.Hash
		// The scalar is public once used, so only the fixed destination may be paid.
		callFail(t, c, ptlc, unlock(id, scalar), znn, units(0), watch("embedded.ptlc.getById", id))
		callFail(t, c, ptlc, proxy(id, c.Address, scalar), znn, units(0), watch("embedded.ptlc.getById", id))
		callFail(t, b, ptlc, unlock(id, seed("wrong scalar")), znn, units(0), watch("embedded.ptlc.getById", id))
		callFail(t, b, ptlc, unlock(id, make([]byte, 32)), znn, units(0), watch("embedded.ptlc.getById", id)) // zero is not a scalar
		before := bal(t, b.Address, znn)
		callOK(t, c, ptlc, proxy(id, b.Address, scalar), znn, units(0)) // anyone may deliver it to the destination
		receiveAll(t, b, 1)
		wantBig(t, "ZNN paid to the fixed destination", bal(t, b.Address, znn), add(before, coins(1)))
	})

	t.Run("PT-9_after_expiry_unlock_fails_and_reclaim_pays_back", func(t *testing.T) {
		cover(t, "E12", "contract-method", "ptlc.Unlock", "ptlc.Reclaim")
		cover(t, "E1", "contract-method", "ptlc.Reclaim")
		cover(t, "E4", "contract-method", "ptlc.ReclaimWithSignature")
		before := bal(t, a.Address, znn)
		expiry := now(t) + 40
		id := callOK(t, a, ptlc, create(expiry, definition.PointTypeED25519, edLock, none), znn, coins(4)).Send.Hash
		waitUntil(t, expiry)
		callFail(t, b, ptlc, unlock(id, signEd(unlockMsg(0, id, b.Address))), znn, units(0), watch("embedded.ptlc.getById", id))
		// An entry without a reclaim lock is not reclaimed through the signature method.
		callFail(t, a, ptlc, definition.ABIPtlc.PackMethodPanic(definition.ReclaimWithSignaturePtlcMethodName, id, make([]byte, 64)), znn, units(0), watch("embedded.ptlc.getById", id))
		callOK(t, a, ptlc, reclaim(id), znn, units(0))
		receiveAll(t, a, 1)
		wantBig(t, "ZNN after reclaim", bal(t, a.Address, znn), before)
		gone(t, id)
	})

	t.Run("PT-10_reclaim_lock", func(t *testing.T) {
		cover(t, "E1", "contract-method", "ptlc.CreateWithReclaimLock", "ptlc.ReclaimWithSignature")
		cover(t, "E2", "contract-method", "ptlc.CreateWithReclaimLock")
		cover(t, "E4", "contract-method", "ptlc.Reclaim")
		cover(t, "E5", "contract-method", "ptlc.ReclaimWithSignature")
		cover(t, "E12", "contract-method", "ptlc.ReclaimWithSignature")
		withLock := func(expiry int64, reclaimType uint8, reclaimLock []byte) []byte {
			return definition.ABIPtlc.PackMethodPanic(definition.CreateWithReclaimLockPtlcMethodName, expiry, definition.PointTypeBIP340, schnorrLock, none, reclaimType, reclaimLock)
		}
		sigReclaim := func(id types.Hash, sig []byte) []byte {
			return definition.ABIPtlc.PackMethodPanic(definition.ReclaimWithSignaturePtlcMethodName, id, sig)
		}
		// A scalar lock cannot guard a reclaim: its witness would be public.
		callReject(t, constants.ErrInvalidPointType.Error(), a, ptlc, withLock(now(t)+3600, definition.PointTypeSecp256k1Point, scalarLock), znn, coins(1))
		callReject(t, constants.ErrInvalidPointLock.Error(), a, ptlc, withLock(now(t)+3600, definition.PointTypeED25519, edLock[:31]), znn, coins(1))

		before := bal(t, a.Address, znn)
		expiry := now(t) + 50
		id := callOK(t, a, ptlc, withLock(expiry, definition.PointTypeED25519, edLock), znn, coins(5)).Send.Hash
		var lock obj
		get(t, &lock, "embedded.ptlc.getReclaimLockById", id)
		if str(lock["pointType"]) != "0" {
			t.Errorf("reclaim lock read back: %v", lock)
		}
		entry := &definition.PtlcInfo{TimeLocked: a.Address, Destination: none, TokenStandard: znn, Amount: coins(5),
			ExpirationTime: expiry, PointType: definition.PointTypeBIP340, PointLock: schnorrLock}
		good := signEd(definition.GetPtlcReclaimMessage(net.ChainID, definition.PointTypeED25519, entry))

		callFail(t, a, ptlc, sigReclaim(id, good), znn, units(0), watch("embedded.ptlc.getById", id), watch("embedded.ptlc.getReclaimLockById", id)) // not expired yet
		waitUntil(t, expiry)
		callFail(t, a, ptlc, reclaim(id), znn, units(0), watch("embedded.ptlc.getById", id), watch("embedded.ptlc.getReclaimLockById", id))                                      // the plain reclaim is closed for this entry
		callFail(t, a, ptlc, sigReclaim(id, signEd([]byte("something else"))), znn, units(0), watch("embedded.ptlc.getById", id), watch("embedded.ptlc.getReclaimLockById", id)) // wrong message
		callFail(t, b, ptlc, sigReclaim(id, good), znn, units(0), watch("embedded.ptlc.getById", id), watch("embedded.ptlc.getReclaimLockById", id))                             // right signature, wrong account
		callReject(t, constants.ErrInvalidPointSignature.Error(), a, ptlc, sigReclaim(id, good[:63]), znn, units(0))
		callOK(t, a, ptlc, sigReclaim(id, good), znn, units(0))
		receiveAll(t, a, 1)
		wantBig(t, "ZNN after the signed reclaim", bal(t, a.Address, znn), before)
		gone(t, id)
		if err := net.Call(&lock, "embedded.ptlc.getReclaimLockById", id); err == nil {
			t.Errorf("reclaim lock still readable after the reclaim: %v", lock)
		}
	})
}
