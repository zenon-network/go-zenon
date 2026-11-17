package implementation

import (
	"sort"

	"github.com/zenon-network/go-zenon/consensus/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

// jailStreak is a pillar's run of missed eligible slots: missed counts
// them and first is the election tick of the first. missed is 0 when
// the pillar has no such run.
type jailStreak struct {
	missed uint64
	first  uint64
}

// advanceJailStreaks walks periods, oldest first, and updates the
// streak of every pillar in streaks. A tick in which no momentum was
// produced is skipped. A pillar that produced in a tick loses its
// streak. A pillar that was eligible and elected in a tick and produced
// nothing adds its elected slots to its streak, and the first such tick
// starts the streak. Names absent from streaks are ignored.
func advanceJailStreaks(periods []*api.PeriodStats, streaks map[string]*jailStreak) {
	for _, period := range periods {
		var blocks uint64
		for _, s := range period.Pillars {
			blocks += s.BlockNum
		}
		if blocks == 0 {
			continue
		}
		for name, streak := range streaks {
			s, ok := period.Pillars[name]
			if !ok {
				continue
			}
			if s.BlockNum > 0 {
				*streak = jailStreak{}
				continue
			}
			if s.Eligible && s.ExpectedBlockNum > 0 {
				if streak.missed == 0 {
					streak.first = period.Tick
				}
				streak.missed += s.ExpectedBlockNum
			}
		}
	}
}

// streakJails reports whether a streak jails its pillar once the ticks
// up to lastTick are judged: at least constants.PillarJailMinMissedSlots
// missed slots, the first of them at least
// constants.PillarJailWindowPeriods ticks before the end of lastTick.
func streakJails(streak *jailStreak, lastTick uint64) bool {
	return streak.missed >= constants.PillarJailMinMissedSlots &&
		lastTick+1-streak.first >= constants.PillarJailWindowPeriods
}

// jailInactivePillars judges the finished election ticks the jail rule
// has not judged yet, at most constants.PillarJailMaxTicksPerUpdate of
// them, and jails every active, non-jailed pillar whose streak then
// jails it (see advanceJailStreaks and streakJails). The first
// judgement covers the last constants.PillarJailWindowPeriods finished
// ticks. A jailed pillar's record is saved with Jailed set, Since set
// to the context's momentum time and its streak cleared. Other records
// are saved when their streak changes, and a record left with no jail
// history and no streak is deleted. Records are written in sorted name
// order. Storage and reader errors are returned.
func jailInactivePillars(context vm_context.AccountVmContext) error {
	latestStats, err := context.FinishedPeriodStats(1)
	if err != nil {
		return err
	}
	if len(latestStats) == 0 {
		return nil
	}
	end := latestStats[0].Tick + 1

	next := uint64(0)
	if end > constants.PillarJailWindowPeriods {
		next = end - constants.PillarJailWindowPeriods
	}
	cursor, err := definition.GetPillarJailCursor(context.Storage())
	if err == nil {
		next = cursor.NextTick
	} else if err != constants.ErrDataNonExistent {
		return err
	}
	if next >= end {
		return nil
	}
	count := end - next
	if count > constants.PillarJailMaxTicksPerUpdate {
		count = constants.PillarJailMaxTicksPerUpdate
	}
	periods, err := context.FinishedPeriodStats(count)
	if err != nil {
		return err
	}

	pillars, err := definition.GetPillarsList(context.Storage(), true, definition.AnyPillarType)
	if err != nil {
		return err
	}
	records := make(map[string]*definition.PillarJail, len(pillars))
	for _, pillar := range pillars {
		records[pillar.Name] = &definition.PillarJail{Name: pillar.Name}
	}
	stored := make(map[string]bool)
	jails, err := definition.GetPillarJailList(context.Storage())
	if err != nil {
		return err
	}
	for _, jail := range jails {
		if _, ok := records[jail.Name]; ok {
			records[jail.Name] = jail
			stored[jail.Name] = true
		}
	}

	streaks := make(map[string]*jailStreak, len(records))
	for name, record := range records {
		if !record.Jailed {
			streaks[name] = &jailStreak{missed: record.MissedSlots, first: record.FirstMissTick}
		}
	}
	advanceJailStreaks(periods, streaks)

	frontier, err := context.GetFrontierMomentum()
	if err != nil {
		return err
	}
	names := make([]string, 0, len(streaks))
	for name := range streaks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		record := records[name]
		streak := streaks[name]
		switch {
		case streakJails(streak, end-1):
			record.Jailed = true
			record.Since = frontier.Timestamp.Unix()
			record.MissedSlots = 0
			record.FirstMissTick = 0
		case streak.missed == record.MissedSlots && streak.first == record.FirstMissTick:
			continue
		default:
			record.MissedSlots = streak.missed
			record.FirstMissTick = streak.first
		}
		if !record.Jailed && record.Since == 0 && record.MissedSlots == 0 {
			if stored[name] {
				if err := record.Delete(context.Storage()); err != nil {
					return err
				}
			}
			continue
		}
		if err := record.Save(context.Storage()); err != nil {
			return err
		}
	}

	return (&definition.PillarJailCursor{NextTick: end}).Save(context.Storage())
}
