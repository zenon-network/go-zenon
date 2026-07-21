package chain

import (
	"errors"
	"sync"
	"testing"

	"github.com/zenon-network/go-zenon/chain/momentum"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// fakeMomentumManagerDB is an injectable db.Manager that lets tests control
// exactly when Add and Pop succeed or fail, independently of real chain state.
type fakeMomentumManagerDB struct {
	frontierDB db.DB
	frontiers  []db.DB
	addErr     error
	popErr     error
	popErrAt   int

	addCalls int
	popCalls int
}

func (m *fakeMomentumManagerDB) Frontier() db.DB {
	if len(m.frontiers) > 0 {
		return m.frontiers[len(m.frontiers)-1]
	}
	return m.frontierDB
}
func (m *fakeMomentumManagerDB) Get(types.HashHeight) db.DB {
	return nil
}
func (m *fakeMomentumManagerDB) GetPatch(types.HashHeight) db.Patch {
	return nil
}
func (m *fakeMomentumManagerDB) Add(db.Transaction) error {
	m.addCalls += 1
	return m.addErr
}
func (m *fakeMomentumManagerDB) Pop() error {
	m.popCalls += 1
	if m.popErr != nil && (m.popErrAt == 0 || m.popErrAt == m.popCalls) {
		return m.popErr
	}
	if len(m.frontiers) > 1 {
		m.frontiers = m.frontiers[:len(m.frontiers)-1]
	}
	return nil
}
func (m *fakeMomentumManagerDB) Stop() error {
	return nil
}
func (m *fakeMomentumManagerDB) Location() string {
	return "fake"
}

// fakeMomentumListener records the events broadcast by a momentumPool.
type fakeMomentumListener struct {
	inserts int
	deletes int
}

func (l *fakeMomentumListener) InsertMomentum(*nom.DetailedMomentum) {
	l.inserts += 1
}
func (l *fakeMomentumListener) DeleteMomentum(*nom.DetailedMomentum) {
	l.deletes += 1
}

func newTestMomentumPool(manager db.Manager) (*momentumPool, *fakeMomentumListener) {
	pool := &momentumPool{
		momentumEventManager: newMomentumEventManager(),
		chainManager:         manager,
		genesis:              nil,
		log:                  common.ChainLogger.New("submodule", "test-momentum-pool"),
	}
	listener := &fakeMomentumListener{}
	pool.Register(listener)
	return pool, listener
}

// corruptEntryAtHeight writes undecodable bytes at the height entry, so that
// whichever store reads it back (momentum or account-block) fails to deserialize.
func corruptEntryAtHeight(target db.DB, identifier types.HashHeight) {
	common.DealWithErr(db.SetFrontier(target, identifier, []byte{0xFF}))
}

func newFrontierMomentumDB(identifier types.HashHeight) db.DB {
	mem := db.NewMemDB()
	m := &nom.Momentum{
		Hash:   identifier.Hash,
		Height: identifier.Height,
	}
	data, err := m.Serialize()
	common.DealWithErr(err)
	common.DealWithErr(db.SetFrontier(mem, identifier, data))
	return mem
}

func newMomentumFrontiers(height uint64) []db.DB {
	frontiers := make([]db.DB, 0, height)
	for frontierHeight := uint64(1); frontierHeight <= height; frontierHeight++ {
		mem := db.NewMemDB()
		for momentumHeight := uint64(1); momentumHeight <= frontierHeight; momentumHeight++ {
			identifier := testHashHeight(momentumHeight)
			m := &nom.Momentum{
				Hash:         identifier.Hash,
				PreviousHash: testHashHeight(momentumHeight - 1).Hash,
				Height:       momentumHeight,
			}
			data, err := m.Serialize()
			common.DealWithErr(err)
			common.DealWithErr(db.SetFrontier(mem, identifier, data))
		}
		frontiers = append(frontiers, mem)
	}
	return frontiers
}

func testTransaction(content nom.MomentumContent) *nom.MomentumTransaction {
	return &nom.MomentumTransaction{
		Momentum: &nom.Momentum{
			Hash:    types.NewHash(common.Uint64ToBytes(1)),
			Height:  1,
			Content: content,
		},
	}
}

func TestAddMomentumTransaction_AddFails(t *testing.T) {
	manager := &fakeMomentumManagerDB{
		frontierDB: newFrontierMomentumDB(testHashHeight(0)),
		addErr:     errors.New("add failed"),
	}
	pool, listener := newTestMomentumPool(manager)

	err := pool.AddMomentumTransaction(&sync.Mutex{}, testTransaction(nil))

	common.ExpectTrue(t, err != nil)
	common.Expect(t, manager.popCalls, 0)
	common.Expect(t, listener.inserts, 0)
	common.Expect(t, listener.deletes, 0)
}

func TestAddMomentumTransaction_PrefetchFailsPopSucceeds(t *testing.T) {
	frontier := newFrontierMomentumDB(testHashHeight(0))
	missingAddress := types.Address{}
	missingHeader := &types.AccountHeader{Address: missingAddress, HashHeight: types.HashHeight{Height: 1}}
	accountPrefix := common.JoinBytes(momentum.AccountStorePrefix, missingAddress.Bytes())
	corruptEntryAtHeight(frontier.Subset(accountPrefix), types.HashHeight{Height: 1})

	manager := &fakeMomentumManagerDB{frontierDB: frontier}
	pool, listener := newTestMomentumPool(manager)

	err := pool.AddMomentumTransaction(&sync.Mutex{}, testTransaction(nom.MomentumContent{missingHeader}))

	common.ExpectTrue(t, err != nil)
	var uncertain *ErrCanonicalStateUncertain
	common.ExpectTrue(t, !errors.As(err, &uncertain))
	common.Expect(t, manager.popCalls, 1)
	common.Expect(t, listener.inserts, 0)
	common.Expect(t, listener.deletes, 0)
}

func TestAddMomentumTransaction_SporkValidationFailsPopSucceeds(t *testing.T) {
	frontier := newFrontierMomentumDB(testHashHeight(1))
	corruptEntryAtHeight(frontier, testHashHeight(1))

	manager := &fakeMomentumManagerDB{frontierDB: frontier}
	pool, listener := newTestMomentumPool(manager)

	err := pool.AddMomentumTransaction(&sync.Mutex{}, testTransaction(nil))

	common.ExpectTrue(t, err != nil)
	var uncertain *ErrCanonicalStateUncertain
	common.ExpectTrue(t, !errors.As(err, &uncertain))
	common.Expect(t, manager.popCalls, 1)
	common.Expect(t, listener.inserts, 0)
	common.Expect(t, listener.deletes, 0)
}

func TestAddMomentumTransaction_PopFailsIsUnrecoverable(t *testing.T) {
	frontier := newFrontierMomentumDB(testHashHeight(1))
	corruptEntryAtHeight(frontier, testHashHeight(1))

	manager := &fakeMomentumManagerDB{
		frontierDB: frontier,
		popErr:     errors.New("pop failed"),
	}
	pool, listener := newTestMomentumPool(manager)

	err := pool.AddMomentumTransaction(&sync.Mutex{}, testTransaction(nil))

	var uncertain *ErrCanonicalStateUncertain
	common.ExpectTrue(t, errors.As(err, &uncertain))
	common.ExpectTrue(t, uncertain.Cause != nil)
	common.ExpectTrue(t, uncertain.RollbackErr != nil)
	common.Expect(t, manager.popCalls, 1)
	common.Expect(t, listener.inserts, 0)
	common.Expect(t, listener.deletes, 0)
}

func TestAddMomentumTransaction_Success(t *testing.T) {
	manager := &fakeMomentumManagerDB{frontierDB: newFrontierMomentumDB(testHashHeight(1))}
	pool, listener := newTestMomentumPool(manager)

	err := pool.AddMomentumTransaction(&sync.Mutex{}, testTransaction(nil))

	common.FailIfErr(t, err)
	common.Expect(t, manager.popCalls, 0)
	common.Expect(t, listener.inserts, 1)
	common.Expect(t, listener.deletes, 0)
}

func TestRollbackToFailureAfterPopIsUnrecoverable(t *testing.T) {
	manager := &fakeMomentumManagerDB{
		frontiers: newMomentumFrontiers(3),
		popErr:    errors.New("second pop failed"),
		popErrAt:  2,
	}
	pool, listener := newTestMomentumPool(manager)

	err := pool.RollbackTo(&sync.Mutex{}, testHashHeight(1))

	var uncertain *ErrCanonicalStateUncertain
	common.ExpectTrue(t, errors.As(err, &uncertain))
	common.ExpectTrue(t, uncertain.Cause != nil)
	common.ExpectTrue(t, uncertain.RollbackErr != nil)
	common.Expect(t, manager.popCalls, 2)
	common.Expect(t, listener.inserts, 0)
	common.Expect(t, listener.deletes, 1)
	common.Expect(t, db.GetFrontierIdentifier(manager.Frontier()), testHashHeight(2))
}

func TestRollbackToFirstPopFailureLeavesCanonicalStateCertain(t *testing.T) {
	popErr := errors.New("first pop failed")
	manager := &fakeMomentumManagerDB{
		frontiers: newMomentumFrontiers(2),
		popErr:    popErr,
		popErrAt:  1,
	}
	pool, listener := newTestMomentumPool(manager)

	err := pool.RollbackTo(&sync.Mutex{}, testHashHeight(1))

	var uncertain *ErrCanonicalStateUncertain
	common.ExpectTrue(t, errors.Is(err, popErr))
	common.ExpectTrue(t, !errors.As(err, &uncertain))
	common.Expect(t, manager.popCalls, 1)
	common.Expect(t, listener.inserts, 0)
	common.Expect(t, listener.deletes, 0)
	common.Expect(t, db.GetFrontierIdentifier(manager.Frontier()), testHashHeight(2))
}
