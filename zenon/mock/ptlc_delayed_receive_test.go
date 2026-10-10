package mock

import (
	"math/big"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// activatePtlcSpork creates and activates a spork, makes it the ptlc spork
// for the length of the test, and runs the chain past its enforcement height.
func activatePtlcSpork(t *testing.T, z MockZenon) {
	t.Helper()

	oldSporkId := types.PtlcSpork.SporkId
	var activatedSporkId types.Hash
	t.Cleanup(func() {
		delete(types.ImplementedSporksMap, activatedSporkId)
		types.ImplementedSporksMap[oldSporkId] = true
		types.PtlcSpork.SporkId = oldSporkId
	})

	create := z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data:      definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName, "spork-ptlc", "activate spork for ptlc"),
	}, nil, SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data:      definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName, create.Hash),
	}, nil, SkipVmChanges)
	z.InsertNewMomentum()

	activatedSporkId = create.Hash
	types.PtlcSpork.SporkId = create.Hash
	types.ImplementedSporksMap[create.Hash] = true
	z.InsertMomentumsTo(20)
}

// An unlock is judged against the momentum that confirmed its send block, not
// against the chain's clock when the contract's receive is produced. The
// receive is held back here until the entry's expiration has passed: the
// supervisor still generates it with the confirming momentum acknowledged and
// the unlock still pays, and the same receive acknowledging a later momentum,
// under which the unlock would be expired, is refused by the verifier.
func TestPtlcDelayedReceiveKeepsTheConfirmingMomentum(t *testing.T) {
	z := NewMockZenon(t)
	defer z.StopPanic()
	activatePtlcSpork(t, z)
	mock := z.(*mockZenon)

	frontier := func() *nom.Momentum {
		momentum, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
		common.FailIfErr(t, err)
		return momentum
	}
	entryExists := func(id types.Hash) bool {
		_, err := definition.GetPtlcInfo(z.Chain().GetFrontierAccountStore(types.PtlcContract).Storage(), id)
		if err != nil && err != constants.ErrDataNonExistent {
			t.Fatal(err)
		}
		return err == nil
	}

	expiration := frontier().Timestamp.Unix() + 45
	create := z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PtlcContract,
		Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
			expiration,
			definition.PointTypeED25519,
			g.User2.Public,
			types.ZeroAddress,
		),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	id := create.Hash
	if !entryExists(id) {
		t.Fatalf("the entry was not created")
	}

	signature := g.User2.Sign(definition.GetPtlcUnlockMessage(z.Chain().ChainIdentifier(), definition.PointTypeED25519, id, g.User2.Address))
	unlock := z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.PtlcContract,
		Data:      definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName, id, signature),
	}, nil, SkipVmChanges)

	// From here the contract's receives are generated and thrown away.
	held := types.PtlcContract
	mock.failInsertFor = &held
	z.InsertNewMomentum()
	confirming := frontier()
	if confirming.Timestamp.Unix() >= expiration {
		t.Fatalf("the unlock was confirmed at %d, not before the expiration %d", confirming.Timestamp.Unix(), expiration)
	}
	for frontier().Timestamp.Unix() < expiration {
		z.InsertNewMomentum()
	}
	late := frontier()
	if !entryExists(id) {
		t.Fatalf("the unlock was received while its receive was held back")
	}

	insert := z.Chain().AcquireInsert("ptlc delayed receive test")
	execution, err := mock.supervisor.GenerateAutoReceive(unlock)
	insert.Unlock()
	common.FailIfErr(t, err)
	if execution.ReturnedError != nil {
		t.Fatalf("an unlock confirmed before the expiration was refused after it: %v", execution.ReturnedError)
	}
	receive := execution.Transaction.Block
	if receive.MomentumAcknowledged != confirming.Identifier() {
		t.Fatalf("the delayed receive acknowledges %v, want the confirming momentum %v", receive.MomentumAcknowledged, confirming.Identifier())
	}
	if len(receive.DescendantBlocks) != 1 || receive.DescendantBlocks[0].ToAddress != g.User2.Address {
		t.Fatalf("the delayed receive does not pay the claimant")
	}

	// The same receive, acknowledging the momentum the chain is at now.
	forged := *receive
	forged.MomentumAcknowledged = late.Identifier()
	forged.DescendantBlocks = make([]*nom.AccountBlock, len(receive.DescendantBlocks))
	for i, descendant := range receive.DescendantBlocks {
		copied := *descendant
		copied.MomentumAcknowledged = late.Identifier()
		copied.Hash = copied.ComputeHash()
		forged.DescendantBlocks[i] = &copied
	}
	forged.Hash = forged.ComputeHash()
	insert = z.Chain().AcquireInsert("ptlc delayed receive test")
	_, err = mock.supervisor.ApplyBlock(&forged)
	insert.Unlock()
	common.ExpectError(t, err, verifier.ErrABMAInvalidForAutoGenerated)
	if !entryExists(id) {
		t.Fatalf("the refused receive changed the contract's state")
	}

	// Released, the producer retries the same receive on its next round.
	mock.failInsertFor = nil
	z.InsertNewMomentum()
	if entryExists(id) {
		t.Fatalf("the entry is still there after the receive was let through")
	}
	stored, err := z.Chain().GetFrontierAccountStore(types.PtlcContract).Frontier()
	common.FailIfErr(t, err)
	if stored.FromBlockHash != unlock.Hash || stored.MomentumAcknowledged != confirming.Identifier() {
		t.Fatalf("the chain's receive is from %v acknowledging %v, want %v acknowledging %v", stored.FromBlockHash, stored.MomentumAcknowledged, unlock.Hash, confirming.Identifier())
	}
	if now := frontier().Timestamp.Unix(); now < expiration {
		t.Fatalf("the receive was not delayed past the expiration: %d", now)
	}
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)
}
