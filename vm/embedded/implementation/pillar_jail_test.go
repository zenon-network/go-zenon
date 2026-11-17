package implementation

import (
	"math/big"
	"reflect"
	"testing"
	"time"

	accountstore "github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// jailCount is one pillar's line in a tick: produced and expected
// slots, and whether the pillar was jailed at the tick's proof.
type jailCount struct {
	produced, expected uint64
	jailedAtProof      bool
}

// jailTick builds the stats of election tick tick.
func jailTick(tick uint64, counts map[string]jailCount) *api.PeriodStats {
	pillars := make(map[string]*api.PeriodPillarStats, len(counts))
	for name, c := range counts {
		pillars[name] = &api.PeriodPillarStats{BlockNum: c.produced, ExpectedBlockNum: c.expected, Eligible: !c.jailedAtProof}
	}
	return &api.PeriodStats{Tick: tick, StartTime: int64(300 * tick), Pillars: pillars}
}

// jailTicks builds ticks from to to-1. B produces in every tick, so
// every tick holds a momentum; fill adds the counts of each tick.
func jailTicks(from, to uint64, fill func(tick uint64, counts map[string]jailCount)) []*api.PeriodStats {
	list := make([]*api.PeriodStats, 0, to-from)
	for tick := from; tick < to; tick++ {
		counts := map[string]jailCount{"B": {produced: 3, expected: 3}}
		fill(tick, counts)
		list = append(list, jailTick(tick, counts))
	}
	return list
}

func TestAdvanceJailStreaks(t *testing.T) {
	miss := jailCount{expected: 2}
	cases := []struct {
		name    string
		start   jailStreak
		periods []*api.PeriodStats
		want    jailStreak
	}{
		{"an eligible miss starts a streak", jailStreak{},
			[]*api.PeriodStats{jailTick(7, map[string]jailCount{"A": miss, "B": {produced: 1}})},
			jailStreak{missed: 2, first: 7}},
		{"misses accumulate and keep the first tick", jailStreak{missed: 1, first: 3},
			[]*api.PeriodStats{jailTick(7, map[string]jailCount{"A": miss, "B": {produced: 1}}), jailTick(8, map[string]jailCount{"A": {expected: 1}, "B": {produced: 1}})},
			jailStreak{missed: 4, first: 3}},
		{"production resets the streak", jailStreak{missed: 5, first: 3},
			[]*api.PeriodStats{jailTick(7, map[string]jailCount{"A": {produced: 1, expected: 2}})},
			jailStreak{}},
		{"production while jailed at the proof resets the streak", jailStreak{missed: 5, first: 3},
			[]*api.PeriodStats{jailTick(7, map[string]jailCount{"A": {produced: 1, expected: 1, jailedAtProof: true}})},
			jailStreak{}},
		{"a miss while jailed at the proof is not counted", jailStreak{},
			[]*api.PeriodStats{jailTick(7, map[string]jailCount{"A": {expected: 3, jailedAtProof: true}, "B": {produced: 1}})},
			jailStreak{}},
		{"a tick with no momentum is skipped", jailStreak{missed: 1, first: 3},
			[]*api.PeriodStats{jailTick(7, map[string]jailCount{"A": miss, "B": {expected: 3}})},
			jailStreak{missed: 1, first: 3}},
		{"a tick that does not list the pillar leaves the streak", jailStreak{missed: 1, first: 3},
			[]*api.PeriodStats{jailTick(7, map[string]jailCount{"B": {produced: 1}})},
			jailStreak{missed: 1, first: 3}},
		{"an unelected tick leaves the streak", jailStreak{missed: 1, first: 3},
			[]*api.PeriodStats{jailTick(7, map[string]jailCount{"A": {}, "B": {produced: 1}})},
			jailStreak{missed: 1, first: 3}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			streak := c.start
			advanceJailStreaks(c.periods, map[string]*jailStreak{"A": &streak})
			if streak != c.want {
				t.Fatalf("streak = %+v, want %+v", streak, c.want)
			}
		})
	}
}

func TestStreakJails(t *testing.T) {
	window := constants.PillarJailWindowPeriods
	min := constants.PillarJailMinMissedSlots
	cases := []struct {
		name   string
		streak jailStreak
		last   uint64
		want   bool
	}{
		{"enough slots over the full window", jailStreak{missed: min, first: 10}, 10 + window - 1, true},
		{"enough slots one tick short of the window", jailStreak{missed: min, first: 10}, 10 + window - 2, false},
		{"one slot short over the full window", jailStreak{missed: min - 1, first: 10}, 10 + window - 1, false},
		{"no streak", jailStreak{}, 10 + window, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := streakJails(&c.streak, c.last); got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

// A pillar outside the top 30 is elected about twice an hour. One
// missed slot does not jail it; it is jailed once it has missed enough
// slots, however many judgements that takes.
func TestAdvanceJailStreaks_SparselyScheduledPillar(t *testing.T) {
	streak := &jailStreak{}
	streaks := map[string]*jailStreak{"A": streak}

	oneSlot := jailTicks(0, 12, func(tick uint64, counts map[string]jailCount) {
		if tick == 5 {
			counts["A"] = jailCount{expected: 1}
		}
	})
	advanceJailStreaks(oneSlot, streaks)
	if streakJails(streak, 11) {
		t.Fatal("one missed slot in an hour jailed the pillar")
	}

	// Offline for the next hours, elected once every five ticks.
	var last uint64
	for from := uint64(12); from < 12*6; from += 12 {
		advanceJailStreaks(jailTicks(from, from+12, func(tick uint64, counts map[string]jailCount) {
			if tick%5 == 0 {
				counts["A"] = jailCount{expected: 1}
			}
		}), streaks)
		last = from + 11
		if streakJails(streak, last) {
			break
		}
	}
	if !streakJails(streak, last) {
		t.Fatalf("an offline, sparsely elected pillar was never jailed: %+v", streak)
	}
	if streak.first != 5 {
		t.Fatalf("streak starts at tick %d, want 5", streak.first)
	}
}

// After an unjail in tick 23, the pillar produces in tick 25 and then
// stops. Its production counts, so it is not jailed until a full window
// after its first missed slot in tick 26.
func TestAdvanceJailStreaks_ProductionAfterUnjailCounts(t *testing.T) {
	streak := &jailStreak{}
	ticks := jailTicks(23, 40, func(tick uint64, counts map[string]jailCount) {
		switch {
		case tick < 25:
			counts["A"] = jailCount{expected: 1, jailedAtProof: true}
		case tick == 25:
			counts["A"] = jailCount{produced: 10, expected: 10}
		default:
			counts["A"] = jailCount{expected: 1}
		}
	})
	for _, tick := range ticks {
		advanceJailStreaks([]*api.PeriodStats{tick}, map[string]*jailStreak{"A": streak})
		jailed := streakJails(streak, tick.Tick)
		want := tick.Tick >= 26+constants.PillarJailWindowPeriods-1
		if jailed != want {
			t.Fatalf("after tick %d: jailed %v, want %v; streak %+v", tick.Tick, jailed, want, streak)
		}
	}
}

// jailUpdateContext answers FinishedPeriodStats(count) with the last
// count entries of periodStats and records each count it is asked for.
type jailUpdateContext struct {
	*testEpochBoundContext
	requested []uint64
}

func (ctx *jailUpdateContext) FinishedPeriodStats(count uint64) ([]*api.PeriodStats, error) {
	ctx.requested = append(ctx.requested, count)
	if count > uint64(len(ctx.periodStats)) {
		return ctx.periodStats, nil
	}
	return ctx.periodStats[uint64(len(ctx.periodStats))-count:], nil
}

// newJailUpdateContext stores pillars a to f, e revoked, with c jailed
// and d and f unjailed before, and ticks 0 to 13. a, c and e miss from
// tick 1 on, d and f miss in tick 13 only, and b produces throughout.
func newJailUpdateContext(t *testing.T, enforced bool) *jailUpdateContext {
	t.Helper()
	frontierTime := time.Unix(100000, 0)
	base := &testEpochBoundContext{
		Account: accountstore.NewAccountStore(types.PillarContract, db.NewMemDB()),
		momentum: &nom.Momentum{
			ChainIdentifier: 100,
			Timestamp:       &frontierTime,
			Height:          1000,
		},
		dynPlasmaEnforced: true,
		activeSporks:      map[types.Hash]bool{types.PillarJailSpork.SporkId: enforced},
	}
	ctx := &jailUpdateContext{testEpochBoundContext: base}
	common.FailIfErr(t, (&definition.LastEpochUpdate{LastEpoch: 100000}).Save(ctx.Storage()))

	for i, name := range []string{"pillar-a", "pillar-b", "pillar-c", "pillar-d", "pillar-e", "pillar-f"} {
		revokeTime := int64(0)
		if name == "pillar-e" {
			revokeTime = 1
		}
		common.FailIfErr(t, (&definition.PillarInfo{
			Name:                  name,
			StakeAddress:          termsTestAddress(byte(21 + i)),
			BlockProducingAddress: termsTestAddress(byte(41 + i)),
			RewardWithdrawAddress: termsTestAddress(byte(21 + i)),
			Amount:                constants.PillarStakeAmount,
			PillarType:            definition.NormalPillarType,
			RevokeTime:            revokeTime,
		}).Save(ctx.Storage()))
	}
	for _, jail := range []*definition.PillarJail{
		{Name: "pillar-c", Jailed: true, Since: 42},
		{Name: "pillar-d", Jailed: false, Since: 700},
		{Name: "pillar-f", Jailed: false, Since: 1},
	} {
		common.FailIfErr(t, jail.Save(ctx.Storage()))
	}

	ctx.periodStats = jailTicks(0, 14, func(tick uint64, counts map[string]jailCount) {
		counts["pillar-b"] = jailCount{produced: 1, expected: 1}
		if tick >= 1 {
			for _, name := range []string{"pillar-a", "pillar-c", "pillar-e"} {
				counts[name] = jailCount{expected: 3}
			}
		}
		if tick == 13 {
			counts["pillar-d"] = jailCount{expected: 3}
			counts["pillar-f"] = jailCount{expected: 3}
		}
	})
	return ctx
}

func updateSend() *nom.AccountBlock {
	return &nom.AccountBlock{
		BlockType: nom.BlockTypeUserSend,
		Address:   termsTestAddress(60),
		ToAddress: types.PillarContract,
		Amount:    big.NewInt(0),
		Data:      definition.ABIPillars.PackMethodPanic(definition.UpdateMethodName),
	}
}

func expectNamedJail(t *testing.T, ctx *jailUpdateContext, name string, want *definition.PillarJail) {
	t.Helper()
	got, err := definition.GetPillarJail(ctx.Storage(), name)
	if want == nil {
		if err != constants.ErrDataNonExistent {
			t.Fatalf("%s jail record = %+v, %v; want none", name, got, err)
		}
		return
	}
	common.FailIfErr(t, err)
	if *got != *want {
		t.Fatalf("%s jail record = %+v, want %+v", name, got, want)
	}
}

func expectJailCursor(t *testing.T, ctx *jailUpdateContext, want uint64) {
	t.Helper()
	cursor, err := definition.GetPillarJailCursor(ctx.Storage())
	common.FailIfErr(t, err)
	if cursor.NextTick != want {
		t.Fatalf("jail cursor = %d, want %d", cursor.NextTick, want)
	}
}

func TestUpdate_WritesNoJailBeforeSpork(t *testing.T) {
	ctx := newJailUpdateContext(t, false)
	method := &UpdateEmbeddedPillarMethod{MethodName: definition.UpdateMethodName}

	blocks, err := method.ReceiveBlock(ctx, updateSend())
	common.FailIfErr(t, err)
	if blocks != nil {
		t.Fatalf("descendant blocks = %v, want none", blocks)
	}
	expectNamedJail(t, ctx, "pillar-a", nil)
	expectNamedJail(t, ctx, "pillar-b", nil)
	expectNamedJail(t, ctx, "pillar-c", &definition.PillarJail{Name: "pillar-c", Jailed: true, Since: 42})
	expectNamedJail(t, ctx, "pillar-d", &definition.PillarJail{Name: "pillar-d", Jailed: false, Since: 700})
	expectNamedJail(t, ctx, "pillar-e", nil)
	expectNamedJail(t, ctx, "pillar-f", &definition.PillarJail{Name: "pillar-f", Jailed: false, Since: 1})
	if len(ctx.requested) != 0 {
		t.Fatalf("FinishedPeriodStats requested %v, want no request", ctx.requested)
	}
	if _, err := definition.GetPillarJailCursor(ctx.Storage()); err != constants.ErrDataNonExistent {
		t.Fatalf("jail cursor err = %v, want none stored", err)
	}
}

func TestUpdate_JailsInactivePillarsUnderSpork(t *testing.T) {
	ctx := newJailUpdateContext(t, true)
	method := &UpdateEmbeddedPillarMethod{MethodName: definition.UpdateMethodName}

	blocks, err := method.ReceiveBlock(ctx, updateSend())
	common.FailIfErr(t, err)
	if blocks != nil {
		t.Fatalf("descendant blocks = %v, want none", blocks)
	}
	// The first judgement covers ticks 2 to 13.
	expectNamedJail(t, ctx, "pillar-a", &definition.PillarJail{Name: "pillar-a", Jailed: true, Since: 100000})
	expectNamedJail(t, ctx, "pillar-b", nil)
	expectNamedJail(t, ctx, "pillar-c", &definition.PillarJail{Name: "pillar-c", Jailed: true, Since: 42})
	expectNamedJail(t, ctx, "pillar-d", &definition.PillarJail{Name: "pillar-d", Since: 700, MissedSlots: 3, FirstMissTick: 13})
	expectNamedJail(t, ctx, "pillar-e", nil)
	expectNamedJail(t, ctx, "pillar-f", &definition.PillarJail{Name: "pillar-f", Since: 1, MissedSlots: 3, FirstMissTick: 13})
	expectJailCursor(t, ctx, 14)
	if want := []uint64{1, constants.PillarJailWindowPeriods}; !reflect.DeepEqual(ctx.requested, want) {
		t.Fatalf("FinishedPeriodStats requested %v, want %v", ctx.requested, want)
	}
}

// A later judgement reads only the ticks finished since the last one,
// and streaks carry over.
func TestJailInactivePillars_JudgesOnlyNewTicks(t *testing.T) {
	ctx := newJailUpdateContext(t, true)
	common.FailIfErr(t, jailInactivePillars(ctx))

	// d keeps missing for a full window after tick 13; f produces.
	ctx.periodStats = append(ctx.periodStats, jailTicks(14, 25, func(tick uint64, counts map[string]jailCount) {
		counts["pillar-b"] = jailCount{produced: 1, expected: 1}
		counts["pillar-d"] = jailCount{expected: 1}
		counts["pillar-f"] = jailCount{produced: 1, expected: 1}
	})...)
	ctx.requested = nil
	common.FailIfErr(t, jailInactivePillars(ctx))

	if want := []uint64{1, 11}; !reflect.DeepEqual(ctx.requested, want) {
		t.Fatalf("FinishedPeriodStats requested %v, want %v", ctx.requested, want)
	}
	expectNamedJail(t, ctx, "pillar-d", &definition.PillarJail{Name: "pillar-d", Jailed: true, Since: 100000})
	expectNamedJail(t, ctx, "pillar-f", &definition.PillarJail{Name: "pillar-f", Since: 1})
	expectJailCursor(t, ctx, 25)

	// With no newly finished tick only the latest tick is read.
	ctx.requested = nil
	common.FailIfErr(t, jailInactivePillars(ctx))
	if want := []uint64{1}; !reflect.DeepEqual(ctx.requested, want) {
		t.Fatalf("FinishedPeriodStats requested %v, want %v", ctx.requested, want)
	}
}

// A never-jailed pillar whose streak ends keeps no record.
func TestJailInactivePillars_DeletesAClearedStreakOfANeverJailedPillar(t *testing.T) {
	ctx := newJailUpdateContext(t, true)
	common.FailIfErr(t, (&definition.PillarJail{Name: "pillar-b", MissedSlots: 2, FirstMissTick: 1}).Save(ctx.Storage()))
	common.FailIfErr(t, (&definition.PillarJailCursor{NextTick: 13}).Save(ctx.Storage()))

	common.FailIfErr(t, jailInactivePillars(ctx))
	expectNamedJail(t, ctx, "pillar-b", nil)
}

// Older unjudged ticks beyond the cap are skipped.
func TestJailInactivePillars_CapsTheTicksJudged(t *testing.T) {
	ctx := newJailUpdateContext(t, true)
	ctx.periodStats = jailTicks(0, 400, func(tick uint64, counts map[string]jailCount) {})
	common.FailIfErr(t, (&definition.PillarJailCursor{NextTick: 0}).Save(ctx.Storage()))

	common.FailIfErr(t, jailInactivePillars(ctx))
	if want := []uint64{1, constants.PillarJailMaxTicksPerUpdate}; !reflect.DeepEqual(ctx.requested, want) {
		t.Fatalf("FinishedPeriodStats requested %v, want %v", ctx.requested, want)
	}
	expectJailCursor(t, ctx, 400)
}
