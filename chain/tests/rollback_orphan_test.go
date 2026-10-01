package tests

import (
	"math/big"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// TestRollbackEvictsOrphanedReceive reproduces the scenario where a pending
// receive becomes orphaned after a momentum rollback: the send was cemented
// in a momentum that gets rolled back, but the receiver's address was never
// in that momentum.  The pool must evict the orphaned receive so the next
// momentum production does not fail with "Can't find from-block in store".
func TestRollbackEvictsOrphanedReceive(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(3)
	before := z.Chain().GetFrontierMomentumStore().Identifier()

	send := z.InsertSendBlock(&nom.AccountBlock{
		Address: g.User1.Address, ToAddress: g.User2.Address,
		TokenStandard: types.ZnnTokenStandard, Amount: big.NewInt(1),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum() // M: cements the send (User1 only)
	m := z.Chain().GetFrontierMomentumStore().Identifier()
	recv := z.InsertReceiveBlock(send.Header(), nil, nil, mock.SkipVmChanges)
	t.Logf("M=%v recv.MomentumAcknowledged=%v", m, recv.MomentumAcknowledged)

	insert := z.Chain().AcquireInsert("rollback orphan test")
	err := z.Chain().RollbackTo(insert, before)
	insert.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	frontier := z.Chain().GetFrontierMomentumStore()
	sendOnChain, _ := frontier.GetAccountBlockByHash(send.Hash)
	orphans := 0
	for _, b := range z.Chain().GetAllUncommittedAccountBlocks() {
		if b.Hash == recv.Hash {
			orphans++
		}
	}
	t.Logf("frontier=%v sendStillOnChain=%v pendingRecvKept=%d", frontier.Identifier(), sendOnChain != nil, orphans)

	// The orphaned receive must have been evicted from the pool.
	if orphans > 0 && sendOnChain == nil {
		t.Errorf("pool kept receive %v whose send %v is no longer on chain; acknowledged momentum %v was rolled back",
			recv.Hash, send.Hash, recv.MomentumAcknowledged)
	}

	// The next momentum must be produced without error.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("next momentum production panicked: %v", r)
			}
		}()
		z.InsertNewMomentum()
		newFrontier := z.Chain().GetFrontierMomentumStore().Identifier()
		t.Logf("after next momentum: frontier=%v pending=%d", newFrontier, len(z.Chain().GetAllUncommittedAccountBlocks()))
		common.Expect(t, newFrontier.Height, before.Height+1)
	}()
}

// TestRollbackKeepsUnrelatedPending verifies that pending blocks from
// addresses unrelated to the rollback survive, which is the whole point of
// the narrow eviction (issue #104).
func TestRollbackKeepsUnrelatedPending(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(3)

	// User1 sends to User2, gets cemented in momentum 4.
	send1 := z.InsertSendBlock(&nom.AccountBlock{
		Address: g.User1.Address, ToAddress: g.User2.Address,
		TokenStandard: types.ZnnTokenStandard, Amount: big.NewInt(1),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum() // M4: cements send1
	before := z.Chain().GetFrontierMomentumStore().Identifier()

	// User1 sends again, gets cemented in momentum 5.
	send1b := z.InsertSendBlock(&nom.AccountBlock{
		Address: g.User1.Address, ToAddress: g.User2.Address,
		TokenStandard: types.ZnnTokenStandard, Amount: big.NewInt(2),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum() // M5: cements send1b

	// User3 has a pending send (NOT in any momentum — inserted after M5).
	// Set its MomentumAcknowledged to m4 (before the rollback point) so it
	// remains canonical after the rollback.  Without this, the default MA
	// would be m5 — the very momentum being removed — making this a
	// height-mismatch case rather than testing narrow eviction of blocks
	// whose acknowledgements survive.
	send3 := z.InsertSendBlock(&nom.AccountBlock{
		Address: g.User3.Address, ToAddress: g.User4.Address,
		TokenStandard: types.ZnnTokenStandard, Amount: big.NewInt(1),
		MomentumAcknowledged: before,
	}, nil, mock.SkipVmChanges)

	// Rollback M5 only.
	insert := z.Chain().AcquireInsert("rollback keep unrelated")
	err := z.Chain().RollbackTo(insert, before)
	insert.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	// User3's pending send must survive.
	pending := z.Chain().GetAllUncommittedAccountBlocks()
	found := false
	for _, b := range pending {
		if b.Hash == send3.Hash {
			found = true
			break
		}
	}
	if !found {
		t.Error("unrelated pending block from User3 was lost after rollback")
	}

	// User1's second send must NOT survive (it was in the rolled-back momentum).
	foundSend1b := false
	for _, b := range pending {
		if b.Hash == send1b.Hash {
			foundSend1b = true
			break
		}
	}
	if foundSend1b {
		t.Error("send1b should have been evicted (it was in the rolled-back momentum)")
	}

	// The first send (cemented in M4, before the rollback point) must still be on chain.
	frontier := z.Chain().GetFrontierMomentumStore()
	sendOnChain, _ := frontier.GetAccountBlockByHash(send1.Hash)
	if sendOnChain == nil {
		t.Error("send1 should still be on chain (cemented before rollback point)")
	}
}

// TestRollbackReplacesMomentum_DropsStaleAcknowledged tests the stale-MA
// eviction in DeleteMomentum: a pending block whose MomentumAcknowledged
// height is >= the popped momentum's height acknowledges a momentum that is
// no longer canonical after the rollback and must be evicted before momentum
// selection can pick it up.
//
// This is the scenario edgepillar identified in review of PR #115: the height
// check alone is not enough — a replacement momentum at the same height has a
// different hash, and replaying the cached patch without re-verifying would
// skip the canonical-acknowledgement check that normal account-block
// verification performs (verifier/account_block.go momentumAcknowledged).
func TestRollbackReplacesMomentum_DropsStaleAcknowledged(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(3)
	m3 := z.Chain().GetFrontierMomentumStore().Identifier()

	// Create momentum 4.
	z.InsertNewMomentum()
	m4 := z.Chain().GetFrontierMomentumStore().Identifier()

	// Insert a PENDING send (not in any momentum) acknowledging m4.
	send := z.InsertSendBlock(&nom.AccountBlock{
		Address: g.User1.Address, ToAddress: g.User2.Address,
		TokenStandard: types.ZnnTokenStandard, Amount: big.NewInt(1),
	}, nil, mock.SkipVmChanges)
	t.Logf("send.MA=%v m4=%v", send.MomentumAcknowledged, m4)

	// Rollback to m3 — removes m4.  The send was NOT in m4's content, so
	// User1's manager survives DeleteMomentum.
	insert := z.Chain().AcquireInsert("rollback fork test")
	err := z.Chain().RollbackTo(insert, m3)
	insert.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	// Insert a different pending block (from User3) before creating the
	// replacement momentum.  This changes the momentum content, ensuring a
	// different hash at the same height (the mock clock is deterministic,
	// so without different content the replacement would be identical).
	z.InsertSendBlock(&nom.AccountBlock{
		Address: g.User3.Address, ToAddress: g.User4.Address,
		TokenStandard: types.ZnnTokenStandard, Amount: big.NewInt(99),
	}, nil, mock.SkipVmChanges)

	// Insert a new momentum at height 4 — different content means different hash.
	z.InsertNewMomentum()
	newM4 := z.Chain().GetFrontierMomentumStore().Identifier()
	t.Logf("old m4=%v new m4=%v", m4, newM4)
	if newM4.Hash == m4.Hash {
		t.Fatal("expected different hash at same height (fork)")
	}

	// The send's MA still points to the OLD m4 hash.
	// DeleteMomentum already evicted it during rollback (MA.Height 4 >= popped
	// height 4), so it must not be cemented into the replacement momentum.
	// Trigger momentum selection by inserting another momentum.
	z.InsertNewMomentum()

	// The stale-acknowledged send must NOT be on chain.
	frontier := z.Chain().GetFrontierMomentumStore()
	sendOnChain, _ := frontier.GetAccountBlockByHash(send.Hash)
	if sendOnChain != nil {
		t.Errorf("stale-acknowledged send %v was cemented on chain despite MomentumAcknowledged height %d >= popped height %d",
			send.Hash, send.MomentumAcknowledged.Height, m4.Height)
	}

	// It must also not remain in the pending pool.
	for _, b := range z.Chain().GetAllUncommittedAccountBlocks() {
		if b.Hash == send.Hash {
			t.Errorf("stale-acknowledged send %v still pending despite MomentumAcknowledged height %d >= popped height %d",
				b.Hash, b.MomentumAcknowledged.Height, m4.Height)
		}
	}
}

// TestRollbackCanonicalAck_KeepsValidPending is the control: a pending block
// whose MomentumAcknowledged hash still matches the canonical momentum after a
// rollback to a point BEFORE its acknowledged height must survive.
//
// This addresses the review observation that TestRollbackKeepsUnrelatedPending
// creates its pending send after M5, so its default acknowledgement is exactly
// the momentum that gets removed — making it a height-mismatch case, not a
// hash-mismatch case.  Here we explicitly set the MA to a momentum that
// survives the rollback.
func TestRollbackCanonicalAck_KeepsValidPending(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(3)

	// Create momentum 4 — this one will survive.
	z.InsertNewMomentum()
	m4 := z.Chain().GetFrontierMomentumStore().Identifier()

	// Create momentum 5.
	z.InsertNewMomentum()
	m5 := z.Chain().GetFrontierMomentumStore().Identifier()

	// Insert a pending send acknowledging m4 (which is BELOW the rollback
	// target m5, so m4 survives the rollback).
	send := z.InsertSendBlock(&nom.AccountBlock{
		Address:              g.User1.Address,
		ToAddress:            g.User2.Address,
		TokenStandard:        types.ZnnTokenStandard,
		Amount:               big.NewInt(1),
		MomentumAcknowledged: m4,
	}, nil, mock.SkipVmChanges)
	t.Logf("send.MA=%v m4=%v m5=%v", send.MomentumAcknowledged, m4, m5)

	// Rollback to m4 (removes m5 only).  m4 is still canonical.
	insert := z.Chain().AcquireInsert("rollback canonical ack")
	err := z.Chain().RollbackTo(insert, m4)
	insert.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	// The pending send must still be in the pool after rollback — its MA
	// hash matches the canonical momentum at height 4.
	found := false
	for _, b := range z.Chain().GetAllUncommittedAccountBlocks() {
		if b.Hash == send.Hash {
			found = true
			break
		}
	}
	if !found {
		t.Error("pending send with canonical MomentumAcknowledged was incorrectly dropped by rollback")
	}

	// Trigger rebuild by inserting a new momentum.  The send should be
	// cemented (not dropped).
	z.InsertNewMomentum()

	// The send must now be on chain (cemented in the new momentum).
	frontier := z.Chain().GetFrontierMomentumStore()
	sendOnChain, _ := frontier.GetAccountBlockByHash(send.Hash)
	if sendOnChain == nil {
		t.Error("pending send with canonical MomentumAcknowledged was dropped during rebuild")
	}
}
