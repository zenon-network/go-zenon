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

// jailTick builds the stats of the tick starting at start. Each count
// is {produced, expected}.
func jailTick(start int64, counts map[string][2]uint64) *api.PeriodStats {
	pillars := make(map[string]*api.PeriodPillarStats, len(counts))
	for name, c := range counts {
		pillars[name] = &api.PeriodPillarStats{BlockNum: c[0], ExpectedBlockNum: c[1]}
	}
	return &api.PeriodStats{StartTime: start, Pillars: pillars}
}

// jailTicks builds n ticks with StartTime 300*i. When withB is set,
// pillar B produces in every tick, so every tick holds a momentum
// unless fill removes B. fill adds the counts of tick i.
func jailTicks(n int, withB bool, fill func(i int, counts map[string][2]uint64)) []*api.PeriodStats {
	list := make([]*api.PeriodStats, 0, n)
	for i := 0; i < n; i++ {
		counts := map[string][2]uint64{}
		if withB {
			counts["B"] = [2]uint64{3, 3}
		}
		fill(i, counts)
		list = append(list, jailTick(int64(300*i), counts))
	}
	return list
}

func TestInactivePillars(t *testing.T) {
	zero := map[string]int64{"A": 0, "B": 0}
	only := func(ticks ...int) func(i int, counts map[string][2]uint64) {
		return func(i int, counts map[string][2]uint64) {
			for _, tick := range ticks {
				if tick == i {
					counts["A"] = [2]uint64{0, 3}
				}
			}
		}
	}

	stall := func() []*api.PeriodStats {
		return jailTicks(14, false, func(i int, counts map[string][2]uint64) {
			switch i {
			case 0, 1:
				counts["A"] = [2]uint64{3, 3}
				counts["U"] = [2]uint64{3, 3}
			case 4, 5, 8, 9, 12, 13:
				counts["J"] = [2]uint64{3, 3}
				counts["A"] = [2]uint64{0, 3}
				counts["U"] = [2]uint64{0, 3}
			}
		})
	}

	for _, tc := range []struct {
		name    string
		periods []*api.PeriodStats
		since   map[string]int64
		want    []string
	}{
		{
			"expected and none produced is selected",
			jailTicks(3, true, func(i int, c map[string][2]uint64) { c["A"] = [2]uint64{0, 3} }),
			zero, []string{"A"},
		},
		{
			"one production in a counted tick clears",
			jailTicks(3, true, func(i int, c map[string][2]uint64) {
				c["A"] = [2]uint64{uint64(i % 2), 3}
			}),
			zero, []string{},
		},
		{
			"zero expected is not a miss",
			jailTicks(3, true, func(i int, c map[string][2]uint64) { c["A"] = [2]uint64{0, 0} }),
			zero, []string{},
		},
		{
			"absent from every tick is not a miss",
			jailTicks(3, true, func(i int, c map[string][2]uint64) {}),
			zero, []string{},
		},
		{
			"tick without momentums counts for no one",
			jailTicks(3, true, func(i int, c map[string][2]uint64) {
				if i == 1 {
					delete(c, "B")
					c["A"] = [2]uint64{0, 3}
				}
			}),
			zero, []string{},
		},
		{
			"unelected producer name is ignored",
			jailTicks(3, true, func(i int, c map[string][2]uint64) {
				c[""] = [2]uint64{2, 0}
				c["A"] = [2]uint64{1, 3}
			}),
			zero, []string{},
		},
		{
			"names absent from since are ignored",
			jailTicks(3, true, func(i int, c map[string][2]uint64) { c["C"] = [2]uint64{0, 3} }),
			zero, []string{},
		},
		{
			"output is sorted by name",
			jailTicks(3, true, func(i int, c map[string][2]uint64) {
				c["c"] = [2]uint64{0, 3}
				c["a"] = [2]uint64{0, 3}
				c["b"] = [2]uint64{0, 3}
				c["D"] = [2]uint64{3, 3}
			}),
			map[string]int64{"a": 0, "b": 0, "c": 0, "D": 0}, []string{"a", "b", "c"},
		},
		{
			"only the last window of ticks counts",
			jailTicks(14, true, func(i int, c map[string][2]uint64) {
				if i < 2 {
					c["A"] = [2]uint64{1, 3}
				} else {
					c["A"] = [2]uint64{0, 3}
				}
			}),
			zero, []string{"A"},
		},
		{
			"production before the window does not protect and misses before it do not count",
			jailTicks(14, true, only(0, 1)),
			zero, []string{},
		},
		{
			"ticks whose T-2 started before the unjail do not count",
			jailTicks(6, true, only(2, 3)),
			map[string]int64{"A": 310, "B": 0}, []string{},
		},
		{
			"a counted tick whose T-2 started after the unjail selects",
			jailTicks(6, true, only(2, 3, 4)),
			map[string]int64{"A": 310, "B": 0}, []string{"A"},
		},
		{
			"unjail exactly at a tick start counts from two ticks later",
			jailTicks(5, true, only(4)),
			map[string]int64{"A": 600, "B": 0}, []string{"A"},
		},
		{
			"stale tick counts for an unjailed pillar whose proof tick started after the unjail",
			jailTicks(5, true, func(i int, c map[string][2]uint64) {
				if i == 2 {
					delete(c, "B")
				}
				if i == 4 {
					c["A"] = [2]uint64{0, 3}
				}
			}),
			map[string]int64{"A": 100, "B": 0}, []string{"A"},
		},
		{
			"stale tick whose proof tick started before the unjail does not count",
			jailTicks(5, true, func(i int, c map[string][2]uint64) {
				if i == 2 {
					delete(c, "B")
				}
				if i == 4 {
					c["A"] = [2]uint64{0, 3}
				}
			}),
			map[string]int64{"A": 310, "B": 0}, []string{},
		},
		{
			"ticks 0 and 1 do not count for an unjailed pillar",
			jailTicks(2, true, only(0, 1)),
			map[string]int64{"A": 1, "B": 0}, []string{},
		},
		{
			"a tick whose proof lies before periods does not count for an unjailed pillar",
			jailTicks(14, true, func(i int, c map[string][2]uint64) {
				if i == 0 {
					delete(c, "B")
				}
				if i == 2 {
					c["A"] = [2]uint64{0, 3}
				}
			}),
			map[string]int64{"A": 1, "B": 0}, []string{},
		},
		{
			"the same tick counts for a never-jailed pillar",
			jailTicks(14, true, func(i int, c map[string][2]uint64) {
				if i == 0 {
					delete(c, "B")
				}
				if i == 2 {
					c["A"] = [2]uint64{0, 3}
				}
			}),
			zero, []string{"A"},
		},
		{
			"a jail-induced stall judges never-jailed and unjailed offline pillars alike",
			stall(),
			map[string]int64{"A": 0, "U": 100}, []string{"A", "U"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := inactivePillars(tc.periods, tc.since)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("inactivePillars = %v, want %v", got, tc.want)
			}
		})
	}
}

// jailUpdateContext records the counts its FinishedPeriodStats is
// asked for.
type jailUpdateContext struct {
	*testEpochBoundContext
	requested []uint64
}

func (ctx *jailUpdateContext) FinishedPeriodStats(count uint64) ([]*api.PeriodStats, error) {
	ctx.requested = append(ctx.requested, count)
	return ctx.periodStats, nil
}

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

	ctx.periodStats = jailTicks(4, true, func(i int, counts map[string][2]uint64) {
		if i >= 1 {
			for _, name := range []string{"pillar-a", "pillar-c", "pillar-e"} {
				counts[name] = [2]uint64{0, 3}
			}
		}
		if i == 3 {
			counts["pillar-d"] = [2]uint64{0, 3}
			counts["pillar-f"] = [2]uint64{0, 3}
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
			t.Fatalf("%s jail record err = %v, want %v", name, err, constants.ErrDataNonExistent)
		}
		return
	}
	common.FailIfErr(t, err)
	if got.Name != want.Name || got.Jailed != want.Jailed || got.Since != want.Since {
		t.Fatalf("%s jail record = %+v, want %+v", name, got, want)
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
}

func TestUpdate_JailsInactivePillarsUnderSpork(t *testing.T) {
	ctx := newJailUpdateContext(t, true)
	method := &UpdateEmbeddedPillarMethod{MethodName: definition.UpdateMethodName}

	blocks, err := method.ReceiveBlock(ctx, updateSend())
	common.FailIfErr(t, err)
	if blocks != nil {
		t.Fatalf("descendant blocks = %v, want none", blocks)
	}
	expectNamedJail(t, ctx, "pillar-a", &definition.PillarJail{Name: "pillar-a", Jailed: true, Since: 100000})
	expectNamedJail(t, ctx, "pillar-b", nil)
	expectNamedJail(t, ctx, "pillar-c", &definition.PillarJail{Name: "pillar-c", Jailed: true, Since: 42})
	expectNamedJail(t, ctx, "pillar-d", &definition.PillarJail{Name: "pillar-d", Jailed: false, Since: 700})
	expectNamedJail(t, ctx, "pillar-e", nil)
	expectNamedJail(t, ctx, "pillar-f", &definition.PillarJail{Name: "pillar-f", Jailed: true, Since: 100000})
	if want := []uint64{14}; !reflect.DeepEqual(ctx.requested, want) {
		t.Fatalf("FinishedPeriodStats requested %v, want %v", ctx.requested, want)
	}
}
