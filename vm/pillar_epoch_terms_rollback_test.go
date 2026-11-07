package vm

import (
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

// Issue #228 at the VM boundary: when a write of the per-epoch reward
// terms (or of the epoch history that records them) fails inside an
// embedded pillar receive, generateEmbeddedReceive must produce a
// resultFail receive and rollbackEmbedded must discard every write the
// method made, so no partial terms record set or half-settled epoch is
// left behind. The context is the real vm_context account context over
// an in-memory pillar contract store whose storage fails one chosen Put
// or Delete.

var errInjectedTermsWrite = errors.New("injected pillar storage write failure")

// termsFailingAccount wraps the contract's account store so that its
// storage, and the storage of every snapshot taken from it, fails a Put
// or Delete of one key.
type termsFailingAccount struct {
	store.Account
	failure *termsWriteFailure
}

type termsWriteFailure struct {
	put    []byte
	delete []byte
}

func (a *termsFailingAccount) Storage() db.DB {
	return &termsFailingDB{DB: a.Account.Storage(), failure: a.failure}
}

func (a *termsFailingAccount) Snapshot() store.Account {
	return &termsFailingAccount{Account: a.Account.Snapshot(), failure: a.failure}
}

type termsFailingDB struct {
	db.DB
	failure *termsWriteFailure
}

func (d *termsFailingDB) Put(key, value []byte) error {
	if d.failure.put != nil && string(key) == string(d.failure.put) {
		return errInjectedTermsWrite
	}
	return d.DB.Put(key, value)
}

func (d *termsFailingDB) Delete(key []byte) error {
	if d.failure.delete != nil && string(key) == string(d.failure.delete) {
		return errInjectedTermsWrite
	}
	return d.DB.Delete(key)
}

// termsSporkCache reports only types.EpochRewardTermsSpork as active.
type termsSporkCache struct {
	store.Cache
}

func (termsSporkCache) IsSporkActive(spork *types.ImplementedSpork) (bool, error) {
	return spork.SporkId == types.EpochRewardTermsSpork.SporkId, nil
}

// termsPillarReader has 1-second epochs from unix 0 and, for epochs 1
// and 2 only, statistics in which termsVMPillar produced 10 of 10
// momentums with the whole weight delegated by termsVMBacker.
type termsPillarReader struct{}

func (termsPillarReader) GetPillarWeights() (map[string]*big.Int, error) { return nil, nil }
func (termsPillarReader) EpochTicker() common.Ticker {
	return common.NewTicker(time.Unix(0, 0), time.Second)
}
func (termsPillarReader) EpochStats(epoch uint64) (*api.EpochStats, error) {
	if epoch != 1 && epoch != 2 {
		return nil, errors.New("unexpected epoch")
	}
	return &api.EpochStats{
		Epoch: epoch,
		Pillars: map[string]*api.EpochPillarStats{
			termsVMPillar: {Epoch: epoch, Name: termsVMPillar, BlockNum: 10, ExpectedBlockNum: 10, Weight: big.NewInt(1000)},
		},
		TotalWeight: big.NewInt(1000),
		TotalBlocks: 10,
	}, nil
}
func (termsPillarReader) GetPillarDelegationsByEpoch(epoch uint64) (map[string]*types.PillarDelegationDetail, error) {
	return map[string]*types.PillarDelegationDetail{
		termsVMPillar: {
			PillarDelegation: types.PillarDelegation{Name: termsVMPillar, Weight: big.NewInt(1000)},
			Backers:          map[types.Address]*big.Int{termsVMBacker: big.NewInt(1000)},
		},
	}, nil
}

const termsVMPillar = "vm-terms-pillar"

var (
	termsVMOwner       = termsVMAddress(1)
	termsVMReward      = termsVMAddress(2)
	termsVMBacker      = termsVMAddress(3)
	termsVMNewProducer = termsVMAddress(4)
)

func termsVMAddress(b byte) types.Address {
	var a types.Address
	a[0] = types.UserAddrByte
	a[1] = b
	a[types.AddressSize-1] = b
	return a
}

type termsVMHarness struct {
	t       *testing.T
	vm      *VM
	ctx     vm_context.AccountVmContext
	failure *termsWriteFailure
	ms      *rollbackMomentumStore
}

// newTermsVMHarness seeds the pillar contract with termsVMPillar at
// 50/50, the given settled epoch and terms records, and a frontier
// momentum at unix frontierTime.
func newTermsVMHarness(t *testing.T, frontierTime int64, lastEpoch int64, records ...definition.PillarEpochTerms) *termsVMHarness {
	t.Helper()
	failure := &termsWriteFailure{}
	acc := &termsFailingAccount{Account: account.NewAccountStore(types.PillarContract, db.NewMemDB()), failure: failure}
	storage := acc.Storage()
	common.FailIfErr(t, (&definition.PillarInfo{
		Name:                         termsVMPillar,
		BlockProducingAddress:        termsVMOwner,
		RewardWithdrawAddress:        termsVMReward,
		StakeAddress:                 termsVMOwner,
		Amount:                       constants.PillarStakeAmount,
		GiveBlockRewardPercentage:    50,
		GiveDelegateRewardPercentage: 50,
		PillarType:                   definition.NormalPillarType,
	}).Save(storage))
	common.FailIfErr(t, (&definition.ProducingPillar{Name: termsVMPillar, Producing: &termsVMOwner}).Save(storage))
	common.FailIfErr(t, (&definition.LastEpochUpdate{LastEpoch: lastEpoch}).Save(storage))
	for i := range records {
		common.FailIfErr(t, records[i].Save(storage))
	}

	ts := time.Unix(frontierTime, 0)
	ms := &rollbackMomentumStore{
		frontier: &nom.Momentum{
			Hash:      types.HexToHashPanic("01aab631b44daf47f68810a416bea4ed3695e89c0ab9668a0ab8acb834dfb900"),
			Height:    constants.UpdateMinNumMomentums + 10,
			Timestamp: &ts,
		},
	}
	ctx := vm_context.NewAccountContext(ms, acc, termsSporkCache{}, termsPillarReader{})
	return &termsVMHarness{t: t, vm: NewVM(ctx, ms), ctx: ctx, failure: failure, ms: ms}
}

// receive runs the pillar contract's receive of a send carrying data
// and returns the generated block and the method's error.
func (h *termsVMHarness) receive(data []byte) (*nom.AccountBlock, error) {
	h.t.Helper()
	send := &nom.AccountBlock{
		BlockType:     nom.BlockTypeUserSend,
		Address:       termsVMOwner,
		ToAddress:     types.PillarContract,
		Amount:        big.NewInt(0),
		TokenStandard: types.ZnnTokenStandard,
		Data:          data,
	}
	send.Hash = send.ComputeHash()
	h.ms.sendBlock = send
	block, methodErr, err := h.vm.generateEmbeddedReceive(send.Hash)
	common.FailIfErr(h.t, err)
	if block == nil {
		h.t.Fatal("no receive block")
	}
	return block, methodErr
}

func (h *termsVMHarness) updatePillar(giveBlock, giveDelegate uint8) (*nom.AccountBlock, error) {
	return h.receive(definition.ABIPillars.PackMethodPanic(definition.UpdatePillarMethodName,
		termsVMPillar, termsVMNewProducer, termsVMReward, giveBlock, giveDelegate))
}

func (h *termsVMHarness) update() (*nom.AccountBlock, error) {
	return h.receive(definition.ABIPillars.PackMethodPanic(definition.UpdateMethodName))
}

func (h *termsVMHarness) expectStatus(block *nom.AccountBlock, want uint64) {
	h.t.Helper()
	if got := common.BytesToUint64(block.Data); got != want {
		h.t.Fatalf("receive status %d, want %d", got, want)
	}
}

func (h *termsVMHarness) expectTerms(want ...[3]uint64) {
	h.t.Helper()
	list, err := definition.GetPillarEpochTermsList(h.ctx.Storage(), termsVMPillar)
	common.FailIfErr(h.t, err)
	got := make([][3]uint64, 0, len(list))
	for _, r := range list {
		got = append(got, [3]uint64{r.EffectiveEpoch, uint64(r.GiveBlockRewardPercentage), uint64(r.GiveDelegateRewardPercentage)})
	}
	if len(got) != len(want) {
		h.t.Fatalf("terms records %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			h.t.Fatalf("terms records %v, want %v", got, want)
		}
	}
}

// expectPillarUnchanged checks that the pillar still has its seeded
// split and producing address and that the new producing address was
// not bound.
func (h *termsVMHarness) expectPillarUnchanged() {
	h.t.Helper()
	pillar, err := definition.GetPillarInfo(h.ctx.Storage(), termsVMPillar)
	common.FailIfErr(h.t, err)
	if pillar.GiveBlockRewardPercentage != 50 || pillar.GiveDelegateRewardPercentage != 50 || pillar.BlockProducingAddress != termsVMOwner {
		h.t.Fatalf("pillar changed despite the rollback: %d/%d producer %s", pillar.GiveBlockRewardPercentage, pillar.GiveDelegateRewardPercentage, pillar.BlockProducingAddress)
	}
	if _, err := definition.GetProducingPillarName(h.ctx.Storage(), termsVMNewProducer); err != constants.ErrDataNonExistent {
		h.t.Fatalf("new producing address bound despite the rollback: err %v", err)
	}
}

func (h *termsVMHarness) expectLastEpoch(want int64) {
	h.t.Helper()
	last, err := definition.GetLastEpochUpdate(h.ctx.Storage())
	common.FailIfErr(h.t, err)
	if last.LastEpoch != want {
		h.t.Fatalf("last settled epoch %d, want %d", last.LastEpoch, want)
	}
}

func (h *termsVMHarness) depositOf(addr types.Address) *big.Int {
	h.t.Helper()
	deposit, err := definition.GetRewardDeposit(h.ctx.Storage(), &addr)
	common.FailIfErr(h.t, err)
	return deposit.Znn
}

func expectInjectedTermsWrite(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, errInjectedTermsWrite) {
		t.Fatalf("method error %v, want the injected write failure", err)
	}
}

// UpdatePillar at unix 10 (epoch 10), first change: writes the baseline
// at EffectiveEpoch 0 and the new split at 11, and binds a new producing
// address before either. A failure of either terms write rolls all of
// it back.
func TestGenerateEmbeddedReceive_UpdatePillarTermsWriteFailureRollsBack(t *testing.T) {
	t.Run("success control", func(t *testing.T) {
		h := newTermsVMHarness(t, 10, 0)
		block, err := h.updatePillar(80, 10)
		common.FailIfErr(t, err)
		h.expectStatus(block, resultSuccess)
		h.expectTerms([3]uint64{0, 50, 50}, [3]uint64{11, 80, 10})
		name, err := definition.GetProducingPillarName(h.ctx.Storage(), termsVMNewProducer)
		common.FailIfErr(t, err)
		common.Expect(t, name.Name, termsVMPillar)
	})
	for _, tc := range []struct {
		name string
		key  []byte
	}{
		{"baseline", definition.GetPillarEpochTermsKey(termsVMPillar, 0)},
		{"new terms", definition.GetPillarEpochTermsKey(termsVMPillar, 11)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTermsVMHarness(t, 10, 0)
			h.failure.put = tc.key
			block, err := h.updatePillar(80, 10)
			expectInjectedTermsWrite(t, err)
			h.expectStatus(block, resultFail)
			h.expectTerms()
			h.expectPillarUnchanged()
		})
	}
}

// UpdatePillar at unix 10 with epochs up to 5 settled and records at 0
// and 3: the record at 0 can no longer govern an unsettled epoch and is
// deleted. A failure of that delete rolls the receive back.
func TestGenerateEmbeddedReceive_UpdatePillarPruneFailureRollsBack(t *testing.T) {
	records := []definition.PillarEpochTerms{
		{Name: termsVMPillar, EffectiveEpoch: 0, GiveBlockRewardPercentage: 40, GiveDelegateRewardPercentage: 40},
		{Name: termsVMPillar, EffectiveEpoch: 3, GiveBlockRewardPercentage: 50, GiveDelegateRewardPercentage: 50},
	}
	t.Run("success control", func(t *testing.T) {
		h := newTermsVMHarness(t, 10, 5, records...)
		block, err := h.updatePillar(80, 10)
		common.FailIfErr(t, err)
		h.expectStatus(block, resultSuccess)
		h.expectTerms([3]uint64{3, 50, 50}, [3]uint64{11, 80, 10})
	})
	t.Run("delete fails", func(t *testing.T) {
		h := newTermsVMHarness(t, 10, 5, records...)
		h.failure.delete = definition.GetPillarEpochTermsKey(termsVMPillar, 0)
		block, err := h.updatePillar(80, 10)
		expectInjectedTermsWrite(t, err)
		h.expectStatus(block, resultFail)
		h.expectTerms([3]uint64{0, 40, 40}, [3]uint64{3, 50, 50})
		h.expectPillarUnchanged()
	})
}

// Update at unix 3603 settles epochs 1 and 2 in one receive. The
// record at 0 governs epoch 1 (0/100) and the record at 2 governs epoch
// 2 (50/50, while PillarInfo says 50/50 too). A failure of the epoch 2
// history write, after epoch 1 has been fully credited, rolls back both
// epoch markers and every credit.
func TestGenerateEmbeddedReceive_UpdateEpochHistoryFailureRollsBack(t *testing.T) {
	records := []definition.PillarEpochTerms{
		{Name: termsVMPillar, EffectiveEpoch: 0, GiveBlockRewardPercentage: 0, GiveDelegateRewardPercentage: 100},
		{Name: termsVMPillar, EffectiveEpoch: 2, GiveBlockRewardPercentage: 50, GiveDelegateRewardPercentage: 50},
	}
	frontier := int64(2 + 1 + constants.RewardTimeLimit)

	t.Run("success control", func(t *testing.T) {
		h := newTermsVMHarness(t, frontier, 0, records...)
		block, err := h.update()
		common.FailIfErr(t, err)
		h.expectStatus(block, resultSuccess)
		h.expectLastEpoch(2)
		h.expectHistory(1, 0, 100)
		h.expectHistory(2, 50, 50)
		// each epoch: 833333330 momentum + 400000000 delegation reward;
		// epoch 1 at 0/100 gives the delegator 400000000, epoch 2 at
		// 50/50 gives it 616666665
		common.Expect(t, h.epochCreditOf(1, termsVMBacker).String(), "400000000")
		common.Expect(t, h.epochCreditOf(2, termsVMBacker).String(), "616666665")
		common.Expect(t, h.depositOf(termsVMBacker).String(), "1016666665")
		common.Expect(t, h.depositOf(termsVMReward).String(), "1449999995")
	})
	t.Run("history write fails after a credited epoch", func(t *testing.T) {
		h := newTermsVMHarness(t, frontier, 0, records...)
		scratch := db.NewMemDB()
		common.FailIfErr(t, (&definition.PillarEpochHistory{Name: termsVMPillar, Epoch: 2, Weight: big.NewInt(0)}).Save(scratch))
		it := scratch.NewIterator(nil)
		if !it.Next() {
			t.Fatal("no history key")
		}
		h.failure.put = append([]byte(nil), it.Key()...)
		it.Release()

		block, err := h.update()
		expectInjectedTermsWrite(t, err)
		h.expectStatus(block, resultFail)
		h.expectLastEpoch(0)
		for _, epoch := range []uint64{1, 2} {
			history, err := definition.GetPillarEpochHistoryList(h.ctx.Storage(), epoch)
			common.FailIfErr(t, err)
			if len(history) != 0 {
				t.Fatalf("epoch %d history %+v survived the rollback", epoch, history)
			}
			common.Expect(t, h.epochCreditOf(epoch, termsVMBacker).Sign(), 0)
			common.Expect(t, h.epochCreditOf(epoch, termsVMReward).Sign(), 0)
		}
		common.Expect(t, h.depositOf(termsVMBacker).Sign(), 0)
		common.Expect(t, h.depositOf(termsVMReward).Sign(), 0)
		last, err := definition.GetLastUpdate(h.ctx.Storage())
		common.FailIfErr(t, err)
		common.Expect(t, last.Height, uint64(0))
	})
}

func (h *termsVMHarness) expectHistory(epoch uint64, giveBlock, giveDelegate uint8) {
	h.t.Helper()
	history, err := definition.GetPillarEpochHistoryList(h.ctx.Storage(), epoch)
	common.FailIfErr(h.t, err)
	if len(history) != 1 || history[0].GiveBlockRewardPercentage != giveBlock || history[0].GiveDelegateRewardPercentage != giveDelegate {
		h.t.Fatalf("epoch %d history %+v, want one %d/%d entry", epoch, history, giveBlock, giveDelegate)
	}
}

func (h *termsVMHarness) epochCreditOf(epoch uint64, addr types.Address) *big.Int {
	h.t.Helper()
	credit, err := definition.GetRewardDepositHistory(h.ctx.Storage(), epoch, &addr)
	common.FailIfErr(h.t, err)
	return credit.Znn
}
