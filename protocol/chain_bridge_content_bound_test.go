package protocol_test

import (
	"math/big"
	"strings"
	"testing"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// These tests pin where the block count of an imported momentum is decided
// and which rule applies. Before dynamic plasma a momentum may list at most
// chain.MaxAccountBlocksInMomentum blocks; once the spork is active for the
// chain the momentum extends, a plasma budget applies instead and allows
// more. The rule lives in verifier.MaxMomentumContent; the bridge applies it
// before any block of a momentum reaches the pool, and the momentum verifier
// applies it again.
//
// The legacy count is lowered to two for the tests that need a real momentum
// above it, the way chain's own tests do, so that "above the legacy count"
// needs three blocks rather than a hundred and one.

// lowerLegacyCount sets the legacy per-momentum block count for the test.
func lowerLegacyCount(t *testing.T, count int) {
	t.Helper()
	saved := chain.MaxAccountBlocksInMomentum
	chain.MaxAccountBlocksInMomentum = count
	t.Cleanup(func() { chain.MaxAccountBlocksInMomentum = saved })
}

// activateDynamicPlasma creates and activates the dynamic-plasma spork on the
// mock chain and advances to top, past its enforcement, restoring the
// process-wide spork registration when the test ends.
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

// enforcementHeight returns the first height at which the chain reports the
// dynamic-plasma spork active, so a momentum extending that height is judged
// by the plasma budget and one extending the height below by the legacy count.
func enforcementHeight(t *testing.T, z mock.MockZenon) uint64 {
	t.Helper()
	top := z.Chain().GetFrontierMomentumStore().Identifier().Height
	for height := uint64(2); height <= top; height++ {
		store := z.Chain().GetMomentumStore(momentumAt(t, z.Chain(), height).Identifier())
		active, err := store.IsSporkActive(types.DynamicPlasmaSpork)
		common.FailIfErr(t, err)
		if active {
			return height
		}
	}
	t.Fatal("dynamic plasma never became active")
	return 0
}

// setPlasmaBudget lets the genesis spork address lower the per-momentum
// plasma budget to its minimum and returns the variables in force afterwards.
func setPlasmaBudget(t *testing.T, z mock.MockZenon) *definition.PlasmaVariables {
	t.Helper()

	call := z.CallContract(&nom.AccountBlock{
		Address:       g.Spork.Address,
		ToAddress:     types.PlasmaContract,
		TokenStandard: types.ZeroTokenStandard,
		Amount:        common.Big0,
		Data: definition.ABIPlasma.PackMethodPanic(definition.SetVariablesMethodName,
			definition.MaxBasePlasmaInMomentumLowerLimit,
			definition.MaxBasePlasmaInMomentumLowerLimit/2,
			definition.MaxBasePlasmaInMomentumLowerLimit/2,
			uint8(10), uint8(20)),
	})
	z.InsertMomentumsTo(z.Chain().GetFrontierMomentumStore().Identifier().Height + 2)
	call.Error(t, nil)

	config, err := z.Chain().GetFrontierMomentumStore().GetPlasmaVariables()
	common.FailIfErr(t, err)
	common.Expect(t, config.MaxBasePlasmaInMomentum, definition.MaxBasePlasmaInMomentumLowerLimit)
	return config
}

// contentBlocks returns n distinct user blocks that carry no plasma and a
// content listing them, with the blocks in content order.
func contentBlocks(n int) (nom.MomentumContent, []*nom.AccountBlock) {
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
	content := nom.NewMomentumContent(blocks)
	byIdentifier := make(map[types.HashHeight]*nom.AccountBlock, n)
	for _, block := range blocks {
		byIdentifier[block.Identifier()] = block
	}
	ordered := make([]*nom.AccountBlock, n)
	for i, header := range content {
		ordered[i] = byIdentifier[header.Identifier()]
	}
	return content, ordered
}

// momentumVersionFor returns the version and prices a momentum extending
// target must carry to get past the verifier's header checks: 2 with the
// minimum prices once dynamic plasma is active at target, 1 before.
func momentumVersionFor(t *testing.T, z mock.MockZenon, target *nom.Momentum) (version, fusionPrice, workPrice uint64) {
	t.Helper()
	active, err := z.Chain().GetMomentumStore(target.Identifier()).IsSporkActive(types.DynamicPlasmaSpork)
	common.FailIfErr(t, err)
	if active {
		return nom.DynamicPlasmaMomentumVersion, dp.MinResourcePrice, dp.MinResourcePrice
	}
	return 1, 0, 0
}

// sideChainListing builds a side chain whose head extends target, is signed
// by the elected producer and lists n unpaid blocks, with a tail that keeps
// the side chain taller than the frontier. The head's blocks are in content
// order so the bridge's structural preflight passes and the count is judged
// against the chain at target.
func sideChainListing(t *testing.T, z mock.MockZenon, target *nom.Momentum, n int) []*nom.DetailedMomentum {
	t.Helper()
	frontier := z.Chain().GetFrontierMomentumStore().Identifier().Height
	content, blocks := contentBlocks(n)
	head := unsignedMomentum(target, target.Height+1)
	head.Version, head.NextFusionPrice, head.NextWorkPrice = momentumVersionFor(t, z, target)
	head.Content = content
	head = signedBy(head, electedKey(t, z, *head.Timestamp))
	return append([]*nom.DetailedMomentum{{Momentum: head, AccountBlocks: blocks}}, invalidTail(head, frontier+1)...)
}

// extendingListing builds an unsigned momentum that extends the frontier and
// lists n unpaid blocks in content order.
func extendingListing(t *testing.T, z mock.MockZenon, n int) *nom.DetailedMomentum {
	t.Helper()
	frontier := momentumAt(t, z.Chain(), z.Chain().GetFrontierMomentumStore().Identifier().Height)
	content, blocks := contentBlocks(n)
	next := unsignedMomentum(frontier, frontier.Height+1)
	next.Version, next.NextFusionPrice, next.NextWorkPrice = momentumVersionFor(t, z, frontier)
	next.Content = content
	next.Hash = next.ComputeHash()
	return &nom.DetailedMomentum{Momentum: next, AccountBlocks: blocks}
}

// frontierDetailed returns the frontier momentum with its blocks.
func frontierDetailed(t *testing.T, z mock.MockZenon) *nom.DetailedMomentum {
	t.Helper()
	store := z.Chain().GetFrontierMomentumStore()
	momentum, err := store.GetFrontierMomentum()
	common.FailIfErr(t, err)
	detailed, err := store.PrefetchMomentum(momentum)
	common.FailIfErr(t, err)
	return detailed
}

// rollBackFrontier takes the frontier momentum off the canonical chain and
// the cache, so it can be imported through the bridge the way a
// peer-supplied momentum is. The rollback drops its blocks from the pool.
func rollBackFrontier(t *testing.T, z mock.MockZenon, momentum *nom.Momentum) {
	t.Helper()
	common.Expect(t, z.Chain().GetFrontierMomentumStore().Identifier(), momentum.Identifier())
	insert := z.Chain().AcquireInsert("test rollback")
	common.FailIfErr(t, z.Chain().RollbackTo(insert, momentum.Previous()))
	common.FailIfErr(t, z.Chain().RollbackCacheTo(insert, momentum.Previous()))
	insert.Unlock()
	common.Expect(t, z.Chain().GetFrontierMomentumStore().Identifier(), momentum.Previous())
	common.Expect(t, z.Chain().GetFrontierCacheStore().Identifier(), momentum.Previous())
	common.Expect(t, len(z.Chain().GetAllUncommittedAccountBlocks()), 0)
}

// pendingListing builds an unsigned momentum that extends the frontier and
// lists the given pending blocks in content order. Its header is well formed
// for the chain it extends, so the verifier reaches its content.
func pendingListing(t *testing.T, z mock.MockZenon, blocks []*nom.AccountBlock) *nom.DetailedMomentum {
	t.Helper()
	frontier := momentumAt(t, z.Chain(), z.Chain().GetFrontierMomentumStore().Identifier().Height)
	next := unsignedMomentum(frontier, frontier.Height+1)
	next.Version, next.NextFusionPrice, next.NextWorkPrice = momentumVersionFor(t, z, frontier)
	next.Content = nom.NewMomentumContent(blocks)
	next.Hash = next.ComputeHash()
	byIdentifier := make(map[types.HashHeight]*nom.AccountBlock, len(blocks))
	for _, block := range blocks {
		byIdentifier[block.Identifier()] = block
	}
	ordered := make([]*nom.AccountBlock, len(blocks))
	for i, header := range next.Content {
		ordered[i] = byIdentifier[header.Identifier()]
	}
	return &nom.DetailedMomentum{Momentum: next, AccountBlocks: ordered}
}

// paidSends has User1 send n units to User2 in n separate blocks, each paid
// from fused plasma, and returns them.
func paidSends(z mock.MockZenon, n int) []*nom.AccountBlock {
	sends := make([]*nom.AccountBlock, n)
	for i := range sends {
		sends[i] = z.InsertSendBlock(&nom.AccountBlock{
			Address:       g.User1.Address,
			ToAddress:     g.User2.Address,
			TokenStandard: types.ZnnTokenStandard,
			Amount:        big.NewInt(1),
		}, nil, mock.SkipVmChanges)
	}
	return sends
}

// expectImported checks that a momentum produced on this chain, rolled back
// and imported again, left the chain, the cache and the pool as production
// had, with every one of its blocks in the chain.
func expectImported(t *testing.T, z mock.MockZenon, produced chainSnapshot, blocks []*nom.AccountBlock) {
	t.Helper()
	expectChainEquals(t, z.Chain(), produced)
	common.Expect(t, len(z.Chain().GetAllUncommittedAccountBlocks()), 0)
	for _, block := range blocks {
		stored, err := z.Chain().GetFrontierAccountStore(block.Address).ByHeight(block.Height)
		common.FailIfErr(t, err)
		if stored == nil || !stored.EqualBytes(block) {
			t.Fatalf("block %v is not in the chain as produced", block.Header())
		}
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

// countingBridge wires a bridge whose rollbacks, pool force-adds and
// momentum verifications are counted.
type countingBridge struct {
	chain    *rollbackCountingChain
	pool     *snapshotCountingChain
	verifier *countingVerifier
	bridge   protocol.ChainBridge
}

func newCountingBridge(z mock.MockZenon) *countingBridge {
	pool := &snapshotCountingChain{Chain: z.Chain()}
	rollbacks := &rollbackCountingChain{Chain: pool}
	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	verifying := &countingVerifier{Verifier: verifier.NewVerifier(rollbacks, z.Consensus(), vm.CanonicalBasePlasma)}
	return &countingBridge{
		chain:    rollbacks,
		pool:     pool,
		verifier: verifying,
		bridge:   protocol.NewChainBridge(rollbacks, z.Consensus(), verifying, supervisor),
	}
}

// expectUntouched checks that a refused momentum left the canonical chain,
// the cache and the pool as they were.
func expectUntouched(t *testing.T, z mock.MockZenon, original chainSnapshot, b *countingBridge) {
	t.Helper()
	expectChainEquals(t, z.Chain(), original)
	common.Expect(t, b.chain.rollbacks, 0)
	common.Expect(t, len(b.pool.forceAdded), 0)
	common.Expect(t, len(z.Chain().GetAllUncommittedAccountBlocks()), 0)
}

// expectRefusedOnCount checks that the momentum was refused by the count
// rule and nothing else.
func expectRefusedOnCount(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, verifier.ErrMContentTooBig) {
		t.Fatalf("expected %v, got %v", verifier.ErrMContentTooBig, err)
	}
}

// expectRefusedOnBlocks checks that the momentum got past the count rule and
// was refused on its blocks, which carry no plasma. The message is the
// verifier's dynamic-plasma price check; it has no sentinel.
func expectRefusedOnBlocks(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected the unpaid blocks to be refused")
	}
	if errors.Is(err, verifier.ErrMContentTooBig) {
		t.Fatalf("refused on count, expected refusal on the blocks: %v", err)
	}
	if !strings.Contains(err.Error(), "block price is too small") {
		t.Fatalf("expected the price check to refuse the blocks, got %v", err)
	}
}

// TestInsertChain_ImportsMomentumAboveLegacyCountUnderDynamicPlasma imports a
// real producer-built momentum that lists more blocks than the legacy count
// allows, which the plasma budget permits: it must land, with its blocks in
// the chain and nothing left pending.
func TestInsertChain_ImportsMomentumAboveLegacyCountUnderDynamicPlasma(t *testing.T) {
	lowerLegacyCount(t, 2)
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activateDynamicPlasma(t, z, 30)

	// Three paid sends from one account go into one momentum.
	sends := paidSends(z, 3)
	z.InsertNewMomentum()
	detailed := frontierDetailed(t, z)
	common.Expect(t, len(detailed.AccountBlocks), len(sends))
	if len(detailed.AccountBlocks) <= chain.MaxAccountBlocksInMomentum {
		t.Fatal("the momentum does not exceed the legacy count")
	}
	produced := snapshotChain(t, z.Chain())
	rollBackFrontier(t, z, detailed.Momentum)

	// On the extending path the momentum is verified inside the supervisor,
	// so the bridge's own verifier is not consulted; the import is judged by
	// its outcome: the blocks are applied again and the momentum lands.
	b := newCountingBridge(z)
	_, err := b.bridge.InsertChain([]*nom.DetailedMomentum{detailed})
	common.FailIfErr(t, err)
	common.Expect(t, len(b.pool.forceAdded), len(sends))
	expectImported(t, z, produced, sends)
}

// TestInsertChain_ContentCountFollowsActivationOfTheChainExtended pins that
// the count rule is chosen by the chain a momentum extends, not by the
// frontier: with the frontier past activation, a side-chain head extending
// the last pre-activation momentum is still held to the legacy count, and
// one extending the first post-activation momentum to the plasma budget.
// Either way the head is judged before any rollback. The extending path
// after activation is covered by the successful import above.
func TestInsertChain_ContentCountFollowsActivationOfTheChainExtended(t *testing.T) {
	lowerLegacyCount(t, 2)
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activateDynamicPlasma(t, z, 30)
	enforced := enforcementHeight(t, z)
	if enforced < 3 || 30-(enforced-1) > 30 {
		t.Fatalf("enforcement height %d is outside the side-chain window", enforced)
	}
	original := snapshotChain(t, z.Chain())
	n := chain.MaxAccountBlocksInMomentum + 1

	t.Run("extending the last pre-activation momentum: legacy count", func(t *testing.T) {
		b := newCountingBridge(z)
		_, err := b.bridge.InsertChain(sideChainListing(t, z, momentumAt(t, z.Chain(), enforced-1), n))
		expectRefusedOnCount(t, err)
		common.Expect(t, b.verifier.momentums, 1)
		expectUntouched(t, z, original, b)
	})

	t.Run("extending the first post-activation momentum: plasma budget", func(t *testing.T) {
		b := newCountingBridge(z)
		_, err := b.bridge.InsertChain(sideChainListing(t, z, momentumAt(t, z.Chain(), enforced), n))
		expectRefusedOnBlocks(t, err)
		common.Expect(t, b.verifier.momentums, 1)
		expectUntouched(t, z, original, b)
	})
}

// TestInsertChain_ContentCountAtThePlasmaBudget pins both boundaries the
// plasma budget sets once dynamic plasma is active, with the budget lowered
// to its minimum through governance. The count ceiling (the user blocks the
// budget pays for plus the contract-block cap) is what the count rule
// checks: a listing at it passes the rule, one block more is refused on
// count, on the side-chain path (verifier before rollback) and on the
// extending path (bridge before any pool work). The plasma budget itself is
// what the verifier sums: a momentum of paid sends that spends exactly the
// budget imports, one more paid send below the count ceiling is refused.
func TestInsertChain_ContentCountAtThePlasmaBudget(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activateDynamicPlasma(t, z, 30)
	config := setPlasmaBudget(t, z)
	ceiling := int(config.MaxBasePlasmaInMomentum/constants.AccountBlockBasePlasma + dp.MaxContractBlocks(config))
	paidBlocks := int(config.MaxBasePlasmaInMomentum / constants.AccountBlockBasePlasma)
	if ceiling >= chain.MaxAccountBlocksInMomentum || paidBlocks+1 >= ceiling {
		t.Fatalf("ceiling %d and paid-block budget %d do not isolate the plasma rules", ceiling, paidBlocks)
	}
	maxContent, err := verifier.MaxMomentumContent(z.Chain().GetFrontierMomentumStore())
	common.FailIfErr(t, err)
	common.Expect(t, int(maxContent), ceiling)

	// The side chain forks two below the frontier; those momentums must
	// already carry the lowered budget, since the head is judged against
	// the chain at its target.
	frontier := z.Chain().GetFrontierMomentumStore().Identifier().Height
	z.InsertMomentumsTo(frontier + 2)
	original := snapshotChain(t, z.Chain())
	target := momentumAt(t, z.Chain(), frontier)
	atTarget, err := z.Chain().GetMomentumStore(target.Identifier()).GetPlasmaVariables()
	common.FailIfErr(t, err)
	common.Expect(t, atTarget.MaxBasePlasmaInMomentum, config.MaxBasePlasmaInMomentum)

	t.Run("side chain at the count ceiling passes the count rule", func(t *testing.T) {
		b := newCountingBridge(z)
		_, err := b.bridge.InsertChain(sideChainListing(t, z, target, ceiling))
		expectRefusedOnBlocks(t, err)
		common.Expect(t, b.verifier.momentums, 1)
		expectUntouched(t, z, original, b)
	})

	t.Run("side chain one over the count ceiling is refused on count", func(t *testing.T) {
		b := newCountingBridge(z)
		_, err := b.bridge.InsertChain(sideChainListing(t, z, target, ceiling+1))
		expectRefusedOnCount(t, err)
		common.Expect(t, b.verifier.momentums, 1)
		expectUntouched(t, z, original, b)
	})

	t.Run("extending momentum one over the count ceiling is refused before pool work", func(t *testing.T) {
		b := newCountingBridge(z)
		_, err := b.bridge.InsertChain([]*nom.DetailedMomentum{extendingListing(t, z, ceiling+1)})
		expectRefusedOnCount(t, err)
		common.Expect(t, b.verifier.momentums, 0)
		expectUntouched(t, z, original, b)
	})

	// The two plasma-budget cases share pending sends and run in order: the
	// mock prices a template at the minimum, which a full momentum raises,
	// so the over-budget listing is tried first, while every pending send
	// is still priced for the chain it acknowledges, and the producer's
	// next momentum, which takes exactly the sends the budget pays for, is
	// the one imported afterwards.
	sends := paidSends(z, paidBlocks+1)

	t.Run("momentum one paid send over the plasma budget is refused", func(t *testing.T) {
		// The sends are pending in the pool, so the bridge skips applying
		// them and the momentum reaches the verifier, which sums their
		// plasma. The count ceiling is not reached.
		before := snapshotChain(t, z.Chain())
		common.Expect(t, len(z.Chain().GetAllUncommittedAccountBlocks()), len(sends))

		b := newCountingBridge(z)
		_, err := b.bridge.InsertChain([]*nom.DetailedMomentum{pendingListing(t, z, sends)})
		expectRefusedOnCount(t, err)
		expectChainEquals(t, z.Chain(), before)
		common.Expect(t, b.chain.rollbacks, 0)
		// The pool is as it was: the pending sends survive the refusal.
		common.Expect(t, len(z.Chain().GetAllUncommittedAccountBlocks()), len(sends))
		for _, send := range sends {
			stored, err := z.Chain().GetFrontierAccountStore(send.Address).ByHeight(send.Height)
			common.FailIfErr(t, err)
			if stored == nil || !stored.EqualBytes(send) {
				t.Fatalf("pending send %v was lost or changed by the refused import", send.Header())
			}
		}
	})

	t.Run("momentum spending exactly the plasma budget imports", func(t *testing.T) {
		// The producer, held to the same budget, takes the sends that fit.
		z.InsertNewMomentum()
		detailed := frontierDetailed(t, z)
		common.Expect(t, len(detailed.AccountBlocks), paidBlocks)
		spent := uint64(0)
		for _, block := range detailed.AccountBlocks {
			spent += block.BasePlasma
		}
		common.Expect(t, spent, config.MaxBasePlasmaInMomentum)
		produced := snapshotChain(t, z.Chain())
		rollBackFrontier(t, z, detailed.Momentum)

		b := newCountingBridge(z)
		_, err := b.bridge.InsertChain([]*nom.DetailedMomentum{detailed})
		common.FailIfErr(t, err)
		common.Expect(t, len(b.pool.forceAdded), paidBlocks)
		expectImported(t, z, produced, detailed.AccountBlocks)
	})
}

// TestInsertChain_OversizedContentIsRefusedBeforePoolWork pins that under
// legacy rules, on the extending path where the momentum verifier only runs
// after the blocks were force-added, the count rule is applied first: an
// oversized momentum costs no VM work and leaves no block in the pool.
func TestInsertChain_OversizedContentIsRefusedBeforePoolWork(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(10)
	original := snapshotChain(t, z.Chain())
	b := newCountingBridge(z)

	_, err := b.bridge.InsertChain([]*nom.DetailedMomentum{extendingListing(t, z, chain.MaxAccountBlocksInMomentum+1)})
	expectRefusedOnCount(t, err)
	common.Expect(t, b.verifier.momentums, 0)
	common.Expect(t, b.pool.snapshots, 0)
	common.Expect(t, b.pool.restores, 0)
	expectUntouched(t, z, original, b)
}

// TestInsertChain_OversizedSideChainIsRefusedBeforeRollback pins the legacy
// count on the side-chain path: a producer-signed head listing one block
// more than the legacy count is refused by the momentum verifier before any
// rollback.
func TestInsertChain_OversizedSideChainIsRefusedBeforeRollback(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(10)
	original := snapshotChain(t, z.Chain())
	b := newCountingBridge(z)

	target := momentumAt(t, z.Chain(), 8)
	_, err := b.bridge.InsertChain(sideChainListing(t, z, target, chain.MaxAccountBlocksInMomentum+1))
	expectRefusedOnCount(t, err)
	common.Expect(t, b.verifier.momentums, 1)
	common.Expect(t, b.pool.snapshots, 0)
	common.Expect(t, b.pool.restores, 0)
	expectUntouched(t, z, original, b)
}
