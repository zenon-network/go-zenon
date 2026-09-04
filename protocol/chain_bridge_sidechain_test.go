package protocol_test

import (
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain"
	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// rollbackCountingChain records how often the bridge rolls the canonical
// chain back.
type rollbackCountingChain struct {
	chain.Chain
	rollbacks int
}

func (c *rollbackCountingChain) RollbackTo(insertLocker sync.Locker, identifier types.HashHeight) error {
	c.rollbacks++
	return c.Chain.RollbackTo(insertLocker, identifier)
}

// chainSnapshot is everything a failed side-chain insert must leave alone.
type chainSnapshot struct {
	frontier      types.HashHeight
	cacheFrontier types.HashHeight
	hashes        map[uint64]types.Hash
	balances      map[types.Address]string
}

func snapshotChain(t *testing.T, c chain.Chain) chainSnapshot {
	t.Helper()
	store := c.GetFrontierMomentumStore()
	frontier, err := store.GetFrontierMomentum()
	common.FailIfErr(t, err)
	snapshot := chainSnapshot{
		frontier:      frontier.Identifier(),
		cacheFrontier: c.GetFrontierCacheStore().Identifier(),
		hashes:        make(map[uint64]types.Hash, frontier.Height),
	}
	for height := uint64(1); height <= frontier.Height; height++ {
		momentum, err := store.GetMomentumByHeight(height)
		common.FailIfErr(t, err)
		snapshot.hashes[height] = momentum.Hash
	}
	common.Expect(t, snapshot.cacheFrontier, snapshot.frontier)
	snapshot.balances = make(map[types.Address]string)
	for _, address := range []types.Address{g.User1.Address, g.User2.Address, g.Pillar1.Address, types.PillarContract} {
		balance, err := store.GetAccountStore(address).GetBalance(types.ZnnTokenStandard)
		common.FailIfErr(t, err)
		snapshot.balances[address] = balance.String()
	}
	return snapshot
}

func expectChainEquals(t *testing.T, c chain.Chain, expected chainSnapshot) {
	t.Helper()
	actual := snapshotChain(t, c)
	common.Expect(t, actual.frontier, expected.frontier)
	common.Expect(t, actual.cacheFrontier, expected.cacheFrontier)
	common.Expect(t, len(actual.hashes), len(expected.hashes))
	for height, hash := range expected.hashes {
		common.Expect(t, actual.hashes[height], hash)
	}
	for address, balance := range expected.balances {
		common.Expect(t, actual.balances[address], balance)
	}
}

// attackerMomentum builds a momentum on top of previous that is internally
// consistent (hash recomputed, signed by a key that is not an elected
// producer) but carries a changes-hash the node cannot reproduce. It passes
// every check that needs no chain state and fails the first one that does.
func attackerMomentum(previous *nom.Momentum, height uint64) *nom.Momentum {
	timestamp := time.Unix(int64(previous.TimestampUnix+10), 0)
	momentum := &nom.Momentum{
		Version:         previous.Version,
		ChainIdentifier: previous.ChainIdentifier,
		PreviousHash:    previous.Hash,
		Height:          height,
		TimestampUnix:   previous.TimestampUnix + 10,
		Timestamp:       &timestamp,
		Content:         nom.NewMomentumContent(nil),
		ChangesHash:     types.NewHash([]byte("not the state this momentum produces")),
	}
	momentum.Hash = momentum.ComputeHash()
	momentum.PublicKey = g.User1.Public
	momentum.Signature = g.User1.Sign(momentum.Hash.Bytes())
	return momentum
}

func detailedOf(momentums ...*nom.Momentum) []*nom.DetailedMomentum {
	detailed := make([]*nom.DetailedMomentum, len(momentums))
	for i, momentum := range momentums {
		detailed[i] = &nom.DetailedMomentum{Momentum: momentum, AccountBlocks: []*nom.AccountBlock{}}
	}
	return detailed
}

func momentumAt(t *testing.T, c chain.Chain, height uint64) *nom.Momentum {
	t.Helper()
	momentum, err := c.GetFrontierMomentumStore().GetMomentumByHeight(height)
	common.FailIfErr(t, err)
	if momentum == nil {
		t.Fatalf("no momentum at height %d", height)
	}
	return momentum
}

func rollbackChainAndCache(t *testing.T, c chain.Chain, identifier types.HashHeight) {
	t.Helper()
	insert := c.AcquireInsert("test rollback")
	defer insert.Unlock()
	common.FailIfErr(t, c.RollbackTo(insert, identifier))
	common.FailIfErr(t, c.RollbackCacheTo(insert, identifier))
}

// expectRolledBack asserts the canonical chain was rolled back at least once;
// a restore rolls the failed candidates back again, so the count is not one.
func expectRolledBack(t *testing.T, c *rollbackCountingChain) {
	t.Helper()
	if c.rollbacks == 0 {
		t.Fatal("expected the side chain to have triggered a rollback")
	}
}

func newSideChainBridge(z mock.MockZenon) (*rollbackCountingChain, protocol.ChainBridge) {
	counting := &rollbackCountingChain{Chain: z.Chain()}
	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	return counting, protocol.NewChainBridge(counting, z.Consensus(), z.Verifier(), supervisor)
}

func TestInsertChain_SideChainRestoredWhenCandidateFailsAfterRollback(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	counting, bridge := newSideChainBridge(z)
	before := snapshotChain(t, z.Chain())

	// Side chain forking one below the frontier, longer than ours.
	target := momentumAt(t, z.Chain(), 5)
	head := attackerMomentum(target, 6)
	tail := attackerMomentum(head, 7)

	_, err := bridge.InsertChain(detailedOf(head, tail))
	if err == nil {
		t.Fatal("expected the side chain to be rejected")
	}
	// The candidate passes every stateless check, so the rollback happens;
	// the failure comes from the state-dependent checks after it.
	expectRolledBack(t, counting)
	expectChainEquals(t, z.Chain(), before)
}

func TestInsertChain_SideChainWithInvalidSignatureIsRejectedBeforeRollback(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	counting, bridge := newSideChainBridge(z)
	before := snapshotChain(t, z.Chain())

	target := momentumAt(t, z.Chain(), 5)
	head := attackerMomentum(target, 6)
	head.Signature[0] ^= 0xff
	tail := attackerMomentum(head, 7)

	_, err := bridge.InsertChain(detailedOf(head, tail))
	if err == nil {
		t.Fatal("expected the side chain to be rejected")
	}
	common.Expect(t, counting.rollbacks, 0)
	expectChainEquals(t, z.Chain(), before)
}

func TestInsertChain_SideChainWithTamperedHashIsRejectedBeforeRollback(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	counting, bridge := newSideChainBridge(z)
	before := snapshotChain(t, z.Chain())

	target := momentumAt(t, z.Chain(), 5)
	head := attackerMomentum(target, 6)
	head.Hash = types.NewHash([]byte("advertised hash that does not match the content"))
	tail := attackerMomentum(head, 7)

	_, err := bridge.InsertChain(detailedOf(head, tail))
	if err == nil {
		t.Fatal("expected the side chain to be rejected")
	}
	common.Expect(t, counting.rollbacks, 0)
	expectChainEquals(t, z.Chain(), before)
}

// forkFixture holds a node at an original branch plus a genuine, valid
// alternative branch forking one below its frontier.
type forkFixture struct {
	z        mock.MockZenon
	original chainSnapshot
	fork     []*nom.DetailedMomentum
}

func newForkFixture(t *testing.T) *forkFixture {
	z := mock.NewMockZenon(t)
	z.InsertMomentumsTo(6)
	store := z.Chain().GetFrontierMomentumStore()
	originalSix, err := store.PrefetchMomentum(momentumAt(t, z.Chain(), 6))
	common.FailIfErr(t, err)
	base := momentumAt(t, z.Chain(), 5).Identifier()

	// Produce a different branch from height 5: add a transaction so the
	// content differs, then let the pillars build two momentums.
	rollbackChainAndCache(t, z.Chain(), base)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(1),
	}, nil, mock.SkipVmChanges)
	z.InsertMomentumsTo(7)
	fork := make([]*nom.DetailedMomentum, 0, 2)
	for height := uint64(6); height <= 7; height++ {
		detailed, err := z.Chain().GetFrontierMomentumStore().PrefetchMomentum(momentumAt(t, z.Chain(), height))
		common.FailIfErr(t, err)
		fork = append(fork, detailed)
	}
	if fork[0].Momentum.Hash == originalSix.Momentum.Hash {
		t.Fatal("fork did not diverge from the original branch")
	}

	// Back to the original branch.
	rollbackChainAndCache(t, z.Chain(), base)
	_, bridge := newSideChainBridge(z)
	_, err = bridge.InsertChain([]*nom.DetailedMomentum{originalSix})
	common.FailIfErr(t, err)

	return &forkFixture{z: z, original: snapshotChain(t, z.Chain()), fork: fork}
}

func TestInsertChain_ValidLongerSideChainReplacesBranch(t *testing.T) {
	f := newForkFixture(t)
	defer f.z.StopPanic()
	counting, bridge := newSideChainBridge(f.z)

	_, err := bridge.InsertChain(f.fork)
	common.FailIfErr(t, err)
	common.Expect(t, counting.rollbacks, 1)
	common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.fork[1].Momentum.Identifier())
	common.Expect(t, f.z.Chain().GetFrontierCacheStore().Identifier(), f.fork[1].Momentum.Identifier())
}

func TestInsertChain_SideChainRestoredWhenLaterCandidateFails(t *testing.T) {
	f := newForkFixture(t)
	defer f.z.StopPanic()
	counting, bridge := newSideChainBridge(f.z)

	// A genuine first candidate that replaces our branch, followed by one
	// that fails only after it has been applied.
	bad := attackerMomentum(f.fork[0].Momentum, 7)
	_, err := bridge.InsertChain([]*nom.DetailedMomentum{f.fork[0], detailedOf(bad)[0]})
	if err == nil {
		t.Fatal("expected the side chain to be rejected")
	}
	expectRolledBack(t, counting)
	expectChainEquals(t, f.z.Chain(), f.original)

	// The restored state is real state: the genuine fork still replaces the
	// branch afterwards, which requires its changes-hash to be reproducible
	// from what was put back.
	_, err = bridge.InsertChain(f.fork)
	common.FailIfErr(t, err)
	common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.fork[1].Momentum.Identifier())
}

func TestInsertChain_SideChainDepthBoundary(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(40)
	counting, bridge := newSideChainBridge(z)
	before := snapshotChain(t, z.Chain())

	// Depth 31: rejected before anything is touched.
	tooDeep := momentumAt(t, z.Chain(), 9)
	head := attackerMomentum(tooDeep, 10)
	tail := attackerMomentum(head, 41)
	_, err := bridge.InsertChain(detailedOf(head, tail))
	if err == nil {
		t.Fatal("expected a 31-deep side chain to be rejected")
	}
	common.Expect(t, counting.rollbacks, 0)
	expectChainEquals(t, z.Chain(), before)

	// Depth 30: allowed, rolls back, fails after, and every removed momentum
	// comes back.
	deepest := momentumAt(t, z.Chain(), 10)
	head = attackerMomentum(deepest, 11)
	tail = attackerMomentum(head, 41)
	_, err = bridge.InsertChain(detailedOf(head, tail))
	if err == nil {
		t.Fatal("expected the side chain to be rejected")
	}
	expectRolledBack(t, counting)
	expectChainEquals(t, z.Chain(), before)
}

// storedPatchDumps returns the serialized state patch of every momentum above
// identifier, as the momentum store holds them right now.
func storedPatchDumps(t *testing.T, c chain.Chain, identifier types.HashHeight) [][]byte {
	t.Helper()
	insert := c.AcquireInsert("test capture")
	defer insert.Unlock()
	removed, err := c.CaptureBranchAbove(insert, identifier)
	common.FailIfErr(t, err)
	dumps := make([][]byte, len(removed))
	for i, momentum := range removed {
		dumps[i] = momentum.Changes.Dump()
	}
	return dumps
}

func TestInsertChain_RepeatedRestoresKeepStoredPatchesIdentical(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	_, bridge := newSideChainBridge(z)
	before := snapshotChain(t, z.Chain())
	target := momentumAt(t, z.Chain(), 4).Identifier()
	patchesBefore := storedPatchDumps(t, z.Chain(), target)

	for attempt := 0; attempt < 3; attempt++ {
		head := attackerMomentum(momentumAt(t, z.Chain(), 4), 5)
		tail := attackerMomentum(head, 7)
		_, err := bridge.InsertChain(detailedOf(head, tail))
		if err == nil {
			t.Fatalf("attempt %d: expected the side chain to be rejected", attempt)
		}
		expectChainEquals(t, z.Chain(), before)

		patchesAfter := storedPatchDumps(t, z.Chain(), target)
		common.Expect(t, len(patchesAfter), len(patchesBefore))
		for i := range patchesBefore {
			if string(patchesAfter[i]) != string(patchesBefore[i]) {
				t.Fatalf("attempt %d: stored patch for height %d changed: %d bytes before, %d bytes after", attempt, target.Height+uint64(i)+1, len(patchesBefore[i]), len(patchesAfter[i]))
			}
		}
	}
}
