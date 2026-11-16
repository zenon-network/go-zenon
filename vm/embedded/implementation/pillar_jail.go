package implementation

import (
	"sort"

	"github.com/zenon-network/go-zenon/consensus/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

// inactivePillars returns, sorted by name, the pillars in since that
// were expected to produce at least one momentum and produced none
// across the counted ticks.
//
// periods is FinishedPeriodStats(PillarJailWindowPeriods+2), oldest
// first. Only its last PillarJailWindowPeriods entries are counted;
// earlier entries are read to find where a counted tick's election
// proof lies. When fewer entries exist, periods[0] is tick 0.
//
// A tick counts only when it held at least one momentum. since[name]
// is 0 for a pillar that has never been jailed and the unix time of
// its unjail otherwise. The election of tick T is proved by the last
// momentum before the end of T-2, which lies in the proof tick: T-2
// when T-2 held a momentum, otherwise the latest earlier tick that
// did, in which case T ran on the stall fallback that ignores jails.
// For a pillar with an unjail time, T also counts only when its proof
// tick is in periods and started at or after that time, so T's
// producers were elected from a momentum at or after the one that
// confirmed the unjail. Names absent from since are ignored.
func inactivePillars(periods []*api.PeriodStats, since map[string]int64) []string {
	held := func(p *api.PeriodStats) bool {
		var blocks uint64
		for _, s := range p.Pillars {
			blocks += s.BlockNum
		}
		return blocks > 0
	}

	expected := make(map[string]uint64)
	produced := make(map[string]uint64)
	first := 0
	if len(periods) > int(constants.PillarJailWindowPeriods) {
		first = len(periods) - int(constants.PillarJailWindowPeriods)
	}
	for i := first; i < len(periods); i++ {
		if !held(periods[i]) {
			continue
		}
		proof := -1
		for j := i - 2; j >= 0; j-- {
			if held(periods[j]) {
				proof = j
				break
			}
		}
		for name, s := range periods[i].Pillars {
			at, ok := since[name]
			if !ok {
				continue
			}
			if at > 0 && (proof < 0 || periods[proof].StartTime < at) {
				continue
			}
			expected[name] += s.ExpectedBlockNum
			produced[name] += s.BlockNum
		}
	}

	result := make([]string, 0)
	for name, num := range expected {
		if num > 0 && produced[name] == 0 {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result
}

// jailInactivePillars jails every active, non-jailed pillar that
// inactivePillars selects from the finished election ticks at the
// context's momentum, saving each record with Since set to that
// momentum's time. Storage and reader errors are returned.
func jailInactivePillars(context vm_context.AccountVmContext) error {
	pillars, err := definition.GetPillarsList(context.Storage(), true, definition.AnyPillarType)
	if err != nil {
		return err
	}
	since := make(map[string]int64, len(pillars))
	for _, pillar := range pillars {
		since[pillar.Name] = 0
	}

	jails, err := definition.GetPillarJailList(context.Storage())
	if err != nil {
		return err
	}
	for _, jail := range jails {
		if _, ok := since[jail.Name]; !ok {
			continue
		}
		if jail.Jailed {
			delete(since, jail.Name)
		} else {
			since[jail.Name] = jail.Since
		}
	}

	periods, err := context.FinishedPeriodStats(constants.PillarJailWindowPeriods + 2)
	if err != nil {
		return err
	}
	frontier, err := context.GetFrontierMomentum()
	if err != nil {
		return err
	}

	for _, name := range inactivePillars(periods, since) {
		jail := &definition.PillarJail{Name: name, Jailed: true, Since: frontier.Timestamp.Unix()}
		if err := jail.Save(context.Storage()); err != nil {
			return err
		}
	}
	return nil
}
