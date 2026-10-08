package db

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

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
	t.Cleanup(func() { common.FailIfErr(t, m.Stop()) })

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
	t.Cleanup(func() { common.FailIfErr(t, m.Stop()) })

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
	t.Cleanup(func() { common.FailIfErr(t, m.Stop()) })
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
	// The explicit Stop below releases dir for the reopen; the Cleanup
	// covers the failure paths above it and is a no-op after that Stop.
	t.Cleanup(func() { common.FailIfErr(t, m.Stop()) })

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
	t.Cleanup(func() { common.FailIfErr(t, m2.Stop()) })
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
	t.Cleanup(func() { common.FailIfErr(t, m.Stop()) })

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

// keyedTransaction builds a one-commit transaction on top of the frontier of
// db that writes a single key. The commit hash covers the write and the
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

// TestManagerStopIsIdempotent pins the Manager.Stop contract for both
// implementations: the first Stop releases the resources and a second Stop
// is a no-op that returns nil. A test that stops a manager explicitly, for
// example to reopen its directory, must still be able to register an
// unconditional Stop in t.Cleanup so the handle is released on every failure
// path too. For the LevelDB manager the directory is reopened after the first
// Stop to pin that the handle, not just the flag, was released.
func TestManagerStopIsIdempotent(t *testing.T) {
	cases := []struct {
		name string
		open func(t *testing.T) (Manager, func())
	}{
		{"leveldb", func(t *testing.T) (Manager, func()) {
			dir := t.TempDir()
			return NewLevelDBManager(dir), func() {
				// The raw handle has no nil-on-repeat contract, so its
				// single Close belongs to the cleanup that is registered
				// as soon as the open succeeds.
				ldb, err := leveldb.OpenFile(dir, nil)
				common.FailIfErr(t, err)
				t.Cleanup(func() { common.FailIfErr(t, ldb.Close()) })
			}
		}},
		{"memdb", func(t *testing.T) (Manager, func()) {
			return NewMemDBManager(NewMemDB()), func() {}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, reopen := tc.open(t)
			// Registered before any assertion; a repeated Stop is the
			// contract under test, so the explicit calls below stay.
			t.Cleanup(func() { common.FailIfErr(t, m.Stop()) })
			common.FailIfErr(t, m.Add(newMockTransaction(1, m.Frontier())))
			common.FailIfErr(t, m.Stop())
			reopen()
			common.FailIfErr(t, m.Stop())
			if m.Frontier() != nil {
				t.Fatalf("Frontier must be nil after Stop")
			}
		})
	}
}

// TestLevelDBManagerStopAfterFailedClose pins that Stop marks the manager
// stopped even when closing the handle fails. goleveldb flags itself closed
// before it does any work and answers every later call with ErrClosed, so
// the handle is unusable after a failed Close and a retry of Stop has to be
// the documented no-op rather than a second error.
func TestLevelDBManagerStopAfterFailedClose(t *testing.T) {
	m := NewLevelDBManager(t.TempDir()).(*ldbManager)
	t.Cleanup(func() { common.FailIfErr(t, m.Stop()) })
	// The handle is closed underneath the manager, so the failure the first
	// Stop sees is goleveldb's ErrClosed; an I/O failure of a first Close
	// is not what this fixture produces.
	common.FailIfErr(t, m.ldb.Close())
	if err := m.Stop(); err != leveldb.ErrClosed {
		t.Fatalf("expected Stop to report leveldb.ErrClosed, got %v", err)
	}
	common.FailIfErr(t, m.Stop())
	if m.Frontier() != nil {
		t.Fatalf("Frontier must be nil after a Stop that reported an error")
	}
}

// TestMemDBManagerStopConcurrentWithAdd pins that Stop on the in-memory
// manager synchronises with the other methods. Run under the race detector
// an unlocked Stop is reported against the concurrent Add; with the lock the
// Add either lands before the Stop or is refused, and never panics.
// barrierCommit pauses Serialize until released, which holds Add between
// its early stopped check and the publication of the new state.
type barrierCommit struct {
	*mockCommit
	entered chan struct{} // closed when Serialize is first called
	release chan struct{} // Serialize returns once this is closed
	once    sync.Once
}

func (c *barrierCommit) Serialize() ([]byte, error) {
	c.once.Do(func() { close(c.entered) })
	<-c.release
	return c.mockCommit.Serialize()
}

// TestMemDBManagerStopDuringAdd pins the final stopped check in Add: a Stop
// that lands after Add has passed its early check and taken its snapshot
// must still make Add fail and leave nothing behind. Dropping only that
// final check turns this test red; the race-detector test below cannot
// order the two calls and so does not.
func TestMemDBManagerStopDuringAdd(t *testing.T) {
	m := NewMemDBManager(NewMemDB())
	t.Cleanup(func() { common.FailIfErr(t, m.Stop()) })
	transaction := newMockTransaction(1, m.Frontier())
	commit := &barrierCommit{
		mockCommit: transaction.commit.(*mockCommit),
		entered:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	transaction.commit = commit
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(commit.release) }) }
	t.Cleanup(release)

	type outcome struct {
		err      error
		panicked interface{}
	}
	done := make(chan outcome, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- outcome{panicked: r}
			}
		}()
		done <- outcome{err: m.Add(transaction)}
	}()

	select {
	case <-commit.entered:
	case <-time.After(5 * time.Second):
		release()
		t.Fatal("Add did not reach Serialize")
	}
	common.FailIfErr(t, m.Stop())
	release()

	select {
	case result := <-done:
		if result.panicked != nil {
			t.Fatalf("Add panicked after Stop: %v", result.panicked)
		}
		if result.err == nil {
			t.Fatal("expected Add to fail after Stop")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Add did not return")
	}
	identifier := commit.Identifier()
	if m.Frontier() != nil || m.Get(identifier) != nil || m.GetPatch(identifier) != nil {
		t.Fatal("state published by an Add that lost to Stop")
	}
}

func TestMemDBManagerStopConcurrentWithAdd(t *testing.T) {
	m := NewMemDBManager(NewMemDB())
	transaction := newMockTransaction(1, m.Frontier())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		common.FailIfErr(t, m.Stop())
	}()
	_ = m.Add(transaction)
	<-stopped
	if err := m.Add(newMockTransaction(2, NewMemDB())); err == nil {
		t.Fatalf("expected Add after Stop to fail")
	}
	common.FailIfErr(t, m.Stop())
}
