package api

import (
	"bytes"
	"encoding/binary"
	"math/big"
	"sort"
	"testing"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/account/mailbox"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

var (
	unreceivedRecipient = types.PillarContract
	unreceivedSender    = types.PlasmaContract
)

// unreceivedMomentumStore is a frontier momentum store holding a real
// mailbox for the recipient and the send blocks it lists. Only the methods
// GetUnreceivedBlocksByAddress and the block conversion reach are
// implemented; block reads are counted.
type unreceivedMomentumStore struct {
	store.Momentum
	mailbox    store.AccountMailbox
	blocks     map[types.Hash]*nom.AccountBlock
	frontier   *nom.Momentum
	blockReads int
	keyReads   int
	nexts      int
}

func (s *unreceivedMomentumStore) GetAccountMailbox(address types.Address) store.AccountMailbox {
	if address != unreceivedRecipient {
		return mailbox.NewAccountMailbox(address, db.NewMemDB())
	}
	return &countingMailbox{AccountMailbox: s.mailbox, store: s}
}
func (s *unreceivedMomentumStore) GetAccountBlockByHash(hash types.Hash) (*nom.AccountBlock, error) {
	s.blockReads++
	return s.blocks[hash], nil
}

// GetBlockWhichReceives reads the send block first, as the real momentum
// store does (chain/momentum/unreceived.go), and finds no receive for it.
func (s *unreceivedMomentumStore) GetBlockWhichReceives(hash types.Hash) (*nom.AccountBlock, error) {
	if _, err := s.GetAccountBlockByHash(hash); err != nil {
		return nil, err
	}
	return nil, nil
}

// nextCountingDB counts the Next calls made on its iterators. Wrapped
// around the mailbox's database it counts every advance a scan asks for,
// including one that would step over tombstones to find no further entry.
type nextCountingDB struct {
	db.DB
	nexts *int
}

func (d nextCountingDB) NewIterator(prefix []byte) db.StorageIterator {
	return nextCountingIterator{StorageIterator: d.DB.NewIterator(prefix), nexts: d.nexts}
}

type nextCountingIterator struct {
	db.StorageIterator
	nexts *int
}

func (i nextCountingIterator) Next() bool {
	*i.nexts++
	return i.StorageIterator.Next()
}

// countingMailbox counts the pending keys a scan reads.
type countingMailbox struct {
	store.AccountMailbox
	store *unreceivedMomentumStore
}

func (m *countingMailbox) GetUnreceivedAccountBlockHashes(atMost uint64) ([]types.Hash, error) {
	hashes, err := m.AccountMailbox.GetUnreceivedAccountBlockHashes(atMost)
	m.store.keyReads += len(hashes)
	return hashes, err
}
func (m *countingMailbox) ForEachUnreceivedAccountBlockHash(fn func(types.Hash) (bool, error)) error {
	return m.AccountMailbox.ForEachUnreceivedAccountBlockHash(func(hash types.Hash) (bool, error) {
		m.store.keyReads++
		return fn(hash)
	})
}
func (s *unreceivedMomentumStore) GetFrontierMomentum() (*nom.Momentum, error) {
	return s.frontier, nil
}
func (s *unreceivedMomentumStore) GetBlockConfirmationHeight(types.Hash) (uint64, error) {
	return s.frontier.Height, nil
}
func (s *unreceivedMomentumStore) GetMomentumByHeight(uint64) (*nom.Momentum, error) {
	return s.frontier, nil
}

// countingAccountStore is the recipient's frontier account store with its
// IsReceived lookups counted.
type countingAccountStore struct {
	store.Account
	lookups int
}

func (a *countingAccountStore) IsReceived(hash types.Hash) bool {
	a.lookups++
	return a.Account.IsReceived(hash)
}

// unreceivedChain hands the API the stores above and counts how often the
// frontier stores are taken, so a test can check the call pins them.
type unreceivedChain struct {
	chain.Chain
	momentum     *unreceivedMomentumStore
	account      *countingAccountStore
	momentumPins int
	accountPins  int
	// pins records the order in which the stores were taken.
	pins string
}

func (c *unreceivedChain) GetFrontierMomentumStore() store.Momentum {
	c.momentumPins++
	c.pins += "m"
	return c.momentum
}
func (c *unreceivedChain) GetFrontierAccountStore(types.Address) store.Account {
	c.accountPins++
	c.pins += "a"
	return c.account
}

func (c *unreceivedChain) resetCounters() {
	c.momentumPins, c.accountPins, c.pins = 0, 0, ""
	c.momentum.blockReads, c.momentum.keyReads, c.momentum.nexts, c.account.lookups = 0, 0, 0, 0
}

type unreceivedFixture struct {
	api *LedgerApi
	c   *unreceivedChain
	// pending lists every mailbox entry in key (ascending hash) order.
	pending []types.Hash
	// received marks the entries the account store has already received.
	received map[types.Hash]bool
}

// unreceived returns the entries of pending, in order, that are not
// received: the set the API has to page through.
func (f *unreceivedFixture) unreceived(pending []types.Hash) []types.Hash {
	out := make([]types.Hash, 0, len(pending))
	for _, h := range pending {
		if !f.received[h] {
			out = append(out, h)
		}
	}
	return out
}

// newUnreceivedFixture puts n send blocks in the recipient's mailbox and
// marks as received, in the account store, the entries whose position in
// key order satisfies isReceived, so received entries are interleaved with
// unreceived ones wherever the test wants them.
func newUnreceivedFixture(t *testing.T, n int, isReceived func(position int) bool) *unreceivedFixture {
	t.Helper()
	momentum := &unreceivedMomentumStore{}
	box := mailbox.NewAccountMailbox(unreceivedRecipient, nextCountingDB{DB: db.NewMemDB(), nexts: &momentum.nexts})
	accountStore := account.NewAccountStore(unreceivedRecipient, db.NewMemDB())
	now := common.Clock.Now()
	frontier := &nom.Momentum{Height: 7, Hash: types.NewHash([]byte("frontier")), Timestamp: &now}
	blocks := make(map[types.Hash]*nom.AccountBlock, n)

	pending := make([]types.Hash, 0, n)
	for i := 0; i < n; i++ {
		var seed [8]byte
		binary.BigEndian.PutUint64(seed[:], uint64(i))
		// Hashing the index makes key order unrelated to insertion order.
		hash := types.NewHash(seed[:])
		pending = append(pending, hash)
		blocks[hash] = &nom.AccountBlock{
			BlockType:        nom.BlockTypeUserSend,
			Hash:             hash,
			Height:           uint64(i + 1),
			Address:          unreceivedSender,
			ToAddress:        unreceivedRecipient,
			Amount:           big.NewInt(int64(i + 1)),
			DescendantBlocks: []*nom.AccountBlock{},
		}
		if err := box.MarkAsUnreceived(hash); err != nil {
			t.Fatal(err)
		}
	}
	sort.Slice(pending, func(i, j int) bool { return bytes.Compare(pending[i].Bytes(), pending[j].Bytes()) < 0 })

	received := make(map[types.Hash]bool)
	for position, hash := range pending {
		if isReceived(position) {
			received[hash] = true
			if err := accountStore.MarkAsReceived(hash); err != nil {
				t.Fatal(err)
			}
		}
	}

	momentum.mailbox, momentum.blocks, momentum.frontier = box, blocks, frontier
	c := &unreceivedChain{
		momentum: momentum,
		account:  &countingAccountStore{Account: accountStore},
	}
	return &unreceivedFixture{
		api:      &LedgerApi{chain: c, log: common.RPCLogger.New("module", "ledger_api_test")},
		c:        c,
		pending:  pending,
		received: received,
	}
}

// get calls the API and checks the per-call invariants: the frontier
// stores are taken once, account store first; only the returned page's
// blocks are read (twice each: once for the page and once when the
// conversion looks for the receive); and at most unreceivedMaxScan
// entries are examined, reading no key past the last one examined.
func (f *unreceivedFixture) get(t *testing.T, pageIndex, pageSize uint32) *AccountBlockList {
	t.Helper()
	f.c.resetCounters()
	res, err := f.api.GetUnreceivedBlocksByAddress(unreceivedRecipient, pageIndex, pageSize)
	if err != nil {
		t.Fatalf("page %d size %d: %v", pageIndex, pageSize, err)
	}
	if f.c.momentumPins != 1 || f.c.accountPins != 1 {
		t.Fatalf("page %d size %d: frontier taken %d (momentum) and %d (account) times, want once each", pageIndex, pageSize, f.c.momentumPins, f.c.accountPins)
	}
	if f.c.pins != "am" {
		t.Fatalf("page %d size %d: stores taken in order %q, want the account store first", pageIndex, pageSize, f.c.pins)
	}
	if f.c.momentum.blockReads != 2*len(res.List) {
		t.Fatalf("page %d size %d: read %d blocks for a page of %d", pageIndex, pageSize, f.c.momentum.blockReads, len(res.List))
	}
	if f.c.account.lookups > unreceivedMaxScan || f.c.momentum.keyReads != f.c.account.lookups {
		t.Fatalf("page %d size %d: examined %d entries reading %d keys, bound is %d", pageIndex, pageSize, f.c.account.lookups, f.c.momentum.keyReads, unreceivedMaxScan)
	}
	// A scan that stopped early advanced the iterator once per examined
	// entry; only a scan that reached the end advanced it once more.
	if wantNexts := f.c.account.lookups; res.More && f.c.momentum.nexts != wantNexts || !res.More && f.c.momentum.nexts != wantNexts+1 {
		t.Fatalf("page %d size %d: %d iterator advances for %d entries with more %v", pageIndex, pageSize, f.c.momentum.nexts, f.c.account.lookups, res.More)
	}
	return res
}

func hashesOf(list []*AccountBlock) []types.Hash {
	out := make([]types.Hash, 0, len(list))
	for _, b := range list {
		out = append(out, b.Hash)
	}
	return out
}

func expectHashes(t *testing.T, what string, got, want []types.Hash) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d hashes, want %d", what, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: hash %d is %s, want %s", what, i, got[i], want[i])
		}
	}
}

// walk pages through the unreceived set the way a client does, until a
// page reaches Count with More false, and returns every hash listed.
func (f *unreceivedFixture) walk(t *testing.T, pageSize uint32) ([]types.Hash, *AccountBlockList) {
	t.Helper()
	var all []types.Hash
	for pageIndex := uint32(0); ; pageIndex++ {
		res := f.get(t, pageIndex, pageSize)
		all = append(all, hashesOf(res.List)...)
		if !res.More && uint64(pageIndex+1)*uint64(pageSize) >= uint64(res.Count) {
			return all, res
		}
		if len(res.List) != int(pageSize) {
			t.Fatalf("page %d size %d: short page of %d with more pages to come (count %d, more %v)", pageIndex, pageSize, len(res.List), res.Count, res.More)
		}
		if pageIndex > uint32(len(f.pending)) {
			t.Fatalf("walk with page size %d does not terminate", pageSize)
		}
	}
}

// Issue #267: with more than 500 pending entries, and received entries
// interleaved among them, walking every page lists each unreceived block
// exactly once, in key order, and the last page reports the exact total.
func TestGetUnreceivedBlocksByAddress_PagesPastFirst500(t *testing.T) {
	f := newUnreceivedFixture(t, 1200, func(position int) bool { return position%5 == 2 })
	want := f.unreceived(f.pending)
	if len(want) != 960 {
		t.Fatalf("fixture has %d unreceived blocks, want 960", len(want))
	}

	for _, pageSize := range []uint32{50, 37, 1} {
		got, last := f.walk(t, pageSize)
		expectHashes(t, "walk", got, want)
		if last.Count != len(want) || last.More {
			t.Fatalf("size %d: last page has count %d more %v, want count %d more false", pageSize, last.Count, last.More, len(want))
		}
	}

	// The first page keeps its old Count and More: the unreceived blocks
	// among the first 500 entries, with more entries past them.
	first := f.get(t, 0, 50)
	if wantCount := len(f.unreceived(f.pending[:unreceivedQuerySize])); first.Count != wantCount || !first.More {
		t.Fatalf("first page: count %d more %v, want count %d more true", first.Count, first.More, wantCount)
	}
	expectHashes(t, "first page", hashesOf(first.List), want[:50])
	if f.c.account.lookups != unreceivedQuerySize {
		t.Fatalf("first page examined %d entries, want %d", f.c.account.lookups, unreceivedQuerySize)
	}

	// Pages 10 and later were rejected before; page 15 of 50 now lists
	// unreceived blocks 750..799 even though received entries precede it.
	deep := f.get(t, 15, 50)
	expectHashes(t, "page 15", hashesOf(deep.List), want[750:800])
	// The 800th unreceived block is the 1000th entry; the scan stops there.
	if !deep.More || deep.Count != 800 || f.c.account.lookups != 1000 {
		t.Fatalf("page 15: count %d more %v after %d entries, want count 800 more true after 1000", deep.Count, deep.More, f.c.account.lookups)
	}
}

// Received entries clustered at the end of the 500-entry window leave it
// short of a page; the scan goes on until the page is full instead of
// returning a short page.
func TestGetUnreceivedBlocksByAddress_WidensPastReceivedEntries(t *testing.T) {
	f := newUnreceivedFixture(t, 2000, func(position int) bool { return position >= 450 && position < 600 })
	want := f.unreceived(f.pending)

	page := f.get(t, 9, 50)
	expectHashes(t, "page 9", hashesOf(page.List), want[450:500])
	// The 500th unreceived block is the 650th entry; the scan stops there.
	if !page.More || page.Count != 500 || f.c.account.lookups != 650 {
		t.Fatalf("page 9: count %d more %v after %d entries, want count 500 more true after 650", page.Count, page.More, f.c.account.lookups)
	}

	got, last := f.walk(t, 50)
	expectHashes(t, "walk", got, want)
	if last.Count != len(want) || last.More {
		t.Fatalf("last page: count %d more %v, want count %d more false", last.Count, last.More, len(want))
	}
}

// Below the 500-entry window nothing changes for current clients: Count is
// the total, More is false and a page past the end is empty. Exactly 500
// pending entries report More, as before, and the page after them is empty
// with More false.
func TestGetUnreceivedBlocksByAddress_SmallMailboxUnchanged(t *testing.T) {
	f := newUnreceivedFixture(t, 30, func(position int) bool { return position%6 == 0 })
	want := f.unreceived(f.pending)

	page := f.get(t, 2, 10)
	expectHashes(t, "page 2", hashesOf(page.List), want[20:25])
	if page.Count != 25 || page.More {
		t.Fatalf("page 2: count %d more %v, want count 25 more false", page.Count, page.More)
	}
	past := f.get(t, 3, 10)
	if len(past.List) != 0 || past.Count != 25 || past.More {
		t.Fatalf("page 3: %d blocks count %d more %v, want empty count 25 more false", len(past.List), past.Count, past.More)
	}
	// pageIndex 10 and beyond is no longer an error.
	tenth := f.get(t, 12, 2)
	expectHashes(t, "page 12 of 2", hashesOf(tenth.List), want[24:25])

	if _, err := f.api.GetUnreceivedBlocksByAddress(unreceivedRecipient, 0, unreceivedMaxPageSize+1); err != ErrPageSizeParamTooBig {
		t.Fatalf("page size %d: got %v, want ErrPageSizeParamTooBig", unreceivedMaxPageSize+1, err)
	}

	full := newUnreceivedFixture(t, unreceivedQuerySize, func(int) bool { return false })
	if res := full.get(t, 0, 50); res.Count != unreceivedQuerySize || !res.More {
		t.Fatalf("exactly %d pending: count %d more %v, want count %d more true", unreceivedQuerySize, res.Count, res.More, unreceivedQuerySize)
	}
	if res := full.get(t, 10, 50); len(res.List) != 0 || res.Count != unreceivedQuerySize || res.More {
		t.Fatalf("page after %d pending: %d blocks count %d more %v, want empty count %d more false", unreceivedQuerySize, len(res.List), res.Count, res.More, unreceivedQuerySize)
	}
}

// Confirmed receives leave tombstones under the mailbox's pending prefix.
// A scan that stops at its 500th entry must not advance the iterator again,
// since that advance would step over every tombstone after it.
func TestGetUnreceivedBlocksByAddress_StopsBeforeTrailingTombstones(t *testing.T) {
	f := newUnreceivedFixture(t, 2000, func(int) bool { return false })
	for _, hash := range f.pending[unreceivedQuerySize:] {
		if err := f.c.momentum.mailbox.MarkAsReceived(hash); err != nil {
			t.Fatal(err)
		}
	}
	res := f.get(t, 0, 50)
	expectHashes(t, "page 0", hashesOf(res.List), f.pending[:50])
	if res.Count != unreceivedQuerySize || !res.More || f.c.momentum.nexts != unreceivedQuerySize {
		t.Fatalf("page 0: count %d more %v after %d advances, want count %d more true after %d", res.Count, res.More, f.c.momentum.nexts, unreceivedQuerySize, unreceivedQuerySize)
	}
}

// A call never examines more than unreceivedMaxScan entries. The page that
// reaches the bound is served, a page that would start past it is
// rejected, and both report More.
func TestGetUnreceivedBlocksByAddress_ScanBound(t *testing.T) {
	// One received entry in every 100 leaves 9900 unreceived blocks among
	// the first 10000 entries.
	f := newUnreceivedFixture(t, unreceivedMaxScan+200, func(position int) bool { return position%100 == 0 })
	want := f.unreceived(f.pending[:unreceivedMaxScan])
	if len(want) != 9900 {
		t.Fatalf("fixture has %d unreceived blocks within the bound, want 9900", len(want))
	}

	last := f.get(t, 197, 50)
	expectHashes(t, "page 197", hashesOf(last.List), want[9850:9900])
	if last.Count != 9900 || !last.More {
		t.Fatalf("page 197: count %d more %v, want count 9900 more true", last.Count, last.More)
	}
	if f.c.account.lookups != unreceivedMaxScan || f.c.momentum.keyReads != unreceivedMaxScan {
		t.Fatalf("page 197 examined %d entries reading %d keys, want %d", f.c.account.lookups, f.c.momentum.keyReads, unreceivedMaxScan)
	}

	// A page that starts past the unreceived blocks within the bound.
	f.c.resetCounters()
	if _, err := f.api.GetUnreceivedBlocksByAddress(unreceivedRecipient, 198, 50); err != ErrPageIndexParamTooBig {
		t.Fatalf("page 198: got %v, want ErrPageIndexParamTooBig", err)
	}
	if f.c.account.lookups > unreceivedMaxScan || f.c.momentum.keyReads > unreceivedMaxScan || f.c.momentum.blockReads != 0 {
		t.Fatalf("page 198: examined %d entries, read %d keys and %d blocks", f.c.account.lookups, f.c.momentum.keyReads, f.c.momentum.blockReads)
	}

	// A page whose offset alone is past the bound is rejected before any
	// store is read.
	f.c.resetCounters()
	if _, err := f.api.GetUnreceivedBlocksByAddress(unreceivedRecipient, unreceivedMaxScan/50, 50); err != ErrPageIndexParamTooBig {
		t.Fatalf("page %d: got %v, want ErrPageIndexParamTooBig", unreceivedMaxScan/50, err)
	}
	if f.c.momentumPins != 0 || f.c.accountPins != 0 {
		t.Fatal("an out-of-bound page index must be rejected before reading the stores")
	}
	if _, err := f.api.GetUnreceivedBlocksByAddress(unreceivedRecipient, ^uint32(0), unreceivedMaxPageSize); err != ErrPageIndexParamTooBig {
		t.Fatalf("largest page index: got %v, want ErrPageIndexParamTooBig", err)
	}
}
