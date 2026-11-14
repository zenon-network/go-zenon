package tests

import (
	"errors"
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/chain"
	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// activateAcknowledgementRecency creates and activates DynamicPlasmaSpork,
// which gates the acknowledgement recency rule, on z, points
// DynamicPlasmaSpork at it for the duration of t, and returns its
// EnforcementHeight H without advancing to it. The spork id is the hash of
// the CreateSpork send.
func activateAcknowledgementRecency(t *testing.T, z mock.MockZenon) uint64 {
	saveSporkState(t)

	createSend := z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"dynamic-plasma",
			"Activates Dynamic Plasma",
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	id := createSend.Hash

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data:      definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName, id),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum() // the spork contract receives the activation
	types.DynamicPlasmaSpork.SporkId = id
	types.ImplementedSporksMap[id] = true

	sporks, err := embedded.NewSporkApi(z).GetAll(0, 10)
	common.FailIfErr(t, err)
	for _, spork := range sporks.List {
		if spork.Id == id && spork.Activated {
			return spork.EnforcementHeight
		}
	}
	t.Fatal("dynamic plasma spork is not activated")
	return 0
}

func momentumAtHeight(t *testing.T, z mock.MockZenon, height uint64) *nom.Momentum {
	momentum, err := z.Chain().GetFrontierMomentumStore().GetMomentumByHeight(height)
	common.FailIfErr(t, err)
	if momentum == nil {
		t.Fatalf("no momentum at height %d", height)
	}
	return momentum
}

// signedUserBlock completes template as the next block of key's account
// chain, acknowledging acknowledged, and signs it.
func signedUserBlock(t *testing.T, z mock.MockZenon, key *wallet.KeyPair, template *nom.AccountBlock, acknowledged types.HashHeight) *nom.AccountBlock {
	frontier, err := z.Chain().GetFrontierAccountStore(key.Address).Frontier()
	common.FailIfErr(t, err)
	block := template
	block.Version = 1
	block.ChainIdentifier = z.Chain().ChainIdentifier()
	block.Address = key.Address
	block.Height = frontier.Height + 1
	block.PreviousHash = frontier.Hash
	block.MomentumAcknowledged = acknowledged
	if block.Amount == nil {
		block.Amount = big.NewInt(0)
	}
	// Enough fused plasma to meet the price once Dynamic Plasma is active;
	// nothing here applies the block, so the account's fusion is not read.
	block.FusedPlasma = 100 * constants.AccountBlockBasePlasma
	block.Hash = block.ComputeHash()
	signature, _, publicKey, err := key.Signer(block.Hash.Bytes())
	common.FailIfErr(t, err)
	block.Signature = signature
	block.PublicKey = publicKey
	return block
}

func userSend(t *testing.T, z mock.MockZenon, acknowledged types.HashHeight) *nom.AccountBlock {
	return signedUserBlock(t, z, g.User1, &nom.AccountBlock{
		BlockType:     nom.BlockTypeUserSend,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(1),
	}, acknowledged)
}

// momentumOn is a momentum on parent confirming blocks, enough for the
// momentum verifier's Momentum, which does not check the hash or the
// producer: version 1 while DynamicPlasmaSpork is not active at parent,
// otherwise version 2 with the resource prices the blocks set.
func momentumOn(t *testing.T, z mock.MockZenon, parent *nom.Momentum, blocks ...*nom.AccountBlock) *nom.DetailedMomentum {
	m := &nom.Momentum{
		Version:         1,
		ChainIdentifier: z.Chain().ChainIdentifier(),
		PreviousHash:    parent.Hash,
		Height:          parent.Height + 1,
		TimestampUnix:   parent.TimestampUnix + 10,
		Content:         nom.NewMomentumContent(blocks),
	}
	store := z.Chain().GetMomentumStore(parent.Identifier())
	active, err := store.IsSporkActive(types.DynamicPlasmaSpork)
	common.FailIfErr(t, err)
	if active {
		config, err := store.GetPlasmaVariables()
		common.FailIfErr(t, err)
		plasma := dp.NewDynamicPlasma(parent, config)
		priced := make([]*nom.AccountBlock, 0, len(blocks))
		for _, block := range blocks {
			copied := *block
			if !types.IsEmbeddedAddress(block.Address) {
				copied.BasePlasma, err = vm.CanonicalBasePlasma(z.Chain(), block)
				common.FailIfErr(t, err)
			}
			priced = append(priced, &copied)
		}
		base := plasma.ComputeTotalBasePlasma(priced)
		m.Version = nom.DynamicPlasmaMomentumVersion
		m.NextFusionPrice = plasma.NextFusionPrice(base.Fusion)
		m.NextWorkPrice = plasma.NextWorkPrice(base.Pow)
	}
	m.EnsureCache()
	return &nom.DetailedMomentum{Momentum: m, AccountBlocks: blocks}
}

func expectIs(t *testing.T, what string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("%s: want %v, got %v", what, want, err)
	}
}

// With the gate enforced at H, a user block acknowledging a momentum from
// before H passes admission and the momentum verifier on parent H-1 (the
// momentum at H may still include it) and fails both on parent H; the same
// block acknowledging H passes on parent H.
func TestMomentumContent_StaleAcknowledgementRejectedAfterEnforcement(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	v := verifier.NewVerifier(z.Chain(), z.Consensus(), vm.CanonicalBasePlasma)

	old := frontierMomentum(t, z).Identifier()
	H := activateAcknowledgementRecency(t, z)
	z.InsertMomentumsTo(H - 1)
	stale := userSend(t, z, old)

	// Frontier H-1: not enforced at the parent yet.
	common.FailIfErr(t, v.AccountBlock(stale))
	parentBefore := frontierMomentum(t, z)
	common.FailIfErr(t, v.Momentum(momentumOn(t, z, parentBefore, stale)))

	z.InsertMomentumsTo(H)
	active, err := z.Chain().GetFrontierMomentumStore().IsSporkActive(types.DynamicPlasmaSpork)
	common.FailIfErr(t, err)
	common.ExpectTrue(t, active)

	parent := frontierMomentum(t, z)
	expectIs(t, "admission at H", v.AccountBlock(stale), verifier.ErrABMAStale)
	expectIs(t, "momentum H+1", v.Momentum(momentumOn(t, z, parent, stale)), verifier.ErrMStaleAcknowledgement)
	// Momentum H, on parent H-1, still accepts it.
	common.FailIfErr(t, v.Momentum(momentumOn(t, z, parentBefore, stale)))

	fresh := userSend(t, z, parent.Identifier())
	common.FailIfErr(t, v.AccountBlock(fresh))
	common.FailIfErr(t, v.Momentum(momentumOn(t, z, parent, fresh)))

	// An acknowledgement of H-1 is stale at parent H as well.
	staleByOne := userSend(t, z, parentBefore.Identifier())
	expectIs(t, "admission of H-1", v.AccountBlock(staleByOne), verifier.ErrABMAStale)
}

// The bypass the issue describes: a user receive carrying data whose
// acknowledged momentum predates the Dynamic Plasma spork runs under the
// pre-spork receive-data rule, which reads the spork state at that
// acknowledgement. Until the spork is enforced at the frontier it is
// accepted, as before; once it is enforced the recency rule rejects it, and
// an acknowledgement recent enough to pass makes the receive-data rule
// apply.
func TestMomentumContent_StaleAcknowledgementReceiveWithDataRejected(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	v := verifier.NewVerifier(z.Chain(), z.Consensus(), vm.CanonicalBasePlasma)

	send := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User3.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	preSpork := frontierMomentum(t, z).Identifier()

	receive := func(acknowledged types.HashHeight) *nom.AccountBlock {
		return signedUserBlock(t, z, g.User3, &nom.AccountBlock{
			BlockType:     nom.BlockTypeUserReceive,
			FromBlockHash: send.Hash,
			Data:          []byte("pre-spork data"),
		}, acknowledged)
	}

	H := activateAcknowledgementRecency(t, z)
	z.InsertMomentumsTo(H - 1)
	// Not enforced at the frontier yet: accepted, the pre-spork answer.
	common.FailIfErr(t, v.AccountBlock(receive(preSpork)))

	z.InsertMomentumsTo(H)
	expectIs(t, "stale receive with data", v.AccountBlock(receive(preSpork)), verifier.ErrABMAStale)
	expectIs(t, "fresh receive with data", v.AccountBlock(receive(frontierMomentum(t, z).Identifier())), verifier.ErrABDataMustBeEmpty)
}

// End to end on the real chain: a stale block that sits in the pool after
// H (forced in, as a block queued before H would be) is left out of the
// next momentum by the producer and evicted from the pool when that
// momentum is inserted, together with the account's dependent block above
// it. Another account's block in the same momentum is confirmed.
func TestAccountPool_StaleAcknowledgementEvictedWithSuffix_EndToEnd(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	old := frontierMomentum(t, z).Identifier()
	H := activateAcknowledgementRecency(t, z)
	z.InsertMomentumsTo(H)
	user1Before, err := z.Chain().GetFrontierMomentumStore().GetAccountStore(g.User1.Address).Frontier()
	common.FailIfErr(t, err)

	stale := userSend(t, z, old)
	insert := z.Chain().AcquireInsert("test")
	common.FailIfErr(t, z.Chain().ForceAddAccountBlockTransaction(insert, &nom.AccountBlockTransaction{Block: stale, Changes: db.NewPatch()}))
	insert.Unlock()
	dependent := userSend(t, z, frontierMomentum(t, z).Identifier())
	insert = z.Chain().AcquireInsert("test")
	common.FailIfErr(t, z.Chain().ForceAddAccountBlockTransaction(insert, &nom.AccountBlockTransaction{Block: dependent, Changes: db.NewPatch()}))
	insert.Unlock()
	common.Expect(t, len(z.Chain().GetUncommittedAccountBlocksByAddress(g.User1.Address)), 2)

	other := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     g.User1.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(1),
	}, nil, mock.SkipVmChanges)

	z.InsertNewMomentum()
	frontier := frontierMomentum(t, z)
	common.Expect(t, frontier.Height, H+1)
	common.Expect(t, len(frontier.Content), 1)
	common.Expect(t, frontier.Content[0].Hash, other.Hash)

	common.Expect(t, len(z.Chain().GetUncommittedAccountBlocksByAddress(g.User1.Address)), 0)
	user1After, err := z.Chain().GetFrontierMomentumStore().GetAccountStore(g.User1.Address).Frontier()
	common.FailIfErr(t, err)
	common.Expect(t, user1After.Hash, user1Before.Hash)
}

// A requirement built on the real chain agrees with the verifier: E(P) is
// read from the spork contract and the acknowledgement from the chain
// cache, and the activation momentum's rollback restores the pre-gate
// answer.
func TestMomentumContent_PreActivationParentUnaffected(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	old := frontierMomentum(t, z).Identifier()
	H := activateAcknowledgementRecency(t, z)
	z.InsertMomentumsTo(H)

	requirementAt := func(height uint64) *chain.AcknowledgementRequirement {
		parent := z.Chain().GetMomentumStore(momentumAtHeight(t, z, height).Identifier())
		requirement, err := chain.NewAcknowledgementRequirement(parent, z.Chain().GetCacheStore)
		common.FailIfErr(t, err)
		return requirement
	}
	block := &nom.AccountBlock{Address: g.User1.Address, MomentumAcknowledged: old}
	if requirementAt(H-1) != nil {
		t.Fatal("rule applied on a parent before enforcement")
	}
	expectIs(t, "parent H", requirementAt(H).Check(block), chain.ErrStaleMomentumAcknowledged)

	// Roll back across the enforcement momentum: the frontier parent no
	// longer has the gate enforced.
	beforeH := momentumAtHeight(t, z, H-1).Identifier()
	insert := z.Chain().AcquireInsert("test")
	common.FailIfErr(t, z.Chain().RollbackTo(insert, beforeH))
	common.FailIfErr(t, z.Chain().RollbackCacheTo(insert, beforeH))
	insert.Unlock()
	requirement, err := chain.NewAcknowledgementRequirement(z.Chain().GetFrontierMomentumStore(), z.Chain().GetCacheStore)
	common.FailIfErr(t, err)
	if requirement != nil {
		t.Fatal("rule still applies after rolling back below the enforcement height")
	}
	v := verifier.NewVerifier(z.Chain(), z.Consensus(), vm.CanonicalBasePlasma)
	common.FailIfErr(t, v.AccountBlock(userSend(t, z, old)))
}

// The interleaving an RPC submission can hit: the block is verified while
// the frontier is H-1, a momentum moves the frontier to H, and only then is
// the block handed to the pool under the insert lock. The pool rejects it.
func TestAccountPool_StaleAcknowledgementRejectedAfterFrontierMoves(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	v := verifier.NewVerifier(z.Chain(), z.Consensus(), vm.CanonicalBasePlasma)

	old := frontierMomentum(t, z).Identifier()
	H := activateAcknowledgementRecency(t, z)
	z.InsertMomentumsTo(H - 1)
	stale := userSend(t, z, old)
	common.FailIfErr(t, v.AccountBlock(stale))

	z.InsertMomentumsTo(H)
	insert := z.Chain().AcquireInsert("test")
	err := z.Chain().AddAccountBlockTransaction(insert, &nom.AccountBlockTransaction{Block: stale, Changes: db.NewPatch()})
	insert.Unlock()
	expectIs(t, "pool insert at H", err, chain.ErrStaleMomentumAcknowledged)
	common.Expect(t, len(z.Chain().GetUncommittedAccountBlocksByAddress(g.User1.Address)), 0)
}
