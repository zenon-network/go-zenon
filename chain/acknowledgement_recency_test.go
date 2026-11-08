package chain

import (
	"errors"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// The tests in this file drive AcknowledgementRequirement against a
// scripted chain: momentum hashes by height, the spork records at the
// parent, and a chain cache that reports each spork active from a chosen
// height. The cache answer is scripted separately from the spork records
// so that the delayed-activation case (record committed after its
// EnforcementHeight) can be expressed.

func ackHash(height uint64) types.Hash {
	return types.Hash{0xac, byte(height >> 8), byte(height)}
}

func ackAt(height uint64) types.HashHeight {
	return types.HashHeight{Hash: ackHash(height), Height: height}
}

func canonicalAt(height uint64) (*nom.Momentum, error) {
	return &nom.Momentum{Hash: ackHash(height), Height: height}, nil
}

// scriptedCache answers IsSporkActive from activeFrom: a spork is active
// at the cache's height when it is listed with a height at or below it.
type scriptedCache struct {
	store.Cache
	identifier types.HashHeight
	activeFrom map[types.Hash]uint64
	err        error
}

func (c *scriptedCache) Identifier() types.HashHeight { return c.identifier }
func (c *scriptedCache) IsSporkActive(spork *types.ImplementedSpork) (bool, error) {
	if c.err != nil {
		return false, c.err
	}
	from, ok := c.activeFrom[spork.SporkId]
	return ok && from <= c.identifier.Height, nil
}

func scriptedCacheAt(activeFrom map[types.Hash]uint64) CacheStoreAt {
	return func(identifier types.HashHeight) store.Cache {
		return &scriptedCache{identifier: identifier, activeFrom: activeFrom}
	}
}

func enforcedSpork(id types.Hash, height uint64) *definition.Spork {
	return &definition.Spork{Id: id, Activated: true, EnforcementHeight: height}
}

func sporkList(sporks ...*definition.Spork) func() ([]*definition.Spork, error) {
	return func() ([]*definition.Spork, error) { return sporks, nil }
}

func userBlock(address types.Address, height uint64, acknowledged types.HashHeight) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:              address,
		BlockType:            nom.BlockTypeUserSend,
		Height:               height,
		MomentumAcknowledged: acknowledged,
	}
}

var (
	ackUser      = types.Address{0, 0xa1}
	ackOtherUser = types.Address{0, 0xa2}
	otherSporkId = types.Hash{0x0b}
)

// withDynamicPlasmaSporkId gives the gate, DynamicPlasmaSpork, a test id
// for the duration of t, as the release flow does when the placeholder is
// replaced.
func withDynamicPlasmaSporkId(t *testing.T) types.Hash {
	saved := types.DynamicPlasmaSpork.SporkId
	id := types.Hash{0x0a}
	types.DynamicPlasmaSpork.SporkId = id
	t.Cleanup(func() { types.DynamicPlasmaSpork.SporkId = saved })
	return id
}

func mustRequirement(t *testing.T, parent uint64, sporks func() ([]*definition.Spork, error), cacheAt CacheStoreAt) *AcknowledgementRequirement {
	t.Helper()
	requirement, err := newAcknowledgementRequirement(ackAt(parent), sporks, canonicalAt, cacheAt)
	common.FailIfErr(t, err)
	if requirement == nil {
		t.Fatalf("expected the rule to apply at parent %d", parent)
	}
	return requirement
}

// Timely activation: the gate is enforced at H and its record is in the
// cache from H. The momentum at H+1 (parent H) rejects an acknowledgement
// of H-1 and accepts one of H; the momentum at H (parent H-1) is not
// subject to the rule at all.
func TestAcknowledgementRequirement_StaleAcknowledgementRejectedAfterEnforcement(t *testing.T) {
	gate := withDynamicPlasmaSporkId(t)
	const H = 100
	cache := scriptedCacheAt(map[types.Hash]uint64{gate: H})
	sporks := sporkList(enforcedSpork(gate, H))

	atH, err := newAcknowledgementRequirement(ackAt(H-1), sporks, canonicalAt, cache)
	common.FailIfErr(t, err)
	if atH != nil {
		t.Fatalf("rule must not apply to the momentum at H, whose parent predates enforcement")
	}
	common.FailIfErr(t, atH.Check(userBlock(ackUser, 1, ackAt(H-2))))

	afterH := mustRequirement(t, H, sporks, cache)
	if err := afterH.Check(userBlock(ackUser, 1, ackAt(H-1))); !errors.Is(err, ErrStaleMomentumAcknowledged) {
		t.Fatalf("acknowledging H-1 at parent H: want ErrStaleMomentumAcknowledged, got %v", err)
	}
	common.FailIfErr(t, afterH.Check(userBlock(ackUser, 1, ackAt(H))))

	later := mustRequirement(t, H+50, sporks, cache)
	common.FailIfErr(t, later.Check(userBlock(ackUser, 1, ackAt(H+10))))
	if err := later.Check(userBlock(ackUser, 1, ackAt(H-1))); !errors.Is(err, ErrStaleMomentumAcknowledged) {
		t.Fatalf("acknowledging H-1 at parent H+50: want ErrStaleMomentumAcknowledged, got %v", err)
	}
}

// Two sporks enforced at the parent: an acknowledgement that has only the
// first is rejected, one that has both passes, whichever order the
// records are enumerated in.
func TestAcknowledgementRequirement_TwoSporksEnforced(t *testing.T) {
	gate := withDynamicPlasmaSporkId(t)
	cache := scriptedCacheAt(map[types.Hash]uint64{gate: 100, otherSporkId: 200})
	orders := [][]*definition.Spork{
		{enforcedSpork(gate, 100), enforcedSpork(otherSporkId, 200)},
		{enforcedSpork(otherSporkId, 200), enforcedSpork(gate, 100)},
	}
	for _, order := range orders {
		requirement := mustRequirement(t, 250, sporkList(order...), cache)
		if err := requirement.Check(userBlock(ackUser, 1, ackAt(150))); !errors.Is(err, ErrStaleMomentumAcknowledged) {
			t.Fatalf("acknowledgement with only the gate enforced: want ErrStaleMomentumAcknowledged, got %v", err)
		}
		common.FailIfErr(t, requirement.Check(userBlock(ackUser, 1, ackAt(200))))
	}
}

// A defined but inactive spork and one activated but not yet enforced at
// the parent do not enter E(P).
func TestAcknowledgementRequirement_InactiveAndScheduledSporksImposeNothing(t *testing.T) {
	gate := withDynamicPlasmaSporkId(t)
	inactive := types.Hash{0x0c}
	cache := scriptedCacheAt(map[types.Hash]uint64{gate: 100})
	requirement := mustRequirement(t, 150, sporkList(
		enforcedSpork(gate, 100),
		&definition.Spork{Id: inactive},
		enforcedSpork(otherSporkId, 151), // scheduled for the momentum after the parent
	), cache)
	common.FailIfErr(t, requirement.Check(userBlock(ackUser, 1, ackAt(120))))

	// The same scheduled spork does gate once it is enforced at the parent.
	later := mustRequirement(t, 151, sporkList(enforcedSpork(gate, 100), enforcedSpork(otherSporkId, 151)),
		scriptedCacheAt(map[types.Hash]uint64{gate: 100, otherSporkId: 151}))
	if err := later.Check(userBlock(ackUser, 1, ackAt(150))); !errors.Is(err, ErrStaleMomentumAcknowledged) {
		t.Fatalf("want ErrStaleMomentumAcknowledged once the scheduled spork is enforced, got %v", err)
	}
}

// Delayed activation: the activation send is confirmed at A, so the record
// says EnforcementHeight A+6, but the contract receive that writes it is
// confirmed at R > A+6, and the cache sees the spork only from R. An
// acknowledgement in [A+6, R) clears the height but not the set, and is
// rejected; one in [R, P] passes.
func TestAcknowledgementRequirement_DelayedActivationReceive(t *testing.T) {
	gate := withDynamicPlasmaSporkId(t)
	const A, R, P = 100, 120, 130
	requirement := mustRequirement(t, P, sporkList(enforcedSpork(gate, A+6)),
		scriptedCacheAt(map[types.Hash]uint64{gate: R}))
	for _, acknowledged := range []uint64{A + 6, R - 1} {
		if err := requirement.Check(userBlock(ackUser, 1, ackAt(acknowledged))); !errors.Is(err, ErrStaleMomentumAcknowledged) {
			t.Fatalf("acknowledging %d: want ErrStaleMomentumAcknowledged, got %v", acknowledged, err)
		}
	}
	for _, acknowledged := range []uint64{R, P} {
		common.FailIfErr(t, requirement.Check(userBlock(ackUser, 1, ackAt(acknowledged))))
	}
}

// A parent without the gate enforced builds no requirement, and a nil
// requirement passes every block and filters nothing.
func TestAcknowledgementRequirement_PreActivationParentUnaffected(t *testing.T) {
	gate := withDynamicPlasmaSporkId(t)
	for _, sporks := range [][]*definition.Spork{
		nil,
		{enforcedSpork(otherSporkId, 10)},
		{{Id: gate}},
		{enforcedSpork(gate, 101)},
	} {
		requirement, err := newAcknowledgementRequirement(ackAt(100), sporkList(sporks...), canonicalAt, nil)
		common.FailIfErr(t, err)
		if requirement != nil {
			t.Fatalf("rule applied with sporks %v", sporks)
		}
	}
	// Genesis is never subject to it, without reading sporks at all.
	requirement, err := newAcknowledgementRequirement(ackAt(1), func() ([]*definition.Spork, error) {
		t.Fatal("sporks read at genesis")
		return nil, nil
	}, canonicalAt, nil)
	common.FailIfErr(t, err)
	if requirement != nil {
		t.Fatal("rule applied at genesis")
	}

	var none *AcknowledgementRequirement
	blocks := []*nom.AccountBlock{userBlock(ackUser, 1, ackAt(1))}
	common.FailIfErr(t, none.Check(blocks[0]))
	if got := none.Filter(blocks, nil); &got[0] != &blocks[0] || len(got) != 1 {
		t.Fatal("a nil requirement must return the input slice itself")
	}
}

// Embedded account blocks are exempt: contract receives, their batched
// sends, whatever they acknowledge.
func TestAcknowledgementRequirement_EmbeddedBlocksExempt(t *testing.T) {
	gate := withDynamicPlasmaSporkId(t)
	requirement := mustRequirement(t, 150, sporkList(enforcedSpork(gate, 100)), nil)
	for _, blockType := range []uint64{nom.BlockTypeContractReceive, nom.BlockTypeContractSend} {
		block := &nom.AccountBlock{Address: types.PlasmaContract, BlockType: blockType, Height: 5, MomentumAcknowledged: ackAt(3)}
		common.FailIfErr(t, requirement.Check(block))
	}
}

// An acknowledgement that is not the canonical momentum at its height, or
// is above the parent, fails the rule.
func TestAcknowledgementRequirement_NonCanonicalAcknowledgementRejected(t *testing.T) {
	gate := withDynamicPlasmaSporkId(t)
	requirement := mustRequirement(t, 150, sporkList(enforcedSpork(gate, 100)),
		scriptedCacheAt(map[types.Hash]uint64{gate: 100}))
	for _, acknowledged := range []types.HashHeight{
		{Hash: types.Hash{0xff}, Height: 140},
		ackAt(151),
	} {
		if err := requirement.Check(userBlock(ackUser, 1, acknowledged)); !errors.Is(err, ErrMomentumAcknowledgedNotCanonical) {
			t.Fatalf("acknowledging %v: want ErrMomentumAcknowledgedNotCanonical, got %v", acknowledged, err)
		}
	}
}

// A store that cannot be read, while computing either set, is an error,
// never a pass and never a rejection.
func TestAcknowledgementRequirement_SporkReadFailureIsError(t *testing.T) {
	gate := withDynamicPlasmaSporkId(t)
	readErr := errors.New("injected read failure")

	// E(P) unreadable.
	requirement, err := newAcknowledgementRequirement(ackAt(150), func() ([]*definition.Spork, error) {
		return nil, readErr
	}, canonicalAt, nil)
	if !errors.Is(err, readErr) || requirement != nil {
		t.Fatalf("unreadable sporks at the parent: want the read error and no requirement, got %v, %v", requirement, err)
	}

	sporks := sporkList(enforcedSpork(gate, 100))
	block := userBlock(ackUser, 1, ackAt(120))
	cases := map[string]*AcknowledgementRequirement{
		"cache read error": mustRequirement(t, 150, sporks, func(identifier types.HashHeight) store.Cache {
			return &scriptedCache{identifier: identifier, err: readErr}
		}),
		"no cache store":  mustRequirement(t, 150, sporks, func(types.HashHeight) store.Cache { return nil }),
		"no cache reader": mustRequirement(t, 150, sporks, nil),
	}
	momentumErr, err := newAcknowledgementRequirement(ackAt(150), sporks, func(uint64) (*nom.Momentum, error) {
		return nil, readErr
	}, scriptedCacheAt(map[types.Hash]uint64{gate: 100}))
	common.FailIfErr(t, err)
	cases["momentum read error"] = momentumErr

	for name, requirement := range cases {
		err := requirement.Check(block)
		if err == nil || IsAcknowledgementRejection(err) {
			t.Fatalf("%s: want a read error, got %v", name, err)
		}
	}
}

// NewAcknowledgementRequirement reads E(P) from the spork contract's
// storage at the parent store and fails on an unreadable store.
func TestNewAcknowledgementRequirement_ReadsSporkContractStorage(t *testing.T) {
	gate := withDynamicPlasmaSporkId(t)
	storage := db.NewMemDB()
	enforcedSpork(gate, 100).Save(storage)
	parent := &recencyParentStore{identifier: ackAt(150), storage: storage}

	requirement, err := NewAcknowledgementRequirement(parent, scriptedCacheAt(map[types.Hash]uint64{gate: 100}))
	common.FailIfErr(t, err)
	if requirement == nil {
		t.Fatal("rule must apply when the gate is enforced in the spork contract's storage")
	}
	if err := requirement.Check(userBlock(ackUser, 1, ackAt(99))); !errors.Is(err, ErrStaleMomentumAcknowledged) {
		t.Fatalf("want ErrStaleMomentumAcknowledged, got %v", err)
	}

	if _, err := NewAcknowledgementRequirement(nil, nil); err == nil {
		t.Fatal("a missing parent store must be an error")
	}
}

// Filter drops an account's lowest failing block and every block above it,
// including a dependent block whose own acknowledgement passes, keeps the
// other accounts in their original order, and reports each cut once.
func TestAcknowledgementRequirement_FilterDropsFailingSuffix(t *testing.T) {
	gate := withDynamicPlasmaSporkId(t)
	requirement := mustRequirement(t, 150, sporkList(enforcedSpork(gate, 100)),
		scriptedCacheAt(map[types.Hash]uint64{gate: 100}))

	contract := &nom.AccountBlock{Address: types.PlasmaContract, BlockType: nom.BlockTypeContractReceive, Height: 9, MomentumAcknowledged: ackAt(50)}
	fresh := userBlock(ackOtherUser, 4, ackAt(140))
	freshAbove := userBlock(ackOtherUser, 5, ackAt(150))
	kept := userBlock(ackUser, 6, ackAt(120))
	stale := userBlock(ackUser, 7, ackAt(90))
	dependent := userBlock(ackUser, 8, ackAt(150)) // passes on its own
	blocks := []*nom.AccountBlock{contract, kept, fresh, stale, freshAbove, dependent}

	var reported []*nom.AccountBlock
	got := requirement.Filter(blocks, func(block *nom.AccountBlock, err error) {
		if !errors.Is(err, ErrStaleMomentumAcknowledged) {
			t.Fatalf("reported %v with %v", block.Header(), err)
		}
		reported = append(reported, block)
	})
	want := []*nom.AccountBlock{contract, kept, fresh, freshAbove}
	if len(got) != len(want) {
		t.Fatalf("kept %d blocks, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("block %d: got %v, want %v", i, got[i].Header(), want[i].Header())
		}
	}
	if len(reported) != 1 || reported[0] != stale {
		t.Fatalf("reported %v, want only the stale block", reported)
	}
	if len(blocks) != 6 || blocks[3] != stale {
		t.Fatal("Filter must not modify its input")
	}
}

// TestAcknowledgementRequirement_FilterBeforeLegacyLimits shows why the
// pre-dynamic-plasma producer filters the candidates before the legacy
// selection rather than after it: blocks the pool keeps because their
// acknowledgement could not be read must not use up the block-count cap.
func TestAcknowledgementRequirement_FilterBeforeLegacyLimits(t *testing.T) {
	gate := withDynamicPlasmaSporkId(t)
	setMaxAccountBlocksInMomentum(t, 2)
	unreadable := ackAt(60)
	cacheAt := func(identifier types.HashHeight) store.Cache {
		if identifier == unreadable {
			return nil
		}
		return scriptedCacheAt(map[types.Hash]uint64{gate: 100})(identifier)
	}
	newRequirement := func() *AcknowledgementRequirement {
		return mustRequirement(t, 150, sporkList(enforcedSpork(gate, 100)), cacheAt)
	}

	stuck1 := userBlock(ackUser, 1, unreadable)
	stuck2 := userBlock(ackUser, 2, ackAt(150))
	eligible := userBlock(ackOtherUser, 1, ackAt(140))
	candidates := []*nom.AccountBlock{stuck1, stuck2, eligible}
	ap := &accountPool{}

	if got := newRequirement().Filter(ap.SelectNewMomentumContent(candidates), nil); len(got) != 0 {
		t.Fatalf("selecting first kept %d blocks; the test no longer shows the crowding", len(got))
	}
	got := ap.SelectNewMomentumContent(newRequirement().Filter(candidates, nil))
	if len(got) != 1 || got[0] != eligible {
		t.Fatalf("got %d blocks, want only the eligible one", len(got))
	}
}

// recencyParentStore is the part of store.Momentum the requirement reads at
// the parent, plus what InsertMomentum's pricing refresh asks of it.
type recencyParentStore struct {
	store.Momentum
	identifier types.HashHeight
	storage    db.DB
}

func (s *recencyParentStore) Identifier() types.HashHeight { return s.identifier }
func (s *recencyParentStore) GetMomentumByHeight(height uint64) (*nom.Momentum, error) {
	return canonicalAt(height)
}
func (s *recencyParentStore) GetAccountStore(types.Address) store.Account {
	return &recencyAccountStore{storage: s.storage}
}
func (s *recencyParentStore) IsSporkActive(*types.ImplementedSpork) (bool, error) {
	return false, nil
}

type recencyAccountStore struct {
	store.Account
	storage db.DB
}

func (s *recencyAccountStore) Storage() db.DB { return s.storage }

// recencyStable is a Stable whose frontier momentum store carries the
// spork records the eviction pass reads.
type recencyStable struct {
	frontier *recencyParentStore
}

func (s *recencyStable) GetStableAccountDB(types.Address) db.DB { return db.NewMemDB() }
func (s *recencyStable) GetFrontierMomentumStore() store.Momentum {
	return s.frontier
}

// seedChain queues a user account chain in the pool, one block per
// acknowledged height, bypassing admission.
func seedChain(t *testing.T, ap *accountPool, address types.Address, acknowledged ...uint64) {
	t.Helper()
	manager := ap.getAccountManager(address)
	previous := types.ZeroHash
	for i, height := range acknowledged {
		block := userBlock(address, uint64(i+1), ackAt(height))
		block.PreviousHash = previous
		block.Hash = block.ComputeHash()
		common.FailIfErr(t, manager.Add(&nom.AccountBlockTransaction{Block: block, Changes: db.NewPatch()}))
		previous = block.Hash
	}
}

func pendingHeights(ap *accountPool, address types.Address) int {
	return len(ap.getUncommittedAccountBlocksByAddress(address))
}

// Pool eviction is eager and keyed on InsertMomentum: when the frontier
// reaches the gate's enforcement height H, a queued block acknowledging
// below H is evicted together with the account's queued block above it,
// whose own acknowledgement passes; other accounts are untouched. One
// momentum earlier, the same blocks stay queued.
func TestAccountPool_StaleAcknowledgementEvictedWithSuffix(t *testing.T) {
	gate := withDynamicPlasmaSporkId(t)
	const H = 100
	storage := db.NewMemDB()
	enforcedSpork(gate, H).Save(storage)
	cacheAt := scriptedCacheAt(map[types.Hash]uint64{gate: H})

	newPool := func(frontier uint64, cacheAt CacheStoreAt) *accountPool {
		ap := newAccountPool(&recencyStable{frontier: &recencyParentStore{identifier: ackAt(frontier), storage: storage}})
		ap.cacheAt = cacheAt
		return ap
	}
	insert := func(ap *accountPool, frontier uint64) {
		ap.InsertMomentum(&nom.DetailedMomentum{Momentum: &nom.Momentum{Hash: ackHash(frontier), Height: frontier}})
	}

	// Frontier H-1: the rule does not apply to the next momentum yet.
	before := newPool(H-1, cacheAt)
	seedChain(t, before, ackUser, 90, H-1)
	insert(before, H-1)
	common.Expect(t, pendingHeights(before, ackUser), 2)

	// Frontier H: the block acknowledging 90 and the block above it go.
	at := newPool(H, cacheAt)
	seedChain(t, at, ackUser, 90, H)
	seedChain(t, at, ackOtherUser, H)
	insert(at, H)
	if _, ok := at.managers[ackUser]; ok {
		t.Fatal("an emptied manager must be dropped")
	}
	common.Expect(t, pendingHeights(at, ackUser), 0)
	common.Expect(t, pendingHeights(at, ackOtherUser), 1)

	// A failing block above a passing one cuts the chain from there only.
	partial := newPool(H, cacheAt)
	seedChain(t, partial, ackUser, H, H)
	partial.getAccountManager(ackUser).blocks[2].MomentumAcknowledged = ackAt(90)
	insert(partial, H)
	common.Expect(t, pendingHeights(partial, ackUser), 1)

	// With eviction disabled (no cache reader), both stay queued.
	disabled := newPool(H, nil)
	seedChain(t, disabled, ackUser, 90, H)
	insert(disabled, H)
	common.Expect(t, pendingHeights(disabled, ackUser), 2)
}

// A block whose acknowledgement cannot be read is kept by the eviction
// pass; only a rule failure evicts.
func TestAccountPool_UnreadableAcknowledgementKept(t *testing.T) {
	gate := withDynamicPlasmaSporkId(t)
	storage := db.NewMemDB()
	enforcedSpork(gate, 100).Save(storage)
	ap := newAccountPool(&recencyStable{frontier: &recencyParentStore{identifier: ackAt(100), storage: storage}})
	ap.cacheAt = func(types.HashHeight) store.Cache { return nil }
	seedChain(t, ap, ackUser, 90)
	ap.InsertMomentum(&nom.DetailedMomentum{Momentum: &nom.Momentum{Hash: ackHash(100), Height: 100}})
	common.Expect(t, pendingHeights(ap, ackUser), 1)
}

// The pool re-checks every non-forced insert against the frontier the
// insert lands on, so a block the verifier passed one momentum earlier
// is rejected once the frontier enforces a spork it does not
// acknowledge. A forced insert (a block of a momentum being applied) is
// not re-checked, and a rollback below the enforcement height lifts the
// rule again.
func TestAccountPool_StaleAcknowledgementRejectedAtAdmission(t *testing.T) {
	gate := withDynamicPlasmaSporkId(t)
	const H = 100
	storage := db.NewMemDB()
	enforcedSpork(gate, H).Save(storage)
	stable := &recencyStable{frontier: &recencyParentStore{identifier: ackAt(H - 1), storage: storage}}
	ap := newAccountPool(stable)
	ap.cacheAt = scriptedCacheAt(map[types.Hash]uint64{gate: H})
	ap.primeAcknowledgement()

	transaction := func(address types.Address, acknowledged uint64) *nom.AccountBlockTransaction {
		block := userBlock(address, 1, ackAt(acknowledged))
		block.Hash = block.ComputeHash()
		return &nom.AccountBlockTransaction{Block: block, Changes: db.NewPatch()}
	}

	// Frontier H-1: the next momentum may still confirm it.
	common.FailIfErr(t, ap.addAccountBlockTransaction(transaction(types.Address{0, 1}, 90), false))

	// Frontier H.
	stable.frontier = &recencyParentStore{identifier: ackAt(H), storage: storage}
	ap.InsertMomentum(&nom.DetailedMomentum{Momentum: &nom.Momentum{Hash: ackHash(H), Height: H}})
	if err := ap.addAccountBlockTransaction(transaction(types.Address{0, 2}, 90), false); !errors.Is(err, ErrStaleMomentumAcknowledged) {
		t.Fatalf("stale insert at H: want ErrStaleMomentumAcknowledged, got %v", err)
	}
	common.FailIfErr(t, ap.addAccountBlockTransaction(transaction(types.Address{0, 3}, H), false))
	common.FailIfErr(t, ap.addAccountBlockTransaction(transaction(types.Address{0, 4}, 90), true))

	// Rolled back to H-1.
	stable.frontier = &recencyParentStore{identifier: ackAt(H - 1), storage: storage}
	ap.DeleteMomentum(&nom.DetailedMomentum{Momentum: &nom.Momentum{Hash: ackHash(H), Height: H}})
	common.FailIfErr(t, ap.addAccountBlockTransaction(transaction(types.Address{0, 5}, 90), false))
}
