package chain

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/inconshreveable/log15"

	"github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/cache/storage"
	"github.com/zenon-network/go-zenon/chain/momentum"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

type countingCacheManager struct {
	inner        storage.CacheManager
	addCalls     int32
	compactCalls int32
	compactErr   error
	onCompact    func()
}

func (m *countingCacheManager) DB() db.DB { return m.inner.DB() }

func (m *countingCacheManager) Add(id types.HashHeight, patch db.Patch) error {
	atomic.AddInt32(&m.addCalls, 1)
	return m.inner.Add(id, patch)
}

func (m *countingCacheManager) Pop() error { return m.inner.Pop() }

func (m *countingCacheManager) Compact() error {
	atomic.AddInt32(&m.compactCalls, 1)
	if m.onCompact != nil {
		m.onCompact()
	}
	if m.compactErr != nil {
		return m.compactErr
	}
	return m.inner.Compact()
}

func (m *countingCacheManager) Stop() error { return m.inner.Stop() }

type stubBackfillChainManager struct {
	frontier db.DB
	patches  map[types.HashHeight]db.Patch
}

func (m *stubBackfillChainManager) Frontier() db.DB                       { return m.frontier }
func (m *stubBackfillChainManager) Get(types.HashHeight) db.DB            { return nil }
func (m *stubBackfillChainManager) GetPatch(id types.HashHeight) db.Patch { return m.patches[id] }
func (m *stubBackfillChainManager) Add(db.Transaction) error              { return nil }
func (m *stubBackfillChainManager) Pop() error                            { return nil }
func (m *stubBackfillChainManager) Rebase(db.DB) error                    { return nil }
func (m *stubBackfillChainManager) Stop() error                           { return nil }
func (m *stubBackfillChainManager) Location() string                      { return "stub-backfill" }

type stubBackfillMomentumStore struct {
	store.Momentum
	momentums map[uint64]*nom.Momentum
	detailed  map[uint64]*nom.DetailedMomentum
}

func (s *stubBackfillMomentumStore) GetMomentumByHeight(height uint64) (*nom.Momentum, error) {
	m, ok := s.momentums[height]
	if !ok {
		return nil, fmt.Errorf("missing momentum at height %d", height)
	}
	return m, nil
}

func (s *stubBackfillMomentumStore) PrefetchMomentum(m *nom.Momentum) (*nom.DetailedMomentum, error) {
	d, ok := s.detailed[m.Height]
	if !ok {
		return nil, fmt.Errorf("missing detailed momentum at height %d", m.Height)
	}
	return d, nil
}

func buildBackfillFixture(t *testing.T, totalHeights uint64, beneficiary types.Address) (*stubBackfillChainManager, *stubBackfillMomentumStore) {
	t.Helper()

	momentums := make(map[uint64]*nom.Momentum, totalHeights)
	detailed := make(map[uint64]*nom.DetailedMomentum, totalHeights)
	patches := make(map[types.HashHeight]db.Patch, totalHeights)

	fusedKey := common.JoinBytes(
		momentum.AccountStorePrefix,
		types.PlasmaContract.Bytes(),
		account.StorageKeyPrefix,
		definition.FusedAmountKeyPrefix,
		beneficiary.Bytes(),
	)

	var frontierID types.HashHeight
	for h := uint64(1); h <= totalHeights; h++ {
		hash := types.NewHash([]byte(fmt.Sprintf("momentum-%d", h)))
		m := &nom.Momentum{
			Height: h,
			Hash:   hash,
		}
		frontierID = m.Identifier()
		momentums[h] = m

		patch := db.NewPatch()
		var blocks []*nom.AccountBlock
		// Write a fused-amount update every 50 heights and at the final 5 heights
		// so both historical reads and rollback restoration are exercised.
		if h%50 == 0 || h > totalHeights-5 {
			amount := big.NewInt(int64(h * 100))
			patch.Put(fusedKey, amount.Bytes())
			blocks = []*nom.AccountBlock{{
				Address:              types.PlasmaContract,
				MomentumAcknowledged: types.HashHeight{Height: h - 1},
			}}
		}
		detailed[h] = &nom.DetailedMomentum{
			Momentum:      m,
			AccountBlocks: blocks,
		}
		patches[frontierID] = patch
	}

	frontierDB := db.NewMemDB()
	if totalHeights > 0 {
		common.FailIfErr(t, db.SetFrontier(frontierDB, frontierID, nil))
	}

	return &stubBackfillChainManager{
		frontier: frontierDB,
		patches:  patches,
	}, &stubBackfillMomentumStore{
		momentums: momentums,
		detailed:  detailed,
	}
}

func TestChainCacheInit_BackfillCompactsOnce(t *testing.T) {
	inner, err := storage.NewCacheDBManager(t.TempDir())
	common.FailIfErr(t, err)
	t.Cleanup(func() { common.FailIfErr(t, inner.Stop()) })

	mgr := &countingCacheManager{inner: inner}
	c := NewChainCache(mgr)

	var infoLogs int32
	c.log.SetHandler(log15.FuncHandler(func(r *log15.Record) error {
		if r.Lvl == log15.LvlInfo {
			atomic.AddInt32(&infoLogs, 1)
		}
		return nil
	}))

	// Verify Init releases chainCache.changes before calling manager.Compact():
	// GetFrontierCacheStore acquires chainCache.changes, so calling it inside
	// Compact would deadlock if Init still held the lock.
	var readerHeightDuringCompact uint64
	mgr.onCompact = func() {
		fs := c.GetFrontierCacheStore()
		if fs != nil {
			readerHeightDuringCompact = fs.Identifier().Height
		}
	}

	const totalHeights = 250
	beneficiary := types.ParseAddressPanic("z1qqjnwjjpnue8xmmpanz6csze6tcmtzzdtfsww7")
	chainMgr, momStore := buildBackfillFixture(t, totalHeights, beneficiary)

	common.FailIfErr(t, c.Init(chainMgr, momStore))

	if got := atomic.LoadInt32(&mgr.addCalls); got != totalHeights {
		t.Fatalf("Init replayed %d momentums, want %d", got, totalHeights)
	}
	if got := atomic.LoadInt32(&mgr.compactCalls); got != 1 {
		t.Fatalf("Init called Compact %d times across %d momentums, want 1", got, totalHeights)
	}
	if readerHeightDuringCompact != totalHeights {
		t.Fatalf("frontier height readable during Compact = %d, want %d", readerHeightDuringCompact, totalHeights)
	}
	if got := atomic.LoadInt32(&infoLogs); got > 2 {
		t.Fatalf("Init emitted %d Info log lines across %d momentums, want summary-only logging", got, totalHeights)
	}

	// Verify frontier and historical fused-amount values after backfill + compaction.
	frontier := c.GetFrontierCacheStore()
	if frontier == nil {
		t.Fatal("expected non-nil frontier cache store after Init")
	}
	common.Expect(t, frontier.Identifier(), momStore.momentums[totalHeights].Identifier())

	amountAt250, err := frontier.GetStakeBeneficialAmount(beneficiary)
	common.FailIfErr(t, err)
	common.ExpectAmount(t, amountAt250, big.NewInt(totalHeights*100))

	amountAt100, err := c.GetCacheStore(momStore.momentums[100].Identifier()).GetStakeBeneficialAmount(beneficiary)
	common.FailIfErr(t, err)
	common.ExpectAmount(t, amountAt100, big.NewInt(100*100))

	// Re-running Init when already caught up must not replay or compact.
	common.FailIfErr(t, c.Init(chainMgr, momStore))
	if got := atomic.LoadInt32(&mgr.addCalls); got != totalHeights {
		t.Fatalf("second Init replayed momentums: addCalls = %d, want %d", got, totalHeights)
	}
	if got := atomic.LoadInt32(&mgr.compactCalls); got != 1 {
		t.Fatalf("second Init called Compact when already caught up: compactCalls = %d, want 1", got)
	}

	// Rollback across compacted state must still restore the prior height and value.
	var lock sync.Mutex
	target := momStore.momentums[247].Identifier()
	common.FailIfErr(t, c.RollbackCacheTo(&lock, target))
	common.Expect(t, c.GetFrontierCacheStore().Identifier(), target)
	amountAt247, err := c.GetFrontierCacheStore().GetStakeBeneficialAmount(beneficiary)
	common.FailIfErr(t, err)
	common.ExpectAmount(t, amountAt247, big.NewInt(247*100))
}

func TestChainCacheInit_AlreadyCaughtUpDoesNotCompact(t *testing.T) {
	inner, err := storage.NewCacheDBManager(t.TempDir())
	common.FailIfErr(t, err)
	t.Cleanup(func() { common.FailIfErr(t, inner.Stop()) })

	mgr := &countingCacheManager{inner: inner}
	c := NewChainCache(mgr)

	beneficiary := types.ParseAddressPanic("z1qqjnwjjpnue8xmmpanz6csze6tcmtzzdtfsww7")
	chainMgr, momStore := buildBackfillFixture(t, 10, beneficiary)

	var lock sync.Mutex
	for h := uint64(1); h <= 10; h++ {
		d := momStore.detailed[h]
		common.FailIfErr(t, c.UpdateCache(&lock, d, chainMgr.GetPatch(d.Momentum.Identifier())))
	}
	if got := atomic.LoadInt32(&mgr.compactCalls); got != 0 {
		t.Fatalf("UpdateCache triggered %d compactions, want 0", got)
	}

	common.FailIfErr(t, c.Init(chainMgr, momStore))
	if got := atomic.LoadInt32(&mgr.compactCalls); got != 0 {
		t.Fatalf("Init on caught-up cache triggered %d compactions, want 0", got)
	}
}

func TestChainCacheInit_CompactErrorPropagates(t *testing.T) {
	inner, err := storage.NewCacheDBManager(t.TempDir())
	common.FailIfErr(t, err)
	t.Cleanup(func() { common.FailIfErr(t, inner.Stop()) })

	wantErr := errors.New("compaction disk failure")
	mgr := &countingCacheManager{inner: inner, compactErr: wantErr}
	c := NewChainCache(mgr)

	var errorLogs int32
	c.log.SetHandler(log15.FuncHandler(func(r *log15.Record) error {
		if r.Lvl == log15.LvlError {
			atomic.AddInt32(&errorLogs, 1)
		}
		return nil
	}))

	beneficiary := types.ParseAddressPanic("z1qqjnwjjpnue8xmmpanz6csze6tcmtzzdtfsww7")
	chainMgr, momStore := buildBackfillFixture(t, 5, beneficiary)

	err = c.Init(chainMgr, momStore)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Init() error = %v, want %v", err, wantErr)
	}
	if got := atomic.LoadInt32(&mgr.compactCalls); got != 1 {
		t.Fatalf("compactCalls = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&errorLogs); got != 1 {
		t.Fatalf("expected 1 Error log line for compaction failure, got %d", got)
	}
}

func TestChainCacheUpdateCache_DoesNotCompactAndLogsInfo(t *testing.T) {
	inner, err := storage.NewCacheDBManager(t.TempDir())
	common.FailIfErr(t, err)
	t.Cleanup(func() { common.FailIfErr(t, inner.Stop()) })

	mgr := &countingCacheManager{inner: inner}
	c := NewChainCache(mgr)

	var infoLogs int32
	c.log.SetHandler(log15.FuncHandler(func(r *log15.Record) error {
		if r.Lvl == log15.LvlInfo {
			atomic.AddInt32(&infoLogs, 1)
		}
		return nil
	}))

	const totalHeights = 210
	beneficiary := types.ParseAddressPanic("z1qqjnwjjpnue8xmmpanz6csze6tcmtzzdtfsww7")
	chainMgr, momStore := buildBackfillFixture(t, totalHeights, beneficiary)

	var lock sync.Mutex
	for h := uint64(1); h <= totalHeights; h++ {
		d := momStore.detailed[h]
		common.FailIfErr(t, c.UpdateCache(&lock, d, chainMgr.GetPatch(d.Momentum.Identifier())))
	}

	if got := atomic.LoadInt32(&mgr.compactCalls); got != 0 {
		t.Fatalf("UpdateCache across %d heights triggered %d compactions, want 0", totalHeights, got)
	}
	if got := atomic.LoadInt32(&infoLogs); got != totalHeights {
		t.Fatalf("UpdateCache emitted %d Info logs, want %d", got, totalHeights)
	}
	common.Expect(t, c.GetFrontierCacheStore().Identifier(), momStore.momentums[totalHeights].Identifier())
}
