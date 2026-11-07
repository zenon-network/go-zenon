package pillar

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

func recencyHash(height uint64) types.Hash { return types.Hash{0xac, byte(height)} }
func recencyAt(height uint64) types.HashHeight {
	return types.HashHeight{Hash: recencyHash(height), Height: height}
}

// recencyParent is the part of store.Momentum the acknowledgement
// requirement reads at the parent momentum.
type recencyParent struct {
	store.Momentum
	identifier types.HashHeight
	storage    db.DB
}

func (s *recencyParent) Identifier() types.HashHeight { return s.identifier }
func (s *recencyParent) GetMomentumByHeight(height uint64) (*nom.Momentum, error) {
	return &nom.Momentum{Hash: recencyHash(height), Height: height}, nil
}
func (s *recencyParent) GetAccountStore(types.Address) store.Account {
	return &recencyAccount{storage: s.storage}
}

type recencyAccount struct {
	store.Account
	storage db.DB
}

func (s *recencyAccount) Storage() db.DB { return s.storage }

// recencyCache reports the gate active from activeFrom on.
type recencyCache struct {
	store.Cache
	identifier types.HashHeight
	gate       types.Hash
	activeFrom uint64
}

func (c *recencyCache) IsSporkActive(spork *types.ImplementedSpork) (bool, error) {
	return spork.SporkId == c.gate && c.identifier.Height >= c.activeFrom, nil
}

// The selector is fed the unpruned candidates directly, with prices and
// budgets that admit all of them: a block acknowledging below the gate's
// enforcement height H and the account's dependent block above it, whose
// own acknowledgement passes, plus another account's fresh block. With
// the rule the selector includes neither of the first two; without it
// (the plain selector) it includes all three.
func TestContentSelector_SkipsStaleAcknowledgementAndSuffix(t *testing.T) {
	saved := types.AcknowledgementRecencySpork.SporkId
	gate := types.Hash{0x0a}
	types.AcknowledgementRecencySpork.SporkId = gate
	t.Cleanup(func() { types.AcknowledgementRecencySpork.SporkId = saved })

	const H = 100
	storage := db.NewMemDB()
	(&definition.Spork{Id: gate, Activated: true, EnforcementHeight: H}).Save(storage)
	requirement, err := chain.NewAcknowledgementRequirement(
		&recencyParent{identifier: recencyAt(H), storage: storage},
		func(identifier types.HashHeight) store.Cache {
			return &recencyCache{identifier: identifier, gate: gate, activeFrom: H}
		})
	common.FailIfErr(t, err)
	if requirement == nil {
		t.Fatal("rule must apply at parent H")
	}

	stale, other := types.Address{0, 0xa1}, types.Address{0, 0xa2}
	candidate := func(address types.Address, height, acknowledged uint64) *nom.AccountBlock {
		return &nom.AccountBlock{
			Address:              address,
			BlockType:            nom.BlockTypeUserSend,
			Height:               height,
			MomentumAcknowledged: recencyAt(acknowledged),
			BasePlasma:           21000,
			FusedPlasma:          21000,
		}
	}
	blocks := []*nom.AccountBlock{
		candidate(stale, 1, H-1),
		candidate(stale, 2, H),
		candidate(other, 1, H),
	}
	config := &definition.PlasmaVariables{MaxBasePlasmaInMomentum: 21000 * 10}
	plasma := dp.NewDynamicPlasma(&nom.Momentum{NextFusionPrice: 1000, NextWorkPrice: 1000, Version: 2}, config)

	got := NewMomentumContentSelectorWithAcknowledgement(plasma, requirement).Content(blocks)
	if len(got) != 1 || got[0] != blocks[2] {
		t.Fatalf("selected %d blocks, want only the other account's", len(got))
	}

	common.Expect(t, len(NewMomentumContentSelector(plasma).Content(blocks)), 3)
	common.Expect(t, len(NewMomentumContentSelectorWithAcknowledgement(plasma, nil).Content(blocks)), 3)
}
