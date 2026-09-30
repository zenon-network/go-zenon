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
	send3 := z.InsertSendBlock(&nom.AccountBlock{
		Address: g.User3.Address, ToAddress: g.User4.Address,
		TokenStandard: types.ZnnTokenStandard, Amount: big.NewInt(1),
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
