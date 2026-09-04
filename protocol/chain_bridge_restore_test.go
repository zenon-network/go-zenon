package protocol_test

import (
	"errors"
	"math/big"
	"strings"
	"sync"
	"testing"

	"github.com/zenon-network/go-zenon/chain"
	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// restoreFixture holds a mock node that has been rolled back to just before a
// momentum containing one receive block, so that momentum can be replayed
// against an arbitrary pool state.
type restoreFixture struct {
	z          mock.MockZenon
	supervisor *vm.Supervisor
	bridge     protocol.ChainBridge
	sends      [2]*nom.AccountBlock
	receive    *nom.AccountBlockTransaction
	detailed   *nom.DetailedMomentum
	previous   types.HashHeight
}

func newRestoreFixture(t *testing.T) *restoreFixture {
	return newRestoreFixtureWith(t, false)
}

// newRestoreFixtureWith optionally puts a second, independent account's block
// (a User1 send) into the same momentum as the User2 receive.
func newRestoreFixtureWith(t *testing.T, twoBlocks bool) *restoreFixture {
	z := mock.NewMockZenon(t)
	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	f := &restoreFixture{
		z:          z,
		supervisor: supervisor,
		bridge:     protocol.NewChainBridge(z.Chain(), z.Consensus(), z.Verifier(), supervisor),
	}

	// User1 sends two separate units to User2; both get committed together.
	for i := range f.sends {
		f.sends[i] = z.InsertSendBlock(&nom.AccountBlock{
			Address:       g.User1.Address,
			ToAddress:     g.User2.Address,
			TokenStandard: types.ZnnTokenStandard,
			Amount:        big.NewInt(1),
		}, nil, mock.SkipVmChanges)
	}
	z.InsertNewMomentum()

	// User2 receives the first send; that block goes into the next momentum.
	f.receive = f.generateReceive(t, f.sends[0])
	z.Broadcaster().CreateAccountBlock(f.receive)
	expectedBlocks := 1
	if twoBlocks {
		z.InsertSendBlock(&nom.AccountBlock{
			Address:       g.User1.Address,
			ToAddress:     g.User2.Address,
			TokenStandard: types.ZnnTokenStandard,
			Amount:        big.NewInt(1),
		}, nil, mock.SkipVmChanges)
		expectedBlocks = 2
	}
	z.InsertNewMomentum()

	store := z.Chain().GetFrontierMomentumStore()
	momentum, err := store.GetFrontierMomentum()
	common.FailIfErr(t, err)
	f.detailed, err = store.PrefetchMomentum(momentum)
	common.FailIfErr(t, err)
	common.Expect(t, len(f.detailed.AccountBlocks), expectedBlocks)
	f.previous = momentum.Previous()

	insert := z.Chain().AcquireInsert("test rollback")
	common.FailIfErr(t, z.Chain().RollbackTo(insert, f.previous))
	insert.Unlock()
	return f
}

// generateReceive builds and signs a receive for User2 on top of whatever the
// pool currently holds for that account.
func (f *restoreFixture) generateReceive(t *testing.T, send *nom.AccountBlock) *nom.AccountBlockTransaction {
	transaction, err := f.supervisor.GenerateFromTemplate(&nom.AccountBlock{
		BlockType:     nom.BlockTypeUserReceive,
		Address:       g.User2.Address,
		FromBlockHash: send.Hash,
	}, g.User2.Signer)
	common.FailIfErr(t, err)
	return transaction
}

// addToPool inserts an already generated transaction into the pool and fails
// the test on any error, unlike the mock broadcaster which only logs it.
func (f *restoreFixture) addToPool(t *testing.T, transaction *nom.AccountBlockTransaction) {
	t.Helper()
	insert := f.z.Chain().AcquireInsert("test add to pool")
	defer insert.Unlock()
	common.FailIfErr(t, f.z.Chain().AddAccountBlockTransaction(insert, transaction))
}

// tamperedMomentum returns the fixture's momentum with its block's unhashed
// ChangesHash field altered. The block still passes signature and hash checks.
func (f *restoreFixture) tamperedMomentum() *nom.DetailedMomentum {
	block := f.detailed.AccountBlocks[0].Copy()
	block.ChangesHash = types.NewHash([]byte("not the real changes hash"))
	return &nom.DetailedMomentum{Momentum: f.detailed.Momentum, AccountBlocks: []*nom.AccountBlock{block}}
}

func (f *restoreFixture) expectPoolChain(t *testing.T, blocks ...*nom.AccountBlock) {
	t.Helper()
	frontier := f.z.Chain().GetFrontierAccountStore(g.User2.Address)
	common.Expect(t, frontier.Identifier(), blocks[len(blocks)-1].Identifier())
	for _, expected := range blocks {
		stored, err := frontier.ByHeight(expected.Height)
		common.FailIfErr(t, err)
		if stored == nil || !stored.EqualBytes(expected) {
			t.Fatalf("pool block at height %v does not match the expected bytes", expected.Height)
		}
	}
}

func expectChangesHashError(t *testing.T, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "changes-hash") {
		t.Fatalf("expected a changes-hash error, got %v", err)
	}
}

func TestInsertChain_FailedMomentumKeepsPoolCopyAndDescendant(t *testing.T) {
	f := newRestoreFixture(t)
	defer f.z.StopPanic()

	// Pool holds the canonical copy of the momentum's block plus a descendant.
	common.FailIfErr(t, f.bridge.AddAccountBlocks([]*nom.AccountBlock{f.receive.Block}))
	descendant := f.generateReceive(t, f.sends[1])
	f.addToPool(t, descendant)
	f.expectPoolChain(t, f.receive.Block, descendant.Block)

	_, err := f.bridge.InsertChain([]*nom.DetailedMomentum{f.tamperedMomentum()})
	expectChangesHashError(t, err)

	common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.previous)
	f.expectPoolChain(t, f.receive.Block, descendant.Block)

	// The genuine momentum still goes through afterwards.
	_, err = f.bridge.InsertChain([]*nom.DetailedMomentum{f.detailed})
	common.FailIfErr(t, err)
	common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.detailed.Momentum.Identifier())
}

func TestInsertChain_FailedMomentumKeepsCompetingPoolChain(t *testing.T) {
	f := newRestoreFixture(t)
	defer f.z.StopPanic()

	// Pool holds a different, valid chain for the same account: the two
	// receives in the opposite order, so the block at the momentum's height
	// has a different hash from the momentum's block.
	first := f.generateReceive(t, f.sends[1])
	f.addToPool(t, first)
	second := f.generateReceive(t, f.sends[0])
	f.addToPool(t, second)
	common.Expect(t, first.Block.Height, f.receive.Block.Height)
	if first.Block.Hash == f.receive.Block.Hash {
		t.Fatal("competing block unexpectedly has the same hash")
	}
	f.expectPoolChain(t, first.Block, second.Block)

	_, err := f.bridge.InsertChain([]*nom.DetailedMomentum{f.tamperedMomentum()})
	expectChangesHashError(t, err)

	common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.previous)
	f.expectPoolChain(t, first.Block, second.Block)
}

// committingButFailingChain lets the momentum commit and then reports an
// error, reproducing a failure that happens after the store has advanced.
type committingButFailingChain struct {
	chain.Chain
}

var errAfterCommit = errors.New("failure after momentum commit")

func (c committingButFailingChain) AddMomentumTransaction(insertLocker sync.Locker, transaction *nom.MomentumTransaction) error {
	if err := c.Chain.AddMomentumTransaction(insertLocker, transaction); err != nil {
		return err
	}
	return errAfterCommit
}

func TestInsertChain_FailureAfterCommitDoesNotRestoreStalePool(t *testing.T) {
	f := newRestoreFixture(t)
	defer f.z.StopPanic()
	bridge := protocol.NewChainBridge(committingButFailingChain{f.z.Chain()}, f.z.Consensus(), f.z.Verifier(), f.supervisor)

	// Pool holds a competing chain that the momentum's block replaces.
	first := f.generateReceive(t, f.sends[1])
	f.addToPool(t, first)
	second := f.generateReceive(t, f.sends[0])
	f.addToPool(t, second)

	_, err := bridge.InsertChain([]*nom.DetailedMomentum{f.detailed})
	if !errors.Is(err, errAfterCommit) {
		t.Fatalf("expected the post-commit error, got %v", err)
	}

	// The momentum is committed, so a snapshot taken against the old stable
	// state must not be put back: the competing chain it holds no longer
	// links to the committed block. The pool keeps the momentum's copy.
	common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.detailed.Momentum.Identifier())
	stored, err := f.z.Chain().GetFrontierAccountStore(g.User2.Address).ByHeight(f.receive.Block.Height)
	common.FailIfErr(t, err)
	if stored == nil || !stored.EqualBytes(f.receive.Block) {
		t.Fatal("pool does not hold the committed block's bytes")
	}
}

func TestInsertChain_SecondBlockFailureRestoresFirstBlockReplacement(t *testing.T) {
	f := newRestoreFixture(t)
	defer f.z.StopPanic()

	// Pool holds a competing chain; the momentum carries the canonical block
	// followed by a block that cannot be applied.
	first := f.generateReceive(t, f.sends[1])
	f.addToPool(t, first)
	second := f.generateReceive(t, f.sends[0])
	f.addToPool(t, second)

	broken := second.Block.Copy()
	broken.Signature[0] ^= 0xff
	detailed := &nom.DetailedMomentum{
		Momentum:      f.detailed.Momentum,
		AccountBlocks: []*nom.AccountBlock{f.detailed.AccountBlocks[0], broken},
	}
	_, err := f.bridge.InsertChain([]*nom.DetailedMomentum{detailed})
	if err == nil {
		t.Fatal("expected the second block to fail")
	}

	common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.previous)
	f.expectPoolChain(t, first.Block, second.Block)
}

// snapshotCountingChain records how often the bridge snapshots the pool.
type snapshotCountingChain struct {
	chain.Chain
	snapshots int
}

func (c *snapshotCountingChain) SnapshotUncommitted(insertLocker sync.Locker, addresses []types.Address) *chain.UncommittedSnapshot {
	c.snapshots++
	return c.Chain.SnapshotUncommitted(insertLocker, addresses)
}

func TestInsertChain_RejectsMalformedPrefetchedBlocksBeforeTouchingPool(t *testing.T) {
	f := newRestoreFixture(t)
	defer f.z.StopPanic()
	counting := &snapshotCountingChain{Chain: f.z.Chain()}
	bridge := protocol.NewChainBridge(counting, f.z.Consensus(), f.z.Verifier(), f.supervisor)

	// Pool holds a competing chain so any force-add would be visible.
	first := f.generateReceive(t, f.sends[1])
	f.addToPool(t, first)
	second := f.generateReceive(t, f.sends[0])
	f.addToPool(t, second)

	genuine := f.detailed.AccountBlocks[0]
	oversized := make([]*nom.AccountBlock, chain.MaxAccountBlocksInMomentum+1)
	for i := range oversized {
		oversized[i] = genuine
	}
	cases := map[string][]*nom.AccountBlock{
		"oversized list":      oversized,
		"length mismatch":     {genuine, second.Block},
		"nil entry":           {nil},
		"identifier mismatch": {second.Block},
		"empty list":          {},
	}
	for name, blocks := range cases {
		detailed := &nom.DetailedMomentum{Momentum: f.detailed.Momentum, AccountBlocks: blocks}
		_, err := bridge.InsertChain([]*nom.DetailedMomentum{detailed})
		if err == nil {
			t.Fatalf("%s: expected an error", name)
		}
		common.Expect(t, counting.snapshots, 0)
		common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.previous)
		f.expectPoolChain(t, first.Block, second.Block)
	}

	// The well-formed momentum still inserts through the same bridge.
	_, err := bridge.InsertChain([]*nom.DetailedMomentum{f.detailed})
	common.FailIfErr(t, err)
	common.Expect(t, counting.snapshots, 1)
}

func TestInsertChain_AcceptsPrefetchedBlocksInAnyOrder(t *testing.T) {
	f := newRestoreFixtureWith(t, true)
	defer f.z.StopPanic()

	blocks := f.detailed.AccountBlocks
	if blocks[0].Address == blocks[1].Address {
		t.Fatal("fixture should hold blocks from two different accounts")
	}
	swapped := &nom.DetailedMomentum{
		Momentum:      f.detailed.Momentum,
		AccountBlocks: []*nom.AccountBlock{blocks[1], blocks[0]},
	}
	_, err := f.bridge.InsertChain([]*nom.DetailedMomentum{swapped})
	common.FailIfErr(t, err)
	common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.detailed.Momentum.Identifier())
}

func TestInsertChain_RejectsDuplicatePrefetchedBlocks(t *testing.T) {
	f := newRestoreFixtureWith(t, true)
	defer f.z.StopPanic()
	counting := &snapshotCountingChain{Chain: f.z.Chain()}
	bridge := protocol.NewChainBridge(counting, f.z.Consensus(), f.z.Verifier(), f.supervisor)

	blocks := f.detailed.AccountBlocks
	duplicated := &nom.DetailedMomentum{
		Momentum:      f.detailed.Momentum,
		AccountBlocks: []*nom.AccountBlock{blocks[0], blocks[0]},
	}
	_, err := bridge.InsertChain([]*nom.DetailedMomentum{duplicated})
	if err == nil {
		t.Fatal("expected duplicate prefetched blocks to be rejected")
	}
	common.Expect(t, counting.snapshots, 0)
	common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.previous)
}

func TestInsertChain_MalformedSideChainDoesNotRollBack(t *testing.T) {
	f := newRestoreFixture(t)
	defer f.z.StopPanic()

	// Advance one momentum so the node has a frontier the side chain competes with.
	_, err := f.bridge.InsertChain([]*nom.DetailedMomentum{f.detailed})
	common.FailIfErr(t, err)
	frontier := f.z.Chain().GetFrontierMomentumStore().Identifier()
	common.Expect(t, frontier, f.detailed.Momentum.Identifier())

	// A longer side chain forking below the frontier, whose first momentum
	// carries an oversized block list. Only the linkage and length are
	// inspected before the rollback decision, so the momentums need no
	// valid hashes or signatures.
	oversized := make([]*nom.AccountBlock, chain.MaxAccountBlocksInMomentum+1)
	for i := range oversized {
		oversized[i] = f.detailed.AccountBlocks[0]
	}
	head := &nom.Momentum{Version: 1, ChainIdentifier: 1, Height: frontier.Height, PreviousHash: f.previous.Hash}
	head.Hash = types.NewHash([]byte("side-chain head"))
	tail := &nom.Momentum{Version: 1, ChainIdentifier: 1, Height: frontier.Height + 1, PreviousHash: head.Hash}
	tail.Hash = types.NewHash([]byte("side-chain tail"))
	side := []*nom.DetailedMomentum{
		{Momentum: head, AccountBlocks: oversized},
		{Momentum: tail, AccountBlocks: nil},
	}
	_, err = f.bridge.InsertChain(side)
	if err == nil {
		t.Fatal("expected the malformed side chain to be rejected")
	}
	common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), frontier)
}

func TestInsertChain_DoesNotMutateCallerDetailedMomentum(t *testing.T) {
	f := newRestoreFixtureWith(t, true)
	defer f.z.StopPanic()

	blocks := f.detailed.AccountBlocks
	supplied := &nom.DetailedMomentum{
		Momentum:      f.detailed.Momentum,
		AccountBlocks: []*nom.AccountBlock{blocks[1], blocks[0]},
	}
	before := append([]*nom.AccountBlock(nil), supplied.AccountBlocks...)

	// The fetcher hands the same object to a broadcast goroutine before it
	// calls InsertChain, so a concurrent reader must be safe.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10000; i++ {
			for _, block := range supplied.AccountBlocks {
				_ = block.Height
			}
		}
	}()
	_, err := f.bridge.InsertChain([]*nom.DetailedMomentum{supplied})
	<-done
	common.FailIfErr(t, err)

	common.Expect(t, len(supplied.AccountBlocks), len(before))
	for i := range before {
		if supplied.AccountBlocks[i] != before[i] {
			t.Fatalf("caller's block list was reordered at index %d", i)
		}
	}
}

func TestInsertChain_DoesNotWriteToCallerAccountBlocks(t *testing.T) {
	f := newRestoreFixture(t)
	defer f.z.StopPanic()

	// Pool holds a byte-different copy, so the momentum's block takes the
	// replacement path through the VM, which sets plasma fields on the block
	// it is given.
	poolCopy := f.receive.Block.Copy()
	poolCopy.Signature = alternateSign(g.User2, poolCopy.Hash.Bytes())
	common.FailIfErr(t, f.bridge.AddAccountBlocks([]*nom.AccountBlock{poolCopy}))

	supplied := f.detailed.AccountBlocks[0].Copy()
	const sentinel = uint64(12345)
	supplied.BasePlasma = sentinel
	supplied.TotalPlasma = sentinel
	detailed := &nom.DetailedMomentum{Momentum: f.detailed.Momentum, AccountBlocks: []*nom.AccountBlock{supplied}}

	// Models the fetcher's concurrent broadcast reading the same block.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10000; i++ {
			_ = supplied.BasePlasma + supplied.TotalPlasma
		}
	}()
	_, err := f.bridge.InsertChain([]*nom.DetailedMomentum{detailed})
	<-done
	common.FailIfErr(t, err)

	common.Expect(t, supplied.BasePlasma, sentinel)
	common.Expect(t, supplied.TotalPlasma, sentinel)

	// What reached the chain is the canonical bytes, not the sentinel.
	committed, err := f.z.Chain().GetFrontierMomentumStore().GetAccountBlock(f.receive.Block.Header())
	common.FailIfErr(t, err)
	if committed == nil || !committed.EqualBytes(f.receive.Block) {
		t.Fatal("committed block does not match the canonical bytes")
	}
}
