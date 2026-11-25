//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
)

// The TestFast groups need the test build with fast constants
// (docs/TESTING.md, procedure P11): a 10-minute epoch, locks of minutes and
// a bridge administrator whose key the driver holds. They cover what cannot
// be reached on a stock binary in a test run: expiry of locks, rewards and
// administration. The rules are the node's own; only the durations and one
// address differ.

// frontierTime is the timestamp of the frontier momentum.
func frontierTime(t *testing.T) int64 {
	t.Helper()
	m, err := net.Frontier()
	if err != nil {
		t.Fatal(err)
	}
	return int64(m.TimestampUnix)
}

// rewardBook is what a reward contract shows for one address at one moment:
// how many epochs it has processed, the uncollected amount, and the amount
// credited per epoch. The count is read before and after the other two and
// the reading is repeated until it did not change in between. The count is
// held against the chain's clock: it cannot be more than the finished
// epochs, nor lag them by more than one.
type rewardBook struct {
	processed int64
	credit    *big.Int
	other     *big.Int // the deposit's amount of the other token
	perEpoch  map[int64]*big.Int
	perOther  map[int64]*big.Int // the history of the other token
	tip       uint64             // height of the reward contract's newest block when the book was read; set by quietBook
}

var digits = regexp.MustCompile(`^[0-9]+$`)

// sum is what the book credits for epochs from..to-1.
func (b rewardBook) sum(from, to int64) *big.Int {
	s := new(big.Int)
	for e := from; e < to; e++ {
		if v := b.perEpoch[e]; v != nil {
			s.Add(s, v)
		}
	}
	return s
}

func readRewardBook(t *testing.T, contract string, addr types.Address, field string) rewardBook {
	t.Helper()
	return readBook(t, contract, addr, field, -1)
}

// readBook reads the book. With exact >= 0, every count it sees, on each
// try, must be exact: a book read after a collect that sees the processed
// count move, even once and back, makes the run inconclusive.
func readBook(t *testing.T, contract string, addr types.Address, field string, exact int64) rewardBook {
	t.Helper()
	// Rows decode into their wire types (RewardHistoryEntryMarshal): an
	// epoch that is not a JSON integer, such as "0" or 1e-400, fails the
	// decode instead of rounding, and a missing field stays nil.
	type row struct {
		Epoch *int64  `json:"epoch"`
		Znn   *string `json:"znnAmount"`
		Qsr   *string `json:"qsrAmount"`
	}
	type page struct {
		Count *int64 `json:"count"`
		List  *[]row `json:"list"`
	}
	render := func(l []row) string {
		var b strings.Builder
		for _, r := range l {
			fmt.Fprintf(&b, "[%d %s %s]", *r.Epoch, *r.Znn, *r.Qsr)
		}
		return b.String()
	}
	for try := 0; try < 6; try++ {
		var first, second page
		var reward obj
		finishedLow := epochNow(t)
		get(t, &first, "embedded."+contract+".getFrontierRewardByPage", addr, 0, 50)
		get(t, &reward, "embedded."+contract+".getUncollectedReward", addr)
		get(t, &second, "embedded."+contract+".getFrontierRewardByPage", addr, 0, 50)
		finishedHigh := epochNow(t)
		if first.Count == nil || first.List == nil || second.Count == nil || second.List == nil {
			t.Fatalf("%s reward history of %s has no count or no list", contract, addr)
		}
		if exact >= 0 && (*first.Count != exact || *second.Count != exact) {
			t.Fatalf("%s: the processed epoch count read %d and %d after the collect, %d before; an epoch was processed around the collect (or the read went back), so this run is inconclusive", contract, *first.Count, *second.Count, exact)
		}
		if *first.Count != *second.Count {
			continue
		}
		// Both pages must have the wire form of the history: an integral
		// epoch number and amounts as strings of digits. Then they must say
		// the same, row for row: a value that changes between them is not a
		// reading of the book.
		for _, l := range []*[]row{first.List, second.List} {
			for _, e := range *l {
				if e.Epoch == nil || e.Znn == nil || e.Qsr == nil || !digits.MatchString(*e.Znn) || !digits.MatchString(*e.Qsr) {
					t.Fatalf("%s reward history of %s: an entry not in the history's form: %+v", contract, addr, e)
				}
			}
		}
		if a, b := render(*first.List), render(*second.List); a != b {
			if exact >= 0 {
				t.Fatalf("%s: the reward history read %s and then %s after the collect; the book changed while it was read, so this run is inconclusive", contract, a, b)
			}
			continue
		}
		// The history is dense, one entry per processed epoch, zero amounts
		// included (rpc/api/embedded/shared.go), so the first page of 50
		// holds every epoch while there are at most 50.
		if *second.Count > 50 {
			t.Fatalf("%s reward history of %s has %d epochs, more than one page of 50; this case reads one page", contract, addr, *second.Count)
		}
		if int64(len(*second.List)) != *second.Count {
			t.Fatalf("%s reward history of %s: %d entries for %d processed epochs", contract, addr, len(*second.List), *second.Count)
		}
		if *second.Count > finishedHigh || *second.Count < finishedLow-1 {
			t.Fatalf("the %s contract has processed %d epoch(s); %d to %d were finished when that was read", contract, *second.Count, finishedLow, finishedHigh)
		}
		otherField := map[string]string{"qsrAmount": "znnAmount", "znnAmount": "qsrAmount"}[field]
		if _, ok := reward[field]; !ok || reward[otherField] == nil || str(reward["address"]) != addr.String() {
			t.Fatalf("%s uncollected reward of %s: want address, %s and %s, got %v", contract, addr, field, otherField, reward)
		}
		book := rewardBook{processed: *second.Count, credit: bigOf(t, reward[field]), other: bigOf(t, reward[otherField]), perEpoch: map[int64]*big.Int{}, perOther: map[int64]*big.Int{}}
		for _, e := range *second.List {
			mine, theirs := *e.Qsr, *e.Znn
			if field == "znnAmount" {
				mine, theirs = *e.Znn, *e.Qsr
			}
			epoch := *e.Epoch
			v, ok := new(big.Int).SetString(mine, 10)
			if _, twice := book.perEpoch[epoch]; !ok || twice || v.Sign() < 0 || epoch < 0 || epoch >= book.processed {
				t.Fatalf("%s reward history of %s: bad entry %s with %d epochs processed", contract, addr, render([]row{e}), book.processed)
			}
			o, ok := new(big.Int).SetString(theirs, 10)
			if !ok || o.Sign() < 0 {
				t.Fatalf("%s reward history of %s: bad %s in entry %s", contract, addr, otherField, render([]row{e}))
			}
			book.perEpoch[epoch], book.perOther[epoch] = v, o
		}
		return book
	}
	t.Fatalf("%s reward history of %s changed during six readings in a row", contract, addr)
	return rewardBook{}
}

// quietBook waits until a collect can be judged exactly and returns the
// book read then: the contract has processed every finished epoch, and the
// running epoch has at least two minutes left, both from one frontier
// momentum read just after the book. A collect pays the stored deposit and
// processes no epoch itself (CollectRewardMethod in
// vm/embedded/implementation/common.go); epochs are processed by update
// calls to the contract once an epoch has ended. The book also records the
// height of the contract's newest block, read before and after it and the
// same, so the collect can later be placed on the contract's chain relative
// to every update.
func quietBook(t *testing.T, contract string, to, addr types.Address, field string) rewardBook {
	t.Helper()
	epoch := int64(constants.MomentumsPerEpoch) * 10
	var first struct {
		List []struct {
			Timestamp int64 `json:"timestamp"`
		} `json:"list"`
	}
	get(t, &first, "ledger.getMomentumsByHeight", 1, 1)
	if len(first.List) != 1 || first.List[0].Timestamp == 0 {
		t.Fatalf("genesis momentum: %+v", first)
	}
	var book rewardBook
	eventually(t, 2*time.Duration(epoch)*time.Second, "the "+contract+" contract has processed every finished epoch with two minutes of the running one left", func() bool {
		tipBefore := contractTip(t, to)
		book = readRewardBook(t, contract, addr, field)
		if contractTip(t, to) != tipBefore {
			return false
		}
		book.tip = tipBefore
		since := frontierTime(t) - first.List[0].Timestamp
		return book.processed == since/epoch && epoch-since%epoch >= 120
	})
	return book
}

// contractTip is the height of the contract's newest block.
func contractTip(t *testing.T, contract types.Address) uint64 {
	t.Helper()
	var tip *api.AccountBlock
	get(t, &tip, "ledger.getFrontierAccountBlock", contract)
	if tip == nil || tip.Address != contract || tip.Height == 0 {
		t.Fatalf("the newest block of %s reads as %+v", contract, tip)
	}
	return tip.Height
}

// collectExactly collects a reward for an account that has never collected
// one, from a book read by quietBook and already checked by the case, and
// requires the outcome to the unit.
//
// Before: the credit is positive and exactly what the history credits,
// since nothing was ever collected, and the deposit holds nothing of the
// other token. The collect's receive must make exactly one instruction to
// the token contract, a mint of that credit to the account; the token
// contract's receive of it must succeed and make exactly one send, that
// amount of that token to the account, and that send is the one the driver
// receives. The balance rises by exactly the credit.
//
// Afterwards the history of the epochs processed before is unchanged and
// the other token is still nil. If no epoch was processed meanwhile,
// nothing is uncollected. If some were, every update call the contract
// answered since the book was read must come after the collect on the
// contract's chain, and what is uncollected is exactly what those epochs
// credit.
func collectExactly(t *testing.T, kp *wallet.KeyPair, contract string, to types.Address, data []byte, zts types.ZenonTokenStandard, field string, before rewardBook,
	epochOK func(epoch int64, v *big.Int) string) (paid *big.Int) {
	t.Helper()
	for e := int64(0); e < before.processed; e++ {
		if before.perOther[e].Sign() != 0 {
			t.Fatalf("%s: epoch %d credits %s of the other token to an account that earns only %s", contract, e, before.perOther[e], zts)
		}
	}
	if before.credit.Sign() <= 0 || before.credit.Cmp(before.sum(0, before.processed)) != 0 || before.other.Sign() != 0 {
		t.Fatalf("%s: %s is uncollected and %s of the other token, but the history of an account that never collected credits %s over %d epoch(s)", contract, before.credit, before.other, before.sum(0, before.processed), before.processed)
	}
	balance := bal(t, kp.Address, zts)
	res := callOK(t, kp, to, data, znn, units(0))
	var mints []*nom.AccountBlock
	for _, d := range res.Receive.DescendantBlocks {
		if d.Address == to && nom.IsSendBlock(d.BlockType) {
			mints = append(mints, d)
		}
	}
	if len(mints) != 1 || len(res.Receive.DescendantBlocks) != 1 || mints[0].BlockType != nom.BlockTypeContractSend || mints[0].ToAddress != types.TokenContract || mints[0].Amount == nil || mints[0].Amount.Sign() != 0 {
		t.Fatalf("%s collect: want exactly one zero-amount instruction to the token contract, the receive made %+v", contract, res.Receive.DescendantBlocks)
	}
	mint := new(definition.MintParam)
	if err := definition.ABIToken.UnpackMethod(mint, definition.MintMethodName, mints[0].Data); err != nil {
		t.Fatalf("%s collect: the instruction to the token contract is not a mint: %v", contract, err)
	}
	if mint.TokenStandard != zts || mint.ReceiveAddress != kp.Address || mint.Amount == nil || mint.Amount.Cmp(before.credit) != 0 {
		t.Fatalf("%s collect: mint %s of %s to %s; want %s of %s to %s, the credit", contract, mint.Amount, mint.TokenStandard, mint.ReceiveAddress, before.credit, zts, kp.Address)
	}
	// The token contract's answer to the mint, the same on every endpoint.
	minted, err := net.WaitAnswer(mints[0], wait)
	if err != nil {
		t.Fatalf("%s collect: the token contract's receive of the mint: %v", contract, err)
	}
	if !minted.OK() || len(minted.Receive.DescendantBlocks) != 1 {
		t.Fatalf("%s collect: the token contract answered the mint with status %d and %d blocks: %+v", contract, minted.Status, len(minted.Receive.DescendantBlocks), minted.Receive.DescendantBlocks)
	}
	pay := minted.Receive.DescendantBlocks[0]
	if pay.Address != types.TokenContract || pay.ToAddress != kp.Address || pay.TokenStandard != zts || pay.Amount == nil || pay.Amount.Cmp(before.credit) != 0 || pay.BlockType != nom.BlockTypeContractSend {
		t.Fatalf("%s collect: the token contract sent %s of %s to %s; want %s of %s to %s", contract, pay.Amount, pay.TokenStandard, pay.ToAddress, before.credit, zts, kp.Address)
	}
	// The receipt of the mint, the same on every endpoint: succeeded, the
	// same payment field by field, and confirmed in one and the same momentum
	// (hash and height) that lists it. An endpoint that does not have the
	// receipt yet gets a while.
	confirmedIn := minted.Receive.ConfirmationDetail // as WaitAnswer read it on the first endpoint
	receiptOK := func(r *api.AccountBlock) bool {
		return r.BlockType == nom.BlockTypeContractReceive && r.Address == types.TokenContract && r.Hash == minted.Receive.Hash &&
			r.Height == minted.Receive.Height && r.MomentumAcknowledged == minted.Receive.MomentumAcknowledged && r.PreviousHash == minted.Receive.PreviousHash &&
			len(r.Data) == 8 && binary.BigEndian.Uint64(r.Data) == StatusSuccess && r.FromBlockHash == mints[0].Hash &&
			len(r.DescendantBlocks) == 1 && net.SameBlock(r.DescendantBlocks[0], pay)
	}
	for i := range net.URLs {
		var r *api.AccountBlock
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		for {
			var err error
			r, err = net.BlockCtx(ctx, i, minted.Receive.Hash)
			if err == nil && r != nil && !receiptOK(r) {
				cancel()
				t.Fatalf("%s: the token contract's receive %s of the mint reads differently: %+v", net.URLs[i], minted.Receive.Hash, r)
			}
			if err == nil && r != nil && r.ConfirmationDetail != nil {
				break
			}
			if err != nil || ctx.Err() != nil {
				cancel()
				t.Fatalf("%s: no confirmed receipt %s of the mint within a minute (error %v)", net.URLs[i], minted.Receive.Hash, err)
			}
			time.Sleep(time.Second)
		}
		cancel()
		if !receiptOK(r) {
			t.Fatalf("%s: the token contract's receive %s of the mint reads differently: %+v", net.URLs[i], minted.Receive.Hash, r)
		}
		if err := net.ConfirmedOn(i, r); err != nil {
			t.Fatalf("%s: %v", net.URLs[i], err)
		}
		if confirmedIn == nil || r.ConfirmationDetail.MomentumHash != confirmedIn.MomentumHash || r.ConfirmationDetail.MomentumHeight != confirmedIn.MomentumHeight {
			t.Fatalf("%s confirms the mint's receipt in momentum %s at height %d, %s in %s at height %d", net.URLs[0], confirmedIn.MomentumHash, confirmedIn.MomentumHeight,
				net.URLs[i], r.ConfirmationDetail.MomentumHash, r.ConfirmationDetail.MomentumHeight)
		}
	}
	receiveAll(t, kp, 1)
	if !net.ReceivedSend(pay.Hash) {
		t.Fatalf("%s collect: the account did not receive the token contract's send %s", contract, pay.Hash)
	}
	paid = new(big.Int).Sub(bal(t, kp.Address, zts), balance)
	after := readBook(t, contract, kp.Address, field, before.processed)
	if after.processed < before.processed {
		t.Fatalf("%s: %d epochs were processed before the collect and %d after", contract, before.processed, after.processed)
	}
	for e := int64(0); e < before.processed; e++ {
		if before.perEpoch[e].Cmp(after.perEpoch[e]) != 0 || before.perOther[e].Cmp(after.perOther[e]) != 0 {
			t.Errorf("%s: the reward history of epoch %d changed across the collect, from %v and %v to %v and %v", contract, e, before.perEpoch[e], before.perOther[e], after.perEpoch[e], after.perOther[e])
		}
	}
	for e := before.processed; e < after.processed; e++ {
		if why := epochOK(e, after.perEpoch[e]); why != "" {
			t.Errorf("%s: epoch %d, processed around the collect: %s", contract, e, why)
		}
		if after.perOther[e].Sign() != 0 {
			t.Errorf("%s: epoch %d, processed around the collect, credits %s of the other token", contract, e, after.perOther[e])
		}
	}
	if paid.Cmp(before.credit) != 0 || after.other.Sign() != 0 {
		t.Errorf("%s collect: the balance rose by %s and %s of the other token is uncollected; the credit was %s", contract, paid, after.other, before.credit)
	}
	// The collect was made with every finished epoch processed and two
	// minutes of the running one left (quietBook). If an epoch was processed
	// around it all the same, the public reads cannot tell which receive
	// processed it, before the collect or after: the outcome is not judged,
	// and the case fails as inconclusive. Run it again.
	if after.processed != before.processed {
		t.Fatalf("%s: %d epoch(s) were processed around the collect; which credit the collect paid cannot be told from the public reads, so this run is inconclusive", contract, after.processed-before.processed)
	}
	if after.credit.Sign() != 0 {
		t.Errorf("%s collect: %s left uncollected, and no epoch was processed around the collect", contract, after.credit)
	}
	return paid
}

// epochNow is the index of the running epoch, from the chain's own clock:
// the frontier momentum's time, the genesis momentum's time, and the epoch
// length the driver was built with. It is also the number of finished epochs.
func epochNow(t *testing.T) int64 {
	t.Helper()
	var first struct {
		List []struct {
			Height    uint64 `json:"height"`
			Timestamp int64  `json:"timestamp"`
		} `json:"list"`
	}
	get(t, &first, "ledger.getMomentumsByHeight", 1, 1)
	if len(first.List) != 1 || first.List[0].Height != 1 || first.List[0].Timestamp == 0 {
		t.Fatalf("genesis momentum: %+v", first)
	}
	return (frontierTime(t) - first.List[0].Timestamp) / (int64(constants.MomentumsPerEpoch) * 10)
}

func listOf(v obj) []interface{} {
	list, _ := v["list"].([]interface{})
	return list
}

func TestFastPlasma(t *testing.T) {
	fastGroup(t)
	a := newAccount(t, 110, 0, 100, 1000)
	plasma := types.PlasmaContract
	var id types.Hash

	t.Run("FP-1_cancel_after_expiry_returns_the_qsr", func(t *testing.T) {
		cover(t, "E1", "contract-method", "plasma.CancelFuse")
		cover(t, "E12", "contract-method", "plasma.CancelFuse")
		before := bal(t, a.Address, qsr)
		id = callOK(t, a, plasma, definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, a.Address), qsr, coins(50)).Send.Hash
		cancel := definition.ABIPlasma.PackMethodPanic(definition.CancelFuseMethodName, id)
		callFail(t, a, plasma, cancel, znn, units(0)) // still inside the lock
		eventually(t, 6*time.Minute, "the fusion becomes revocable", func() bool {
			var entries obj
			get(t, &entries, "embedded.plasma.getEntriesByAddress", a.Address, 0, 10)
			list := listOf(entries)
			return len(list) == 1 && str(list[0].(obj)["isRevocable"]) == "true"
		})
		callOK(t, a, plasma, cancel, znn, units(0))
		receiveAll(t, a, 1)
		wantBig(t, "QSR after the cancel", bal(t, a.Address, qsr), before)
		var entries obj
		get(t, &entries, "embedded.plasma.getEntriesByAddress", a.Address, 0, 10)
		if str(entries["count"]) != "0" {
			t.Errorf("fusion entries after the cancel: %v", entries)
		}
	})

	t.Run("FP-2_second_cancel_fails", func(t *testing.T) {
		cover(t, "E4", "contract-method", "plasma.CancelFuse")
		callFail(t, a, plasma, definition.ABIPlasma.PackMethodPanic(definition.CancelFuseMethodName, id), znn, units(0))
	})
}

func TestFastStake(t *testing.T) {
	fastGroup(t)
	a := newAccount(t, 112, 200, 0, 1500)
	idle := newAccount(t, 113, 0, 0, 500)
	stake := types.StakeContract
	collect := definition.ABIStake.PackMethodPanic(definition.CollectRewardMethodName)
	stakedIn := int64(-1) // the epoch in which the stakes were made
	// The two stakes as the contract holds them: start and revoke times are
	// the timestamps of the momentums their receives acknowledge (the
	// contract stamps its frontier momentum's time), weights from the
	// contract's own entries. From DynamicPlasmaSpork a second cancel is
	// refused and leaves the revoke time unchanged.
	var short, long stakeRecord

	t.Run("FS-1_cancel_after_expiry_returns_the_znn", func(t *testing.T) {
		cover(t, "E1", "contract-method", "stake.Cancel")
		cover(t, "E12", "contract-method", "stake.Cancel")
		before := bal(t, a.Address, znn)
		shortRes := callOK(t, a, stake, definition.ABIStake.PackMethodPanic(definition.StakeMethodName, constants.StakeTimeMinSec), znn, coins(10))
		id := shortRes.Send.Hash
		short.start = ackTime(t, shortRes)
		// A second, long stake keeps earning for FS-2.
		longRes := callOK(t, a, stake, definition.ABIStake.PackMethodPanic(definition.StakeMethodName, constants.StakeTimeMaxSec), znn, coins(100))
		long.start = ackTime(t, longRes)
		// The epoch the stakes count from is the one the contract executed
		// in: that of the momentum the long stake's receive acknowledges.
		stakedIn = (long.start - genesisTime(t)) / (int64(constants.MomentumsPerEpoch) * 10)
		// What each stake weighs, worked out from what was signed, not read
		// from the node: amount times (9 + duration in stake time units) / 10
		// (getWeightedStakeAmount).
		short.weight = weighted(coins(10), constants.StakeTimeMinSec)
		long.weight = weighted(coins(100), constants.StakeTimeMaxSec)
		var entries obj
		get(t, &entries, "embedded.stake.getEntriesByAddress", a.Address, 0, 10)
		var expiry int64
		// Each signed stake must be listed exactly once, and nothing else:
		// the weights checked below are those of entries actually returned.
		seen := map[string]int{}
		for _, e := range listOf(entries) {
			seen[str(e.(obj)["id"])]++
		}
		if len(listOf(entries)) != 2 || seen[id.String()] != 1 || seen[longRes.Send.Hash.String()] != 1 || str(entries["count"]) != "2" {
			t.Fatalf("stake entries: want the short stake %s and the long stake %s once each and nothing else, got %v", id, longRes.Send.Hash, entries)
		}
		for _, e := range listOf(entries) {
			switch str(e.(obj)["id"]) {
			case id.String():
				fmt.Sscan(str(e.(obj)["expirationTimestamp"]), &expiry)
				if w := bigOf(t, e.(obj)["weightedAmount"]); w.Cmp(short.weight) != 0 {
					t.Errorf("the short stake weighs %s; staked for its duration it must weigh %s", w, short.weight)
				}
			case longRes.Send.Hash.String():
				if w := bigOf(t, e.(obj)["weightedAmount"]); w.Cmp(long.weight) != 0 {
					t.Errorf("the long stake weighs %s; staked for its duration it must weigh %s", w, long.weight)
				}
			}
		}
		if short.weight == nil || long.weight == nil || short.weight.Sign() <= 0 || long.weight.Sign() <= 0 {
			t.Fatalf("stake entries without a positive weighted amount: %v", entries)
		}
		if expiry == 0 {
			t.Fatalf("stake entry not found: %v", entries)
		}
		cancel := definition.ABIStake.PackMethodPanic(definition.CancelStakeMethodName, id)
		// A cancelled entry stays, with amount zero, until the reward update
		// of the epoch it was cancelled in, which runs after that epoch
		// ends. Both cancels must therefore execute in one epoch: the first
		// is made with three minutes of it left, and an epoch that ends
		// between them makes the run inconclusive, not a failure.
		epochLen := int64(constants.MomentumsPerEpoch) * 10
		genesis := genesisTime(t)
		epochOf := func(at int64) int64 { return (at - genesis) / epochLen }
		eventually(t, 5*time.Minute+2*time.Duration(epochLen)*time.Second, "the stake expires with three minutes of the running epoch left", func() bool {
			at := frontierTime(t)
			return at >= expiry && epochLen-(at-genesis)%epochLen >= 180
		})
		short.revoke = ackTime(t, callOK(t, a, stake, cancel, znn, units(0)))
		firstIn := epochOf(short.revoke)
		receiveAll(t, a, 1)
		wantBig(t, "ZNN after the cancel", bal(t, a.Address, znn), new(big.Int).Sub(before, coins(100)))
		if now := epochOf(frontierTime(t)); now != firstIn {
			t.Fatalf("epoch %d ended before the second cancel (the first executed in it, now %d): its reward update may have removed the entry, so this run is inconclusive; run the group again", firstIn, now)
		}
		// Within the epoch, once DynamicPlasmaSpork is active, a second cancel
		// is refused with ErrStakeAlreadyRevoked, emits no descendant block,
		// and leaves short.revoke at the first cancel's timestamp.
		after := bal(t, a.Address, znn)
		// The epoch the second cancel executed in is read before its outcome
		// is judged: a refusal after the epoch ended could come after the
		// reward update removed the entry, making the run inconclusive.
		res, err := call(a, stake, cancel, znn, units(0))
		if err == nil && res != nil && res.Receive != nil {
			if in := epochOf(ackTime(t, res)); in != firstIn {
				t.Fatalf("the second cancel executed in epoch %d, the first in %d (status %d): this run is inconclusive; run the group again", in, firstIn, res.Status)
			}
		}
		mustFail(t, res, err)
		wantBig(t, "ZNN after a second cancel", bal(t, a.Address, znn), after)
		cover(t, "E4", "contract-method", "stake.Cancel")
	})

	t.Run("FS-2_collect_the_reward_of_a_finished_epoch", func(t *testing.T) {
		cover(t, "E1", "contract-method", "stake.CollectReward")
		if stakedIn < 0 {
			t.Fatalf("FS-1 did not record the epoch of the stake; run the group as a whole on a fresh chain")
		}
		// Wait for the contract to process the epoch the stakes were made in:
		// a credit from an earlier epoch (the short stake can open one epoch
		// before the long) is not yet the epoch this case judges.
		eventually(t, 25*time.Minute, "the stake contract processes the epoch the stakes were made in", func() bool {
			b := readRewardBook(t, "stake", a.Address, "qsrAmount")
			return b.processed > stakedIn && b.credit.Sign() > 0
		})
		// One reading of the books, checked here and then carried into the
		// collect: the count of processed epochs, held against the clock by
		// readRewardBook, the credit and the history per epoch.
		book := quietBook(t, "stake", stake, a.Address, "qsrAmount")
		amount := book.credit
		// Independent of the node's own accrual: this account holds the only
		// stakes on a fresh chain (P11).
		if stakedIn < 0 {
			t.Fatalf("FS-1 did not record the epoch of the stake; run the group as a whole on a fresh chain")
		}
		// What each processed epoch must credit, to the unit, worked out as
		// the contract does (computeStakeRewardsForEpoch, getWeightedStake):
		// each stake weighs its weighted amount times the seconds it was
		// active within the epoch, its share is the epoch's pool times its
		// weight over the total, rounded down, and this account, the only
		// staker on a fresh chain, gets the sum of its two shares.
		if short.start == 0 || long.start == 0 || short.revoke == 0 {
			t.Fatalf("FS-1 did not record both stakes and the cancel; run the group as a whole on a fresh chain")
		}
		genesis := genesisTime(t)
		length := int64(constants.MomentumsPerEpoch) * 10
		expected := func(e int64) *big.Int {
			from, to := genesis+e*length, genesis+(e+1)*length
			shares := []*big.Int{short.weightIn(from, to), long.weightIn(from, to)}
			total := new(big.Int).Add(shares[0], shares[1])
			sum := new(big.Int)
			if total.Sign() == 0 {
				return sum
			}
			pool := constants.StakeQsrRewardPerEpoch(uint64(e))
			for _, w := range shares {
				sum.Add(sum, new(big.Int).Quo(new(big.Int).Mul(pool, w), total))
			}
			return sum
		}
		epochOK := func(e int64, v *big.Int) string {
			if want := expected(e); v.Cmp(want) != 0 {
				return fmt.Sprintf("pays %s QSR; from the two stakes' weights and times the contract owes %s", v, want)
			}
			return ""
		}
		if book.processed <= stakedIn {
			t.Fatalf("the stake contract has processed %d epoch(s) and the stake was made in epoch %d", book.processed, stakedIn)
		}
		for e := int64(0); e < book.processed; e++ {
			v := book.perEpoch[e]
			if v == nil {
				v = new(big.Int)
			}
			if why := epochOK(e, v); why != "" {
				t.Errorf("reward history, epoch %d: %s", e, why)
			}
		}
		// The credit is exactly what the processed epochs owe.
		owed := new(big.Int)
		for e := int64(0); e < book.processed; e++ {
			owed.Add(owed, expected(e))
		}
		if amount.Cmp(owed) != 0 {
			t.Fatalf("stake reward %s; over the %d processed epoch(s) the two stakes are owed %s", amount, book.processed, owed)
		}
		paid := collectExactly(t, a, "stake", stake, collect, qsr, "qsrAmount", book, epochOK)
		t.Logf("collected %s QSR units", paid)
		t.Logf("stake reward: %s QSR units for %d settled epoch(s), pool %s per epoch", amount, book.processed-stakedIn, constants.StakeQsrRewardPerEpoch(0))
	})

	t.Run("FS-3_collect_with_nothing_to_collect_fails", func(t *testing.T) {
		cover(t, "E4", "contract-method", "stake.CollectReward")
		callFail(t, idle, stake, collect, znn, units(0))
	})
}

func TestFastPillar(t *testing.T) {
	fastGroup(t)
	delegator := newAccount(t, 114, 1000, 0, 1000)
	pillar := types.PillarContract
	pack := func(name string, args ...interface{}) []byte {
		return definition.ABIPillars.PackMethodPanic(name, args...)
	}
	collect := pack(definition.CollectRewardMethodName)

	// Delegate first: the reward of an epoch goes to those delegating in it.
	callOK(t, delegator, pillar, pack(definition.DelegateMethodName, "dev1"), znn, units(0))

	var cost string
	if err := net.Call(&cost, "embedded.pillar.getQsrRegistrationCost"); err != nil {
		t.Fatal(err)
	}
	costQsr, _ := new(big.Int).SetString(cost, 10)
	costCoins := new(big.Int).Div(costQsr, coins(1)).Int64()
	owner := fundFrom(t, net.Key(8), 115, 15010, costCoins+10)
	name := "e2e-fast-" + runTag
	producer := net.Key(runBase + 116).Address

	t.Run("FPI-1_register_with_the_qsr_deposit", func(t *testing.T) {
		cover(t, "E1", "contract-method", "pillars.Register", "pillars.DepositQsr")
		callOK(t, owner, pillar, pack(definition.DepositQsrMethodName), qsr, costQsr)
		before := bal(t, owner.Address, znn)
		callOK(t, owner, pillar, pack(definition.RegisterMethodName, name, producer, owner.Address, uint8(0), uint8(100)), znn, constants.PillarStakeAmount)
		wantBig(t, "ZNN staked", bal(t, owner.Address, znn), new(big.Int).Sub(before, constants.PillarStakeAmount))
		var info, all obj
		get(t, &info, "embedded.pillar.getByName", name)
		get(t, &all, "embedded.pillar.getAll", 0, 10)
		if str(info["ownerAddress"]) != owner.Address.String() || str(info["producerAddress"]) != producer.String() || str(all["count"]) != "4" {
			t.Errorf("pillar read back: %v, count %v", info, all["count"])
		}
		var deposited string
		get(t, &deposited, "embedded.pillar.getDepositedQsr", owner.Address)
		if deposited != "0" {
			t.Errorf("deposited QSR after registration: %s, want 0 (it is burned)", deposited)
		}
		var free bool
		get(t, &free, "embedded.pillar.checkNameAvailability", name)
		if free {
			t.Errorf("name of a registered pillar reported as available")
		}
	})

	t.Run("FPI-3_update_by_the_owner", func(t *testing.T) {
		cover(t, "E1", "contract-method", "pillars.UpdatePillar")
		callOK(t, owner, pillar, pack(definition.UpdatePillarMethodName, name, producer, owner.Address, uint8(10), uint8(90)), znn, units(0))
		var info obj
		get(t, &info, "embedded.pillar.getByName", name)
		if str(info["giveMomentumRewardPercentage"]) != "10" || str(info["giveDelegateRewardPercentage"]) != "90" ||
			str(info["producerAddress"]) != producer.String() || str(info["withdrawAddress"]) != owner.Address.String() {
			t.Errorf("pillar after the update: %v, want give percentages 10 and 90 and unchanged addresses", info)
		}
		// A stranger's update must change none of it.
		callFail(t, delegator, pillar, pack(definition.UpdatePillarMethodName, name, producer, delegator.Address, uint8(50), uint8(50)), znn, units(0))
		var after obj
		get(t, &after, "embedded.pillar.getByName", name)
		if str(after["giveMomentumRewardPercentage"]) != "10" || str(after["giveDelegateRewardPercentage"]) != "90" || str(after["withdrawAddress"]) != owner.Address.String() {
			t.Errorf("pillar after a stranger's update: %v", after)
		}
		cover(t, "E3", "contract-method", "pillars.UpdatePillar")
	})

	// FPI-7 and FPI-8 pin the electorate of an accelerator tally (#274):
	// with the dynamic plasma spork enforced, which the test build's genesis
	// does from height 0, only the votes of pillars active at the update
	// count, and the turnout is measured against those pillars. The pillar
	// of this group casts the only vote on a project of its own. One vote of
	// four active pillars is short of the turnout (100 <= 4 * 33). Once the
	// pillar is revoked, three are active; counted, its stored vote would
	// clear the turnout (100 > 3 * 33) and accept the project, which is
	// what a node that still counts every stored vote does.
	az := types.AcceleratorContract
	packAZ := func(name string, args ...interface{}) []byte {
		return definition.ABIAccelerator.PackMethodPanic(name, args...)
	}
	activePillars := func(t *testing.T) int {
		t.Helper()
		var all obj
		get(t, &all, "embedded.pillar.getAll", 0, 10)
		active := 0
		for _, p := range listOf(all) {
			if str(p.(obj)["revokeTimestamp"]) == "0" {
				active++
			}
		}
		return active
	}
	breakdownIsOneYes := func(t *testing.T, id types.Hash, when string) {
		t.Helper()
		var votes obj
		get(t, &votes, "embedded.accelerator.getVoteBreakdown", id)
		if str(votes["yes"]) != "1" || str(votes["no"]) != "0" || str(votes["total"]) != "1" {
			t.Fatalf("stored votes on the project %s: %v, want the one yes of %s", when, votes, name)
		}
	}
	var electorateProject types.Hash
	// revokedAt is the revoke's time as the contract stamps it: the
	// timestamp of the momentum its receive acknowledges. getByName does
	// not return a revoked pillar, so FPI-8 cannot read it there.
	var revokedAt int64

	t.Run("FPI-7_one_vote_of_four_active_pillars_does_not_accept_a_project", func(t *testing.T) {
		cover(t, "E1", "contract-method", "accelerator.VoteByName")
		var info obj
		get(t, &info, "embedded.pillar.getByName", name)
		if str(info["revokeTimestamp"]) != "0" {
			t.Fatalf("the pillar %s is revoked before its vote: %v", name, info)
		}
		if n := activePillars(t); n != 4 {
			t.Fatalf("active pillars before the vote: %d, want 4", n)
		}
		project := callOK(t, delegator, az, packAZ(definition.CreateProjectMethodName, "e2e electorate "+runTag, "voted on by one pillar", "e2e.test", coins(10), coins(100)), znn, constants.ProjectCreationAmount).Send.Hash
		callOK(t, owner, az, packAZ(definition.VoteByNameMethodName, project, name, definition.VoteYes), znn, units(0))
		breakdownIsOneYes(t, project, "after the vote")
		at := updateExecutedAbove(t, delegator, frontierHeight(t))
		var got obj
		get(t, &got, "embedded.accelerator.getProjectById", project)
		if str(got["status"]) != fmt.Sprint(definition.VotingStatus) {
			t.Fatalf("project after the update at momentum %d, with one vote of four active pillars: status %v, want %d (voting)", at, got["status"], definition.VotingStatus)
		}
		electorateProject = project
	})

	t.Run("FPI-4_revoke_inside_the_lock_fails_then_succeeds_in_the_window", func(t *testing.T) {
		cover(t, "E1", "contract-method", "pillars.Revoke")
		cover(t, "E12", "contract-method", "pillars.Revoke")
		revoke := pack(definition.RevokeMethodName, name)
		var info obj
		get(t, &info, "embedded.pillar.getByName", name)
		// The lock (PillarEpochLockTime) and the revoke window repeat for as
		// long as the pillar exists. The refusal is tried only when at least a
		// minute of lock is left, so that the call is executed well inside it
		// whatever the earlier cases took; that state always comes round.
		var left int64
		eventually(t, 6*time.Minute, "the pillar is locked with at least a minute left", func() bool {
			get(t, &info, "embedded.pillar.getByName", name)
			fmt.Sscan(str(info["revokeCooldown"]), &left)
			return str(info["isRevocable"]) == "false" && left >= 60
		})
		t.Logf("revoke attempted with %d s of lock left", left)
		callFail(t, owner, pillar, revoke, znn, units(0))
		get(t, &info, "embedded.pillar.getByName", name)
		if str(info["revokeTimestamp"]) != "0" {
			t.Fatalf("a revoke refused inside the lock left a revoke time: %v", info)
		}
		eventually(t, 6*time.Minute, "the pillar enters its revoke window", func() bool {
			get(t, &info, "embedded.pillar.getByName", name)
			return str(info["isRevocable"]) == "true"
		})
		before := bal(t, owner.Address, znn)
		revokedAt = ackTime(t, callOK(t, owner, pillar, revoke, znn, units(0)))
		receiveAll(t, owner, 1)
		wantBig(t, "ZNN after the revoke", bal(t, owner.Address, znn), new(big.Int).Add(before, constants.PillarStakeAmount))
		var all obj
		get(t, &all, "embedded.pillar.getAll", 0, 10)
		active := 0
		for _, p := range listOf(all) {
			if str(p.(obj)["revokeTimestamp"]) == "0" {
				active++
			}
		}
		if active != 3 {
			t.Errorf("active pillars after the revoke: %d, want 3 (%v)", active, all)
		}
		callFail(t, owner, pillar, revoke, znn, units(0)) // already revoked
	})

	t.Run("FPI-8_a_revoked_pillars_vote_no_longer_counts", func(t *testing.T) {
		if electorateProject == (types.Hash{}) {
			t.Fatal("FPI-7 did not leave a project with the pillar's vote on it")
		}
		var info obj
		get(t, &info, "embedded.pillar.getByName", name)
		if str(info["revokeTimestamp"]) == "0" {
			t.Fatalf("the pillar %s is not revoked: %v", name, info)
		}
		if n := activePillars(t); n != 3 {
			t.Fatalf("active pillars after the revoke: %d, want 3", n)
		}
		at := updateExecutedAbove(t, delegator, frontierHeight(t))
		var got obj
		get(t, &got, "embedded.accelerator.getProjectById", electorateProject)
		// The case says something only if every tally after the voting
		// window ran with the pillar already revoked. FPI-4 can wait up to
		// twelve minutes before it revokes; if the window ended first, the
		// final tally counted one vote of four active pillars and closed the
		// project, as a node that counts every stored vote would too.
		var project struct {
			CreationTimestamp int64 `json:"creationTimestamp"`
		}
		get(t, &project, "embedded.accelerator.getProjectById", electorateProject)
		created, revoked := project.CreationTimestamp, revokedAt
		if created == 0 || revoked == 0 {
			t.Fatalf("project created at %d, pillar revoked at %d: cannot tell whether the revoke came before the window ended", created, revoked)
		}
		if end := created + constants.AcceleratorProjectVotingPeriod; revoked > end {
			t.Fatalf("the pillar was revoked at %d, %d s after the project's voting window ended at %d: the final tally may have run before the revoke, so this run is inconclusive; run the group again", revoked, revoked-end, end)
		}
		// Voting while the window is open; closed if the window ended first
		// and the final tally ran. Accepted only on the revoked vote.
		switch str(got["status"]) {
		case fmt.Sprint(definition.VotingStatus), fmt.Sprint(definition.ClosedStatus):
			t.Logf("project status %v after the update at momentum %d (window ends at %d, revoked at %d)", got["status"], at, created+constants.AcceleratorProjectVotingPeriod, revoked)
		case fmt.Sprint(definition.ActiveStatus):
			t.Fatalf("project accepted at or before the update at momentum %d on the stored vote of the revoked pillar %s alone; post-spork only the votes of active pillars count", at, name)
		default:
			t.Fatalf("project status after the update at momentum %d: %v", at, got["status"])
		}
		// The stored record keeps the vote: a revoke does not rewrite it.
		breakdownIsOneYes(t, electorateProject, "after the revoke")
	})

	t.Run("FPI-5_delegator_collects_the_reward_of_a_finished_epoch", func(t *testing.T) {
		cover(t, "E1", "contract-method", "pillars.CollectReward")
		var reward obj
		eventually(t, 25*time.Minute, "a delegation reward is credited after an epoch", func() bool {
			get(t, &reward, "embedded.pillar.getUncollectedReward", delegator.Address)
			return bigOf(t, reward["znnAmount"]).Sign() > 0
		})
		// An upper bound that does not come from the node: nobody can be paid
		// more than the delegation pool of an epoch for that epoch. The exact
		// share depends on what each pillar produced, which this case does not
		// model.
		perMomentum, _ := constants.PillarRewardPerMomentum(0)
		pool := new(big.Int).Mul(perMomentum, units(constants.MomentumsPerEpoch))
		epochOK := func(e int64, v *big.Int) string {
			if v.Cmp(pool) > 0 {
				return fmt.Sprintf("%s is more than the delegation pool of an epoch, %s", v, pool)
			}
			return ""
		}
		// One reading of the books, checked here and carried into the collect.
		// collectExactly requires the credit to be backed by the history, so
		// a credit with no epoch behind it fails.
		book := quietBook(t, "pillar", pillar, delegator.Address, "znnAmount")
		for e, v := range book.perEpoch {
			if why := epochOK(e, v); why != "" {
				t.Errorf("delegation reward history, epoch %d: %s", e, why)
			}
		}
		amount := book.credit
		paid := collectExactly(t, delegator, "pillar", pillar, collect, znn, "znnAmount", book, epochOK)
		t.Logf("collected %s ZNN units", paid)
		t.Logf("delegation reward: %s ZNN units; the delegation pool is %s per epoch", amount, pool)
	})

	t.Run("FPI-6_epoch_history_is_recorded", func(t *testing.T) {
		var history, byEpoch obj
		get(t, &history, "embedded.pillar.getPillarEpochHistory", "dev1", 0, 10)
		get(t, &byEpoch, "embedded.pillar.getPillarsHistoryByEpoch", 0, 0, 10)
		if count(t, history, "count") < 1 || count(t, byEpoch, "count") < 3 {
			t.Errorf("epoch history after a finished epoch: dev1 has %v entries, epoch 0 lists %v pillars, want at least 1 and 3", history["count"], byEpoch["count"])
		}
	})
}

func TestFastSentinel(t *testing.T) {
	fastGroup(t)
	a := fundFrom(t, net.Key(8), 118, 5010, 50100)
	sentinel := types.SentinelContract
	pack := func(name string) []byte { return definition.ABISentinel.PackMethodPanic(name) }

	t.Run("FSE-1_deposit_then_withdraw_qsr", func(t *testing.T) {
		cover(t, "E1", "contract-method", "sentinel.WithdrawQsr", "sentinel.DepositQsr")
		before := bal(t, a.Address, qsr)
		callOK(t, a, sentinel, pack(definition.DepositQsrMethodName), qsr, coins(10))
		callOK(t, a, sentinel, pack(definition.WithdrawQsrMethodName), znn, units(0))
		receiveAll(t, a, 1)
		wantBig(t, "QSR after the withdraw", bal(t, a.Address, qsr), before)
	})

	t.Run("FSE-2_revoke_in_the_window_returns_the_stake", func(t *testing.T) {
		cover(t, "E1", "contract-method", "sentinel.Revoke")
		cover(t, "E12", "contract-method", "sentinel.Revoke")
		znnBefore, qsrBefore := bal(t, a.Address, znn), bal(t, a.Address, qsr)
		callOK(t, a, sentinel, pack(definition.DepositQsrMethodName), qsr, constants.SentinelQsrDepositAmount)
		callOK(t, a, sentinel, pack(definition.RegisterSentinelMethodName), znn, constants.SentinelZnnRegisterAmount)
		var info obj
		eventually(t, 6*time.Minute, "the sentinel enters its revoke window", func() bool {
			get(t, &info, "embedded.sentinel.getByOwner", a.Address)
			return str(info["isRevocable"]) == "true"
		})
		res := callOK(t, a, sentinel, pack(definition.RevokeSentinelMethodName), znn, units(0))
		receiveAll(t, a, len(res.Receive.DescendantBlocks))
		wantBig(t, "ZNN after the revoke", bal(t, a.Address, znn), znnBefore)
		wantBig(t, "QSR after the revoke", bal(t, a.Address, qsr), qsrBefore)
		get(t, &info, "embedded.sentinel.getByOwner", a.Address)
		if str(info["active"]) == "true" {
			t.Errorf("sentinel still active after the revoke: %v", info)
		}
		callFail(t, a, sentinel, pack(definition.RevokeSentinelMethodName), znn, units(0)) // already revoked
	})
}

func TestFastAccelerator(t *testing.T) {
	fastGroup(t)
	a := newAccount(t, 120, 10, 10, 2000)
	az := types.AcceleratorContract
	pack := func(name string, args ...interface{}) []byte {
		return definition.ABIAccelerator.PackMethodPanic(name, args...)
	}
	owners := map[string]uint32{"dev1": IdxOwner1, "dev2": IdxOwner2, "dev3": IdxOwner3}
	voteAll := func(t *testing.T, id types.Hash) {
		t.Helper()
		for name, idx := range owners {
			callOK(t, net.Key(idx), az, pack(definition.VoteByNameMethodName, id, name, definition.VoteYes), znn, units(0))
		}
	}
	// The contract acts on votes when it is updated. Pillars send the update
	// by themselves; the driver also sends one, and a refusal for being too
	// soon after the last update is expected.
	// update reports whether the driver's own Update call was executed. The
	// contract refusing it (status failed: too soon) is expected; any other
	// error, a refusal at publish or a receipt the endpoints disagree on,
	// fails the case.
	update := func(t *testing.T) bool {
		t.Helper()
		ok, _ := acceleratorUpdate(t, a)
		return ok
	}
	var project types.Hash

	t.Run("FAZ-1_project_accepted_by_the_pillars", func(t *testing.T) {
		project = callOK(t, a, az, pack(definition.CreateProjectMethodName, "e2e fast "+runTag, "funded by the driver", "e2e.test", coins(10), coins(100)), znn, constants.ProjectCreationAmount).Send.Hash
		voteAll(t, project)
		var got obj
		updated := false
		eventually(t, 6*time.Minute, "the project is accepted", func() bool {
			updated = update(t) || updated
			get(t, &got, "embedded.accelerator.getProjectById", project)
			return str(got["status"]) == fmt.Sprint(definition.ActiveStatus)
		})
		if updated {
			cover(t, "E1", "contract-method", "accelerator.Update")
		}
	})

	t.Run("FAZ-2_phase_added_updated_voted_and_paid", func(t *testing.T) {
		cover(t, "E1", "contract-method", "accelerator.AddPhase", "accelerator.UpdatePhase")
		cover(t, "E3", "contract-method", "accelerator.AddPhase")
		// The project is accepted and has no phase, so the owner may add one
		// (just below); the same phase from someone else may not.
		addPhase := pack(definition.AddPhaseMethodName, project, "phase one", "first", "e2e.test", coins(5), coins(50))
		callFail(t, net.Key(IdxOwner1), az, addPhase, znn, units(0)) // not the project owner
		var refusedFor obj
		get(t, &refusedFor, "embedded.accelerator.getProjectById", project)
		if ids, _ := refusedFor["phaseIds"].([]interface{}); len(ids) != 0 || len(listOfKey(refusedFor, "phases")) != 0 || str(refusedFor["status"]) != fmt.Sprint(definition.ActiveStatus) {
			t.Fatalf("after a stranger's refused AddPhase the project must be accepted with no phase: %v", refusedFor)
		}
		znnBefore, qsrBefore := bal(t, a.Address, znn), bal(t, a.Address, qsr)
		phase := callOK(t, a, az, addPhase, znn, units(0)).Send.Hash
		// One pillar votes on the phase as first proposed.
		callOK(t, net.Key(IdxOwner1), az, pack(definition.VoteByNameMethodName, phase, "dev1", definition.VoteYes), znn, units(0))
		var votes obj
		get(t, &votes, "embedded.accelerator.getVoteBreakdown", phase)
		if str(votes["yes"]) != "1" || str(votes["total"]) != "1" {
			t.Fatalf("votes on the phase before it is revised: %v", votes)
		}
		// Revising a phase replaces it: a new id, no votes carried over, and
		// the revised amounts are what gets paid.
		revised := callOK(t, a, az, pack(definition.UpdatePhaseMethodName, project, "phase one", "first, revised", "e2e.test", coins(6), coins(60)), znn, units(0)).Send.Hash
		var got obj
		get(t, &got, "embedded.accelerator.getProjectById", project)
		phases, _ := got["phases"].([]interface{})
		if len(phases) != 1 {
			t.Fatalf("phases of the project: %v", got["phases"])
		}
		current := types.HexToHashPanic(str(phases[0].(obj)["phase"].(obj)["id"]))
		if current == phase || current != revised {
			t.Fatalf("phase id after the revision is %s; want the id of the revising block %s, not the original %s", current, revised, phase)
		}
		get(t, &votes, "embedded.accelerator.getVoteBreakdown", current)
		if str(votes["total"]) != "0" {
			t.Fatalf("the revised phase starts with votes: %v", votes)
		}
		var p0 obj
		get(t, &p0, "embedded.accelerator.getPhaseById", current)
		if ph := p0["phase"].(obj); str(ph["znnFundsNeeded"]) != coins(6).String() || str(ph["qsrFundsNeeded"]) != coins(60).String() || str(ph["status"]) != fmt.Sprint(definition.VotingStatus) {
			t.Fatalf("revised phase: %v", ph)
		}
		// The payout comes from whichever update executes it: a pillar's,
		// which no driver account signs, or the driver's own. Announce it, so
		// the two sends are owed whoever triggered them, and nothing else is.
		// Both are made by one receive of an update call to the accelerator.
		updateCall := pack(definition.UpdateMethodName)
		// Each payout carries the id of the phase it pays (accelerator.go).
		net.Expect(a.Address, az, znn, coins(6), updateCall, "FAZ-2 "+current.String(), current.Bytes())
		net.Expect(a.Address, az, qsr, coins(60), updateCall, "FAZ-2 "+current.String(), current.Bytes())
		voteAll(t, current)
		eventually(t, 6*time.Minute, "the phase is paid", func() bool {
			update(t)
			var p obj
			get(t, &p, "embedded.accelerator.getPhaseById", current)
			return str(p["phase"].(obj)["status"]) == fmt.Sprint(definition.PaidStatus)
		})
		receiveAll(t, a, 2)
		wantBig(t, "ZNN paid for the phase", bal(t, a.Address, znn), new(big.Int).Add(znnBefore, coins(6)))
		wantBig(t, "QSR paid for the phase", bal(t, a.Address, qsr), new(big.Int).Add(qsrBefore, coins(60)))
	})
}

func TestFastBridgeAdmin(t *testing.T) {
	fastGroup(t)
	admin := net.Key(9) // constants.InitialBridgeAdministrator on the test build
	stranger := newAccount(t, 122, 0, 0, 1500)
	callOK(t, net.Key(IdxFunder), types.PlasmaContract, definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, admin.Address), qsr, coins(3000))
	bridge := types.BridgeContract
	pack := func(name string, args ...interface{}) []byte {
		return definition.ABIBridge.PackMethodPanic(name, args...)
	}
	const class, chain = uint32(2), uint32(31337)
	contract := "0x" + strings.Repeat("ab", 20)
	tokenAddr := "0x" + strings.Repeat("cd", 20)
	setNetwork := func(class, chain uint32, name, contract, meta string) []byte {
		return pack(definition.SetNetworkMethodName, class, chain, name, contract, meta)
	}
	setPair := func(zts types.ZenonTokenStandard, owned bool, fee, delay uint32, meta string) []byte {
		return pack(definition.SetTokenPairMethod, class, chain, zts, tokenAddr, true, true, owned, units(1), fee, delay, meta)
	}

	t.Run("FB-1_administrator_is_the_drivers_account", func(t *testing.T) {
		var info obj
		get(t, &info, "embedded.bridge.getBridgeInfo")
		if str(info["administrator"]) != admin.Address.String() {
			t.Fatalf("bridge administrator is %v, want %s", info["administrator"], admin.Address)
		}
	})

	t.Run("FB-2_set_network", func(t *testing.T) {
		cover(t, "E1", "contract-method", "bridge.SetNetwork")
		cover(t, "E2", "contract-method", "bridge.SetNetwork")
		cover(t, "E3", "contract-method", "bridge.SetNetwork")
		cover(t, "E6", "contract-method", "bridge.SetNetwork")
		callFail(t, stranger, bridge, setNetwork(class, chain, "e2e-net", contract, "{}"), znn, units(0), watch("embedded.bridge.getAllNetworks", 0, 10))
		callReject(t, constants.ErrInvalidNetworkName.Error(), admin, bridge, setNetwork(class, chain, "ab", contract, "{}"), znn, units(0))
		callReject(t, constants.ErrInvalidNetworkName.Error(), admin, bridge, setNetwork(class, chain, strings.Repeat("n", 33), contract, "{}"), znn, units(0))
		callReject(t, constants.ErrForbiddenParam.Error(), admin, bridge, setNetwork(0, chain, "e2e-net", contract, "{}"), znn, units(0))
		callReject(t, constants.ErrForbiddenParam.Error(), admin, bridge, setNetwork(class, 0, "e2e-net", contract, "{}"), znn, units(0))
		callReject(t, constants.ErrInvalidContractAddress.Error(), admin, bridge, setNetwork(class, chain, "e2e-net", "not an address", "{}"), znn, units(0))
		callReject(t, constants.ErrInvalidJsonContent.Error(), admin, bridge, setNetwork(class, chain, "e2e-net", contract, "not json"), znn, units(0))
		callOK(t, admin, bridge, setNetwork(class, chain, "e2e-net", contract, "{}"), znn, units(0))
		var got, all obj
		get(t, &got, "embedded.bridge.getNetworkInfo", class, chain)
		get(t, &all, "embedded.bridge.getAllNetworks", 0, 10)
		if str(got["name"]) != "e2e-net" || str(all["count"]) != "1" {
			t.Errorf("network read back: %v, count %v", got, all["count"])
		}
	})

	t.Run("FB-3_set_token_pair", func(t *testing.T) {
		cover(t, "E1", "contract-method", "bridge.SetTokenPair")
		cover(t, "E2", "contract-method", "bridge.SetTokenPair")
		cover(t, "E3", "contract-method", "bridge.SetTokenPair")
		forbidden := constants.ErrForbiddenParam.Error()
		callFail(t, stranger, bridge, setPair(znn, false, 100, 2, "{}"), znn, units(0), watch("embedded.bridge.getNetworkInfo", class, chain))
		callReject(t, forbidden, admin, bridge, setPair(znn, true, 100, 2, "{}"), znn, units(0)) // ZNN cannot be owned by the bridge
		callReject(t, forbidden, admin, bridge, setPair(znn, false, constants.MaximumFee+1, 2, "{}"), znn, units(0))
		callReject(t, forbidden, admin, bridge, setPair(znn, false, 100, 0, "{}"), znn, units(0))
		callReject(t, forbidden, admin, bridge, setPair(types.ZeroTokenStandard, false, 100, 2, "{}"), znn, units(0))
		callReject(t, constants.ErrInvalidJsonContent.Error(), admin, bridge, setPair(znn, false, 100, 2, "nope"), znn, units(0))
		// Setting a token pair is time-challenged: the first call opens the
		// challenge, and the same call applies it once the soft delay is over.
		cover(t, "E12", "contract-method", "bridge.SetTokenPair")
		good := setPair(znn, false, constants.MaximumFee, 2, "{}") // fee exactly at the maximum
		opened := callOK(t, admin, bridge, good, znn, units(0))
		var got, security, challenges obj
		get(t, &got, "embedded.bridge.getNetworkInfo", class, chain)
		if pairs, _ := got["tokenPairs"].([]interface{}); len(pairs) != 0 {
			// P11 runs on a fresh chain, where no challenge can be open yet.
			t.Fatalf("the first call applied the token pair without waiting for the challenge: %v", pairs)
		}
		get(t, &challenges, "embedded.bridge.getTimeChallengesInfo")
		open := listOf(challenges)
		if len(open) != 1 || str(open[0].(obj)["MethodName"]) != "SetTokenPair" ||
			str(open[0].(obj)["ChallengeStartHeight"]) != fmt.Sprint(opened.Receive.MomentumAcknowledged.Height) {
			t.Fatalf("open time challenges after the first call: %v; want one for SetTokenPair started at height %d", challenges, opened.Receive.MomentumAcknowledged.Height)
		}
		callFail(t, admin, bridge, good, znn, units(0), watch("embedded.bridge.getNetworkInfo", class, chain)) // the delay is not over
		get(t, &security, "embedded.bridge.getSecurityInfo")
		var delay uint64
		fmt.Sscan(str(security["softDelay"]), &delay)
		// The delay is the one compiled into the build, not whatever the node says.
		if delay != constants.MinSoftDelay {
			t.Fatalf("soft delay of a fresh chain is %d momentums, the build's minimum is %d", delay, constants.MinSoftDelay)
		}
		// The boundary, from vm/embedded/implementation TimeChallenge: with the
		// challenge started at height S, a call the contract executes on
		// frontier height h is refused while h <= S+delay and applies from
		// h = S+delay+1. One call is published per momentum around that
		// height, and each is judged by the height its receive acknowledges:
		// the one at S+delay must fail and the one at S+delay+1 must succeed.
		start := opened.Receive.MomentumAcknowledged.Height
		edge := start + delay
		var sent []*nom.AccountBlock
		for at := edge - 2; at <= edge; at++ {
			if err := net.WaitHeight(at, 10*time.Minute); err != nil {
				t.Fatal(err)
			}
			blk, err := net.Send(admin, &nom.AccountBlock{BlockType: nom.BlockTypeUserSend, ToAddress: bridge, Data: good, TokenStandard: znn, Amount: units(0)}, nil)
			if err != nil {
				t.Fatalf("publishing the call at height %d: %v", at, err)
			}
			sent = append(sent, blk)
		}
		// When the pair is installed is observed while the three calls are
		// answered. A getter reads the contract's newest state, which already
		// holds a receive that is not yet in a momentum and not yet shown as
		// paired with its send. So each reading of the pair list is tied to
		// the contract's own chain: the bridge's newest block, height and
		// hash, is read before and after, and the reading counts only if it
		// did not change. Once the receives are known, each reading's block
		// is checked to be the bridge's block at that height, and the reading
		// is judged by whether the applying receive was on the chain then.
		type reading struct {
			chainHeight uint64
			chainHash   types.Hash
			pairs       int
			list        []interface{}
		}
		// The pair as it was set, every field, with its JSON type
		// (TokenPairMarshall in vm/embedded/definition/bridge.go).
		pairsAsSet := func(list []interface{}) string {
			if len(list) != 1 {
				return fmt.Sprintf("%d pairs: %v", len(list), list)
			}
			return pairAsSet(list[0], tokenAddr)
		}
		bridgeTip := func() (uint64, types.Hash) {
			var tip *api.AccountBlock
			get(t, &tip, "ledger.getFrontierAccountBlock", bridge)
			if tip == nil || tip.Height == 0 || tip.Address != bridge || tip.Hash == (types.Hash{}) {
				t.Fatalf("the bridge contract has no newest block: %+v", tip)
			}
			return tip.Height, tip.Hash
		}
		answered := func() int {
			n := 0
			for _, blk := range sent {
				if send, err := net.Block(0, blk.Hash); err == nil && send != nil && send.PairedAccountBlock != nil && send.ConfirmationDetail != nil {
					n++
				}
			}
			return n
		}
		var readings []reading
		read := func() {
			height, hash := bridgeTip()
			var info obj
			get(t, &info, "embedded.bridge.getNetworkInfo", class, chain)
			list, isList := info["tokenPairs"].([]interface{})
			if !isList || str(info["networkClass"]) != fmt.Sprint(class) || str(info["chainId"]) != fmt.Sprint(chain) {
				t.Fatalf("network info read during the boundary has no token pair list or is about another network: %v", info)
			}
			if h2, hash2 := bridgeTip(); h2 == height && hash2 == hash {
				readings = append(readings, reading{height, hash, len(list), list})
			}
		}
		for deadline := time.Now().Add(4 * time.Minute); ; time.Sleep(300 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("the three calls around the boundary were not all answered in 4 minutes")
			}
			read()
			if answered() == len(sent) {
				break
			}
		}
		// The state on the bridge's newest block once every call is answered.
		for i := 0; i < 3; i++ {
			read()
		}
		atEdge, afterEdge, applied := false, false, false
		var seen []string
		var refusalAt, appliedAt uint64 // heights on the bridge's chain of the boundary refusal and of the applying receive
		var appliedHash types.Hash
		for _, blk := range sent {
			res, err := net.WaitContract(blk.Hash, wait)
			if err != nil {
				t.Fatal(err)
			}
			h := res.Receive.MomentumAcknowledged.Height
			seen = append(seen, fmt.Sprintf("%d:%d", h, res.Status))
			switch {
			case applied:
				// After the pair is applied the same call opens a new challenge; not judged.
			case h <= edge && res.Status != StatusFail:
				t.Fatalf("the call executed on height %d was not refused; the challenge started at %d with delay %d, so it is due from %d", h, start, delay, edge+1)
			case h <= edge:
				if h == edge {
					atEdge, refusalAt = true, res.Receive.Height
				}
			case res.Status != StatusSuccess:
				t.Fatalf("the call executed on height %d was refused; the challenge started at %d with delay %d, so it is due from %d", h, start, delay, edge+1)
			default:
				afterEdge, applied = h == edge+1, true
				appliedAt, appliedHash = res.Receive.Height, res.Receive.Hash
			}
		}
		// Every call was judged above by the height it executed on, so a
		// wrong refusal or a wrong success has already failed the case. What
		// is left is timing: the driver does not choose the height a call
		// executes on, and calls that missed one side of the boundary say
		// nothing about the contract. That run is inconclusive.
		if !atEdge || !afterEdge {
			t.Fatalf("inconclusive, run the group again: no call executed exactly on height %d (refusal) and on %d (success), so the boundary was not observed; every call was judged by its height and none contradicts the delay (height:status %v)", edge, edge+1, seen)
		}
		// Each reading's block must be the bridge's block at that height.
		chainAt := map[uint64]types.Hash{}
		for _, r := range readings {
			if _, ok := chainAt[r.chainHeight]; ok {
				continue
			}
			var page struct {
				List []*api.AccountBlock `json:"list"`
			}
			get(t, &page, "ledger.getAccountBlocksByHeight", bridge, r.chainHeight, 1)
			if len(page.List) != 1 || page.List[0] == nil || page.List[0].Height != r.chainHeight || page.List[0].Address != bridge {
				t.Fatalf("the bridge's block at height %d reads as %v", r.chainHeight, page.List)
			}
			chainAt[r.chainHeight] = page.List[0].Hash
		}
		if chainAt[appliedAt] != (types.Hash{}) && chainAt[appliedAt] != appliedHash {
			t.Fatalf("the bridge's block at height %d is %s; the applying receive is %s", appliedAt, chainAt[appliedAt], appliedHash)
		}
		// Every reading is judged; the two that decide are the one on the
		// block just before the applying receive, which must show no pair,
		// and the one on the applying receive, which must show exactly the
		// pair as it was set.
		sawBefore, sawAt := false, false
		for _, r := range readings {
			if chainAt[r.chainHeight] != r.chainHash {
				t.Fatalf("a reading was taken on bridge block %s at height %d; the bridge's block at that height is %s", r.chainHash, r.chainHeight, chainAt[r.chainHeight])
			}
			if (r.pairs != 0) != (r.chainHeight >= appliedAt) {
				t.Fatalf("%d token pair(s) were installed with the bridge's chain at height %d; the applying receive is block %d of that chain", r.pairs, r.chainHeight, appliedAt)
			}
			if r.chainHeight >= appliedAt {
				if why := pairsAsSet(r.list); why != "" {
					t.Fatalf("with the bridge's chain at height %d the token pairs are not the pair as it was set: %s", r.chainHeight, why)
				}
			}
			sawBefore = sawBefore || r.chainHeight == appliedAt-1
			sawAt = sawAt || r.chainHeight == appliedAt && r.chainHash == appliedHash
		}
		if refusalAt >= appliedAt {
			t.Fatalf("the refusal is block %d of the bridge's chain and the applying receive block %d", refusalAt, appliedAt)
		}
		// The getters read the contract's newest state; no RPC reads it as of
		// a given block. So the two decisive readings depend on the polling
		// catching each block between its own two tip reads, and when the
		// applying receive follows the block before it closely, that can
		// miss. Every reading taken was judged above; a miss is inconclusive.
		if !sawBefore || !sawAt {
			t.Fatalf("inconclusive, run the group again: the pair list was not read both on the bridge's block just before the applying receive (block %d) and on that receive itself (block %d, %s); no reading taken contradicts the delay (readings: %+v)", appliedAt-1, appliedAt, appliedHash, readings)
		}
		get(t, &got, "embedded.bridge.getNetworkInfo", class, chain)
		pairs, _ := got["tokenPairs"].([]interface{})
		if len(pairs) != 1 {
			t.Fatalf("token pairs read back: %v", got["tokenPairs"])
		}
		// Every field of the pair as it was set, with its type.
		if why := pairAsSet(pairs[0], tokenAddr); why != "" {
			t.Errorf("token pair read back: %s", why)
		}
	})

	t.Run("FB-4_metadata", func(t *testing.T) {
		cover(t, "E1", "contract-method", "bridge.SetNetworkMetadata", "bridge.SetBridgeMetadata")
		cover(t, "E3", "contract-method", "bridge.SetNetworkMetadata", "bridge.SetBridgeMetadata")
		callFail(t, stranger, bridge, pack(definition.SetNetworkMetadataMethodName, class, chain, `{"a":1}`), znn, units(0), watch("embedded.bridge.getNetworkInfo", class, chain))
		callFail(t, stranger, bridge, pack(definition.SetBridgeMetadataMethodName, `{"b":2}`), znn, units(0), watch("embedded.bridge.getBridgeInfo"))
		callOK(t, admin, bridge, pack(definition.SetNetworkMetadataMethodName, class, chain, `{"a":1}`), znn, units(0))
		callOK(t, admin, bridge, pack(definition.SetBridgeMetadataMethodName, `{"b":2}`), znn, units(0))
		var netInfo, info obj
		get(t, &netInfo, "embedded.bridge.getNetworkInfo", class, chain)
		get(t, &info, "embedded.bridge.getBridgeInfo")
		if str(netInfo["metadata"]) != `{"a":1}` || str(info["metadata"]) != `{"b":2}` {
			t.Errorf("metadata read back: network %v, bridge %v", netInfo["metadata"], info["metadata"])
		}
	})

	t.Run("FB-5_orchestrator_info", func(t *testing.T) {
		cover(t, "E1", "contract-method", "bridge.SetOrchestratorInfo")
		cover(t, "E2", "contract-method", "bridge.SetOrchestratorInfo")
		cover(t, "E3", "contract-method", "bridge.SetOrchestratorInfo")
		orch := func(window uint64, threshold, confirmations, momentumTime uint32) []byte {
			return pack(definition.SetOrchestratorInfoMethodName, window, threshold, confirmations, momentumTime)
		}
		callReject(t, constants.ErrForbiddenParam.Error(), admin, bridge, orch(0, 1, 1, 10), znn, units(0))
		callReject(t, constants.ErrForbiddenParam.Error(), admin, bridge, orch(10, 0, 1, 10), znn, units(0))
		callFail(t, stranger, bridge, orch(10, 1, 1, 10), znn, units(0), watch("embedded.bridge.getOrchestratorInfo"))
		callOK(t, admin, bridge, orch(10, 1, 1, 10), znn, units(0))
		var got obj
		get(t, &got, "embedded.bridge.getOrchestratorInfo")
		if str(got["windowSize"]) != "10" || str(got["confirmationsToFinality"]) != "1" {
			t.Errorf("orchestrator info read back: %v", got)
		}
	})

	t.Run("FB-6_remove_token_pair_and_network", func(t *testing.T) {
		cover(t, "E1", "contract-method", "bridge.RemoveTokenPair", "bridge.RemoveNetwork")
		cover(t, "E3", "contract-method", "bridge.RemoveTokenPair", "bridge.RemoveNetwork")
		removePair := pack(definition.RemoveTokenPairMethodName, class, chain, znn, tokenAddr)
		removeNet := pack(definition.RemoveNetworkMethodName, class, chain)
		callFail(t, stranger, bridge, removePair, znn, units(0), watch("embedded.bridge.getNetworkInfo", class, chain))
		callFail(t, stranger, bridge, removeNet, znn, units(0), watch("embedded.bridge.getNetworkInfo", class, chain))
		callOK(t, admin, bridge, removePair, znn, units(0))
		var got, all obj
		get(t, &got, "embedded.bridge.getNetworkInfo", class, chain)
		if pairs, _ := got["tokenPairs"].([]interface{}); len(pairs) != 0 {
			t.Errorf("token pairs after removal: %v", got["tokenPairs"])
		}
		callOK(t, admin, bridge, removeNet, znn, units(0))
		get(t, &all, "embedded.bridge.getAllNetworks", 0, 10)
		if str(all["count"]) != "0" {
			t.Errorf("networks after removal: %v", all)
		}
	})

	t.Run("FB-7_halt_and_unhalt", func(t *testing.T) {
		cover(t, "E1", "contract-method", "bridge.Halt", "bridge.Unhalt")
		cover(t, "E3", "contract-method", "bridge.Halt", "bridge.Unhalt")
		cover(t, "E4", "contract-method", "bridge.Halt", "bridge.Unhalt")
		halt, unhalt := pack(definition.HaltMethodName, ""), pack(definition.UnhaltMethodName)
		halted := func() string {
			var info obj
			get(t, &info, "embedded.bridge.getBridgeInfo")
			return str(info["halted"])
		}
		callFail(t, admin, bridge, unhalt, znn, units(0), watch("embedded.bridge.getBridgeInfo"))  // not halted
		callFail(t, stranger, bridge, halt, znn, units(0), watch("embedded.bridge.getBridgeInfo")) // a stranger needs the TSS signature
		callOK(t, admin, bridge, halt, znn, units(0))
		if halted() != "true" {
			t.Fatalf("bridge not halted after the administrator's halt")
		}
		callFail(t, admin, bridge, halt, znn, units(0), watch("embedded.bridge.getBridgeInfo")) // already halted
		callFail(t, stranger, bridge, unhalt, znn, units(0), watch("embedded.bridge.getBridgeInfo"))
		callOK(t, admin, bridge, unhalt, znn, units(0))
		if halted() != "false" {
			t.Errorf("bridge still halted after the administrator's unhalt")
		}
	})
}

// pairAsSet says how a token pair differs from the one FB-3 sets, field by
// field and type by type, or "" if it does not. Only the token address, a
// hex string, is compared without regard to case.
func pairAsSet(v interface{}, tokenAddr string) string {
	pair, ok := v.(obj)
	if !ok {
		return fmt.Sprintf("not a pair: %v", v)
	}
	want := map[string]interface{}{
		"tokenStandard": znn.String(), "bridgeable": true, "redeemable": true, "owned": false,
		"minAmount": "1", "feePercentage": float64(constants.MaximumFee), "redeemDelay": float64(2), "metadata": "{}",
	}
	if len(pair) != len(want)+1 {
		return fmt.Sprintf("%d members, want %d: %v", len(pair), len(want)+1, pair)
	}
	for field, w := range want {
		if got, ok := pair[field]; !ok || got != w {
			return fmt.Sprintf("field %s reads %#v (present: %v), want %#v", field, got, ok, w)
		}
	}
	if a, ok := pair["tokenAddress"].(string); !ok || !strings.EqualFold(a, tokenAddr) {
		return fmt.Sprintf("field tokenAddress reads %#v, want %s", pair["tokenAddress"], tokenAddr)
	}
	return ""
}

// stakeRecord is a stake as the stake contract holds it.
type stakeRecord struct {
	start, revoke int64 // unix seconds; revoke 0 while not cancelled
	weight        *big.Int
}

// weightIn is the stake's weight within [from, to): its weighted amount
// times the seconds it was active then, as getWeightedStake computes it.
func (r stakeRecord) weightIn(from, to int64) *big.Int {
	if r.start > from {
		from = r.start
	}
	if r.revoke != 0 && r.revoke < to {
		to = r.revoke
	}
	if from >= to {
		return new(big.Int)
	}
	return new(big.Int).Mul(big.NewInt(to-from), r.weight)
}

// ackTime is the timestamp of the momentum a contract receive acknowledges:
// the frontier momentum the contract executed against.
func ackTime(t *testing.T, res *Result) int64 {
	t.Helper()
	return momentumTime(t, res.Receive.MomentumAcknowledged.Height, res.Receive.MomentumAcknowledged.Hash)
}

// momentumTime is the timestamp of the momentum at that height, which must
// have that hash.
func momentumTime(t *testing.T, height uint64, hash types.Hash) int64 {
	t.Helper()
	var page struct {
		List []struct {
			Height    uint64     `json:"height"`
			Hash      types.Hash `json:"hash"`
			Timestamp int64      `json:"timestamp"`
		} `json:"list"`
	}
	get(t, &page, "ledger.getMomentumsByHeight", height, 1)
	if len(page.List) != 1 || page.List[0].Height != height || page.List[0].Hash != hash || page.List[0].Timestamp == 0 {
		t.Fatalf("momentum %s at height %d reads as %+v", hash, height, page.List)
	}
	return page.List[0].Timestamp
}

// genesisTime is the genesis momentum's timestamp, where epoch 0 starts.
func genesisTime(t *testing.T) int64 {
	t.Helper()
	var page struct {
		List []struct {
			Height    uint64 `json:"height"`
			Timestamp int64  `json:"timestamp"`
		} `json:"list"`
	}
	get(t, &page, "ledger.getMomentumsByHeight", 1, 1)
	if len(page.List) != 1 || page.List[0].Height != 1 || page.List[0].Timestamp == 0 {
		t.Fatalf("genesis momentum: %+v", page.List)
	}
	return page.List[0].Timestamp
}

// weighted is the weighted amount of a stake: amount times
// (9 + duration in stake time units) / 10, as getWeightedStakeAmount
// computes it.
func weighted(amount *big.Int, seconds int64) *big.Int {
	w := new(big.Int).Mul(amount, big.NewInt(9+seconds/constants.StakeTimeUnitSec))
	return w.Quo(w, big.NewInt(10))
}

// acceleratorUpdate sends one accelerator Update call from caller and
// reports whether it executed, with the acknowledged height of the latest
// update the contract has executed as of the receipt: the driver's own if it
// executed, else the last one before it, a pillar's or the driver's (0 if
// none). The contract refusing the call for being too soon is expected; any
// other error, a refusal at publish or a receipt the endpoints disagree on,
// fails the case.
func acceleratorUpdate(t *testing.T, caller *wallet.KeyPair) (bool, uint64) {
	t.Helper()
	az := types.AcceleratorContract
	res, err := call(caller, az, definition.ABIAccelerator.PackMethodPanic(definition.UpdateMethodName), znn, units(0))
	if err != nil {
		t.Fatalf("the driver's update call: %v", err)
	}
	// Executed or refused, the outcome must be what CanPerformUpdate
	// decides from the last update that executed before this receipt on
	// its own chain: due once UpdateMinNumMomentums momentums separate
	// their acknowledged heights. A refusal is a VM receive failure.
	last := lastUpdateBefore(t, az, res.Receive)
	at := res.Receive.MomentumAcknowledged.Height
	due := last+constants.UpdateMinNumMomentums <= at
	if !res.OK() && due {
		t.Fatalf("the accelerator refused an update at momentum %d; the last executed update was at %d, so it was due", at, last)
	}
	if res.OK() && !due {
		t.Fatalf("the accelerator executed an update at momentum %d; the last executed update was at %d, so it was too soon", at, last)
	}
	if res.OK() {
		return true, at
	}
	return false, last
}

// updateExecutedAbove sends accelerator Update calls from caller until an
// update, the driver's or a pillar's, has executed with an acknowledged
// momentum above height, and returns that momentum's height. An update
// reads the active pillars at the momentum it acknowledges, so one above
// the frontier read after a pillar change sees the change.
func updateExecutedAbove(t *testing.T, caller *wallet.KeyPair, height uint64) uint64 {
	t.Helper()
	var at uint64
	eventually(t, 3*time.Minute, fmt.Sprintf("an accelerator update executes above momentum %d", height), func() bool {
		_, at = acceleratorUpdate(t, caller)
		return at > height
	})
	return at
}

// frontierHeight is the height of the rpc endpoint's frontier momentum.
func frontierHeight(t *testing.T) uint64 {
	t.Helper()
	m, err := net.Frontier()
	if err != nil {
		t.Fatalf("frontier: %v", err)
	}
	return m.Height
}

// lastUpdateBefore is the momentum height the last update the contract
// executed before the receipt acknowledged, or 0 if none: what the contract
// keeps as its last update height. It walks back from the receipt by the
// previous-block hashes, so the history read is the one the receipt was
// built on: each block must be the contract's, at the height below the last,
// read by its hash. An update is a receive of a send to the contract whose
// data is exactly the Update call, with status success.
func lastUpdateBefore(t *testing.T, contract types.Address, receipt *api.AccountBlock) uint64 {
	t.Helper()
	update := definition.ABICommon.PackMethodPanic(definition.UpdateMethodName)
	if receipt == nil || receipt.Address != contract {
		t.Fatalf("the update receipt %+v is not a block of %s", receipt, contract)
	}
	prev, height := receipt.PreviousHash, receipt.Height
	for height > 1 {
		b, err := net.Block(0, prev)
		if err != nil || b == nil {
			t.Fatalf("block %s, below height %d of %s: %v", prev, height, contract, err)
		}
		if b.Hash != prev || b.Address != contract || b.Height != height-1 {
			t.Fatalf("block %s below height %d of %s reads as %s, block %d of %s", prev, height, contract, b.Hash, b.Height, b.Address)
		}
		prev, height = b.PreviousHash, b.Height
		if b.BlockType != nom.BlockTypeContractReceive || receiveStatus(b) != StatusSuccess {
			continue
		}
		call, err := net.Block(0, b.FromBlockHash)
		if err != nil || call == nil || call.Hash != b.FromBlockHash {
			t.Fatalf("the call %s answered by block %d of %s: %v", b.FromBlockHash, b.Height, contract, err)
		}
		if call.ToAddress != contract || !nom.IsSendBlock(call.BlockType) {
			t.Fatalf("block %d of %s answers %s, a block of type %d to %s", b.Height, contract, call.Hash, call.BlockType, call.ToAddress)
		}
		if bytes.Equal(call.Data, update) {
			return b.MomentumAcknowledged.Height
		}
	}
	if height == 1 && prev != (types.Hash{}) {
		t.Fatalf("the first block of %s names a previous block %s", contract, prev)
	}
	return 0
}

// listOfKey is the list under key, or nil if it is missing or not a list.
func listOfKey(v obj, key string) []interface{} {
	list, _ := v[key].([]interface{})
	return list
}
