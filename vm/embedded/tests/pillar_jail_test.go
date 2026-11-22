package tests

import (
	"math/big"
	"reflect"
	"testing"
	"time"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

func jailTickSeconds() int64 {
	return constants.ConsensusConfig.BlockTime * int64(constants.ConsensusConfig.NodeCount)
}

func jailNodeCount() int {
	return int(constants.ConsensusConfig.NodeCount)
}

func tickOf(z mock.MockZenon, ts time.Time) uint64 {
	genesis := z.Chain().GetGenesisMomentum().Timestamp
	return uint64((ts.Unix() - genesis.Unix()) / jailTickSeconds())
}

func tickStart(z mock.MockZenon, tick uint64) time.Time {
	genesis := z.Chain().GetGenesisMomentum().Timestamp
	return genesis.Add(time.Duration(int64(tick)*jailTickSeconds()) * time.Second)
}

func frontierOf(t *testing.T, z mock.MockZenon) *nom.Momentum {
	t.Helper()
	frontier, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.FailIfErr(t, err)
	return frontier
}

func frontierTick(t *testing.T, z mock.MockZenon) uint64 {
	t.Helper()
	return tickOf(z, *frontierOf(t, z).Timestamp)
}

func jailRecord(t *testing.T, z mock.MockZenon, name string) *definition.PillarJail {
	t.Helper()
	jail, err := definition.GetPillarJail(z.Chain().GetFrontierAccountStore(types.PillarContract).Storage(), name)
	if err == constants.ErrDataNonExistent {
		return nil
	}
	common.FailIfErr(t, err)
	return jail
}

func expectJailRecord(t *testing.T, z mock.MockZenon, name string, wantJailed bool) *definition.PillarJail {
	t.Helper()
	jail := jailRecord(t, z, name)
	if jail == nil || jail.Jailed != wantJailed {
		t.Fatalf("jail record of %s = %+v, want jailed %v", name, jail, wantJailed)
	}
	return jail
}

func expectNoJailRecord(t *testing.T, z mock.MockZenon, name string) {
	t.Helper()
	if jail := jailRecord(t, z, name); jail != nil {
		t.Fatalf("jail record of %s = %+v, want none", name, jail)
	}
}

func rpcIsJailed(t *testing.T, z mock.MockZenon, name string) bool {
	t.Helper()
	pillar, err := embedded.NewPillarApi(z, true).GetByName(name)
	common.FailIfErr(t, err)
	return pillar.IsJailed
}

// insertUntilPillarUpdate inserts momentums until the last Update the
// pillar contract performed acknowledged a momentum in minTick or
// later. On return, the frontier is the momentum that included that
// Update receive.
func insertUntilPillarUpdate(t *testing.T, z mock.MockZenon, minTick uint64, max int) {
	t.Helper()
	for i := 0; i < max; i++ {
		z.InsertNewMomentum()
		update, err := definition.GetLastUpdate(z.Chain().GetFrontierAccountStore(types.PillarContract).Storage())
		common.FailIfErr(t, err)
		if update.Height == 0 {
			continue
		}
		acknowledged, err := z.Chain().GetFrontierMomentumStore().GetMomentumByHeight(update.Height)
		common.FailIfErr(t, err)
		if tickOf(z, *acknowledged.Timestamp) >= minTick {
			return
		}
	}
	t.Fatalf("no pillar Update acknowledged a momentum in tick %d or later within %d momentums", minTick, max)
}

// insertPastTick inserts momentums until the frontier is in a tick
// after tick.
func insertPastTick(t *testing.T, z mock.MockZenon, tick uint64) {
	t.Helper()
	for i := 0; i < 5000; i++ {
		if frontierTick(t, z) > tick {
			return
		}
		z.InsertNewMomentum()
	}
	t.Fatalf("frontier did not pass tick %d within 5000 momentums", tick)
}

// offlineFromNextTick inserts momentums up to the last slot of the
// frontier's tick, marks producers offline and returns the first tick
// they produce nothing in. Every producer scheduled in the frontier's
// tick must be online when it is called.
func offlineFromNextTick(t *testing.T, z mock.MockZenon, producers ...types.Address) uint64 {
	t.Helper()
	for i := 0; ; i++ {
		frontier := frontierOf(t, z)
		if tickOf(z, frontier.Timestamp.Add(10*time.Second)) != tickOf(z, *frontier.Timestamp) {
			break
		}
		if i == 40 {
			t.Fatal("frontier did not reach the last slot of its tick within 40 momentums")
		}
		z.InsertNewMomentum()
	}
	for _, producer := range producers {
		z.SetProducerOffline(producer, true)
	}
	return frontierTick(t, z) + 1
}

// tickMomentums returns the frontier-store momentums with timestamps in
// tick, oldest first.
func tickMomentums(t *testing.T, z mock.MockZenon, tick uint64) []*nom.Momentum {
	t.Helper()
	store := z.Chain().GetFrontierMomentumStore()
	start := tickStart(z, tick)
	end := tickStart(z, tick+1)
	var list []*nom.Momentum
	for height := frontierOf(t, z).Height; height > 0; height-- {
		momentum, err := store.GetMomentumByHeight(height)
		common.FailIfErr(t, err)
		if momentum.Timestamp.Before(start) {
			break
		}
		if momentum.Timestamp.Before(end) {
			list = append(list, momentum)
		}
	}
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list
}

func periodAt(t *testing.T, z mock.MockZenon, tick uint64) *api.PeriodStats {
	t.Helper()
	frontier := frontierOf(t, z)
	periods, err := z.Consensus().FixedPillarReader(frontier.Identifier()).FinishedPeriodStats(tickOf(z, *frontier.Timestamp))
	common.FailIfErr(t, err)
	if tick >= uint64(len(periods)) {
		t.Fatalf("tick %d is not finished: %d finished ticks", tick, len(periods))
	}
	return periods[tick]
}

// expectFullTick requires the tick to hold a momentum in every slot,
// each produced by one of producers.
func expectFullTick(t *testing.T, z mock.MockZenon, tick uint64, producers ...types.Address) {
	t.Helper()
	momentums := tickMomentums(t, z, tick)
	if len(momentums) != jailNodeCount() {
		t.Fatalf("tick %d holds %d momentums, want %d", tick, len(momentums), jailNodeCount())
	}
	for k, momentum := range momentums {
		want := tickStart(z, tick).Add(time.Duration(k) * 10 * time.Second)
		if !momentum.Timestamp.Equal(want) {
			t.Fatalf("tick %d momentum %d at %v, want %v", tick, k, momentum.Timestamp, want)
		}
		found := false
		for _, producer := range producers {
			found = found || momentum.Producer() == producer
		}
		if !found {
			t.Fatalf("tick %d momentum %d produced by %v, not one of %v", tick, k, momentum.Producer(), producers)
		}
	}
}

func expectScheduled(t *testing.T, z mock.MockZenon, tick uint64, producer types.Address, want bool) {
	t.Helper()
	scheduled := false
	for k := 0; k < jailNodeCount(); k++ {
		expected, err := z.Consensus().GetMomentumProducer(tickStart(z, tick).Add(time.Duration(k) * 10 * time.Second))
		common.FailIfErr(t, err)
		scheduled = scheduled || *expected == producer
	}
	if scheduled != want {
		t.Fatalf("producer %v scheduled in tick %d = %v, want %v", producer, tick, scheduled, want)
	}
}

func registerPillar(t *testing.T, z mock.MockZenon, key *wallet.KeyPair, name string) {
	t.Helper()
	cost, err := embedded.NewPillarApi(z, true).GetQsrRegistrationCost()
	common.FailIfErr(t, err)
	amount, ok := new(big.Int).SetString(cost, 10)
	if !ok {
		t.Fatalf("unparsable QSR registration cost %q", cost)
	}
	deposit := z.CallContract(&nom.AccountBlock{
		Address:       key.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        amount,
	})
	insertMomentums(z, 2)
	deposit.Error(t, nil)
	register := z.CallContract(&nom.AccountBlock{
		Address:       key.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.RegisterMethodName, name, key.Address, key.Address, uint8(0), uint8(100)),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.PillarStakeAmount,
	})
	insertMomentums(z, 2)
	register.Error(t, nil)
}

// unjailAndConfirm sends Unjail from the pillar's owner and inserts 4
// momentums.
func unjailAndConfirm(t *testing.T, z mock.MockZenon, key *wallet.KeyPair, name string) {
	t.Helper()
	unjail := z.CallContract(unjailSend(key.Address, name, constants.PillarUnjailAmount))
	insertMomentums(z, 4)
	unjail.Error(t, nil)
}

// expectColdReplayMatches replays z's chain on a fresh node and requires
// the same frontier, producer schedule, election tick statistics and
// jail records.
func expectColdReplayMatches(t *testing.T, z mock.MockZenon) {
	t.Helper()
	frontier := frontierOf(t, z)
	store := z.Chain().GetFrontierMomentumStore()
	detailed := make([]*nom.DetailedMomentum, 0, frontier.Height)
	for height := uint64(2); height <= frontier.Height; height++ {
		momentum, err := store.GetMomentumByHeight(height)
		common.FailIfErr(t, err)
		prefetched, err := store.PrefetchMomentum(momentum)
		common.FailIfErr(t, err)
		detailed = append(detailed, prefetched)
	}

	// z produces nothing after this point, because common.Clock follows
	// cold.
	cold := mock.NewMockZenon(t)
	defer cold.StopPanic()
	supervisor := vm.NewSupervisor(cold.Chain(), cold.Consensus())
	momentumVerifier := verifier.NewVerifier(cold.Chain(), cold.Consensus(), vm.CanonicalBasePlasma)
	bridge := protocol.NewChainBridge(cold.Chain(), cold.Consensus(), momentumVerifier, supervisor)
	for from := 0; from < len(detailed); from += 100 {
		to := from + 100
		if to > len(detailed) {
			to = len(detailed)
		}
		if _, err := bridge.InsertChain(detailed[from:to]); err != nil {
			t.Fatalf("cold replay of momentums %d to %d failed: %v", detailed[from].Momentum.Height, detailed[to-1].Momentum.Height, err)
		}
	}

	coldFrontier, err := cold.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.FailIfErr(t, err)
	if coldFrontier.Hash != frontier.Hash {
		t.Fatalf("cold frontier = %v, want %v", coldFrontier.Identifier(), frontier.Identifier())
	}

	genesis := z.Chain().GetGenesisMomentum().Timestamp
	for slot := *genesis; !slot.After(*frontier.Timestamp); slot = slot.Add(10 * time.Second) {
		want, err := z.Consensus().GetMomentumProducer(slot)
		common.FailIfErr(t, err)
		got, err := cold.Consensus().GetMomentumProducer(slot)
		common.FailIfErr(t, err)
		if *got != *want {
			t.Fatalf("producer at %v = %v on cold node, want %v", slot, *got, *want)
		}
	}

	finished := tickOf(z, *frontier.Timestamp)
	want, err := z.Consensus().FixedPillarReader(frontier.Identifier()).FinishedPeriodStats(finished)
	common.FailIfErr(t, err)
	got, err := cold.Consensus().FixedPillarReader(frontier.Identifier()).FinishedPeriodStats(finished)
	common.FailIfErr(t, err)
	if uint64(len(want)) != finished || !reflect.DeepEqual(got, want) {
		t.Fatalf("finished period stats differ on the cold node (%d and %d entries, want %d)", len(got), len(want), finished)
	}

	wantJails, err := definition.GetPillarJailList(z.Chain().GetFrontierAccountStore(types.PillarContract).Storage())
	common.FailIfErr(t, err)
	gotJails, err := definition.GetPillarJailList(cold.Chain().GetFrontierAccountStore(types.PillarContract).Storage())
	common.FailIfErr(t, err)
	if !reflect.DeepEqual(gotJails, wantJails) {
		t.Fatalf("jail records on the cold node = %+v, want %+v", gotJails, wantJails)
	}
}

// Without the spork, an offline pillar keeps its slots and no pillar is
// jailed.
func TestPillarJail_NoSporkOfflinePillarKeepsItsSlots(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	o := offlineFromNextTick(t, z, g.Pillar3.Address)
	insertUntilPillarUpdate(t, z, o+constants.PillarJailWindowPeriods, 4500)

	expectNoJailRecord(t, z, g.Pillar3Name)
	if rpcIsJailed(t, z, g.Pillar3Name) {
		t.Fatal("pillar reports isJailed true without a jail record")
	}
	n := frontierTick(t, z) + 1
	insertPastTick(t, z, n)
	if got := len(tickMomentums(t, z, n)); got >= jailNodeCount() {
		t.Fatalf("tick %d holds %d momentums, want fewer than %d", n, got, jailNodeCount())
	}
	expectScheduled(t, z, n, g.Pillar3.Address, true)
}

// One Update jails every offline pillar that missed all its slots, and
// the remaining pillars fill every slot.
func TestPillarJail_MassOfflineJailedInOneUpdate(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	registerPillar(t, z, g.Pillar4, g.Pillar4Name)
	registerPillar(t, z, g.Pillar5, g.Pillar5Name)
	registerPillar(t, z, g.Pillar6, g.Pillar6Name)
	registerPillar(t, z, g.Pillar7, g.Pillar7Name)
	registerPillar(t, z, g.Pillar8, g.Pillar8Name)
	insertPastTick(t, z, frontierTick(t, z)+2)

	offline := []*wallet.KeyPair{g.Pillar3, g.Pillar4, g.Pillar5, g.Pillar6, g.Pillar7, g.Pillar8}
	names := []string{g.Pillar3Name, g.Pillar4Name, g.Pillar5Name, g.Pillar6Name, g.Pillar7Name, g.Pillar8Name}
	addresses := make([]types.Address, 0, len(offline))
	for _, key := range offline {
		addresses = append(addresses, key.Address)
	}
	o := offlineFromNextTick(t, z, addresses...)
	activatePillarJailSpork(t, z)
	insertUntilPillarUpdate(t, z, o+constants.PillarJailWindowPeriods, 7500)
	j := frontierOf(t, z)

	var since int64
	for i, name := range names {
		jail := expectJailRecord(t, z, name, true)
		if i == 0 {
			since = jail.Since
		}
		if jail.Since != since {
			t.Fatalf("jail record of %s has Since %d, want the shared %d", name, jail.Since, since)
		}
	}
	if since < tickStart(z, o+constants.PillarJailWindowPeriods).Unix() || since > j.Timestamp.Unix() {
		t.Fatalf("Since %d is outside [%d, %d]", since, tickStart(z, o+constants.PillarJailWindowPeriods).Unix(), j.Timestamp.Unix())
	}
	expectNoJailRecord(t, z, g.Pillar1Name)
	expectNoJailRecord(t, z, g.Pillar2Name)

	all, err := embedded.NewPillarApi(z, true).GetAll(0, 50)
	common.FailIfErr(t, err)
	jailed := map[string]bool{}
	for _, pillar := range all.List {
		if pillar.IsJailed {
			jailed[pillar.Name] = true
		}
	}
	if len(jailed) != len(names) {
		t.Fatalf("pillars reporting isJailed = %v, want exactly %v", jailed, names)
	}
	for _, name := range names {
		if !jailed[name] {
			t.Fatalf("pillar %s does not report isJailed", name)
		}
	}

	after := tickOf(z, *j.Timestamp) + 2
	insertPastTick(t, z, after)
	expectFullTick(t, z, after, g.Pillar1.Address, g.Pillar2.Address)
}

// Jail, paid unjail, return, re-jail, a stall with an unjailed offline
// pillar and an all-but-one jailed set, then a cold node replays the
// whole chain.
func TestPillarJail_LifecycleMatchesColdReplay(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePillarJailSpork(t, z)

	// 1. Jail.
	o := offlineFromNextTick(t, z, g.Pillar3.Address)
	insertUntilPillarUpdate(t, z, o+constants.PillarJailWindowPeriods, 4500)
	j1 := frontierOf(t, z)
	jail := expectJailRecord(t, z, g.Pillar3Name, true)
	s1 := jail.Since
	if s1 < tickStart(z, o+constants.PillarJailWindowPeriods).Unix() {
		t.Fatalf("Since %d is before tick %d starts", s1, o+constants.PillarJailWindowPeriods)
	}
	expectNoJailRecord(t, z, g.Pillar1Name)
	expectNoJailRecord(t, z, g.Pillar2Name)
	if !rpcIsJailed(t, z, g.Pillar3Name) {
		t.Fatal("jailed pillar reports isJailed false")
	}
	insertPastTick(t, z, tickOf(z, *j1.Timestamp)+2)
	expectFullTick(t, z, tickOf(z, *j1.Timestamp)+2, g.Pillar1.Address, g.Pillar2.Address)

	// 2. Unjail and return.
	z.SetProducerOffline(g.Pillar3.Address, false)
	b0 := znnBalanceOf(t, z, g.Pillar3.Address)
	s0 := znnTotalSupply(t, z)
	fee := constants.PillarUnjailAmount
	unjail := z.CallContract(unjailSend(g.Pillar3.Address, g.Pillar3Name, fee))
	z.InsertNewMomentum()
	expectBalance(t, z, g.Pillar3.Address, new(big.Int).Sub(b0, fee))
	insertMomentums(z, 3)
	unjail.Error(t, nil)
	jail = expectJailRecord(t, z, g.Pillar3Name, false)
	s2 := jail.Since
	if s2 <= s1 || s2 > frontierOf(t, z).Timestamp.Unix() {
		t.Fatalf("unjail Since %d is outside (%d, frontier]", s2, s1)
	}
	if rpcIsJailed(t, z, g.Pillar3Name) {
		t.Fatal("unjailed pillar reports isJailed true")
	}
	expectSupply(t, z, new(big.Int).Sub(s0, fee))

	second := z.CallContract(unjailSend(g.Pillar3.Address, g.Pillar3Name, fee))
	z.InsertNewMomentum()
	expectBalance(t, z, g.Pillar3.Address, new(big.Int).Sub(b0, new(big.Int).Mul(fee, big.NewInt(2))))
	insertMomentums(z, 2)
	second.Error(t, constants.ErrPillarNotJailed)
	autoreceive(t, z, g.Pillar3.Address)
	z.InsertNewMomentum()
	expectBalance(t, z, g.Pillar3.Address, new(big.Int).Sub(b0, fee))
	expectSupply(t, z, new(big.Int).Sub(s0, fee))
	if got := expectJailRecord(t, z, g.Pillar3Name, false); got.Since != s2 {
		t.Fatalf("second unjail changed Since to %d, want %d", got.Since, s2)
	}

	u := frontierTick(t, z)
	insertPastTick(t, z, u+2)
	expectFullTick(t, z, u+2, g.Pillar1.Address, g.Pillar2.Address, g.Pillar3.Address)
	expectScheduled(t, z, u+2, g.Pillar3.Address, true)
	insertUntilPillarUpdate(t, z, u+3, 1000)
	if got := expectJailRecord(t, z, g.Pillar3Name, false); got.Since != s2 {
		t.Fatalf("record Since = %d after the Update, want %d", got.Since, s2)
	}

	// 3. Re-jail after paying while offline.
	o3 := offlineFromNextTick(t, z, g.Pillar3.Address)
	insertUntilPillarUpdate(t, z, o3+constants.PillarJailWindowPeriods, 4500)
	expectJailRecord(t, z, g.Pillar3Name, true)
	unjailAndConfirm(t, z, g.Pillar3, g.Pillar3Name)
	jail = expectJailRecord(t, z, g.Pillar3Name, false)
	s4 := jail.Since
	f := frontierTick(t, z)
	tMin := uint64(2)
	for tickStart(z, tMin-2).Unix() < s4 {
		tMin++
	}
	next := tMin
	if f+2 > next {
		next = f + 2
	}
	// Only misses from tick tMin on count, so the re-jail needs a miss a
	// full window after the first of them.
	insertUntilPillarUpdate(t, z, next+constants.PillarJailWindowPeriods, 7500)
	jail = expectJailRecord(t, z, g.Pillar3Name, true)
	s5 := jail.Since
	if s5 < tickStart(z, tMin+constants.PillarJailWindowPeriods).Unix() {
		t.Fatalf("re-jail Since %d is before tick %d starts", s5, tMin+constants.PillarJailWindowPeriods)
	}
	j3 := frontierOf(t, z)

	// 4. Jail a second pillar.
	insertPastTick(t, z, tickOf(z, *j3.Timestamp)+1)
	o4 := offlineFromNextTick(t, z, g.Pillar2.Address)
	insertUntilPillarUpdate(t, z, o4+constants.PillarJailWindowPeriods, 4500)
	j4 := frontierOf(t, z)
	expectJailRecord(t, z, g.Pillar2Name, true)
	if got := expectJailRecord(t, z, g.Pillar3Name, true); got.Since != s5 {
		t.Fatalf("Pillar3 Since = %d, want %d", got.Since, s5)
	}
	expectNoJailRecord(t, z, g.Pillar1Name)

	// 5. Stall with an unjailed offline pillar.
	z.SetProducerOffline(g.Pillar2.Address, false)
	z.SetProducerOffline(g.Pillar3.Address, false)
	insertPastTick(t, z, tickOf(z, *j4.Timestamp)+1)
	unjailAndConfirm(t, z, g.Pillar3, g.Pillar3Name)
	expectJailRecord(t, z, g.Pillar3Name, false)
	f = frontierTick(t, z)
	insertPastTick(t, z, f+2)
	o5 := offlineFromNextTick(t, z, g.Pillar1.Address, g.Pillar3.Address)
	if o5 != f+4 {
		t.Fatalf("offline window starts in tick %d, want %d", o5, f+4)
	}
	z.InsertNewMomentum()
	m := frontierOf(t, z)
	if m.Producer() != g.Pillar2.Address || tickOf(z, *m.Timestamp) != o5+2 {
		t.Fatalf("first momentum after the offline window is by %v in tick %d, want Pillar2 in tick %d", m.Producer(), tickOf(z, *m.Timestamp), o5+2)
	}
	for _, tick := range []uint64{o5, o5 + 1} {
		stats := periodAt(t, z, tick)
		var blocks, expected uint64
		for name, pillar := range stats.Pillars {
			if name == "" {
				t.Fatalf("empty tick %d has an entry for unelected producers", tick)
			}
			blocks += pillar.BlockNum
		}
		for _, name := range []string{g.Pillar1Name, g.Pillar3Name} {
			if stats.Pillars[name] == nil || stats.Pillars[name].ExpectedBlockNum == 0 {
				t.Fatalf("tick %d expects no slot for %s", tick, name)
			}
			expected += stats.Pillars[name].ExpectedBlockNum
		}
		if blocks != 0 || expected != uint64(jailNodeCount()) {
			t.Fatalf("tick %d has %d momentums and %d expected slots for Pillar1 and Pillar3, want 0 and %d", tick, blocks, expected, jailNodeCount())
		}
	}

	// 6. Convergence without an unjail. Until the Update jails the
	// offline pillars, the fallback keeps jailed Pillar2 producing in
	// every tick.
	insertUntilPillarUpdate(t, z, o5+constants.PillarJailWindowPeriods, 7500)
	j6 := frontierOf(t, z)
	for tick := o5 + 2; tick < tickOf(z, *j6.Timestamp); tick++ {
		if len(tickMomentums(t, z, tick)) == 0 {
			t.Fatalf("tick %d between the first fallback momentum and the Update holds no momentum", tick)
		}
	}
	jail1 := expectJailRecord(t, z, g.Pillar1Name, true)
	jail3 := expectJailRecord(t, z, g.Pillar3Name, true)
	if jail1.Since != jail3.Since || jail1.Since < tickStart(z, o5+constants.PillarJailWindowPeriods).Unix() {
		t.Fatalf("Pillar1 and Pillar3 Since = %d and %d, want one shared value from tick %d", jail1.Since, jail3.Since, o5+constants.PillarJailWindowPeriods)
	}
	expectJailRecord(t, z, g.Pillar2Name, true)
	t6 := tickOf(z, *j6.Timestamp)
	insertPastTick(t, z, t6+3)
	for _, tick := range []uint64{t6 + 2, t6 + 3} {
		momentums := tickMomentums(t, z, tick)
		if len(momentums) == 0 {
			t.Fatalf("tick %d holds no momentum", tick)
		}
		for _, momentum := range momentums {
			if momentum.Producer() != g.Pillar2.Address {
				t.Fatalf("tick %d momentum %d produced by %v, want Pillar2", tick, momentum.Height, momentum.Producer())
			}
		}
	}
	expectScheduled(t, z, t6+2, g.Pillar1.Address, true)

	// 7. All but one jailed.
	unjailAndConfirm(t, z, g.Pillar2, g.Pillar2Name)
	u7 := frontierTick(t, z)
	insertPastTick(t, z, u7+2)
	expectFullTick(t, z, u7+2, g.Pillar2.Address)

	// 8. A cold node replays the chain.
	expectColdReplayMatches(t, z)
}

func expectBalance(t *testing.T, z mock.MockZenon, address types.Address, want *big.Int) {
	t.Helper()
	if got := znnBalanceOf(t, z, address); got.Cmp(want) != 0 {
		t.Fatalf("ZNN balance of %v = %v, want %v", address, got, want)
	}
}

func expectSupply(t *testing.T, z mock.MockZenon, want *big.Int) {
	t.Helper()
	if got := znnTotalSupply(t, z); got.Cmp(want) != 0 {
		t.Fatalf("ZNN total supply = %v, want %v", got, want)
	}
}
