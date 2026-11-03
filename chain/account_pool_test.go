package chain

import (
	"strings"
	"testing"

	"github.com/inconshreveable/log15"

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
	setMaxAccountBlocksInMomentum(t, 2)
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

func (m *fakeAccountManagerDB) Rebase(db.DB) error { return nil }

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

// TestAccountPool_MomentumEventsRefreshDynamicPlasma verifies that
// InsertMomentum and DeleteMomentum both refresh the pool's cached pricing
// context by calling refreshDynamicPlasma, not just that higherPriority
// consults the cache once it is populated.
func TestAccountPool_MomentumEventsRefreshDynamicPlasma(t *testing.T) {
	ap := newAccountPool(fakeStable{}) // nil store: any refresh must clear the cache
	stale := dp.NewDynamicPlasma(&nom.Momentum{Version: 2, NextFusionPrice: 1000, NextWorkPrice: 1000}, &definition.PlasmaVariables{})

	ap.plasma = stale
	ap.InsertMomentum(&nom.DetailedMomentum{Momentum: &nom.Momentum{}})
	common.Expect(t, ap.plasma == nil, true)

	ap.plasma = stale
	ap.DeleteMomentum(nil)
	common.Expect(t, ap.plasma == nil, true)
}

// DeleteMomentum must only evict managers for addresses whose blocks were in
// the deleted momentum, not wipe the entire pool.  A full wipe discards
// pending blocks that are unrelated to the rollback and leaves the
// subsequent rebuild (which iterates ap.managers) with nothing to do.
func TestAccountPool_DeleteMomentumOnlyEvictsTouchedAddresses(t *testing.T) {
	ap := newAccountPool(fakeStable{})

	addrA := types.Address{0, 1}
	addrB := types.Address{0, 2}
	addrC := types.Address{0, 3}

	// Seed three managers by inserting one block each.
	for _, addr := range []types.Address{addrA, addrB, addrC} {
		manager := ap.getAccountManager(addr)
		block := &nom.AccountBlock{
			Address:      addr,
			BlockType:    nom.BlockTypeUserSend,
			Height:       1,
			PreviousHash: types.ZeroHash,
		}
		block.Hash = block.ComputeHash()
		common.FailIfErr(t, manager.Add(&nom.AccountBlockTransaction{
			Block:   block,
			Changes: db.NewPatch(),
		}))
	}

	common.Expect(t, len(ap.managers), 3)

	// Delete a momentum that carried blocks for addrA and addrC only.
	ap.DeleteMomentum(&nom.DetailedMomentum{
		Momentum: &nom.Momentum{},
		AccountBlocks: []*nom.AccountBlock{
			{Address: addrA},
			{Address: addrC},
		},
	})

	common.Expect(t, len(ap.managers), 1)
	if _, ok := ap.managers[addrB]; !ok {
		t.Fatal("addrB manager was evicted but its blocks were not in the deleted momentum")
	}
}

// A momentum with no account blocks must not evict any manager.
func TestAccountPool_DeleteMomentumEmptyMomentumKeepsAll(t *testing.T) {
	ap := newAccountPool(fakeStable{})

	addrA := types.Address{0, 1}
	manager := ap.getAccountManager(addrA)
	block := &nom.AccountBlock{
		Address:      addrA,
		BlockType:    nom.BlockTypeUserSend,
		Height:       1,
		PreviousHash: types.ZeroHash,
	}
	block.Hash = block.ComputeHash()
	common.FailIfErr(t, manager.Add(&nom.AccountBlockTransaction{
		Block:   block,
		Changes: db.NewPatch(),
	}))

	ap.DeleteMomentum(&nom.DetailedMomentum{
		Momentum:      &nom.Momentum{},
		AccountBlocks: nil,
	})

	common.Expect(t, len(ap.managers), 1)
}

// Nil detailed (as the plasma-refresh test uses) must not panic and must not
// evict any manager.
func TestAccountPool_DeleteMomentumNilDetailedKeepsAll(t *testing.T) {
	ap := newAccountPool(fakeStable{})

	addrA := types.Address{0, 1}
	ap.getAccountManager(addrA)

	ap.DeleteMomentum(nil)

	common.Expect(t, len(ap.managers), 1)
}

// DeleteMomentum must evict managers holding pending receives whose
// from-block (send) was in the deleted momentum.  Such receives are
// orphaned — their from-block no longer exists on the committed chain —
// and would cause "Can't find from-block in store" during the next
// momentum production, stalling the pillar.
func TestAccountPool_DeleteMomentumEvictsOrphanedReceives(t *testing.T) {
	ap := newAccountPool(fakeStable{})

	sender := types.Address{0, 1}
	receiver := types.Address{0, 2}
	bystander := types.Address{0, 3}

	// Sender has a pending send.
	senderManager := ap.getAccountManager(sender)
	sendBlock := &nom.AccountBlock{
		Address:      sender,
		BlockType:    nom.BlockTypeUserSend,
		Height:       1,
		PreviousHash: types.ZeroHash,
		ToAddress:    receiver,
	}
	sendBlock.Hash = sendBlock.ComputeHash()
	common.FailIfErr(t, senderManager.Add(&nom.AccountBlockTransaction{
		Block:   sendBlock,
		Changes: db.NewPatch(),
	}))

	// Receiver has a pending receive of that send.
	receiverManager := ap.getAccountManager(receiver)
	receiveBlock := &nom.AccountBlock{
		Address:       receiver,
		BlockType:     nom.BlockTypeUserReceive,
		Height:        1,
		PreviousHash:  types.ZeroHash,
		FromBlockHash: sendBlock.Hash,
	}
	receiveBlock.Hash = receiveBlock.ComputeHash()
	common.FailIfErr(t, receiverManager.Add(&nom.AccountBlockTransaction{
		Block:   receiveBlock,
		Changes: db.NewPatch(),
	}))

	// Bystander has an unrelated pending send.
	bystanderManager := ap.getAccountManager(bystander)
	bystanderBlock := &nom.AccountBlock{
		Address:      bystander,
		BlockType:    nom.BlockTypeUserSend,
		Height:       1,
		PreviousHash: types.ZeroHash,
	}
	bystanderBlock.Hash = bystanderBlock.ComputeHash()
	common.FailIfErr(t, bystanderManager.Add(&nom.AccountBlockTransaction{
		Block:   bystanderBlock,
		Changes: db.NewPatch(),
	}))

	common.Expect(t, len(ap.managers), 3)

	// Delete a momentum that carried only the sender's send block.
	ap.DeleteMomentum(&nom.DetailedMomentum{
		Momentum: &nom.Momentum{},
		AccountBlocks: []*nom.AccountBlock{
			sendBlock,
		},
	})

	// Sender evicted (touched), receiver evicted (orphaned receive),
	// bystander survives.
	common.Expect(t, len(ap.managers), 1)
	if _, ok := ap.managers[bystander]; !ok {
		t.Fatal("bystander manager was evicted but its blocks were not in the deleted momentum")
	}
	if _, ok := ap.managers[receiver]; ok {
		t.Fatal("receiver manager was not evicted despite holding a receive of a rolled-back send")
	}
}

// DeleteMomentum must NOT evict managers whose pending receives reference
// sends that were NOT in the deleted momentum.
func TestAccountPool_DeleteMomentumKeepsValidReceives(t *testing.T) {
	ap := newAccountPool(fakeStable{})

	sender := types.Address{0, 1}
	receiver := types.Address{0, 2}

	// Sender has a pending send.
	senderManager := ap.getAccountManager(sender)
	sendBlock := &nom.AccountBlock{
		Address:      sender,
		BlockType:    nom.BlockTypeUserSend,
		Height:       1,
		PreviousHash: types.ZeroHash,
		ToAddress:    receiver,
	}
	sendBlock.Hash = sendBlock.ComputeHash()
	common.FailIfErr(t, senderManager.Add(&nom.AccountBlockTransaction{
		Block:   sendBlock,
		Changes: db.NewPatch(),
	}))

	// Receiver has a pending receive of that send.
	receiverManager := ap.getAccountManager(receiver)
	receiveBlock := &nom.AccountBlock{
		Address:       receiver,
		BlockType:     nom.BlockTypeUserReceive,
		Height:        1,
		PreviousHash:  types.ZeroHash,
		FromBlockHash: sendBlock.Hash,
	}
	receiveBlock.Hash = receiveBlock.ComputeHash()
	common.FailIfErr(t, receiverManager.Add(&nom.AccountBlockTransaction{
		Block:   receiveBlock,
		Changes: db.NewPatch(),
	}))

	common.Expect(t, len(ap.managers), 2)

	// Delete a momentum that carried an unrelated block.
	other := types.Address{0, 9}
	ap.DeleteMomentum(&nom.DetailedMomentum{
		Momentum: &nom.Momentum{},
		AccountBlocks: []*nom.AccountBlock{
			{Address: other, BlockType: nom.BlockTypeUserSend, Hash: types.Hash{9}},
		},
	})

	// Neither sender nor receiver should be evicted.
	common.Expect(t, len(ap.managers), 2)
	if _, ok := ap.managers[receiver]; !ok {
		t.Fatal("receiver manager was evicted but its receive's from-block was not rolled back")
	}
}

// Rebuild must drop blocks whose MomentumAcknowledged is above the new
// frontier (can happen after a deep rollback) and all subsequent blocks
// on the same account chain.  The DB manager must be truncated to match:
// the surviving DB frontier, removal of suffix application writes, and
// successful replacement insertion.
func TestAccountPool_RebuildDropsBlocksAboveFrontier(t *testing.T) {
	ap := newAccountPool(fakeStable{})

	addr := types.Address{0, 1}
	manager := ap.getAccountManager(addr)

	// Insert three blocks: block1 has MA=5 (valid), block2 has MA=10
	// (above the new frontier of 7), block3 builds on block2.
	block1 := &nom.AccountBlock{
		Address:              addr,
		BlockType:            nom.BlockTypeUserSend,
		Height:               1,
		PreviousHash:         types.ZeroHash,
		MomentumAcknowledged: types.HashHeight{Hash: types.Hash{1}, Height: 5},
	}
	block1.Hash = block1.ComputeHash()

	block2 := &nom.AccountBlock{
		Address:              addr,
		BlockType:            nom.BlockTypeUserSend,
		Height:               2,
		PreviousHash:         block1.Hash,
		MomentumAcknowledged: types.HashHeight{Hash: types.Hash{2}, Height: 10},
	}
	block2.Hash = block2.ComputeHash()

	block3 := &nom.AccountBlock{
		Address:              addr,
		BlockType:            nom.BlockTypeUserSend,
		Height:               3,
		PreviousHash:         block2.Hash,
		MomentumAcknowledged: types.HashHeight{Hash: types.Hash{3}, Height: 10},
	}
	block3.Hash = block3.ComputeHash()

	// Use non-empty application patches so DB-level state assertions are
	// meaningful.
	applyState(t, manager, block1, map[string]string{testAppKeyK1: "v1"})
	applyState(t, manager, block2, map[string]string{testAppKeyK2: "v2"})
	applyState(t, manager, block3, map[string]string{testAppKeyShared: "v3"})

	common.Expect(t, len(ap.managers), 1)

	// Simulate a rebuild after rollback: new momentum at height 7.
	// block1 (MA=5) is valid; block2 (MA=10 > 7) must be dropped,
	// and block3 (which builds on block2) must also be dropped.
	ap.InsertMomentum(&nom.DetailedMomentum{
		Momentum: &nom.Momentum{
			Height: 7,
			Hash:   types.Hash{7},
		},
	})

	common.Expect(t, len(ap.managers), 1)
	surviving := ap.managers[addr]
	if surviving == nil {
		t.Fatal("manager should survive rebuild with valid prefix")
	}

	// Only block1 should survive in the blocks map.
	_, err := surviving.BlockByHeight(1)
	common.FailIfErr(t, err)

	if _, err := surviving.BlockByHeight(2); err == nil {
		t.Fatal("block2 should have been dropped (MA above frontier)")
	}
	if _, err := surviving.BlockByHeight(3); err == nil {
		t.Fatal("block3 should have been dropped (builds on dropped block2)")
	}

	// The DB frontier must be truncated to height 1, matching the blocks map.
	frontierID := db.GetFrontierIdentifier(surviving.db.Frontier())
	if frontierID.Height != 1 {
		t.Fatalf("DB frontier height = %d, want 1 — DB manager not truncated to match pruned blocks map",
			frontierID.Height)
	}

	// Suffix application writes must be removed from the DB.
	if got := readValue(t, surviving, testAppKeyK2); got != "" {
		t.Fatalf("%s = %q, want empty — block2 application write still visible after suffix pruning",
			testAppKeyK2, got)
	}
	if got := readValue(t, surviving, testAppKeyShared); got != "" {
		t.Fatalf("%s = %q, want empty — block3 application write still visible after suffix pruning",
			testAppKeyShared, got)
	}

	// Prefix application writes must be retained.
	if got := readValue(t, surviving, testAppKeyK1); got != "v1" {
		t.Fatalf("%s = %q, want %q — prefix application state was lost",
			testAppKeyK1, got, "v1")
	}

	// Replacement insertion on top of the truncated manager must succeed.
	replacement := &nom.AccountBlock{
		Address:              addr,
		BlockType:            nom.BlockTypeUserSend,
		Height:               2,
		PreviousHash:         block1.Hash,
		MomentumAcknowledged: types.HashHeight{Hash: types.Hash{4}, Height: 6},
	}
	replacement.Hash = replacement.ComputeHash()
	applyState(t, surviving, replacement, map[string]string{testAppKeyK2: "v2-new"})

	if got := readValue(t, surviving, testAppKeyK2); got != "v2-new" {
		t.Fatalf("%s = %q after replacement, want %q", testAppKeyK2, got, "v2-new")
	}
	if got := readValue(t, surviving, testAppKeyK1); got != "v1" {
		t.Fatalf("%s = %q after replacement, want %q — prefix state lost",
			testAppKeyK1, got, "v1")
	}
}

// pendingBlock builds a hashed block for the content-selection tests;
// dataLen bytes of Data make it as large on the wire as the test needs.
func pendingBlock(address types.Address, height uint64, blockType uint64, dataLen int) *nom.AccountBlock {
	block := &nom.AccountBlock{
		Address:   address,
		BlockType: blockType,
		Height:    height,
		Data:      make([]byte, dataLen),
	}
	block.Hash = block.ComputeHash()
	return block
}

// setMaxAccountBlocksInMomentum pins the pre-spork block-count cap for one
// test and restores it afterwards, since the cap is a package variable.
func setMaxAccountBlocksInMomentum(t *testing.T, limit int) {
	t.Helper()
	previous := MaxAccountBlocksInMomentum
	MaxAccountBlocksInMomentum = limit
	t.Cleanup(func() { MaxAccountBlocksInMomentum = previous })
}

// captureWarnings replaces the pool's logger with one that records every
// warning message and returns the recorded messages.
func captureWarnings(ap *accountPool) *[]string {
	messages := make([]string, 0)
	logger := log15.New()
	logger.SetHandler(log15.FuncHandler(func(r *log15.Record) error {
		if r.Lvl <= log15.LvlWarn {
			messages = append(messages, r.Msg)
		}
		return nil
	}))
	ap.log = logger
	return &messages
}

func expectSameBlocks(t *testing.T, got, want []*nom.AccountBlock) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("selected %d blocks, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Hash != want[i].Hash {
			t.Fatalf("block %d: selected %v@%d, want %v@%d", i, got[i].Address, got[i].Height, want[i].Address, want[i].Height)
		}
	}
}

// A pending block that is larger than the whole detailed-momentum wire
// budget can never be included by this producer. Before the pre-spork
// selector skipped it, meeting such a block ended selection for that
// momentum, so every pending block the walk had not reached yet was left
// out of it, at a point that may differ between momentums since the walk
// order is the pool's map order.
func TestAccountPool_filterBlocksToCommit_SkipsOversizedBlock(t *testing.T) {
	setMaxAccountBlocksInMomentum(t, 100)
	ap := newAccountPool(fakeStable{})

	a := types.Address{0, 1}
	b := types.Address{0, 2}
	c := types.Address{0, 3}
	oversized := pendingBlock(a, 1, nom.BlockTypeUserReceive, maxDetailedMomentumWireSize)
	b1 := pendingBlock(b, 1, nom.BlockTypeUserSend, 16)
	b2 := pendingBlock(b, 2, nom.BlockTypeUserReceive, 16)
	c1 := pendingBlock(c, 1, nom.BlockTypeUserSend, 16)

	for name, tc := range map[string]struct {
		in, want []*nom.AccountBlock
	}{
		"oversized first":   {[]*nom.AccountBlock{oversized, b1, b2, c1}, []*nom.AccountBlock{b1, b2, c1}},
		"oversized between": {[]*nom.AccountBlock{b1, b2, oversized, c1}, []*nom.AccountBlock{b1, b2, c1}},
		"oversized last":    {[]*nom.AccountBlock{b1, b2, c1, oversized}, []*nom.AccountBlock{b1, b2, c1}},
		"only oversized":    {[]*nom.AccountBlock{oversized}, []*nom.AccountBlock{}},
	} {
		t.Run(name, func(t *testing.T) {
			expectSameBlocks(t, ap.filterBlocksToCommit(tc.in), tc.want)
		})
	}
}

// Momentum content must not have a gap in any account chain, so once a
// block of an account is skipped its later blocks are skipped too, even
// when they are offered after another account; blocks of other accounts
// after it are still selected.
func TestAccountPool_filterBlocksToCommit_SkipsRestOfOversizedAccount(t *testing.T) {
	setMaxAccountBlocksInMomentum(t, 100)
	ap := newAccountPool(fakeStable{})

	a := types.Address{0, 1}
	b := types.Address{0, 2}
	a1 := pendingBlock(a, 1, nom.BlockTypeUserSend, 16)
	a2 := pendingBlock(a, 2, nom.BlockTypeUserReceive, maxDetailedMomentumWireSize)
	a3 := pendingBlock(a, 3, nom.BlockTypeUserSend, 16)
	b1 := pendingBlock(b, 1, nom.BlockTypeUserSend, 16)

	expectSameBlocks(t,
		ap.filterBlocksToCommit([]*nom.AccountBlock{a1, a2, a3, b1}),
		[]*nom.AccountBlock{a1, b1})
	expectSameBlocks(t,
		ap.filterBlocksToCommit([]*nom.AccountBlock{a1, a2, b1, a3}),
		[]*nom.AccountBlock{a1, b1})
}

// A contract batch (the descendant sends a contract receive produced,
// then the receive carrying them) is selected or skipped whole; an
// oversized batch does not end selection either.
func TestAccountPool_filterBlocksToCommit_SkipsOversizedContractBatch(t *testing.T) {
	setMaxAccountBlocksInMomentum(t, 100)
	ap := newAccountPool(fakeStable{})

	embedded := types.PlasmaContract
	user := types.Address{0, 2}
	send1 := pendingBlock(embedded, 1, nom.BlockTypeContractSend, maxDetailedMomentumWireSize)
	send2 := pendingBlock(embedded, 2, nom.BlockTypeContractSend, 16)
	receive := pendingBlock(embedded, 3, nom.BlockTypeContractReceive, 8)
	receive.DescendantBlocks = []*nom.AccountBlock{send1, send2}
	receive.Hash = receive.ComputeHash()
	u1 := pendingBlock(user, 1, nom.BlockTypeUserSend, 16)

	expectSameBlocks(t,
		ap.filterBlocksToCommit([]*nom.AccountBlock{send1, send2, receive, u1}),
		[]*nom.AccountBlock{u1})
}

// A block that fills the budget exactly, by the selector's additive
// estimate (worst-case header, encoded block, content entry), is selected;
// one byte more and it is skipped and reported.
func TestAccountPool_filterBlocksToCommit_BudgetBoundary(t *testing.T) {
	setMaxAccountBlocksInMomentum(t, 100)
	ap := newAccountPool(fakeStable{})
	warnings := captureWarnings(ap)

	a := types.Address{0, 1}
	// Find the data length at which the selector's estimate of the detailed
	// momentum (worst-case header, the block, its content entry) is exactly
	// the budget. The
	// RLP overhead is constant for data lengths of the same width, so one
	// measurement at a nearby length gives the exact length.
	probe := pendingBlock(a, 1, nom.BlockTypeUserReceive, 8*1000*1000)
	total := func(block *nom.AccountBlock) int {
		return worstCaseMomentumHeaderSize() + blockWireSize(block) + contentEntrySize(block)
	}
	exact := pendingBlock(a, 1, nom.BlockTypeUserReceive, len(probe.Data)+maxDetailedMomentumWireSize-total(probe))
	common.Expect(t, total(exact), maxDetailedMomentumWireSize)
	expectSameBlocks(t, ap.filterBlocksToCommit([]*nom.AccountBlock{exact}), []*nom.AccountBlock{exact})
	common.Expect(t, len(*warnings), 0)

	over := pendingBlock(a, 1, nom.BlockTypeUserReceive, len(exact.Data)+1)
	common.Expect(t, total(over), maxDetailedMomentumWireSize+1)
	expectSameBlocks(t, ap.filterBlocksToCommit([]*nom.AccountBlock{over}), []*nom.AccountBlock{})
	common.Expect(t, len(*warnings), 1)

	// Offered behind another account's block, the same block is rejected
	// for the room left; its exact size still shows it can never fit a
	// momentum, so it is still reported, once.
	ap = newAccountPool(fakeStable{})
	warnings = captureWarnings(ap)
	prefix := pendingBlock(types.Address{0, 2}, 1, nom.BlockTypeUserSend, 1024)
	for i := 0; i < 2; i++ {
		expectSameBlocks(t, ap.filterBlocksToCommit([]*nom.AccountBlock{prefix, over}), []*nom.AccountBlock{prefix})
	}
	common.Expect(t, len(*warnings), 1)
}

// A receive whose descendant list holds a nil entry still encodes; the
// raw-size lower bound must not trip over it.
func TestAccountPool_filterBlocksToCommit_NilDescendant(t *testing.T) {
	setMaxAccountBlocksInMomentum(t, 100)
	ap := newAccountPool(fakeStable{})

	receive := pendingBlock(types.PlasmaContract, 1, nom.BlockTypeContractReceive, 8)
	receive.DescendantBlocks = []*nom.AccountBlock{nil}
	if blockWireSize(receive) < 0 {
		t.Skip("a nil descendant no longer encodes")
	}
	expectSameBlocks(t, ap.filterBlocksToCommit([]*nom.AccountBlock{receive}), []*nom.AccountBlock{receive})
}

// When the momentum is simply full, a batch that does not fit the room
// left is skipped for this momentum without a warning, and later batches
// that do fit are still packed; the base selector stopped at the first.
func TestAccountPool_filterBlocksToCommit_FullMomentumPacksLaterBatches(t *testing.T) {
	setMaxAccountBlocksInMomentum(t, 100)
	ap := newAccountPool(fakeStable{})
	warnings := captureWarnings(ap)

	a1 := pendingBlock(types.Address{0, 1}, 1, nom.BlockTypeUserReceive, 5*1024*1024)
	b1 := pendingBlock(types.Address{0, 2}, 1, nom.BlockTypeUserReceive, 4*1024*1024)
	c1 := pendingBlock(types.Address{0, 3}, 1, nom.BlockTypeUserSend, 16)

	expectSameBlocks(t,
		ap.filterBlocksToCommit([]*nom.AccountBlock{a1, b1, c1}),
		[]*nom.AccountBlock{a1, c1})
	common.Expect(t, len(*warnings), 0)
}

// Without a batch that does not fit, the selection is exactly what it
// was: every offered block, in the offered order, up to the block-count
// cap.
func TestAccountPool_filterBlocksToCommit_UnchangedWithoutOversizedBlock(t *testing.T) {
	setMaxAccountBlocksInMomentum(t, 100)
	ap := newAccountPool(fakeStable{})
	warnings := captureWarnings(ap)

	embedded := types.Address{0, 1}
	a := types.Address{0, 2}
	b := types.Address{0, 3}
	offered := []*nom.AccountBlock{
		pendingBlock(embedded, 1, nom.BlockTypeContractSend, 16),
		pendingBlock(embedded, 2, nom.BlockTypeContractReceive, 1024),
		pendingBlock(a, 1, nom.BlockTypeUserSend, 16),
		pendingBlock(a, 2, nom.BlockTypeUserSend, 16*1024),
		pendingBlock(b, 1, nom.BlockTypeUserReceive, 0),
	}
	expectSameBlocks(t, ap.filterBlocksToCommit(offered), offered)
	common.Expect(t, len(*warnings), 0)

	// The block-count cap still cuts the walk short.
	setMaxAccountBlocksInMomentum(t, 3)
	expectSameBlocks(t, ap.filterBlocksToCommit(offered), offered[:3])
	common.Expect(t, len(*warnings), 0)
}

// Selection runs at every momentum, so an oversized block that stays
// pending is reported once per block, not once per momentum.
func TestAccountPool_filterBlocksToCommit_LogsOversizedBlockOnce(t *testing.T) {
	setMaxAccountBlocksInMomentum(t, 100)
	ap := newAccountPool(fakeStable{})
	warnings := captureWarnings(ap)

	a := types.Address{0, 1}
	b := types.Address{0, 2}
	first := pendingBlock(a, 1, nom.BlockTypeUserReceive, maxDetailedMomentumWireSize)
	second := pendingBlock(b, 1, nom.BlockTypeUserReceive, maxDetailedMomentumWireSize+1)

	for i := 0; i < 3; i++ {
		expectSameBlocks(t, ap.filterBlocksToCommit([]*nom.AccountBlock{first, second}), []*nom.AccountBlock{})
	}
	common.Expect(t, len(*warnings), 2)
	for _, message := range *warnings {
		if !strings.Contains(message, "wire budget") {
			t.Fatalf("unexpected warning %q", message)
		}
	}
}

// End to end through the pool: an account whose only pending block is
// oversized does not keep the other accounts' pending blocks out of the
// next momentum, whatever order the pool walks its accounts in.
func TestAccountPool_GetNewMomentumContent_SkipsOversizedReceive(t *testing.T) {
	setMaxAccountBlocksInMomentum(t, 100)
	ap := newAccountPool(fakeStable{})

	add := func(block *nom.AccountBlock) {
		common.FailIfErr(t, ap.getAccountManager(block.Address).Add(&nom.AccountBlockTransaction{
			Block:   block,
			Changes: db.NewPatch(),
		}))
	}
	oversized := pendingBlock(types.Address{0, 1}, 1, nom.BlockTypeUserReceive, maxDetailedMomentumWireSize)
	add(oversized)
	expected := make(map[types.Hash]*nom.AccountBlock)
	for i := byte(2); i < 8; i++ {
		previous := types.ZeroHash
		for height := uint64(1); height <= 2; height++ {
			block := pendingBlock(types.Address{0, i}, height, nom.BlockTypeUserSend, 16)
			block.PreviousHash = previous
			block.Hash = block.ComputeHash()
			previous = block.Hash
			expected[block.Hash] = block
			add(block)
		}
	}

	content := ap.GetNewMomentumContent()
	common.Expect(t, len(content), len(expected))
	lastHeight := make(map[types.Address]uint64)
	for _, block := range content {
		if block.Hash == oversized.Hash {
			t.Fatal("oversized block was selected")
		}
		if _, ok := expected[block.Hash]; !ok {
			t.Fatalf("unexpected block %v@%d selected", block.Address, block.Height)
		}
		common.Expect(t, block.Height, lastHeight[block.Address]+1)
		lastHeight[block.Address] = block.Height
	}
}
