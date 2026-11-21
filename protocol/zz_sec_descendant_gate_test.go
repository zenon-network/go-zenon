package protocol_test

import (
	"math/big"
	"testing"
	"time"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// TestSEC_ContractReceiveRules_AcrossTheRuleHeight drives the contract-receive
// rules through the chain bridge, the way blocks and momentums arrive from the
// network, around definition.CommunitySporkAddressRenewalStartHeight:
//
//   - pool admission refuses a tampered contract receive whatever the rule
//     height, so one cannot reach an upgraded node's pool through
//     publication or gossip, and a momentum insertion that finds the pool
//     clean validates the block itself under the rules for its height;
//   - a momentum carrying a tampered receive, built the way a producer without
//     the rules would build it, is accepted when its height is below the rule
//     height (what every node did before the rules) and rejected at the rule
//     height, with the same verdict after a rollback and re-insertion and
//     whether or not the tampered copy was offered to the pool first.
//
// Local only.
func TestSEC_ContractReceiveRules_AcrossTheRuleHeight(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()

	savedExpiration := constants.FuseExpiration
	constants.FuseExpiration = 10
	defer func() { constants.FuseExpiration = savedExpiration }()
	savedRuleHeight := definition.CommunitySporkAddressRenewalStartHeight
	defer func() { definition.CommunitySporkAddressRenewalStartHeight = savedRuleHeight }()
	// Start with the rules far in the future, as on a network below the
	// height.
	definition.CommunitySporkAddressRenewalStartHeight = 1 << 40

	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	bridge := protocol.NewChainBridge(z.Chain(), z.Consensus(), z.Verifier(), supervisor)
	plasmaApi := embedded.NewPlasmaApi(z)
	ledgerApi := api.NewLedgerApi(z)

	// Fuse, let the entry expire, cancel: the plasma contract's receive of the
	// cancel carries the QSR refund as its single descendant.
	fuseAmount := big.NewInt(100 * g.Zexp)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User1.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        fuseAmount,
	}, nil, mock.SkipVmChanges)
	z.InsertMomentumsTo(26)
	unreceived, err := ledgerApi.GetUnreceivedBlocksByAddress(g.User1.Address, 0, 50)
	common.FailIfErr(t, err)
	for _, block := range unreceived.List {
		z.InsertReceiveBlock(block.AccountBlock.Header(), nil, nil, mock.SkipVmChanges)
	}
	z.InsertMomentumsTo(45)

	entries, err := plasmaApi.GetEntriesByAddress(g.User1.Address, 0, 10)
	common.FailIfErr(t, err)
	var fuseId types.Hash
	for _, e := range entries.Fusions {
		if e.QsrAmount.Cmp(fuseAmount) == 0 {
			fuseId = e.Id
		}
	}
	if fuseId.IsZero() {
		t.Fatalf("fusion entry not found")
	}
	cancelSend := z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PlasmaContract,
		Data:      definition.ABIPlasma.PackMethodPanic(definition.CancelFuseMethodName, fuseId),
	}, nil, mock.SkipVmChanges)
	// The momentum after the cancel commits the send; the one after that
	// carries the contract's receive, which the producer generates.
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	honest, err := z.Chain().GetFrontierAccountStore(types.PlasmaContract).Frontier()
	common.FailIfErr(t, err)
	if honest == nil || honest.FromBlockHash != cancelSend.Hash || len(honest.DescendantBlocks) != 1 {
		t.Fatalf("plasma frontier is not the refund receive with one descendant")
	}
	honestMomentum, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.FailIfErr(t, err)
	if !momentumContains(honestMomentum, honest) {
		t.Fatalf("the frontier momentum does not contain the honest receive")
	}

	// The tampered copy doubles the refund. It keeps the parent's hash, which
	// does not cover descendant content.
	tampered := honest.Copy()
	tampered.DescendantBlocks[0].Amount = new(big.Int).Mul(tampered.DescendantBlocks[0].Amount, big.NewInt(2))
	if tampered.Hash != honest.Hash {
		t.Fatalf("tampering changed the parent hash; the test premise does not hold")
	}

	rollbackTo := func(id types.HashHeight) {
		t.Helper()
		insert := z.Chain().AcquireInsert("test rollback")
		defer insert.Unlock()
		common.FailIfErr(t, z.Chain().RollbackTo(insert, id))
	}
	frontierIs := func(want types.HashHeight) {
		t.Helper()
		common.Expect(t, z.Chain().GetFrontierMomentumStore().Identifier(), want)
	}
	poolHolds := func(block *nom.AccountBlock) bool {
		stored, err := z.Chain().GetFrontierAccountStore(block.Address).ByHeight(block.Height)
		return err == nil && stored != nil && stored.EqualBytes(block)
	}

	// 1. Pool admission refuses the tampered receive below the rule height.
	rollbackTo(honestMomentum.Previous())
	if err := bridge.AddAccountBlocks([]*nom.AccountBlock{tampered}); err == nil {
		t.Fatalf("pool admission accepted a tampered contract receive")
	}
	if poolHolds(tampered) {
		t.Fatalf("the pool holds the tampered receive after a refused admission")
	}

	// 2. Build the momentum a producer without the rules would build: force
	// the tampered copy into the pool, the way such a producer's pool would
	// hold it, and let the local pillar produce from it.
	tx, err := supervisor.ApplyBlockInMomentum(tampered.Copy(), honestMomentum.Height)
	common.FailIfErr(t, err)
	insert := z.Chain().AcquireInsert("test force-add")
	common.FailIfErr(t, z.Chain().ForceAddAccountBlockTransaction(insert, tx))
	insert.Unlock()
	if !poolHolds(tampered) {
		t.Fatalf("force-add did not leave the tampered receive in the pool")
	}
	z.InsertNewMomentum()
	store := z.Chain().GetFrontierMomentumStore()
	tamperedMomentum, err := store.GetFrontierMomentum()
	common.FailIfErr(t, err)
	common.Expect(t, tamperedMomentum.Height, honestMomentum.Height)
	detailed, err := store.PrefetchMomentum(tamperedMomentum)
	common.FailIfErr(t, err)
	var carried *nom.AccountBlock
	for _, b := range detailed.AccountBlocks {
		if b.Address == types.PlasmaContract && b.Height == tampered.Height {
			carried = b
		}
	}
	if carried == nil || !carried.EqualBytes(tampered) {
		t.Fatalf("the produced momentum does not carry the tampered receive")
	}
	rollbackTo(tamperedMomentum.Previous())

	// 3. Below the rule height the momentum is accepted, as every node accepted
	// it before the rules, and the tampered bytes are what gets committed.
	_, err = bridge.InsertChain([]*nom.DetailedMomentum{detailed})
	common.FailIfErr(t, err)
	frontierIs(tamperedMomentum.Identifier())
	committed, err := z.Chain().GetFrontierMomentumStore().GetAccountBlock(tampered.Header())
	common.FailIfErr(t, err)
	if !committed.EqualBytes(tampered) {
		t.Fatalf("committed block does not match the momentum's copy")
	}

	// 4. The same momentum after a rollback is judged the same way.
	rollbackTo(tamperedMomentum.Previous())
	_, err = bridge.InsertChain([]*nom.DetailedMomentum{detailed})
	common.FailIfErr(t, err)
	frontierIs(tamperedMomentum.Identifier())
	rollbackTo(tamperedMomentum.Previous())

	// 5. At the rule height the momentum is rejected, with a clean pool ...
	definition.CommunitySporkAddressRenewalStartHeight = tamperedMomentum.Height
	if _, err := bridge.InsertChain([]*nom.DetailedMomentum{detailed}); err == nil {
		t.Fatalf("a momentum carrying a tampered receive was accepted at the rule height")
	}
	frontierIs(tamperedMomentum.Previous())

	// The bridge remembers a consensus rejection by the bytes served and
	// refuses them before validating again, since the verdict is stable on
	// a network. This test moves the rule height, which a network never
	// does, so every offer after a rejection goes through a bridge with an
	// empty memory, and it is the validation that is exercised.
	freshBridge := func() protocol.ChainBridge {
		return protocol.NewChainBridge(z.Chain(), z.Consensus(), z.Verifier(), supervisor)
	}

	// 6. ... and after the tampered copy was offered to the pool first, which
	// admission refuses, so insertion validates the block itself.
	if err := bridge.AddAccountBlocks([]*nom.AccountBlock{tampered}); err == nil {
		t.Fatalf("pool admission accepted a tampered contract receive at the rule height")
	}
	if poolHolds(tampered) {
		t.Fatalf("the pool holds the tampered receive after a refused admission")
	}
	if _, err := freshBridge().InsertChain([]*nom.DetailedMomentum{detailed}); err == nil {
		t.Fatalf("a momentum carrying a tampered receive was accepted with the copy pre-offered")
	}
	frontierIs(tamperedMomentum.Previous())

	// 7. One below the rule height it is accepted again: the boundary is the
	// momentum's own height.
	definition.CommunitySporkAddressRenewalStartHeight = tamperedMomentum.Height + 1
	_, err = freshBridge().InsertChain([]*nom.DetailedMomentum{detailed})
	common.FailIfErr(t, err)
	frontierIs(tamperedMomentum.Identifier())
}

func momentumContains(momentum *nom.Momentum, block *nom.AccountBlock) bool {
	for _, header := range momentum.Content {
		if header.Address == block.Address && header.Height == block.Height && header.Hash == block.Hash {
			return true
		}
	}
	return false
}
