package db

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

type mockCommit struct {
	hash        types.Hash
	prevHash    types.Hash
	height      uint64
	changesHash types.Hash
}

func (mc *mockCommit) Identifier() types.HashHeight {
	return types.HashHeight{
		Height: mc.height,
		Hash:   mc.hash,
	}
}
func (mc *mockCommit) Previous() types.HashHeight {
	return types.HashHeight{
		Height: mc.height - 1,
		Hash:   mc.prevHash,
	}
}
func (mc *mockCommit) Serialize() ([]byte, error) {
	return common.JoinBytes(mc.hash.Bytes(), mc.prevHash.Bytes(), mc.changesHash.Bytes(), common.Uint64ToBytes(mc.height)), nil
}

type mockTransaction struct {
	patch  Patch
	commit Commit
}

func (m *mockTransaction) GetCommits() []Commit {
	return []Commit{m.commit}
}
func (m *mockTransaction) StealChanges() Patch {
	p := m.patch
	m.patch = nil
	return p
}

func newMockTransaction(seed int64, db DB) *mockTransaction {
	frontier := GetFrontierIdentifier(db)
	ab := &mockCommit{
		prevHash: frontier.Hash,
		height:   frontier.Height + 1,
	}

	r := rand.New(rand.NewSource(seed))
	stressTestConcurrentUse(nil, db, 5, 1, r)

	changes, _ := db.Changes()
	ab.changesHash = PatchHash(changes)
	ab.hash = types.NewHash(ab.changesHash.Bytes())
	return &mockTransaction{
		patch:  changes,
		commit: ab,
	}
}

func TestLevelDBManagerAddWriteFailureIsAtomic(t *testing.T) {
	m := NewLevelDBManager(t.TempDir()).(*ldbManager)
	defer m.Stop()

	before := GetFrontierIdentifier(m.Frontier())
	transaction := newMockTransaction(1, m.Frontier())
	identifier := transaction.commit.Identifier()
	writeErr := errors.New("write failed")
	m.write = func(*leveldb.Batch) error {
		return writeErr
	}

	if err := m.Add(transaction); !errors.Is(err, writeErr) {
		t.Fatalf("expected write error, got %v", err)
	}
	common.Expect(t, GetFrontierIdentifier(m.Frontier()), before)

	for _, key := range [][]byte{
		common.JoinBytes(patchByte, common.Uint64ToBytes(identifier.Height)),
		common.JoinBytes(rollbackByte, common.Uint64ToBytes(identifier.Height)),
	} {
		has, err := m.ldb.Has(key, nil)
		common.FailIfErr(t, err)
		common.ExpectTrue(t, !has)
	}
}

func TestLevelDBManagerPopWriteFailureIsAtomic(t *testing.T) {
	m := NewLevelDBManager(t.TempDir()).(*ldbManager)
	defer m.Stop()

	transaction := newMockTransaction(1, m.Frontier())
	common.FailIfErr(t, m.Add(transaction))
	before := GetFrontierIdentifier(m.Frontier())
	writeErr := errors.New("write failed")
	m.write = func(*leveldb.Batch) error {
		return writeErr
	}

	if err := m.Pop(); !errors.Is(err, writeErr) {
		t.Fatalf("expected write error, got %v", err)
	}
	common.Expect(t, GetFrontierIdentifier(m.Frontier()), before)

	for _, key := range [][]byte{
		common.JoinBytes(patchByte, common.Uint64ToBytes(before.Height)),
		common.JoinBytes(rollbackByte, common.Uint64ToBytes(before.Height)),
	} {
		has, err := m.ldb.Has(key, nil)
		common.FailIfErr(t, err)
		common.ExpectTrue(t, has)
	}
}

func TestVersionedDBConcurrentUse(t *testing.T) {
	m := NewLevelDBManager(t.TempDir())
	v0 := m.Frontier()
	v01 := m.Frontier()

	t1 := newMockTransaction(1, v0)
	common.ExpectString(t, DebugPatch(t1.patch), `
0c697f48392907a0 - a68447a4189deb99
365a858149c6e2d1 - 57e9d1860d1d68d8
4d65822107fcfd52 - 78629a0f5f3f164f
8866cb397916001e - 9408d2ac22c4d294
d5104dc76695721d - b80704bb7b4d7c03`)
	common.DealWithErr(m.Add(t1))

	common.ExpectString(t, DebugDB(v0), `
0c697f48392907a0 - a68447a4189deb99
365a858149c6e2d1 - 57e9d1860d1d68d8
4d65822107fcfd52 - 78629a0f5f3f164f
8866cb397916001e - 9408d2ac22c4d294
d5104dc76695721d - b80704bb7b4d7c03`)

	v1 := m.Frontier()
	frontier1 := GetFrontierIdentifier(v1)
	common.ExpectString(t, fmt.Sprintf("%v", frontier1), `{5c93068287abae27e59aa5507bab95c2779e6e65c6d6210153e9614ae44f1d88 1}`)

	frontier0 := GetFrontierIdentifier(v01)
	common.ExpectString(t, fmt.Sprintf("%v", frontier0), `{0000000000000000000000000000000000000000000000000000000000000000 0}`)

	t1p := newMockTransaction(11, v01)
	if err := m.Add(t1p); err == nil {
		t.Fatalf("expected Add on a stale frontier to fail")
	}
	common.ExpectString(t, DebugDB(v01), `
3d31f9c8d42f02c9 - 27397d8e9ad2a99d
566258666a0baae0 - 0e99d01979f82ec3
64743b955ef87a1f - 759b78575bef8f42
8bb5889840140c59 - 6707ee17a8517db0
e871bc355914f2c3 - 48c39064a7c7e355`)
}

func TestVersionedDBVersions(t *testing.T) {
	dir := t.TempDir()
	m := NewLevelDBManager(dir)

	db := m.Frontier()
	t1 := newMockTransaction(1, db)
	common.ExpectString(t, DebugPatch(t1.patch), `
0c697f48392907a0 - a68447a4189deb99
365a858149c6e2d1 - 57e9d1860d1d68d8
4d65822107fcfd52 - 78629a0f5f3f164f
8866cb397916001e - 9408d2ac22c4d294
d5104dc76695721d - b80704bb7b4d7c03`)
	common.ExpectString(t, fmt.Sprintf("%+v", t1.commit.Identifier()), `{Hash:5c93068287abae27e59aa5507bab95c2779e6e65c6d6210153e9614ae44f1d88 Height:1}`)
	common.DealWithErr(m.Add(t1))

	db = m.Frontier()
	common.ExpectString(t, DebugDB(db), `
00 - 0a220a205c93068287abae27e59aa5507bab95c2779e6e65c6d6210153e9614ae44f1d881001
015c93068287abae27e59aa5507bab95c2779e6e65c6d6210153e9614ae44f1d88 - 0000000000000001
020000000000000001 - 5c93068287abae27e59aa5507bab95c2779e6e65c6d6210153e9614ae44f1d8800000000000000000000000000000000000000000000000000000000000000003fc795fb4006a3d73dd511f493010f6d85c7448155ec6c444acaa9238f30c19e0000000000000001
0c697f48392907a0 - a68447a4189deb99
365a858149c6e2d1 - 57e9d1860d1d68d8
4d65822107fcfd52 - 78629a0f5f3f164f
8866cb397916001e - 9408d2ac22c4d294
d5104dc76695721d - b80704bb7b4d7c03`)
	f1 := GetFrontierIdentifier(db)
	t2 := newMockTransaction(2, db)
	common.ExpectString(t, DebugPatch(t2.patch), `
069728dc67d9db56 - 8f3aa6d8bef36a80
1b213e776add09fe - 903e28f8376a23b8
9569f9e2cb82822f - 21ed4caac044316f
b6666b02a03da270 - 1a634384d0ba8f10
cea06b688be116ca - f6bd65cefe8c20dc`)
	common.ExpectString(t, fmt.Sprintf("%+v", t2.commit.Identifier()), `{Hash:e5a89ef7fcf0f3c3fe81a782ac68e497bdb0155b3c41eff02113ab67fe739249 Height:2}`)
	common.DealWithErr(m.Add(t2))

	db = m.Frontier()
	f2 := GetFrontierIdentifier(db)
	t3 := newMockTransaction(3, db)
	common.ExpectString(t, DebugPatch(t3.patch), `
299c7bb8757d0bdc - 3bf5be2bb76058f7
36bcb1ac1b221c52 - c0fbdb5d49c1a3a8
789229e09e31f58d - 625250fa2140d8fd
dc2864602be7fb85 - d38967f931a50490
f25f4b21eef64b43 - 9c0a8a2bfc0914df`)
	common.ExpectString(t, fmt.Sprintf("%+v", t3.commit.Identifier()), `{Hash:d8ba48392cd7843812028c9fc3d7c92e232b8a725db741d69c930772e8551a85 Height:3}`)
	common.DealWithErr(m.Add(t3))

	db = m.Frontier()
	common.ExpectString(t, DebugDB(db), `
00 - 0a220a20d8ba48392cd7843812028c9fc3d7c92e232b8a725db741d69c930772e8551a851003
015c93068287abae27e59aa5507bab95c2779e6e65c6d6210153e9614ae44f1d88 - 0000000000000001
01d8ba48392cd7843812028c9fc3d7c92e232b8a725db741d69c930772e8551a85 - 0000000000000003
01e5a89ef7fcf0f3c3fe81a782ac68e497bdb0155b3c41eff02113ab67fe739249 - 0000000000000002
020000000000000001 - 5c93068287abae27e59aa5507bab95c2779e6e65c6d6210153e9614ae44f1d8800000000000000000000000000000000000000000000000000000000000000003fc795fb4006a3d73dd511f493010f6d85c7448155ec6c444acaa9238f30c19e0000000000000001
020000000000000002 - e5a89ef7fcf0f3c3fe81a782ac68e497bdb0155b3c41eff02113ab67fe7392495c93068287abae27e59aa5507bab95c2779e6e65c6d6210153e9614ae44f1d889cb71dd39b3e4f3991f66538480b1209e2df16863c33729be03430e1b1500b050000000000000002
020000000000000003 - d8ba48392cd7843812028c9fc3d7c92e232b8a725db741d69c930772e8551a85e5a89ef7fcf0f3c3fe81a782ac68e497bdb0155b3c41eff02113ab67fe739249d3fed0cc8f431f60ecd3b4617b2ad8991299350cb8361f2ccf02fd50de4c23620000000000000003
069728dc67d9db56 - 8f3aa6d8bef36a80
0c697f48392907a0 - a68447a4189deb99
1b213e776add09fe - 903e28f8376a23b8
299c7bb8757d0bdc - 3bf5be2bb76058f7
365a858149c6e2d1 - 57e9d1860d1d68d8
36bcb1ac1b221c52 - c0fbdb5d49c1a3a8
4d65822107fcfd52 - 78629a0f5f3f164f
789229e09e31f58d - 625250fa2140d8fd
8866cb397916001e - 9408d2ac22c4d294
9569f9e2cb82822f - 21ed4caac044316f
b6666b02a03da270 - 1a634384d0ba8f10
cea06b688be116ca - f6bd65cefe8c20dc
d5104dc76695721d - b80704bb7b4d7c03
dc2864602be7fb85 - d38967f931a50490
f25f4b21eef64b43 - 9c0a8a2bfc0914df`)
	f3 := GetFrontierIdentifier(db)
	patch, err := db.Changes()
	common.FailIfErr(t, err)
	common.ExpectString(t, DebugPatch(patch), ``)

	patch, err = m.Get(f2).Changes()
	common.FailIfErr(t, err)
	common.ExpectString(t, DebugPatch(patch), ``)

	common.ExpectString(t, DebugDB(m.Get(f1)), `
00 - 0a220a205c93068287abae27e59aa5507bab95c2779e6e65c6d6210153e9614ae44f1d881001
015c93068287abae27e59aa5507bab95c2779e6e65c6d6210153e9614ae44f1d88 - 0000000000000001
020000000000000001 - 5c93068287abae27e59aa5507bab95c2779e6e65c6d6210153e9614ae44f1d8800000000000000000000000000000000000000000000000000000000000000003fc795fb4006a3d73dd511f493010f6d85c7448155ec6c444acaa9238f30c19e0000000000000001
0c697f48392907a0 - a68447a4189deb99
365a858149c6e2d1 - 57e9d1860d1d68d8
4d65822107fcfd52 - 78629a0f5f3f164f
8866cb397916001e - 9408d2ac22c4d294
d5104dc76695721d - b80704bb7b4d7c03`)
	common.ExpectString(t, DebugDB(m.Get(f2)), `
00 - 0a220a20e5a89ef7fcf0f3c3fe81a782ac68e497bdb0155b3c41eff02113ab67fe7392491002
015c93068287abae27e59aa5507bab95c2779e6e65c6d6210153e9614ae44f1d88 - 0000000000000001
01e5a89ef7fcf0f3c3fe81a782ac68e497bdb0155b3c41eff02113ab67fe739249 - 0000000000000002
020000000000000001 - 5c93068287abae27e59aa5507bab95c2779e6e65c6d6210153e9614ae44f1d8800000000000000000000000000000000000000000000000000000000000000003fc795fb4006a3d73dd511f493010f6d85c7448155ec6c444acaa9238f30c19e0000000000000001
020000000000000002 - e5a89ef7fcf0f3c3fe81a782ac68e497bdb0155b3c41eff02113ab67fe7392495c93068287abae27e59aa5507bab95c2779e6e65c6d6210153e9614ae44f1d889cb71dd39b3e4f3991f66538480b1209e2df16863c33729be03430e1b1500b050000000000000002
069728dc67d9db56 - 8f3aa6d8bef36a80
0c697f48392907a0 - a68447a4189deb99
1b213e776add09fe - 903e28f8376a23b8
365a858149c6e2d1 - 57e9d1860d1d68d8
4d65822107fcfd52 - 78629a0f5f3f164f
8866cb397916001e - 9408d2ac22c4d294
9569f9e2cb82822f - 21ed4caac044316f
b6666b02a03da270 - 1a634384d0ba8f10
cea06b688be116ca - f6bd65cefe8c20dc
d5104dc76695721d - b80704bb7b4d7c03`)
	common.ExpectString(t, DebugDB(m.Get(f3)), `
00 - 0a220a20d8ba48392cd7843812028c9fc3d7c92e232b8a725db741d69c930772e8551a851003
015c93068287abae27e59aa5507bab95c2779e6e65c6d6210153e9614ae44f1d88 - 0000000000000001
01d8ba48392cd7843812028c9fc3d7c92e232b8a725db741d69c930772e8551a85 - 0000000000000003
01e5a89ef7fcf0f3c3fe81a782ac68e497bdb0155b3c41eff02113ab67fe739249 - 0000000000000002
020000000000000001 - 5c93068287abae27e59aa5507bab95c2779e6e65c6d6210153e9614ae44f1d8800000000000000000000000000000000000000000000000000000000000000003fc795fb4006a3d73dd511f493010f6d85c7448155ec6c444acaa9238f30c19e0000000000000001
020000000000000002 - e5a89ef7fcf0f3c3fe81a782ac68e497bdb0155b3c41eff02113ab67fe7392495c93068287abae27e59aa5507bab95c2779e6e65c6d6210153e9614ae44f1d889cb71dd39b3e4f3991f66538480b1209e2df16863c33729be03430e1b1500b050000000000000002
020000000000000003 - d8ba48392cd7843812028c9fc3d7c92e232b8a725db741d69c930772e8551a85e5a89ef7fcf0f3c3fe81a782ac68e497bdb0155b3c41eff02113ab67fe739249d3fed0cc8f431f60ecd3b4617b2ad8991299350cb8361f2ccf02fd50de4c23620000000000000003
069728dc67d9db56 - 8f3aa6d8bef36a80
0c697f48392907a0 - a68447a4189deb99
1b213e776add09fe - 903e28f8376a23b8
299c7bb8757d0bdc - 3bf5be2bb76058f7
365a858149c6e2d1 - 57e9d1860d1d68d8
36bcb1ac1b221c52 - c0fbdb5d49c1a3a8
4d65822107fcfd52 - 78629a0f5f3f164f
789229e09e31f58d - 625250fa2140d8fd
8866cb397916001e - 9408d2ac22c4d294
9569f9e2cb82822f - 21ed4caac044316f
b6666b02a03da270 - 1a634384d0ba8f10
cea06b688be116ca - f6bd65cefe8c20dc
d5104dc76695721d - b80704bb7b4d7c03
dc2864602be7fb85 - d38967f931a50490
f25f4b21eef64b43 - 9c0a8a2bfc0914df`)

	common.FailIfErr(t, m.Stop())
	m2 := NewLevelDBManager(dir)
	db = m2.Frontier()
	common.ExpectString(t, DebugDB(db), `
00 - 0a220a20d8ba48392cd7843812028c9fc3d7c92e232b8a725db741d69c930772e8551a851003
015c93068287abae27e59aa5507bab95c2779e6e65c6d6210153e9614ae44f1d88 - 0000000000000001
01d8ba48392cd7843812028c9fc3d7c92e232b8a725db741d69c930772e8551a85 - 0000000000000003
01e5a89ef7fcf0f3c3fe81a782ac68e497bdb0155b3c41eff02113ab67fe739249 - 0000000000000002
020000000000000001 - 5c93068287abae27e59aa5507bab95c2779e6e65c6d6210153e9614ae44f1d8800000000000000000000000000000000000000000000000000000000000000003fc795fb4006a3d73dd511f493010f6d85c7448155ec6c444acaa9238f30c19e0000000000000001
020000000000000002 - e5a89ef7fcf0f3c3fe81a782ac68e497bdb0155b3c41eff02113ab67fe7392495c93068287abae27e59aa5507bab95c2779e6e65c6d6210153e9614ae44f1d889cb71dd39b3e4f3991f66538480b1209e2df16863c33729be03430e1b1500b050000000000000002
020000000000000003 - d8ba48392cd7843812028c9fc3d7c92e232b8a725db741d69c930772e8551a85e5a89ef7fcf0f3c3fe81a782ac68e497bdb0155b3c41eff02113ab67fe739249d3fed0cc8f431f60ecd3b4617b2ad8991299350cb8361f2ccf02fd50de4c23620000000000000003
069728dc67d9db56 - 8f3aa6d8bef36a80
0c697f48392907a0 - a68447a4189deb99
1b213e776add09fe - 903e28f8376a23b8
299c7bb8757d0bdc - 3bf5be2bb76058f7
365a858149c6e2d1 - 57e9d1860d1d68d8
36bcb1ac1b221c52 - c0fbdb5d49c1a3a8
4d65822107fcfd52 - 78629a0f5f3f164f
789229e09e31f58d - 625250fa2140d8fd
8866cb397916001e - 9408d2ac22c4d294
9569f9e2cb82822f - 21ed4caac044316f
b6666b02a03da270 - 1a634384d0ba8f10
cea06b688be116ca - f6bd65cefe8c20dc
d5104dc76695721d - b80704bb7b4d7c03
dc2864602be7fb85 - d38967f931a50490
f25f4b21eef64b43 - 9c0a8a2bfc0914df`)
}

// A transaction whose previous is a known-but-stale version must be rejected
// instead of overwriting the frontier with stale state.
func TestLevelDBManagerAddRejectsNonFrontierPrevious(t *testing.T) {
	m := NewLevelDBManager(t.TempDir())

	t1 := newMockTransaction(1, m.Frontier())
	id1 := t1.commit.Identifier()
	common.DealWithErr(m.Add(t1))

	t2 := newMockTransaction(2, m.Frontier())
	common.DealWithErr(m.Add(t2))
	frontierBefore := GetFrontierIdentifier(m.Frontier())

	// build a transaction on top of the stale version id1
	stale := newMockTransaction(3, m.Get(id1))
	if err := m.Add(stale); err == nil {
		t.Fatalf("expected Add with stale previous %v to fail", id1)
	}
	if got := GetFrontierIdentifier(m.Frontier()); got != frontierBefore {
		t.Fatalf("frontier changed from %v to %v after rejected Add", frontierBefore, got)
	}
}

// --- Rebase overlay-chain tests -------------------------------------------------

// overlayChainDepth returns the number of nested mergedDB levels reachable
// from d, following enableDeleteDB wrappers.  A freshly snapshotted DB has
// depth 1 (one mergedDB).  Each additional overlay level adds 1.
func overlayChainDepth(d DB) int {
	ed, ok := d.(*enableDeleteDB)
	if !ok {
		return 0
	}
	return rawOverlayDepth(ed.db)
}

func rawOverlayDepth(d db) int {
	if m, ok := d.(*mergedDB); ok {
		maxInner := 0
		for _, inner := range m.dbs {
			if depth := rawOverlayDepth(inner); depth > maxInner {
				maxInner = depth
			}
		}
		return 1 + maxInner
	}
	return 0
}

// TestRebase_SevensOverlayChain verifies that after Rebase the pending
// versions' mergedDb chains are rebuilt directly on the new stable DB and
// no longer reference deleted committed overlays.
func TestRebase_SevensOverlayChain(t *testing.T) {
	// Build: stable(h0) → A(h1) → B(h2) → C(h3)
	// Rebase to h1: B and C are pending; their chains must sit on the new
	// stable DB, not on A's overlay.
	m := NewMemDBManager(NewMemDB()).(*memdbManager)

	// Add block A (height 1) with a real write.
	tA := newMockTransaction(1, m.Frontier())
	common.DealWithErr(m.Add(tA))
	idA := tA.commit.Identifier()
	patchA := m.GetPatch(idA)

	// Add block B (height 2) with a real write.
	tB := newMockTransaction(2, m.Frontier())
	common.DealWithErr(m.Add(tB))

	// Add block C (height 3) with a real write.
	tC := newMockTransaction(3, m.Frontier())
	common.DealWithErr(m.Add(tC))
	idC := tC.commit.Identifier()

	// Before Rebase: C's overlay chain has 3 mergedDB levels
	// (C → B → A → stable).
	depthBefore := overlayChainDepth(m.versions[idC])
	if depthBefore != 3 {
		t.Fatalf("pre-rebase overlay depth = %d, want 3", depthBefore)
	}

	// Build the new stable DB at height 1 by replaying A's patch.
	newStable := NewMemDB()
	common.DealWithErr(ApplyPatch(newStable, patchA))
	common.DealWithErr(SetFrontier(newStable, idA, []byte("block-A")))

	m.Rebase(newStable)

	// After Rebase: C's overlay chain must have depth 2
	// (C' → newStable), not 3.
	depthAfter := overlayChainDepth(m.versions[idC])
	if depthAfter != 2 {
		t.Fatalf("post-rebase overlay depth = %d, want 2 (chain not severed)", depthAfter)
	}
}

// TestRebase_PreservesPendingData verifies that data written by pending
// versions remains readable after Rebase.
func TestRebase_PreservesPendingData(t *testing.T) {
	m := NewMemDBManager(NewMemDB()).(*memdbManager)

	// Block A at height 1.
	tA := newMockTransaction(1, m.Frontier())
	common.DealWithErr(m.Add(tA))
	idA := tA.commit.Identifier()
	patchA := m.GetPatch(idA)

	// Block B at height 2.
	tB := newMockTransaction(2, m.Frontier())
	common.DealWithErr(m.Add(tB))

	// Block C at height 3.
	tC := newMockTransaction(3, m.Frontier())
	common.DealWithErr(m.Add(tC))
	idC := tC.commit.Identifier()

	// Snapshot C's full visible state before Rebase.
	fullCBefore := DebugDB(m.Get(idC))

	// Build new stable at height 1.  Use the real serialized block data so
	// the comparison is exact.
	blockAData, err := tA.commit.Serialize()
	common.FailIfErr(t, err)
	newStable := NewMemDB()
	common.DealWithErr(ApplyPatch(newStable, patchA))
	common.DealWithErr(SetFrontier(newStable, idA, blockAData))

	m.Rebase(newStable)

	// After Rebase, Get(idC) must return the same visible data.
	fullCAfter := DebugDB(m.Get(idC))
	common.ExpectString(t, fullCAfter, fullCBefore)
}

// TestRebase_PopAfterRebase verifies that Pop works correctly after Rebase,
// stopping at the new stable floor.
func TestRebase_PopAfterRebase(t *testing.T) {
	m := NewMemDBManager(NewMemDB()).(*memdbManager)

	// Add three blocks.
	tA := newMockTransaction(1, m.Frontier())
	common.DealWithErr(m.Add(tA))
	idA := tA.commit.Identifier()
	patchA := m.GetPatch(idA)

	tB := newMockTransaction(2, m.Frontier())
	common.DealWithErr(m.Add(tB))

	tC := newMockTransaction(3, m.Frontier())
	common.DealWithErr(m.Add(tC))

	// Rebase to height 1 (commit A).
	newStable := NewMemDB()
	common.DealWithErr(ApplyPatch(newStable, patchA))
	common.DealWithErr(SetFrontier(newStable, idA, []byte("block-A")))
	m.Rebase(newStable)

	// Pop C (h3).
	common.DealWithErr(m.Pop())
	if got := GetFrontierIdentifier(m.Frontier()); got.Height != 2 {
		t.Fatalf("after pop: frontier height = %d, want 2", got.Height)
	}

	// Pop B (h2).
	common.DealWithErr(m.Pop())
	if got := GetFrontierIdentifier(m.Frontier()); got.Height != 1 {
		t.Fatalf("after second pop: frontier height = %d, want 1", got.Height)
	}

	// Pop should now fail (at stable).
	if err := m.Pop(); err == nil {
		t.Fatal("expected Pop at stable floor to fail")
	}
}

// TestRebase_AddAfterRebase verifies that new blocks can be added after
// Rebase.
func TestRebase_AddAfterRebase(t *testing.T) {
	m := NewMemDBManager(NewMemDB()).(*memdbManager)

	tA := newMockTransaction(1, m.Frontier())
	common.DealWithErr(m.Add(tA))
	idA := tA.commit.Identifier()
	patchA := m.GetPatch(idA)

	tB := newMockTransaction(2, m.Frontier())
	common.DealWithErr(m.Add(tB))

	// Rebase to height 1.
	newStable := NewMemDB()
	common.DealWithErr(ApplyPatch(newStable, patchA))
	common.DealWithErr(SetFrontier(newStable, idA, []byte("block-A")))
	m.Rebase(newStable)

	// Add a new block at height 3 (on top of B at height 2).
	tC := newMockTransaction(3, m.Frontier())
	common.DealWithErr(m.Add(tC))

	if got := GetFrontierIdentifier(m.Frontier()); got.Height != 3 {
		t.Fatalf("frontier height = %d, want 3", got.Height)
	}
}

// TestRebase_RollingCommitBoundedDepth simulates a rolling commit pattern
// where each new momentum commits one block and Rebase is called.  Without
// the overlay-chain rebuild, the depth would grow by one per commit; with
// the fix it must stay bounded.
func TestRebase_RollingCommitBoundedDepth(t *testing.T) {
	m := NewMemDBManager(NewMemDB()).(*memdbManager)

	// Add 5 blocks.
	var ids []types.HashHeight
	var patches []Patch
	for i := 0; i < 5; i++ {
		tx := newMockTransaction(int64(i+1), m.Frontier())
		common.DealWithErr(m.Add(tx))
		ids = append(ids, tx.commit.Identifier())
		patches = append(patches, m.GetPatch(ids[i]))
	}

	// Simulate rolling commits: rebase to height 1, 2, 3, 4.
	// After each Rebase, check that the frontier's overlay depth is bounded.
	for rebaseTo := uint64(1); rebaseTo <= 4; rebaseTo++ {
		// Build new stable DB by replaying all patches up to rebaseTo.
		newStable := NewMemDB()
		for h := uint64(0); h < rebaseTo; h++ {
			common.DealWithErr(ApplyPatch(newStable, patches[h]))
		}
		data := ids[rebaseTo-1].Serialize()
		common.DealWithErr(SetFrontier(newStable, ids[rebaseTo-1], data))

		m.Rebase(newStable)

		// Check frontier overlay depth.
		frontierID := GetFrontierIdentifier(m.Frontier())
		depth := overlayChainDepth(m.versions[frontierID])

		// After rebasing to height rebaseTo, the frontier is at height 5.
		// Pending versions are at heights rebaseTo+1..5, so the overlay
		// depth should be 5-rebaseTo (number of pending versions), not 5.
		wantDepth := int(5 - rebaseTo)
		if depth != wantDepth {
			t.Fatalf("after rebase to h%d: overlay depth = %d, want %d (unbounded growth)",
				rebaseTo, depth, wantDepth)
		}
	}
}

// TestRebase_DeletesInPendingPatch verifies that pending versions with
// delete operations in their patches are correctly rebuilt after Rebase.
func TestRebase_DeletesInPendingPatch(t *testing.T) {
	// Use a custom patch that writes then deletes.
	m := NewMemDBManager(NewMemDB()).(*memdbManager)

	// Block A: write some keys.
	tA := newMockTransaction(1, m.Frontier())
	common.DealWithErr(m.Add(tA))
	idA := tA.commit.Identifier()
	patchA := m.GetPatch(idA)

	// Block B: delete a key that A wrote.
	// Build a custom patch for B that deletes one of A's keys.
	dbB := m.Frontier()
	// Find a key from A's patch and delete it.
	pr := &patchRecorder{}
	common.DealWithErr(patchA.Replay(pr))
	if len(pr.puts) == 0 {
		t.Fatal("block A patch has no puts to delete")
	}
	common.DealWithErr(dbB.Delete(pr.puts[0].key))
	changesB, err := dbB.Changes()
	common.FailIfErr(t, err)

	// Create a transaction for B with the delete.
	frontier := GetFrontierIdentifier(m.Frontier())
	commitB := &mockCommit{
		prevHash:    frontier.Hash,
		height:      frontier.Height + 1,
		changesHash: PatchHash(changesB),
	}
	commitB.hash = types.NewHash(commitB.changesHash.Bytes())
	txB := &mockTransaction{patch: changesB, commit: commitB}
	common.DealWithErr(m.Add(txB))
	idB := commitB.Identifier()

	// Verify B's frontier can read the state (key should be deleted).
	dbBefore := m.Get(idB)
	for _, kv := range pr.puts[:1] {
		_, err := dbBefore.Get(kv.key)
		if err != leveldb.ErrNotFound {
			t.Fatalf("key should be deleted before rebase, got err=%v", err)
		}
	}

	// Rebase to height 1.
	newStable := NewMemDB()
	common.DealWithErr(ApplyPatch(newStable, patchA))
	common.DealWithErr(SetFrontier(newStable, idA, []byte("block-A")))
	m.Rebase(newStable)

	// After Rebase, B's view must still show the key as deleted.
	dbAfter := m.Get(idB)
	for _, kv := range pr.puts[:1] {
		_, err := dbAfter.Get(kv.key)
		if err != leveldb.ErrNotFound {
			t.Fatalf("key should be deleted after rebase, got err=%v", err)
		}
	}
}

type kvPair struct {
	key   []byte
	value []byte
}

type patchRecorder struct {
	puts []kvPair
}

func (pr *patchRecorder) Put(key, value []byte) {
	pr.puts = append(pr.puts, kvPair{key: key, value: value})
}
func (pr *patchRecorder) Delete(key []byte) {}

// --- Regression tests for edgepillar's review -------------------------------

// mockBatchTransaction produces multiple commits, mimicking a batched
// embedded account-block transaction whose DescendantBlocks carry the
// contract sends triggered by a contract receive.
type mockBatchTransaction struct {
	patch   Patch
	commits []Commit
}

func (m *mockBatchTransaction) GetCommits() []Commit {
	return m.commits
}
func (m *mockBatchTransaction) StealChanges() Patch {
	p := m.patch
	m.patch = nil
	return p
}

// newMockBatchTransaction creates a batch of count commits starting at the
// current frontier.  The last commit is the head; the earlier ones are
// intermediates.  Each commit gets a distinct write so patches are non-trivial.
func newMockBatchTransaction(seed int64, db DB, count int) *mockBatchTransaction {
	frontier := GetFrontierIdentifier(db)
	r := rand.New(rand.NewSource(seed))

	// Apply stress writes to generate a real patch.
	stressTestConcurrentUse(nil, db, 5, 1, r)
	changes, _ := db.Changes()

	commits := make([]Commit, 0, count)
	prevHash := frontier.Hash
	baseHeight := frontier.Height
	for i := 0; i < count; i++ {
		h := baseHeight + uint64(i) + 1
		mc := &mockCommit{
			prevHash: prevHash,
			height:   h,
		}
		// Give each commit a distinct changesHash so identifiers differ.
		mc.changesHash = types.NewHash(common.Uint64ToBytes(uint64(seed*1000) + uint64(i)))
		mc.hash = types.NewHash(mc.changesHash.Bytes())
		commits = append(commits, mc)
		prevHash = mc.hash
	}

	return &mockBatchTransaction{
		patch:   changes,
		commits: commits,
	}
}

// TestRebase_InterleavedAddCommitBoundedDepth reproduces the exact scenario
// from the review: interleave new Add calls with commits, keeping exactly one
// pending block after each commit, and assert bounded retained overlay depth.
//
// The stable DB is cumulative: one base MemDB accumulates every committed
// patch so that earlier committed application state is carried forward.
func TestRebase_InterleavedAddCommitBoundedDepth(t *testing.T) {
	m := NewMemDBManager(NewMemDB()).(*memdbManager)

	// Start with two blocks so we have something to commit.
	t1 := newMockTransaction(1, m.Frontier())
	common.DealWithErr(m.Add(t1))
	id1 := t1.commit.Identifier()
	patch1 := m.GetPatch(id1)

	t2 := newMockTransaction(2, m.Frontier())
	common.DealWithErr(m.Add(t2))
	id2 := t2.commit.Identifier()
	patch2 := m.GetPatch(id2)

	// Cumulative stable DB: one base MemDB that accumulates all committed
	// patches so that earlier committed application state is carried forward
	// into every subsequent rebase.
	stableDB := NewMemDB()

	// Interleaved pattern: commit one block, add one new block, repeat.
	// After each commit there is exactly one pending block.
	// Simulate 5 rolling commits.
	committedPatches := []Patch{patch1, patch2}
	committedIDs := []types.HashHeight{id1, id2}

	for cycle := 0; cycle < 5; cycle++ {
		// Commit the oldest pending block by applying its patch to the
		// cumulative stable DB and rebasing to its height.
		rebaseTo := committedIDs[0]
		common.DealWithErr(ApplyPatch(stableDB, committedPatches[0]))
		data := rebaseTo.Serialize()
		common.DealWithErr(SetFrontier(stableDB, rebaseTo, data))

		m.Rebase(stableDB)

		// After rebase, exactly one block should be pending (the one that was
		// at height rebaseTo+1).  Add a replacement to keep the chain going.
		frontierID := GetFrontierIdentifier(m.Frontier())
		depth := overlayChainDepth(m.versions[frontierID])

		// With one pending block, the overlay depth should be 1 (just the
		// pending version on top of the new stable DB).
		if depth != 1 {
			t.Fatalf("cycle %d: overlay depth = %d, want 1 (unbounded growth)",
				cycle, depth)
		}

		// Verify representative committed key/value bytes are present in the
		// frontier DB.  Replay the committed patch to pick keys that were
		// written by the block we just committed and are NOT overwritten by
		// the pending block (the pending overlay may legitimately replace
		// values for keys it also writes).
		pr := &patchRecorder{}
		common.DealWithErr(committedPatches[0].Replay(pr))
		pendingPR := &patchRecorder{}
		common.DealWithErr(committedPatches[1].Replay(pendingPR))
		pendingKeys := make(map[string]bool)
		for _, kv := range pendingPR.puts {
			pendingKeys[string(kv.key)] = true
		}
		if len(pr.puts) > 0 {
			frontier := m.Frontier()
			checked := 0
			for _, kv := range pr.puts {
				if pendingKeys[string(kv.key)] {
					continue // pending block overwrote this key; skip
				}
				val, err := frontier.Get(kv.key)
				if err != nil {
					t.Fatalf("cycle %d: committed key %x missing from frontier: %v",
						cycle, kv.key, err)
				}
				if !bytes.Equal(val, kv.value) {
					t.Fatalf("cycle %d: committed key %x has value %x, want %x",
						cycle, kv.key, val, kv.value)
				}
				checked++
				if checked >= 2 {
					break
				}
			}
			if checked == 0 {
				t.Fatalf("cycle %d: no non-overlapping committed keys to verify", cycle)
			}
		}

		// Add a new block on top of the current frontier.
		tx := newMockTransaction(int64(100+cycle), m.Frontier())
		common.DealWithErr(m.Add(tx))
		committedPatches = append(committedPatches, m.GetPatch(tx.commit.Identifier()))
		committedIDs = append(committedIDs, tx.commit.Identifier())

		// Remove the committed entry from our tracking.
		committedPatches = committedPatches[1:]
		committedIDs = committedIDs[1:]
	}
}

// TestRebase_NonEmptyWritesAndDeletes verifies Rebase with non-empty
// application writes and deletions: pending-suffix effects are removed,
// stable-floor state is restored, and replacement insertion works.
func TestRebase_NonEmptyWritesAndDeletes(t *testing.T) {
	m := NewMemDBManager(NewMemDB()).(*memdbManager)

	// Block A (h1): write keys k1, k2.
	tA := newMockTransaction(1, m.Frontier())
	common.DealWithErr(m.Add(tA))
	idA := tA.commit.Identifier()
	patchA := m.GetPatch(idA)

	// Extract keys and values written by A so we can delete one in B and
	// verify the other byte-for-byte after Rebase.
	pr := &patchRecorder{}
	common.DealWithErr(patchA.Replay(pr))
	if len(pr.puts) < 2 {
		t.Fatal("need at least 2 puts in patch A")
	}
	keyToDelete := pr.puts[0].key
	valToDelete := pr.puts[0].value
	keyToKeep := pr.puts[1].key
	valToKeep := pr.puts[1].value

	// Block B (h2): delete k1, write k3.
	dbB := m.Frontier()
	common.DealWithErr(dbB.Delete(keyToDelete))
	// Write a new key.
	newKey := []byte("new-key-from-B")
	newVal := []byte("value-B")
	common.DealWithErr(dbB.Put(newKey, newVal))
	changesB, err := dbB.Changes()
	common.FailIfErr(t, err)

	frontier := GetFrontierIdentifier(m.Frontier())
	commitB := &mockCommit{
		prevHash:    frontier.Hash,
		height:      frontier.Height + 1,
		changesHash: PatchHash(changesB),
	}
	commitB.hash = types.NewHash(commitB.changesHash.Bytes())
	txB := &mockTransaction{patch: changesB, commit: commitB}
	common.DealWithErr(m.Add(txB))
	idB := commitB.Identifier()

	// Verify pre-rebase state: B's frontier shows k1 deleted, k2 present, k3 present.
	dbBefore := m.Get(idB)
	_, err = dbBefore.Get(keyToDelete)
	if err != leveldb.ErrNotFound {
		t.Fatalf("keyToDelete should be absent before rebase, got err=%v", err)
	}
	preVal, err := dbBefore.Get(keyToKeep)
	if err != nil {
		t.Fatalf("keyToKeep should be present before rebase, got err=%v", err)
	}
	if !bytes.Equal(preVal, valToKeep) {
		t.Fatalf("keyToKeep value before rebase = %x, want %x", preVal, valToKeep)
	}
	preNewVal, err := dbBefore.Get(newKey)
	if err != nil || !bytes.Equal(preNewVal, newVal) {
		t.Fatalf("newKey should be present before rebase, got val=%q err=%v", preNewVal, err)
	}

	// Rebase to height 1 (commit A).
	blockAData, err := tA.commit.Serialize()
	common.FailIfErr(t, err)
	newStable := NewMemDB()
	common.DealWithErr(ApplyPatch(newStable, patchA))
	common.DealWithErr(SetFrontier(newStable, idA, blockAData))
	m.Rebase(newStable)

	// After Rebase:
	// 1. B's pending-suffix effects must still be visible (delete + new write).
	dbAfter := m.Get(idB)
	_, err = dbAfter.Get(keyToDelete)
	if err != leveldb.ErrNotFound {
		t.Fatalf("pending-suffix delete lost after rebase: keyToDelete err=%v", err)
	}
	afterNewVal, err := dbAfter.Get(newKey)
	if err != nil || !bytes.Equal(afterNewVal, newVal) {
		t.Fatalf("pending-suffix write lost after rebase: newKey val=%q err=%v", afterNewVal, err)
	}

	// 2. Stable-floor state must be restored: k2 (from A) still present with
	//    the exact byte-for-byte value written in block A.
	afterKeepVal, err := dbAfter.Get(keyToKeep)
	if err != nil {
		t.Fatalf("stable-floor key lost after rebase: keyToKeep err=%v", err)
	}
	if !bytes.Equal(afterKeepVal, valToKeep) {
		t.Fatalf("stable-floor keyToKeep value after rebase = %x, want %x",
			afterKeepVal, valToKeep)
	}

	// 3. Pop B and verify stable-floor state is fully restored (k1 reappears,
	//    k3 is gone).
	common.DealWithErr(m.Pop())
	dbPopped := m.Frontier()
	restoredVal, err := dbPopped.Get(keyToDelete)
	if err != nil {
		t.Fatalf("stable-floor key k1 not restored after Pop: err=%v", err)
	}
	if !bytes.Equal(restoredVal, valToDelete) {
		t.Fatalf("restored keyToDelete value after Pop = %x, want %x",
			restoredVal, valToDelete)
	}
	_, err = dbPopped.Get(newKey)
	if err != leveldb.ErrNotFound {
		t.Fatalf("pending-suffix key k3 not removed after Pop: err=%v", err)
	}

	// 4. Replacement insertion: add a new block C at height 2.
	tC := newMockTransaction(3, m.Frontier())
	common.DealWithErr(m.Add(tC))
	idC := tC.commit.Identifier()
	if idC.Height != 2 {
		t.Fatalf("replacement block C height = %d, want 2", idC.Height)
	}

	// Verify C can read the stable-floor state.
	dbC := m.Get(idC)
	_, err = dbC.Get(keyToDelete)
	if err != nil {
		t.Fatalf("replacement C cannot read stable-floor key k1: err=%v", err)
	}
}

// TestPop_CleansBatchIntermediates verifies that Pop removes intermediate
// batched commits that shared the popped head's DB, preventing stale entries
// from surviving across Rebase.
func TestPop_CleansBatchIntermediates(t *testing.T) {
	m := NewMemDBManager(NewMemDB()).(*memdbManager)

	// Add a single block at height 1 to establish a frontier.
	t0 := newMockTransaction(1, m.Frontier())
	common.DealWithErr(m.Add(t0))

	// Add a batch: intermediate at h2, head at h3.
	batch := newMockBatchTransaction(2, m.Frontier(), 2)
	common.DealWithErr(m.Add(batch))
	intermediateID := batch.commits[0].Identifier()
	headID := batch.commits[1].Identifier()

	// Verify both entries exist.
	if m.Get(intermediateID) == nil {
		t.Fatal("intermediate version not accessible before Pop")
	}
	if m.Get(headID) == nil {
		t.Fatal("head version not accessible before Pop")
	}

	// Pop the batch head.
	common.DealWithErr(m.Pop())

	// The intermediate entry must also be gone.
	if m.Get(intermediateID) != nil {
		t.Fatal("intermediate version survived Pop — stale reference leak")
	}

	// The frontier should be back at h1.
	frontierID := GetFrontierIdentifier(m.Frontier())
	if frontierID.Height != 1 {
		t.Fatalf("frontier height after Pop = %d, want 1", frontierID.Height)
	}
}

// TestRebase_PoppedBatchIntermediateCleaned verifies the full leak path from
// the review: batch Add → Pop → replacement Add → Rebase.  The orphaned
// intermediate must not survive Rebase.
func TestRebase_PoppedBatchIntermediateCleaned(t *testing.T) {
	m := NewMemDBManager(NewMemDB()).(*memdbManager)

	// Add a base block at h1.
	t0 := newMockTransaction(1, m.Frontier())
	common.DealWithErr(m.Add(t0))
	id1 := t0.commit.Identifier()
	patch1 := m.GetPatch(id1)

	// Add a batch: intermediate at h2, head at h3.
	batch := newMockBatchTransaction(2, m.Frontier(), 2)
	common.DealWithErr(m.Add(batch))
	intermediateID := batch.commits[0].Identifier()

	// Pop the batch.
	common.DealWithErr(m.Pop())

	// Add a replacement single block at h2.
	t2 := newMockTransaction(3, m.Frontier())
	common.DealWithErr(m.Add(t2))
	id2 := t2.commit.Identifier()
	patch2 := m.GetPatch(id2)

	// Make an independent copy of patch2's serialized contents before Rebase
	// so we can verify it survives the rebase byte-for-byte.
	// Dump() returns b.data directly rather than copying it, so without the
	// Clone both sides of the comparison below would alias the same slice and
	// an in-place mutation would change the expectation along with the
	// actual, leaving the assertion vacuously true.
	patch2Dump := bytes.Clone(patch2.Dump())

	// Rebase to h1 (commit the base block).
	block1Data, err := t0.commit.Serialize()
	common.FailIfErr(t, err)
	newStable := NewMemDB()
	common.DealWithErr(ApplyPatch(newStable, patch1))
	common.DealWithErr(SetFrontier(newStable, id1, block1Data))
	m.Rebase(newStable)

	// The orphaned intermediate must not be in versions anymore.
	m.changes.Lock()
	_, exists := m.versions[intermediateID]
	m.changes.Unlock()
	if exists {
		t.Fatal("orphaned intermediate survived Rebase — stale DB reference retained")
	}

	// The replacement block must be accessible and correct.
	db2 := m.Get(id2)
	if db2 == nil {
		t.Fatal("replacement block not accessible after Rebase")
	}

	// Verify the frontier is correct.
	frontierID := GetFrontierIdentifier(m.Frontier())
	if frontierID != id2 {
		t.Fatalf("frontier = %v, want %v", frontierID, id2)
	}

	// Verify version map size is bounded: should contain only the stable
	// identifier and the replacement block.
	m.changes.Lock()
	versionCount := len(m.versions)
	m.changes.Unlock()
	if versionCount > 2 {
		t.Fatalf("version map has %d entries, want at most 2 (stable + replacement)", versionCount)
	}

	// Also verify patch2 is intact and byte-identical to the pre-Rebase copy.
	p2 := m.GetPatch(id2)
	if p2 == nil {
		t.Fatal("replacement patch missing after Rebase")
	}
	if !bytes.Equal(p2.Dump(), patch2Dump) {
		t.Fatal("replacement patch2 changed across Rebase — want byte-identical")
	}

	// Negative control for the snapshot above. Dump() hands back the batch's
	// internal slice rather than a copy, so without bytes.Clone the baseline
	// would alias that data and move with it. want is a second independent
	// copy taken here, so comparing it against the baseline detects the
	// aliasing: with the Clone both hold the original bytes and match; without
	// it the baseline has been mutated in place and no longer matches.
	want := bytes.Clone(patch2.Dump())
	own := patch2.Dump()
	if len(own) == 0 {
		t.Fatal("patch2 dump is empty, cannot run the aliasing control")
	}
	own[0] ^= 0xff
	if !bytes.Equal(want, patch2Dump) {
		t.Fatal("mutating the live dump moved the baseline — patch2Dump is not an independent snapshot")
	}
	own[0] ^= 0xff
	if !bytes.Equal(patch2.Dump(), patch2Dump) {
		t.Fatal("patch2 dump did not return to its original bytes after restoring the control byte")
	}
}

// TestRebase_LiveBatchIntermediatePreserved verifies that intermediates
// belonging to live (non-popped) batches are NOT removed by Rebase.
func TestRebase_LiveBatchIntermediatePreserved(t *testing.T) {
	m := NewMemDBManager(NewMemDB()).(*memdbManager)

	// Add a base block at h1.
	t0 := newMockTransaction(1, m.Frontier())
	common.DealWithErr(m.Add(t0))
	id1 := t0.commit.Identifier()
	patch1 := m.GetPatch(id1)

	// Add a batch: intermediate at h2, head at h3.  Do NOT pop.
	batch := newMockBatchTransaction(2, m.Frontier(), 2)
	common.DealWithErr(m.Add(batch))
	intermediateID := batch.commits[0].Identifier()
	headID := batch.commits[1].Identifier()

	// Rebase to h1.
	block1Data, err := t0.commit.Serialize()
	common.FailIfErr(t, err)
	newStable := NewMemDB()
	common.DealWithErr(ApplyPatch(newStable, patch1))
	common.DealWithErr(SetFrontier(newStable, id1, block1Data))
	m.Rebase(newStable)

	// The intermediate must still be accessible (its batch head survived).
	dbIntermediate := m.Get(intermediateID)
	if dbIntermediate == nil {
		t.Fatal("live batch intermediate removed by Rebase — should be preserved")
	}

	// The head must also be accessible.
	dbHead := m.Get(headID)
	if dbHead == nil {
		t.Fatal("batch head removed by Rebase — should be preserved")
	}

	// Both should return the same visible data (they share the same DB).
	common.ExpectString(t, DebugDB(dbIntermediate), DebugDB(dbHead))
}

// TestRebase_RepeatedReplacementBatches verifies that two successive
// batch-add-then-pop cycles at the same heights leave no orphaned
// intermediates or stale patches behind after Rebase.
func TestRebase_RepeatedReplacementBatches(t *testing.T) {
	m := NewMemDBManager(NewMemDB()).(*memdbManager)

	// 1. Stable floor: commit block at h1.
	t0 := newMockTransaction(1, m.Frontier())
	common.DealWithErr(m.Add(t0))
	id1 := t0.commit.Identifier()
	patch1 := m.GetPatch(id1)

	// Extract a representative key/value from the stable-floor patch.
	pr := &patchRecorder{}
	common.DealWithErr(patch1.Replay(pr))
	if len(pr.puts) == 0 {
		t.Fatal("stable-floor patch has no puts")
	}
	floorKey := pr.puts[0].key
	floorVal := pr.puts[0].value

	// 2. Add batch 1 (intermediate h2, head h3), then Pop it.
	batch1 := newMockBatchTransaction(10, m.Frontier(), 2)
	common.DealWithErr(m.Add(batch1))
	intermediate1ID := batch1.commits[0].Identifier()
	head1ID := batch1.commits[1].Identifier()
	common.DealWithErr(m.Pop())

	// 3. Add replacement batch A at same heights (intermediate h2', head h3'),
	//    then Pop it too.
	batchA := newMockBatchTransaction(20, m.Frontier(), 2)
	common.DealWithErr(m.Add(batchA))
	intermediateAID := batchA.commits[0].Identifier()
	headAID := batchA.commits[1].Identifier()
	common.DealWithErr(m.Pop())

	// 4. Add replacement batch B at same heights (intermediate h2'', head h3'').
	batchB := newMockBatchTransaction(30, m.Frontier(), 2)
	common.DealWithErr(m.Add(batchB))
	intermediateBID := batchB.commits[0].Identifier()
	headBID := batchB.commits[1].Identifier()

	// 5. Rebase to h1 (the stable floor).
	block1Data, err := t0.commit.Serialize()
	common.FailIfErr(t, err)
	newStable := NewMemDB()
	common.DealWithErr(ApplyPatch(newStable, patch1))
	common.DealWithErr(SetFrontier(newStable, id1, block1Data))
	m.Rebase(newStable)

	// Assert: orphaned intermediates from BOTH popped batches are absent.
	m.changes.Lock()
	_, exists1 := m.versions[intermediate1ID]
	_, existsA := m.versions[intermediateAID]
	m.changes.Unlock()
	if exists1 {
		t.Fatal("orphaned intermediate from batch 1 survived Rebase")
	}
	if existsA {
		t.Fatal("orphaned intermediate from batch A survived Rebase")
	}

	// Assert: patches of the popped batches are absent.
	if m.GetPatch(intermediate1ID) != nil {
		t.Fatal("patch for batch 1 intermediate still present after Rebase")
	}
	if m.GetPatch(head1ID) != nil {
		t.Fatal("patch for batch 1 head still present after Rebase")
	}
	if m.GetPatch(intermediateAID) != nil {
		t.Fatal("patch for batch A intermediate still present after Rebase")
	}
	if m.GetPatch(headAID) != nil {
		t.Fatal("patch for batch A head still present after Rebase")
	}

	// Assert: retained versions count is bounded (stable + live batch members).
	m.changes.Lock()
	versionCount := len(m.versions)
	m.changes.Unlock()
	// Expected: stable (h1) + intermediate (h2'') + head (h3'') = 3.
	if versionCount > 3 {
		t.Fatalf("version map has %d entries, want at most 3 (stable + 2 live batch)", versionCount)
	}

	// Assert: overlay depth at the frontier is bounded.
	frontierID := GetFrontierIdentifier(m.Frontier())
	depth := overlayChainDepth(m.versions[frontierID])
	if depth != 1 {
		t.Fatalf("overlay depth at frontier = %d, want 1", depth)
	}

	// Assert: live batch lookups work.
	if m.Get(intermediateBID) == nil {
		t.Fatal("live batch intermediate not accessible after Rebase")
	}
	if m.Get(headBID) == nil {
		t.Fatal("live batch head not accessible after Rebase")
	}

	// Assert: application state is correct — representative key from the
	// stable floor is present with the correct byte-for-byte value.
	frontier := m.Frontier()
	val, err := frontier.Get(floorKey)
	if err != nil {
		t.Fatalf("stable-floor key missing from frontier after Rebase: %v", err)
	}
	if !bytes.Equal(val, floorVal) {
		t.Fatalf("stable-floor key value = %x, want %x", val, floorVal)
	}
}

// height, so two branches at the same height get distinct identifiers.
func keyedTransaction(db DB, key, value string) *mockTransaction {
	frontier := GetFrontierIdentifier(db)
	patch := NewPatch()
	patch.Put([]byte(key), []byte(value))
	commit := &mockCommit{
		prevHash:    frontier.Hash,
		height:      frontier.Height + 1,
		changesHash: PatchHash(patch),
	}
	commit.hash = types.NewHash(common.JoinBytes(commit.changesHash.Bytes(), common.Uint64ToBytes(commit.height), []byte(key), []byte(value)))
	return &mockTransaction{patch: patch, commit: commit}
}

func expectValue(t *testing.T, db DB, key, expected string) {
	t.Helper()
	value, err := db.Get([]byte(key))
	common.FailIfErr(t, err)
	common.Expect(t, string(value), expected)
}

func expectMissing(t *testing.T, db DB, key string) {
	t.Helper()
	has, err := db.Has([]byte(key))
	common.FailIfErr(t, err)
	if has {
		t.Fatalf("key %q is visible but must not be", key)
	}
}

// TestLevelDBManagerHistoricalViewHidesLaterKeys pins that a view at a past
// identifier reports a key first written above that identifier as absent,
// through Has and Get as well as through iteration. The rollback overlay
// records such a key as deleted; that record has to use the encoding the
// delete-aware view understands, or the key reads as present and empty.
func TestLevelDBManagerHistoricalViewHidesLaterKeys(t *testing.T) {
	m := NewLevelDBManager(t.TempDir())
	t.Cleanup(func() { common.FailIfErr(t, m.Stop()) })

	common.FailIfErr(t, m.Add(keyedTransaction(m.Frontier(), "k1", "v1")))
	identifier := GetFrontierIdentifier(m.Frontier())
	common.FailIfErr(t, m.Add(keyedTransaction(m.Frontier(), "k2", "v2")))

	view := m.Get(identifier)
	expectValue(t, view, "k1", "v1")
	expectMissing(t, view, "k2")
	if _, err := view.Get([]byte("k2")); err != leveldb.ErrNotFound {
		t.Fatalf("expected ErrNotFound for a key written above the identifier, got %v", err)
	}
	iterator := view.NewIterator([]byte("k"))
	defer iterator.Release()
	keys := []string{}
	for iterator.Next() {
		keys = append(keys, string(iterator.Key()))
	}
	common.FailIfErr(t, iterator.Error())
	common.Expect(t, len(keys), 1)
	common.Expect(t, keys[0], "k1")
}

// TestLevelDBManagerPopInvalidatesRollbackCaches covers the rollback overlay
// Get keeps per identifier. The overlay records the frontier it was built
// against so that later calls replay only the rollbacks above it. Pop moves
// the frontier back and later Adds reuse the popped heights, so an overlay
// kept across a Pop would skip the rollbacks of the momentums added at those
// heights and show their writes at the old identifier. Both cache tiers are
// covered: identifiers within maximumCacheHeightDifference of the frontier
// and those further below it. A Get between the Pops and the Adds rebuilds
// the overlay against the lower frontier and caches that instead, which is
// correct on its own and would hide a missing purge, so it is a separate
// case rather than part of the first two.
func TestLevelDBManagerPopInvalidatesRollbackCaches(t *testing.T) {
	cases := []struct {
		name        string
		above       int
		readBetween bool
	}{
		{"identifier within the near cache window", 2, false},
		{"identifier beyond the near cache window", maximumCacheHeightDifference + 1, false},
		{"view read between the pops and the adds", 3, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewLevelDBManager(t.TempDir())
			t.Cleanup(func() { common.FailIfErr(t, m.Stop()) })

			common.FailIfErr(t, m.Add(keyedTransaction(m.Frontier(), "shared", "at-1")))
			identifier := GetFrontierIdentifier(m.Frontier())
			for i := 1; i < tc.above-1; i++ {
				common.FailIfErr(t, m.Add(keyedTransaction(m.Frontier(), fmt.Sprintf("kept-%d", i), "kept")))
			}
			common.FailIfErr(t, m.Add(keyedTransaction(m.Frontier(), "shared", "old-top-1")))
			common.FailIfErr(t, m.Add(keyedTransaction(m.Frontier(), "old-top", "old-top")))
			common.Expect(t, GetFrontierIdentifier(m.Frontier()).Height, identifier.Height+uint64(tc.above))

			// Warm the overlay for identifier while the frontier is above it.
			expectValue(t, m.Get(identifier), "shared", "at-1")
			expectMissing(t, m.Get(identifier), "old-top")

			// Remove the top two momentums.
			common.FailIfErr(t, m.Pop())
			common.FailIfErr(t, m.Pop())
			if tc.readBetween {
				view := m.Get(identifier)
				expectMissing(t, view, "old-top")
				expectValue(t, view, "shared", "at-1")
			}

			// Replace them with a branch that writes a key the removed
			// momentums never touched.
			common.FailIfErr(t, m.Add(keyedTransaction(m.Frontier(), "new-top-1", "new-top-1")))
			common.FailIfErr(t, m.Add(keyedTransaction(m.Frontier(), "shared", "new-top")))
			expectValue(t, m.Frontier(), "new-top-1", "new-top-1")
			expectValue(t, m.Frontier(), "shared", "new-top")

			// The view at identifier predates both branches.
			view := m.Get(identifier)
			expectMissing(t, view, "new-top-1")
			expectMissing(t, view, "old-top")
			expectValue(t, view, "shared", "at-1")
			if tc.above > 2 {
				expectMissing(t, view, "kept-1")
			}
		})
	}
}
