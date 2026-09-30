package protocol_test

import (
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/cache/storage"
	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/wallet"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// rollbackCountingChain records how often the bridge rolls the canonical
// chain back.
type rollbackCountingChain struct {
	chain.Chain
	rollbacks int
	// failCommitOf, when set, makes the commit of the momentum with this hash
	// fail after its cache update has already gone through, the way a store
	// error would.
	failCommitOf types.Hash
}

func (c *rollbackCountingChain) RollbackTo(insertLocker sync.Locker, identifier types.HashHeight) error {
	c.rollbacks++
	return c.Chain.RollbackTo(insertLocker, identifier)
}

func (c *rollbackCountingChain) AddMomentumTransaction(insertLocker sync.Locker, transaction *nom.MomentumTransaction) error {
	if !c.failCommitOf.IsZero() && transaction.Momentum.Hash == c.failCommitOf {
		return errors.Errorf("injected commit failure for %v", transaction.Momentum.Identifier())
	}
	return c.Chain.AddMomentumTransaction(insertLocker, transaction)
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

// unsignedMomentum builds an empty momentum on top of previous, in the next
// slot, that carries a changes-hash the node cannot reproduce. It passes
// every check that runs against the state at previous and fails the first
// one that needs the state it claims to produce.
func unsignedMomentum(previous *nom.Momentum, height uint64) *nom.Momentum {
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
	return momentum
}

// signedBy recomputes the hash and signs the momentum with key.
func signedBy(momentum *nom.Momentum, key *wallet.KeyPair) *nom.Momentum {
	momentum.Hash = momentum.ComputeHash()
	momentum.PublicKey = key.Public
	momentum.Signature = key.Sign(momentum.Hash.Bytes())
	return momentum
}

// attackerMomentum is an unsignedMomentum signed by a key that is not an
// elected producer: internally consistent, so it costs nothing to make, and
// rejected by the producer check.
func attackerMomentum(previous *nom.Momentum, height uint64) *nom.Momentum {
	return signedBy(unsignedMomentum(previous, height), g.User1)
}

// pillarKey returns the genesis pillar key producing at address.
func pillarKey(t *testing.T, address types.Address) *wallet.KeyPair {
	t.Helper()
	for _, key := range g.PillarKeys {
		if key.Address == address {
			return key
		}
	}
	t.Fatalf("no pillar key produces at %v", address)
	return nil
}

// electedKey returns the key of the pillar elected for the slot at timestamp
// by the election z's frontier implies.
func electedKey(t *testing.T, z mock.MockZenon, timestamp time.Time) *wallet.KeyPair {
	t.Helper()
	producer, err := z.Consensus().GetMomentumProducer(timestamp)
	common.FailIfErr(t, err)
	return pillarKey(t, *producer)
}

// producerMomentum is an unsignedMomentum signed by the pillar elected for
// its slot. It passes every check that runs before the rollback, including
// the producer check, and fails on its changes-hash once applied. The slot
// is within the election z's frontier implies, which the tests keep on the
// same proof block as the election at previous.
func producerMomentum(t *testing.T, z mock.MockZenon, previous *nom.Momentum, height uint64) *nom.Momentum {
	t.Helper()
	momentum := unsignedMomentum(previous, height)
	return signedBy(momentum, electedKey(t, z, *momentum.Timestamp))
}

func detailedOf(momentums ...*nom.Momentum) []*nom.DetailedMomentum {
	detailed := make([]*nom.DetailedMomentum, len(momentums))
	for i, momentum := range momentums {
		detailed[i] = &nom.DetailedMomentum{Momentum: momentum, AccountBlocks: []*nom.AccountBlock{}}
	}
	return detailed
}

// invalidTail chains attacker momentums on top of previous up to and
// including height top. Each passes the stateless checks and fails the first
// state-dependent one.
func invalidTail(previous *nom.Momentum, top uint64) []*nom.DetailedMomentum {
	tail := make([]*nom.Momentum, 0, top-previous.Height)
	for height := previous.Height + 1; height <= top; height++ {
		previous = attackerMomentum(previous, height)
		tail = append(tail, previous)
	}
	return detailedOf(tail...)
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

// expectRolledBack asserts the canonical chain was rolled back at least once;
// a restore rolls the failed candidates back again, so the count is not one.
func expectRolledBack(t *testing.T, c *rollbackCountingChain) {
	t.Helper()
	if c.rollbacks == 0 {
		t.Fatal("expected the side chain to have triggered a rollback")
	}
}

// newSideChainBridge builds the bridge the way the node does, with a real
// verifier; the mock node carries none, and a nil verifier would skip the
// head verification that runs before the rollback.
func newSideChainBridge(z mock.MockZenon) (*rollbackCountingChain, protocol.ChainBridge) {
	counting := &rollbackCountingChain{Chain: z.Chain()}
	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	momentumVerifier := verifier.NewVerifier(counting, z.Consensus(), vm.CanonicalBasePlasma)
	return counting, protocol.NewChainBridge(counting, z.Consensus(), momentumVerifier, supervisor)
}

func TestInsertChain_SideChainRestoredWhenCandidateFailsAfterRollback(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	counting, bridge := newSideChainBridge(z)
	before := snapshotChain(t, z.Chain())

	// Side chain forking one below the frontier, longer than ours, whose
	// head is signed by the elected producer.
	target := momentumAt(t, z.Chain(), 5)
	head := producerMomentum(t, z, target, 6)
	tail := attackerMomentum(head, 7)

	_, err := bridge.InsertChain(detailedOf(head, tail))
	if !errors.Is(err, verifier.ErrMChangesHashInvalid) {
		t.Fatalf("expected the side chain to be rejected on its changes-hash, got %v", err)
	}
	// The head passes every check that runs before the rollback, so the
	// rollback happens; the failure comes from the replacement state.
	expectRolledBack(t, counting)
	expectChainEquals(t, z.Chain(), before)
}

// TestInsertChain_SideChainFromNonProducerIsRejectedBeforeRollback pins the
// cost of triggering a rollback: a candidate that is internally consistent
// but not signed by the pillar elected for its slot is refused before any
// canonical momentum is removed.
func TestInsertChain_SideChainFromNonProducerIsRejectedBeforeRollback(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	counting, bridge := newSideChainBridge(z)
	before := snapshotChain(t, z.Chain())

	target := momentumAt(t, z.Chain(), 5)
	head := attackerMomentum(target, 6)
	tail := attackerMomentum(head, 7)

	_, err := bridge.InsertChain(detailedOf(head, tail))
	if !errors.Is(err, verifier.ErrMProducerInvalid) {
		t.Fatalf("expected the side chain to be rejected on its producer, got %v", err)
	}
	common.Expect(t, counting.rollbacks, 0)
	expectChainEquals(t, z.Chain(), before)
}

// TestInsertChain_SideChainHeadFailingStateChecksIsRejectedBeforeRollback
// pins that the head is verified against the state at the fork point before
// the rollback: it is signed by the elected producer, so only the verifier
// can refuse it, and it belongs to another chain.
func TestInsertChain_SideChainHeadFailingStateChecksIsRejectedBeforeRollback(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	counting, bridge := newSideChainBridge(z)
	before := snapshotChain(t, z.Chain())

	target := momentumAt(t, z.Chain(), 5)
	head := unsignedMomentum(target, 6)
	head.ChainIdentifier = target.ChainIdentifier + 1
	head = signedBy(head, electedKey(t, z, *head.Timestamp))
	tail := attackerMomentum(head, 7)

	_, err := bridge.InsertChain(detailedOf(head, tail))
	if !errors.Is(err, verifier.ErrABChainIdentifierMismatch) {
		t.Fatalf("expected the side chain to be rejected on its chain identifier, got %v", err)
	}
	common.Expect(t, counting.rollbacks, 0)
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
// alternative branch forking one below the original branch's lowest momentum.
type forkFixture struct {
	z        mock.MockZenon
	original chainSnapshot
	// removed is how many momentums the original branch has above the fork
	// point.
	removed int
	fork    []*nom.DetailedMomentum
}

// buildFork produces, on a fresh node that agrees with z up to forkBase, a
// genuine branch spanning forkBase+1..forkTop whose first momentum carries a
// send of amount from User1 to User2, so that branches built with different
// amounts differ from each other and from z's own branch. Both nodes share
// genesis and a deterministic clock, so the momentums up to forkBase are
// byte-identical on both.
func buildFork(t *testing.T, z mock.MockZenon, forkBase, forkTop uint64, amount int64) []*nom.DetailedMomentum {
	t.Helper()
	other := mock.NewMockZenon(t)
	defer other.StopPanic()
	other.InsertMomentumsTo(forkBase)
	common.Expect(t, momentumAt(t, other.Chain(), forkBase).Hash, momentumAt(t, z.Chain(), forkBase).Hash)
	other.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(amount),
	}, nil, mock.SkipVmChanges)
	other.InsertMomentumsTo(forkTop)
	return prefetchAbove(t, other, forkBase, forkTop)
}

// prefetchAbove returns the momentums of z at heights from+1..to with their
// account blocks.
func prefetchAbove(t *testing.T, z mock.MockZenon, from, to uint64) []*nom.DetailedMomentum {
	t.Helper()
	branch := make([]*nom.DetailedMomentum, 0, to-from)
	for height := from + 1; height <= to; height++ {
		detailed, err := z.Chain().GetFrontierMomentumStore().PrefetchMomentum(momentumAt(t, z.Chain(), height))
		common.FailIfErr(t, err)
		branch = append(branch, detailed)
	}
	return branch
}

// newForkFixture builds the original branch on one node and the fork on a
// second node so that neither branch has to be rolled back to reach the fork
// point; the cache only keeps 100 rollback steps, which a long fork exceeds.
// The original branch spans heights forkBase+1..originalTop, the fork spans
// forkBase+1..forkTop.
func newForkFixture(t *testing.T, originalTop, forkTop uint64) *forkFixture {
	const forkBase = uint64(5)
	z := mock.NewMockZenon(t)
	z.InsertMomentumsTo(originalTop)

	fork := buildFork(t, z, forkBase, forkTop, 1)
	if fork[0].Momentum.Hash == momentumAt(t, z.Chain(), forkBase+1).Hash {
		t.Fatal("fork did not diverge from the original branch")
	}

	return &forkFixture{
		z:        z,
		original: snapshotChain(t, z.Chain()),
		removed:  int(originalTop - forkBase),
		fork:     fork,
	}
}

// expectFrontierAt asserts both the canonical and the cache frontier sit at
// the given momentum.
func expectFrontierAt(t *testing.T, c chain.Chain, momentum *nom.Momentum) {
	t.Helper()
	common.Expect(t, c.GetFrontierMomentumStore().Identifier(), momentum.Identifier())
	common.Expect(t, c.GetFrontierCacheStore().Identifier(), momentum.Identifier())
}

func TestInsertChain_ValidLongerSideChainReplacesBranch(t *testing.T) {
	f := newForkFixture(t, 6, 7)
	defer f.z.StopPanic()
	counting, bridge := newSideChainBridge(f.z)

	_, err := bridge.InsertChain(f.fork)
	common.FailIfErr(t, err)
	common.Expect(t, counting.rollbacks, 1)
	common.Expect(t, f.z.Chain().GetFrontierMomentumStore().Identifier(), f.fork[1].Momentum.Identifier())
	common.Expect(t, f.z.Chain().GetFrontierCacheStore().Identifier(), f.fork[1].Momentum.Identifier())
}

func TestInsertChain_SideChainRestoredWhenLaterCandidateFails(t *testing.T) {
	f := newForkFixture(t, 6, 7)
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

// TestInsertChain_RestoreBoundaryAtRemovedBranchLength pins the rule that
// decides between restoring the original branch and keeping the committed
// replacement prefix: the original comes back only while the prefix is no
// longer than it. A longer prefix is a valid chain longer than the one the
// node had, so it stays, exactly as it would have before restores existed.
func TestInsertChain_RestoreBoundaryAtRemovedBranchLength(t *testing.T) {
	cases := []struct {
		name      string
		committed int
		restored  bool
	}{
		{"prefix shorter than removed branch is restored", 1, true},
		{"prefix as long as removed branch is restored", 2, true},
		{"prefix longer than removed branch is kept", 3, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newForkFixture(t, 7, 9)
			defer f.z.StopPanic()
			common.Expect(t, f.removed, 2)
			counting, bridge := newSideChainBridge(f.z)

			// The genuine prefix, then invalid candidates up to a height that
			// keeps the side chain taller than our frontier.
			candidates := append([]*nom.DetailedMomentum{}, f.fork[:tc.committed]...)
			candidates = append(candidates, invalidTail(f.fork[tc.committed-1].Momentum, 10)...)

			index, err := bridge.InsertChain(candidates)
			if err == nil {
				t.Fatal("expected the side chain to be rejected")
			}
			common.Expect(t, index, tc.committed)
			if tc.restored {
				common.Expect(t, counting.rollbacks, 2)
				expectChainEquals(t, f.z.Chain(), f.original)
			} else {
				common.Expect(t, counting.rollbacks, 1)
				expectFrontierAt(t, f.z.Chain(), f.fork[tc.committed-1].Momentum)
			}

			// Whatever was kept is real state: the rest of the genuine fork
			// still inserts on top of it.
			_, err = bridge.InsertChain(f.fork)
			common.FailIfErr(t, err)
			expectFrontierAt(t, f.z.Chain(), f.fork[len(f.fork)-1].Momentum)
		})
	}
}

// TestInsertChain_ReplacementPrefixBeyondCacheWindowIsKept commits more
// replacement momentums than the cache keeps rollback steps for, then fails
// the next candidate. A restore would have to roll the cache back further
// than it can go, after the canonical chain has already been rolled back,
// and the node would exit with a cache it cannot reconcile on restart. The
// committed prefix is longer than the removed branch, so it stays instead.
func TestInsertChain_ReplacementPrefixBeyondCacheWindowIsKept(t *testing.T) {
	committed := storage.GetRollbackCacheSize() + 1
	f := newForkFixture(t, 6, uint64(5+committed+1))
	defer f.z.StopPanic()
	counting, bridge := newSideChainBridge(f.z)

	candidates := append([]*nom.DetailedMomentum{}, f.fork[:committed]...)
	last := f.fork[committed-1].Momentum
	candidates = append(candidates, invalidTail(last, last.Height+1)...)

	index, err := bridge.InsertChain(candidates)
	if err == nil {
		t.Fatal("expected the side chain to be rejected")
	}
	common.Expect(t, index, committed)
	common.Expect(t, counting.rollbacks, 1)
	expectFrontierAt(t, f.z.Chain(), last)

	// The node carries on from the kept prefix.
	_, err = bridge.InsertChain(f.fork[committed:])
	common.FailIfErr(t, err)
	expectFrontierAt(t, f.z.Chain(), f.fork[len(f.fork)-1].Momentum)
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
	head = producerMomentum(t, z, deepest, 11)
	tail = attackerMomentum(head, 41)
	_, err = bridge.InsertChain(detailedOf(head, tail))
	if err == nil {
		t.Fatal("expected the side chain to be rejected")
	}
	expectRolledBack(t, counting)
	expectChainEquals(t, z.Chain(), before)
}

// storedPatchDumps returns the serialized state patch of every momentum above
// identifier exactly as the momentum store holds it, frontier writes included.
// It deliberately does not go through CaptureBranchAbove, which strips those
// writes and would hide the very growth this is used to detect. The accessor
// is a test oracle on the concrete chain, not part of the Chain interface.
func storedPatchDumps(t *testing.T, c chain.Chain, identifier types.HashHeight) [][]byte {
	t.Helper()
	patches, ok := c.(interface {
		GetMomentumPatch(identifier types.HashHeight) db.Patch
	})
	if !ok {
		t.Fatalf("chain %T does not expose its stored momentum patches", c)
	}
	frontier := c.GetFrontierMomentumStore().Identifier()
	dumps := make([][]byte, 0, frontier.Height-identifier.Height)
	for height := identifier.Height + 1; height <= frontier.Height; height++ {
		patch := patches.GetMomentumPatch(momentumAt(t, c, height).Identifier())
		if patch == nil {
			t.Fatalf("no stored patch for height %d", height)
		}
		dumps = append(dumps, patch.Dump())
	}
	return dumps
}

func TestInsertChain_RepeatedRestoresKeepStoredPatchesIdentical(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	counting, bridge := newSideChainBridge(z)
	before := snapshotChain(t, z.Chain())
	target := momentumAt(t, z.Chain(), 4).Identifier()
	patchesBefore := storedPatchDumps(t, z.Chain(), target)

	for attempt := 0; attempt < 3; attempt++ {
		head := producerMomentum(t, z, momentumAt(t, z.Chain(), 4), 5)
		tail := attackerMomentum(head, 7)
		_, err := bridge.InsertChain(detailedOf(head, tail))
		if err == nil {
			t.Fatalf("attempt %d: expected the side chain to be rejected", attempt)
		}
		common.Expect(t, counting.rollbacks, 2*(attempt+1))
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

// TestInsertChain_RestoreBoundaryAtDepthLimit runs the restore/keep decision
// at the deepest fork the depth check admits: a removed branch of 30. A
// committed prefix of 30 is restored, which is the longest restore there is,
// and a prefix of 31 is kept.
func TestInsertChain_RestoreBoundaryAtDepthLimit(t *testing.T) {
	cases := []struct {
		name      string
		committed int
		restored  bool
	}{
		{"prefix as long as the 30-deep removed branch is restored", 30, true},
		{"prefix longer than the 30-deep removed branch is kept", 31, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newForkFixture(t, 35, 37)
			defer f.z.StopPanic()
			common.Expect(t, f.removed, 30)
			counting, bridge := newSideChainBridge(f.z)

			candidates := append([]*nom.DetailedMomentum{}, f.fork[:tc.committed]...)
			last := f.fork[tc.committed-1].Momentum
			candidates = append(candidates, invalidTail(last, last.Height+1)...)

			index, err := bridge.InsertChain(candidates)
			if err == nil {
				t.Fatal("expected the side chain to be rejected")
			}
			common.Expect(t, index, tc.committed)
			if tc.restored {
				common.Expect(t, counting.rollbacks, 2)
				expectChainEquals(t, f.z.Chain(), f.original)
			} else {
				common.Expect(t, counting.rollbacks, 1)
				expectFrontierAt(t, f.z.Chain(), last)
			}

			_, err = bridge.InsertChain(f.fork)
			common.FailIfErr(t, err)
			expectFrontierAt(t, f.z.Chain(), f.fork[len(f.fork)-1].Momentum)
		})
	}
}

// TestInsertChain_KnownPrefixOffsetsReturnedIndex feeds momentums the node
// already has ahead of the side chain, as a downloader batch that starts
// below the fork point does. The known prefix is skipped, the restore/keep
// decision counts only the committed candidates, and the returned index still
// points at the failing candidate's position in the batch as given.
func TestInsertChain_KnownPrefixOffsetsReturnedIndex(t *testing.T) {
	f := newForkFixture(t, 7, 9)
	defer f.z.StopPanic()
	common.Expect(t, f.removed, 2)
	counting, bridge := newSideChainBridge(f.z)

	store := f.z.Chain().GetFrontierMomentumStore()
	known := make([]*nom.DetailedMomentum, 0, 2)
	for height := uint64(4); height <= 5; height++ {
		detailed, err := store.PrefetchMomentum(momentumAt(t, f.z.Chain(), height))
		common.FailIfErr(t, err)
		known = append(known, detailed)
	}

	// Two known momentums, two genuine candidates that commit, then an
	// invalid one: as many committed as removed, so the branch is restored.
	candidates := append([]*nom.DetailedMomentum{}, known...)
	candidates = append(candidates, f.fork[:2]...)
	candidates = append(candidates, invalidTail(f.fork[1].Momentum, 10)...)

	index, err := bridge.InsertChain(candidates)
	if err == nil {
		t.Fatal("expected the side chain to be rejected")
	}
	common.Expect(t, index, len(known)+2)
	common.Expect(t, counting.rollbacks, 2)
	expectChainEquals(t, f.z.Chain(), f.original)

	// The same known prefix ahead of the genuine fork still replaces the
	// branch.
	candidates = append(append([]*nom.DetailedMomentum{}, known...), f.fork...)
	_, err = bridge.InsertChain(candidates)
	common.FailIfErr(t, err)
	expectFrontierAt(t, f.z.Chain(), f.fork[len(f.fork)-1].Momentum)
}

// TestInsertChain_RestoreAfterCommitFailsPastCacheUpdate fails a candidate at
// the last step, after its cache entry is written and before its momentum is
// committed. The insert path compensates by rolling the cache back one step;
// the restore then has to start from a chain and cache that agree, and put
// the original branch back on both.
func TestInsertChain_RestoreAfterCommitFailsPastCacheUpdate(t *testing.T) {
	f := newForkFixture(t, 7, 9)
	defer f.z.StopPanic()
	common.Expect(t, f.removed, 2)
	counting, bridge := newSideChainBridge(f.z)

	// The first candidate commits, the second passes every check and fails
	// only at the commit.
	counting.failCommitOf = f.fork[1].Momentum.Hash
	index, err := bridge.InsertChain(f.fork)
	if err == nil {
		t.Fatal("expected the side chain to be rejected")
	}
	common.Expect(t, index, 1)
	common.Expect(t, counting.rollbacks, 2)
	expectChainEquals(t, f.z.Chain(), f.original)

	// With the fault gone the same fork replaces the branch.
	counting.failCommitOf = types.Hash{}
	_, err = bridge.InsertChain(f.fork)
	common.FailIfErr(t, err)
	expectFrontierAt(t, f.z.Chain(), f.fork[len(f.fork)-1].Momentum)
}

// momentumEvent is one notification a chain listener received.
type momentumEvent struct {
	kind       string
	identifier types.HashHeight
}

// recordingListener keeps every momentum event in the order it was delivered.
type recordingListener struct {
	events []momentumEvent
}

func (l *recordingListener) InsertMomentum(detailed *nom.DetailedMomentum) {
	l.events = append(l.events, momentumEvent{"insert", detailed.Momentum.Identifier()})
}
func (l *recordingListener) DeleteMomentum(detailed *nom.DetailedMomentum) {
	l.events = append(l.events, momentumEvent{"delete", detailed.Momentum.Identifier()})
}

// TestInsertChain_ListenersSeeDeleteThenInsertOnRestore pins the event
// sequence a restore produces: the removed branch is announced as deleted,
// the committed prefix as inserted then deleted, and the original momentums
// as inserted again. Nothing is suppressed or replayed out of order.
func TestInsertChain_ListenersSeeDeleteThenInsertOnRestore(t *testing.T) {
	f := newForkFixture(t, 7, 9)
	defer f.z.StopPanic()
	common.Expect(t, f.removed, 2)
	_, bridge := newSideChainBridge(f.z)
	original6 := momentumAt(t, f.z.Chain(), 6).Identifier()
	original7 := momentumAt(t, f.z.Chain(), 7).Identifier()

	listener := &recordingListener{}
	f.z.Chain().Register(listener)
	defer f.z.Chain().UnRegister(listener)

	// One genuine candidate commits, the next fails: the original two come
	// back.
	candidates := append([]*nom.DetailedMomentum{}, f.fork[:1]...)
	candidates = append(candidates, invalidTail(f.fork[0].Momentum, 8)...)
	_, err := bridge.InsertChain(candidates)
	if err == nil {
		t.Fatal("expected the side chain to be rejected")
	}
	expectChainEquals(t, f.z.Chain(), f.original)

	expected := []momentumEvent{
		{"delete", original7},
		{"delete", original6},
		{"insert", f.fork[0].Momentum.Identifier()},
		{"delete", f.fork[0].Momentum.Identifier()},
		{"insert", original6},
		{"insert", original7},
	}
	common.Expect(t, len(listener.events), len(expected))
	for i := range expected {
		common.Expect(t, listener.events[i], expected[i])
	}
}

// TestInsertChain_PendingPoolAfterRestore pins what a restore does to the
// account pool, which it deliberately does not restore. Blocks that were
// pending before the side chain arrived are dropped by the rollback, exactly
// as any rollback drops them. A valid block carried by the failing candidate
// was force-inserted before the candidate failed and stays pending, as if a
// peer had relayed it on its own.
func TestInsertChain_PendingPoolAfterRestore(t *testing.T) {
	f := newForkFixture(t, 6, 7)
	defer f.z.StopPanic()
	_, bridge := newSideChainBridge(f.z)

	pending := f.z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     g.User1.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(1),
	}, nil, mock.SkipVmChanges)
	common.Expect(t, len(f.z.Chain().GetAllUncommittedAccountBlocks()), 1)

	// A candidate carrying the fork's genuine account block, signed by the
	// elected producer but advertising a changes-hash the node can't
	// reproduce: the block applies, the momentum fails.
	target := momentumAt(t, f.z.Chain(), 5)
	head := unsignedMomentum(target, 6)
	head.Content = f.fork[0].Momentum.Content
	head = signedBy(head, electedKey(t, f.z, *head.Timestamp))
	carried := f.fork[0].AccountBlocks
	if len(carried) == 0 {
		t.Fatal("fork head carries no account block")
	}
	candidates := []*nom.DetailedMomentum{
		{Momentum: head, AccountBlocks: carried},
		detailedOf(attackerMomentum(head, 7))[0],
	}

	_, err := bridge.InsertChain(candidates)
	if err == nil {
		t.Fatal("expected the side chain to be rejected")
	}
	expectChainEquals(t, f.z.Chain(), f.original)

	uncommitted := f.z.Chain().GetAllUncommittedAccountBlocks()
	common.Expect(t, len(uncommitted), len(carried))
	for i, block := range carried {
		common.Expect(t, uncommitted[i].Hash, block.Hash)
	}
	for _, block := range uncommitted {
		if block.Hash == pending.Hash {
			t.Fatal("block pending before the side chain survived the rollback")
		}
	}
}

// TestInsertChain_SecondForkAtSameForkPointReplacesFirst replaces the
// original branch with one fork and then with a longer fork from the same
// fork point. The head of each fork is verified against the state at the
// fork point before the rollback. That state is read through a historical
// view whose rollback overlay is cached against the frontier it was built
// under; once the first fork has replaced the original branch, an overlay
// kept from before that replacement would skip the first fork's rollbacks
// and show its account blocks at the fork point, so the second fork's head
// would fail on the account chains the first fork touched.
func TestInsertChain_SecondForkAtSameForkPointReplacesFirst(t *testing.T) {
	f := newForkFixture(t, 6, 7)
	defer f.z.StopPanic()
	second := buildFork(t, f.z, 5, 8, 2)
	if second[0].Momentum.Hash == f.fork[0].Momentum.Hash {
		t.Fatal("second fork did not diverge from the first")
	}
	counting, bridge := newSideChainBridge(f.z)

	_, err := bridge.InsertChain(f.fork)
	common.FailIfErr(t, err)
	common.Expect(t, counting.rollbacks, 1)
	expectFrontierAt(t, f.z.Chain(), f.fork[len(f.fork)-1].Momentum)

	_, err = bridge.InsertChain(second)
	common.FailIfErr(t, err)
	common.Expect(t, counting.rollbacks, 2)
	expectFrontierAt(t, f.z.Chain(), second[len(second)-1].Momentum)
}

// forkAfterGap builds, on a fresh node that agrees with z up to forkBase, a
// genuine branch whose first momentum sits two ticks after the momentum at
// forkBase, in a slot for which the branch's own election and the election
// z's frontier implies name different producers. The branch's election is
// seeded by its last momentum before the tick's proof time, which is the
// momentum at forkBase; z's is seeded by z's last momentum before that
// time, which lies above forkBase. The branch spans forkBase+1..forkTop.
func forkAfterGap(t *testing.T, z mock.MockZenon, forkBase, forkTop uint64) []*nom.DetailedMomentum {
	t.Helper()
	other := mock.NewMockZenon(t)
	defer other.StopPanic()
	other.InsertMomentumsTo(forkBase)
	base := momentumAt(t, other.Chain(), forkBase)
	common.Expect(t, base.Hash, momentumAt(t, z.Chain(), forkBase).Hash)

	genesis := z.Chain().GetGenesisMomentum().Timestamp
	var slot time.Time
	var producer *types.Address
	for offset := 2 * 300; offset < 3*300; offset += 10 {
		candidate := genesis.Add(time.Duration(offset) * time.Second)
		ours, err := z.Consensus().GetMomentumProducer(candidate)
		common.FailIfErr(t, err)
		theirs, err := other.Consensus().GetMomentumProducer(candidate)
		common.FailIfErr(t, err)
		if *ours != *theirs {
			slot, producer = candidate, theirs
			break
		}
	}
	if producer == nil {
		t.Fatal("both elections name the same producer for every slot of the tick")
	}

	template := &nom.Momentum{
		Version:         base.Version,
		ChainIdentifier: base.ChainIdentifier,
		PreviousHash:    base.Hash,
		Height:          base.Height + 1,
		TimestampUnix:   uint64(slot.Unix()),
		Content:         nom.NewMomentumContent(nil),
	}
	template.EnsureCache()
	detailed := &nom.DetailedMomentum{Momentum: template, AccountBlocks: []*nom.AccountBlock{}}
	transaction, err := vm.NewSupervisor(other.Chain(), other.Consensus()).GenerateMomentum(detailed, pillarKey(t, *producer).Signer)
	common.FailIfErr(t, err)
	other.Broadcaster().CreateMomentum(transaction, detailed)
	other.InsertMomentumsTo(forkTop)
	return prefetchAbove(t, other, forkBase, forkTop)
}

// TestInsertChain_ForkHeadIsJudgedByItsOwnElection pins which election the
// pre-rollback producer check uses. A genuine fork whose first momentum comes
// after a gap, while our branch kept producing, is elected by a proof block
// at the fork point; our frontier implies a proof block above it and names
// another producer for that slot. The fork is valid and longer, so it has to
// replace our branch.
func TestInsertChain_ForkHeadIsJudgedByItsOwnElection(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	fork := forkAfterGap(t, z, 5, 7)
	counting, bridge := newSideChainBridge(z)

	// The election our frontier implies rejects the head.
	isProducer, err := z.Consensus().VerifyMomentumProducer(fork[0].Momentum)
	common.FailIfErr(t, err)
	common.Expect(t, isProducer, false)

	_, err = bridge.InsertChain(fork)
	common.FailIfErr(t, err)
	common.Expect(t, counting.rollbacks, 1)
	expectFrontierAt(t, z.Chain(), fork[len(fork)-1].Momentum)
}
