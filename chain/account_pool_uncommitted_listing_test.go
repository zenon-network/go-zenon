package chain

import (
	"bytes"
	"math/big"
	"sync"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// listingStable is a Stable for one account chain whose committed state
// the test moves by hand. GetStableAccountDB returns current; while queued
// holds entries, each call also moves current to the next one, so a test
// can place a change of the committed state between two reads the pool
// makes inside a single call.
type listingStable struct {
	current db.DB
	queued  []db.DB
}

func (s *listingStable) GetStableAccountDB(types.Address) db.DB {
	result := s.current
	if len(s.queued) > 0 {
		s.current, s.queued = s.queued[0], s.queued[1:]
	}
	return result
}

func (s *listingStable) GetFrontierMomentumStore() store.Momentum {
	return nil
}

// listingChain returns a user account chain of the given length.
func listingChain(length int) []*nom.AccountBlock {
	address := types.ParseAddressPanic("z1qqjfammypam0sjhzst0jfjth60vy9g8w0g5lhg")
	blocks := make([]*nom.AccountBlock, 0, length)
	for i := 0; i < length; i++ {
		block := &nom.AccountBlock{
			Version:         1,
			ChainIdentifier: 1,
			BlockType:       nom.BlockTypeUserSend,
			Address:         address,
			Height:          uint64(i + 1),
			Amount:          big.NewInt(0),
			Signature:       bytes.Repeat([]byte{byte(i + 1)}, 64),
		}
		if i > 0 {
			block.PreviousHash = blocks[i-1].Hash
		}
		block.Hash = block.ComputeHash()
		blocks = append(blocks, block)
	}
	return blocks
}

// committedStates returns, for every height from 0 to len(blocks), an
// account DB in which the blocks up to that height are committed.
func committedStates(t *testing.T, blocks []*nom.AccountBlock) []db.DB {
	t.Helper()
	staging := newAccountPool(&memStable{})
	locker := &sync.Mutex{}
	for _, block := range blocks {
		common.FailIfErr(t, staging.AddAccountBlockTransaction(locker, poolTransaction(block)))
	}
	states := make([]db.DB, 0, len(blocks)+1)
	for height := 0; height <= len(blocks); height++ {
		state := db.NewMemDB()
		for _, block := range blocks[:height] {
			common.FailIfErr(t, db.ApplyPatch(state, staging.GetPatch(block.Address, block.Identifier())))
		}
		states = append(states, state)
	}
	return states
}

func expectListing(t *testing.T, got []*nom.AccountBlock, want ...*nom.AccountBlock) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("listing has %d blocks, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Identifier() != want[i].Identifier() {
			t.Fatalf("listing[%d] = %v, want %v", i, got[i].Identifier(), want[i].Identifier())
		}
	}
}

// A momentum rollback pops the momentum first and tells the pool afterwards.
// In between, the committed state of an account with blocks in that
// momentum is below the state its manager was built on, and the manager
// does not index the blocks in between. Listing the uncommitted blocks
// then must return, not panic. Here the momentum held one block of the
// account.
func TestAccountPool_UncommittedListing_CommittedStateBelowManager(t *testing.T) {
	blocks := listingChain(2)
	address := blocks[0].Address
	states := committedStates(t, blocks)

	stable := &listingStable{current: states[2]}
	ap := newAccountPool(stable)
	// Any read of the frontier registers a manager for the address.
	common.Expect(t, ap.GetFrontierAccountStore(address).Identifier(), blocks[1].Identifier())

	stable.current = states[1]

	expectListing(t, ap.GetAllUncommittedAccountBlocks())
	expectListing(t, ap.GetUncommittedAccountBlocksByAddress(address))
}

// The same window with blocks pending on top of the confirmed ones. The
// listing is the blocks the manager indexes. A block that left the
// committed state is not listed until the pool has handled the rollback,
// whether the momentum held one block of the account or more.
func TestAccountPool_UncommittedListing_CommittedStateBelowManagerWithPending(t *testing.T) {
	blocks := listingChain(4)
	address := blocks[0].Address
	states := committedStates(t, blocks)

	stable := &listingStable{current: states[2]}
	ap := newAccountPool(stable)
	locker := &sync.Mutex{}
	common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, poolTransaction(blocks[2])))
	common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, poolTransaction(blocks[3])))
	expectListing(t, ap.GetUncommittedAccountBlocksByAddress(address), blocks[2], blocks[3])

	stable.current = states[1]

	expectListing(t, ap.GetAllUncommittedAccountBlocks(), blocks[2], blocks[3])
	expectListing(t, ap.GetUncommittedAccountBlocksByAddress(address), blocks[2], blocks[3])
	// The listing ends at the frontier the pool reports for the address.
	common.Expect(t, ap.GetFrontierAccountStore(address).Identifier(), blocks[3].Identifier())

	// A momentum that confirmed two blocks of the account.
	stable.current = states[0]

	expectListing(t, ap.GetAllUncommittedAccountBlocks(), blocks[2], blocks[3])
	expectListing(t, ap.GetUncommittedAccountBlocksByAddress(address), blocks[2], blocks[3])
}

// The listing reads the committed state and then, for an address without a
// manager, reads it again to create one. A momentum inserted between the
// two reads puts the new manager above the height the walk starts at, with
// nothing pending.
func TestAccountPool_UncommittedListing_CommittedStateAdvancesBeforeManagerIsCreated(t *testing.T) {
	blocks := listingChain(2)
	address := blocks[0].Address
	states := committedStates(t, blocks)

	stable := &listingStable{current: states[1], queued: []db.DB{states[2]}}
	ap := newAccountPool(stable)

	expectListing(t, ap.GetUncommittedAccountBlocksByAddress(address))
	common.Expect(t, ap.GetFrontierAccountStore(address).Identifier(), blocks[1].Identifier())
}

// A momentum insert moves the committed state first and tells the pool
// afterwards. In between, the manager still holds the block that was just
// confirmed. The listing leaves it out, as it did before.
func TestAccountPool_UncommittedListing_CommittedStateAboveManager(t *testing.T) {
	blocks := listingChain(4)
	address := blocks[0].Address
	states := committedStates(t, blocks)

	stable := &listingStable{current: states[1]}
	ap := newAccountPool(stable)
	locker := &sync.Mutex{}
	for _, block := range blocks[1:] {
		common.FailIfErr(t, ap.AddAccountBlockTransaction(locker, poolTransaction(block)))
	}
	expectListing(t, ap.GetUncommittedAccountBlocksByAddress(address), blocks[1], blocks[2], blocks[3])

	stable.current = states[2]
	expectListing(t, ap.GetUncommittedAccountBlocksByAddress(address), blocks[2], blocks[3])

	// Every pending block is confirmed and the pool has not been told yet.
	stable.current = states[4]
	expectListing(t, ap.GetAllUncommittedAccountBlocks())
}
