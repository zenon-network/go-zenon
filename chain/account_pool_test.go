package chain

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

type fakeStable struct{}

func (fakeStable) GetStableAccountDB(types.Address) db.DB {
	return db.NewMemDB()
}

// GetFrontierMomentumStore returns nil so the pool's pricing context stays
// empty and higherPriority falls back to the legacy ratio comparator;
// these tests don't exercise dynamic plasma activation.
func (fakeStable) GetFrontierMomentumStore() store.Momentum {
	return nil
}

// A never-committed account has no stable frontier, so its uncommitted block
// count must still be measured against its own frontier height, not skipped.
func TestAccountPool_checkUncommittedBlocksCount_FreshAccount(t *testing.T) {
	address := types.Address{1}
	ap := newAccountPool(fakeStable{})
	manager := ap.getAccountManager(address)

	previousHash := types.ZeroHash
	for height := uint64(1); height <= MaxUncommittedBlocksPerAccount+1; height++ {
		block := &nom.AccountBlock{
			Address:      address,
			BlockType:    nom.BlockTypeUserSend,
			Height:       height,
			PreviousHash: previousHash,
		}
		block.Hash = block.ComputeHash()

		common.FailIfErr(t, manager.Add(&nom.AccountBlockTransaction{
			Block:   block,
			Changes: db.NewPatch(),
		}))
		previousHash = block.Hash
	}

	common.ExpectTrue(t, ap.checkUncommittedBlocksCount(address) != nil)
}

// The uncommitted-block cap must only gate inserts that lengthen the
// pending chain (fast-forward). A same-height replacement (rollback) or
// a resubmission of an already-inserted block must still be accepted
// while the account sits at the cap, since neither grows the
// uncommitted chain.
func TestAccountPool_ReplacementAndDuplicateAcceptedAtCap(t *testing.T) {
	// First byte 0 (types.UserAddrByte) so IsEmbeddedAddress is false and
	// addAccountBlockTransaction's uncommitted-block-count guard actually
	// applies — unlike the sibling FreshAccount test above, this test
	// exercises that guard through addAccountBlockTransaction itself, not
	// just checkUncommittedBlocksCount directly.
	address := types.Address{0, 1}
	ap := newAccountPool(fakeStable{})
	manager := ap.getAccountManager(address)

	previousHash := types.ZeroHash
	var block500 *nom.AccountBlock
	for height := uint64(1); height <= MaxUncommittedBlocksPerAccount; height++ {
		block := &nom.AccountBlock{
			Address:      address,
			BlockType:    nom.BlockTypeUserSend,
			Height:       height,
			PreviousHash: previousHash,
			TotalPlasma:  100,
			BasePlasma:   100,
		}
		block.Hash = block.ComputeHash()
		common.FailIfErr(t, manager.Add(&nom.AccountBlockTransaction{
			Block:   block,
			Changes: db.NewPatch(),
		}))
		previousHash = block.Hash
		block500 = block
	}

	// Sanity: the account is genuinely at the cap for fast-forward inserts.
	common.ExpectTrue(t, ap.checkUncommittedBlocksCount(address) != nil)

	// Resubmitting the already-inserted frontier block must stay
	// idempotent rather than error out because the account is at cap.
	duplicate := &nom.AccountBlockTransaction{Block: block500.Copy(), Changes: db.NewPatch()}
	common.FailIfErr(t, ap.addAccountBlockTransaction(duplicate, false))

	// A same-height, higher-priced replacement (distinct hash via Data,
	// same previous/height as block500) must be accepted through the
	// rollback path: it doesn't grow the uncommitted chain, so the cap
	// must not block it.
	replacement := &nom.AccountBlock{
		Address:      address,
		BlockType:    nom.BlockTypeUserSend,
		Height:       MaxUncommittedBlocksPerAccount,
		PreviousHash: block500.PreviousHash,
		Data:         []byte{1},
		TotalPlasma:  200, // higher TotalPlasma/BasePlasma ratio than block500 wins higherPriority
		BasePlasma:   100,
	}
	replacement.Hash = replacement.ComputeHash()
	common.FailIfErr(t, ap.addAccountBlockTransaction(&nom.AccountBlockTransaction{
		Block:   replacement,
		Changes: db.NewPatch(),
	}, false))

	frontier, err := ap.getFrontierAccountStore(address).Frontier()
	common.FailIfErr(t, err)
	common.ExpectTrue(t, frontier != nil && frontier.Identifier() == replacement.Identifier())
}

func TestAccountPool_filterBlocksToCommit(t *testing.T) {
	ap := accountPool{}
	MaxAccountBlocksInMomentum = 2
	common.Expect(t, len(ap.filterBlocksToCommit([]*nom.AccountBlock{
		{Height: 1, BlockType: nom.BlockTypeUserSend},
		{Height: 2, BlockType: nom.BlockTypeUserSend},
		{Height: 3, BlockType: nom.BlockTypeUserSend},
	})), 2)

	common.Expect(t, len(ap.filterBlocksToCommit([]*nom.AccountBlock{
		{Height: 1, BlockType: nom.BlockTypeContractSend},
		{Height: 2, BlockType: nom.BlockTypeContractSend},
		{Height: 3, BlockType: nom.BlockTypeUserReceive},
	})), 0)
}

type fakeAccountManagerDB struct {
	frontier types.HashHeight
	afterPop types.HashHeight
}

func (m *fakeAccountManagerDB) Frontier() db.DB {
	mem := db.NewMemDB()
	common.DealWithErr(db.SetFrontier(mem, m.frontier, []byte{1}))
	return mem
}

func (m *fakeAccountManagerDB) Get(types.HashHeight) db.DB {
	return nil
}

func (m *fakeAccountManagerDB) GetPatch(types.HashHeight) db.Patch {
	return nil
}

func (m *fakeAccountManagerDB) Add(db.Transaction) error {
	return nil
}

func (m *fakeAccountManagerDB) Pop() error {
	m.frontier = m.afterPop
	return nil
}

func (m *fakeAccountManagerDB) Stop() error {
	return nil
}

func (m *fakeAccountManagerDB) Location() string {
	return "fake"
}

func TestAccountManagerPopDeletesRolledBackBlockRange(t *testing.T) {
	manager := &fakeAccountManagerDB{
		frontier: testHashHeight(4),
		afterPop: testHashHeight(1),
	}
	account := &accountManager{
		db: manager,
		blocks: map[uint64]*nom.AccountBlock{
			1: {Height: 1},
			2: {Height: 2},
			3: {Height: 3},
			4: {Height: 4},
		},
	}

	common.FailIfErr(t, account.Pop())

	if _, ok := account.blocks[1]; !ok {
		t.Fatalf("expected height 1 to remain cached")
	}
	for _, height := range []uint64{2, 3, 4} {
		if _, ok := account.blocks[height]; ok {
			t.Fatalf("expected height %d to be deleted", height)
		}
	}
}

func testHashHeight(height uint64) types.HashHeight {
	return types.HashHeight{
		Hash:   types.NewHash(common.Uint64ToBytes(height)),
		Height: height,
	}
}

func TestHigherPricedBlock_TieBreaksOnSmallestHash(t *testing.T) {
	plasma := dp.NewDynamicPlasma(&nom.Momentum{Version: 2, NextFusionPrice: 1000, NextWorkPrice: 1000}, &definition.PlasmaVariables{})

	smallerHash := &nom.AccountBlock{BasePlasma: 21000, FusedPlasma: 21000}
	smallerHash.Hash = types.Hash{0}
	largerHash := &nom.AccountBlock{BasePlasma: 21000, FusedPlasma: 21000}
	largerHash.Hash = types.Hash{1}

	// Smaller hash wins the tie: a replaces b.
	common.FailIfErr(t, higherPricedBlock(plasma, smallerHash, largerHash))

	// Larger hash loses the tie: a does not replace b.
	common.ExpectError(t, higherPricedBlock(plasma, largerHash, smallerHash), ErrHashTieBreak)

	// An unambiguously cheaper block passes the price result through
	// unchanged; the tie-break never fires.
	cheaper := &nom.AccountBlock{BasePlasma: 21000, FusedPlasma: 100}
	common.ExpectError(t, higherPricedBlock(plasma, cheaper, largerHash), dp.ErrBlockPriceWorse)
}

// TestAccountPool_higherPriorityUsesCachedDynamicPlasma verifies that
// higherPriority is driven by the pool's cached pricing context, not by a
// fresh store read: fakeStable's store is nil, yet the price comparator
// still runs while the cache is set, and stops running once it is cleared.
func TestAccountPool_higherPriorityUsesCachedDynamicPlasma(t *testing.T) {
	ap := newAccountPool(fakeStable{}) // its store is nil: no store answer available
	ap.plasma = dp.NewDynamicPlasma(
		&nom.Momentum{Version: 2, NextFusionPrice: 1000, NextWorkPrice: 1000},
		&definition.PlasmaVariables{},
	)

	cheap := &nom.AccountBlock{BasePlasma: 21000, FusedPlasma: 100}
	cheap.Hash = types.Hash{1}
	rich := &nom.AccountBlock{BasePlasma: 21000, FusedPlasma: 21000}
	rich.Hash = types.Hash{0}

	// With the cache set, the price comparator ran even though fakeStable
	// has no store.
	common.ExpectError(t, ap.higherPriority(cheap, rich), dp.ErrBlockPriceWorse)

	// With the cache cleared, both blocks have TotalPlasma == 0, so the
	// legacy branch's ratio comparison is an equality and resolves on the
	// hash tie-break, which cheap.Hash{1} > rich.Hash{0} makes
	// deterministic.
	ap.plasma = nil
	common.ExpectError(t, ap.higherPriority(cheap, rich), ErrHashTieBreak)
}
