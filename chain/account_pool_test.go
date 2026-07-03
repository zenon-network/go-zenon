package chain

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

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
