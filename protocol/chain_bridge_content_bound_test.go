package protocol_test

import (
	"math/big"
	"testing"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// activateDynamicPlasma creates and activates the dynamic-plasma spork on the
// mock chain and advances past its enforcement, restoring the process-wide
// spork registration when the test ends.
func activateDynamicPlasma(t *testing.T, z mock.MockZenon, top uint64) {
	t.Helper()
	savedId := types.DynamicPlasmaSpork.SporkId
	savedMap := make(map[types.Hash]bool, len(types.ImplementedSporksMap))
	for k, v := range types.ImplementedSporksMap {
		savedMap[k] = v
	}
	t.Cleanup(func() {
		types.DynamicPlasmaSpork.SporkId = savedId
		types.ImplementedSporksMap = savedMap
	})

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"dynamic-plasma",
			"Activates Dynamic Plasma",
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, err := embedded.NewSporkApi(z).GetAll(0, 10)
	common.FailIfErr(t, err)
	id := sporkList.List[0].Id
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data:      definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName, id),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.DynamicPlasmaSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(top)

	active, err := z.Chain().GetFrontierMomentumStore().IsSporkActive(types.DynamicPlasmaSpork)
	common.FailIfErr(t, err)
	if !active {
		t.Fatal("dynamic plasma is not active at the frontier")
	}
}

// countingVerifier records how often the bridge asks for a full momentum
// verification, which only happens once the structural preflight has passed.
type countingVerifier struct {
	verifier.Verifier
	momentums int
}

func (v *countingVerifier) Momentum(detailed *nom.DetailedMomentum) error {
	v.momentums++
	return v.Verifier.Momentum(detailed)
}

func newCountingSideChainBridge(z mock.MockZenon) (*rollbackCountingChain, *countingVerifier, protocol.ChainBridge) {
	counting := &rollbackCountingChain{Chain: z.Chain()}
	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	verifying := &countingVerifier{Verifier: verifier.NewVerifier(counting, z.Consensus(), vm.CanonicalBasePlasma)}
	return counting, verifying, protocol.NewChainBridge(counting, z.Consensus(), verifying, supervisor)
}

// oversizedSideChain builds a side chain forking two below the frontier whose
// head, signed by the elected producer, lists one block more than the legacy
// momentum count, with the blocks supplied in content order so that the
// bridge's structural preflight passes and the count is judged by the
// momentum verifier.
func oversizedSideChain(t *testing.T, z mock.MockZenon) []*nom.DetailedMomentum {
	t.Helper()
	frontier := momentumAt(t, z.Chain(), z.Chain().GetFrontierMomentumStore().Identifier().Height)
	target := momentumAt(t, z.Chain(), frontier.Height-2)

	n := chain.MaxAccountBlocksInMomentum + 1
	blocks := make([]*nom.AccountBlock, n)
	for i := range blocks {
		blocks[i] = &nom.AccountBlock{
			Version:         1,
			ChainIdentifier: 1,
			BlockType:       nom.BlockTypeUserSend,
			Address:         g.User1.Address,
			ToAddress:       g.User2.Address,
			Height:          uint64(i + 1),
			TokenStandard:   types.ZnnTokenStandard,
			Amount:          big.NewInt(1),
			BasePlasma:      constants.AccountBlockBasePlasma,
		}
		blocks[i].Hash = blocks[i].ComputeHash()
	}
	head := unsignedMomentum(target, target.Height+1)
	head.Version = frontier.Version
	head.NextFusionPrice = frontier.NextFusionPrice
	head.NextWorkPrice = frontier.NextWorkPrice
	head.Content = nom.NewMomentumContent(blocks)
	head = signedBy(head, electedKey(t, z, *head.Timestamp))

	byIdentifier := make(map[types.HashHeight]*nom.AccountBlock, n)
	for _, block := range blocks {
		byIdentifier[block.Identifier()] = block
	}
	ordered := make([]*nom.AccountBlock, n)
	for i, header := range head.Content {
		ordered[i] = byIdentifier[header.Identifier()]
	}
	return []*nom.DetailedMomentum{
		{Momentum: head, AccountBlocks: ordered},
		detailedOf(attackerMomentum(head, frontier.Height+1))[0],
	}
}

// TestInsertChain_ContentCountFollowsTheMomentumVerifier pins where the block
// count of an imported momentum is decided. Under legacy rules a head listing
// more than MaxAccountBlocksInMomentum blocks is refused by the momentum
// verifier, before any rollback. With dynamic plasma active the same shape is
// within the plasma budget, so the count rule lets it through and it is
// judged on its blocks instead; the bridge's own preflight must not refuse it
// on count, or a valid momentum an honest producer built under the budget
// could never be imported.
func TestInsertChain_ContentCountFollowsTheMomentumVerifier(t *testing.T) {
	t.Run("legacy rules refuse the count before any rollback", func(t *testing.T) {
		z := mock.NewMockZenon(t)
		defer z.StopPanic()
		z.InsertMomentumsTo(10)
		original := snapshotChain(t, z.Chain())
		counting, verifying, bridge := newCountingSideChainBridge(z)

		_, err := bridge.InsertChain(oversizedSideChain(t, z))
		if !errors.Is(err, verifier.ErrMContentTooBig) {
			t.Fatalf("expected %v, got %v", verifier.ErrMContentTooBig, err)
		}
		common.Expect(t, verifying.momentums, 1)
		common.Expect(t, counting.rollbacks, 0)
		expectChainEquals(t, z.Chain(), original)
	})

	t.Run("dynamic plasma judges the count by the plasma budget", func(t *testing.T) {
		z := mock.NewMockZenon(t)
		defer z.StopPanic()
		activateDynamicPlasma(t, z, 30)
		original := snapshotChain(t, z.Chain())
		counting, verifying, bridge := newCountingSideChainBridge(z)

		config, err := z.Chain().GetFrontierMomentumStore().GetPlasmaVariables()
		common.FailIfErr(t, err)
		if uint64(chain.MaxAccountBlocksInMomentum+1)*constants.AccountBlockBasePlasma > config.MaxBasePlasmaInMomentum {
			t.Fatal("fixture exceeds the plasma budget; the test would not isolate the count rule")
		}

		// The head reaches the momentum verifier (so the bridge's preflight
		// did not refuse it) and is rejected there on its blocks, which
		// carry no plasma, not on their number: a count refusal would be
		// ErrMContentTooBig.
		_, err = bridge.InsertChain(oversizedSideChain(t, z))
		if err == nil {
			t.Fatal("expected the unpaid blocks to be rejected")
		}
		common.Expect(t, verifying.momentums, 1)
		if errors.Is(err, verifier.ErrMContentTooBig) {
			t.Fatalf("count rule refused a momentum within the plasma budget: %v", err)
		}
		t.Logf("rejected past the count rule with: %v", err)
		common.Expect(t, counting.rollbacks, 0)
		expectChainEquals(t, z.Chain(), original)
	})
}
