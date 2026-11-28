package implementation

import (
	"sort"

	"github.com/zenon-network/go-zenon/consensus/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

// jailStreak is a pillar's run of missed eligible slots: missed counts
// them, and first and last are the election ticks of the first and the
// last. missed is 0 when the pillar has no such run. A tick that
// started before notBefore, the unix time of the pillar's last unjail,
// adds no miss.
type jailStreak struct {
	missed    uint64
	first     uint64
	last      uint64
	notBefore int64
}

// advanceJailStreaks walks periods, oldest first, and updates the
// streak of every pillar in streaks. A tick in which no momentum was
// produced is skipped. A pillar that produced in a tick loses its
// streak. A pillar that was eligible and elected in a tick that started
// at or after its notBefore, and produced nothing, adds its elected
// slots to its streak; the tick becomes the streak's last, and its
// first when the streak was empty. Names absent from streaks are
// ignored.
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
				streak.missed, streak.first, streak.last = 0, 0, 0
				continue
			}
			if s.Eligible && s.ExpectedBlockNum > 0 && period.StartTime >= streak.notBefore {
				if streak.missed == 0 {
					streak.first = period.Tick
				}
				streak.last = period.Tick
				streak.missed += s.ExpectedBlockNum
			}
		}
	}
}

// streakJails reports whether a streak jails its pillar: at least
// constants.PillarJailMinMissedSlots missed slots, with the first and
// the last miss at least constants.PillarJailWindowPeriods ticks apart.
func streakJails(streak *jailStreak) bool {
	return streak.missed >= constants.PillarJailMinMissedSlots &&
		streak.last-streak.first >= constants.PillarJailWindowPeriods
}

// jailInactivePillars judges the finished election ticks the jail rule
// has not judged yet and jails every active, non-jailed pillar whose
// streak then jails it (see advanceJailStreaks and streakJails). The
// first judgement covers the last constants.PillarJailWindowPeriods+1
// finished ticks, the fewest that can hold two misses
// constants.PillarJailWindowPeriods ticks apart. When more than constants.PillarJailMaxTicksPerUpdate
// ticks are unjudged, only the newest of them are judged and every
// carried streak is cleared first, since the skipped ticks may hold
// production. A pillar's misses count only in ticks that started at or
// after its Since, so an unjail is never followed by a jail for misses
// from before it. A jailed pillar's record is saved with Jailed set,
// Since set to the context's momentum time and its streak cleared.
// Other records are saved when their streak changes, and a record left
// with no jail history and no streak is deleted. Records are written in
// sorted name order. Storage and reader errors are returned.
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
	if end > constants.PillarJailWindowPeriods+1 {
		next = end - constants.PillarJailWindowPeriods - 1
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
	skipped := false
	if count > constants.PillarJailMaxTicksPerUpdate {
		count = constants.PillarJailMaxTicksPerUpdate
		skipped = true
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
		if record.Jailed {
			continue
		}
		streak := &jailStreak{notBefore: record.Since}
		if !skipped {
			streak.missed, streak.first, streak.last = record.MissedSlots, record.FirstMissTick, record.LastMissTick
		}
		streaks[name] = streak
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
		case streakJails(streak):
			record.Jailed = true
			record.Since = frontier.Timestamp.Unix()
			record.MissedSlots, record.FirstMissTick, record.LastMissTick = 0, 0, 0
		case streak.missed == record.MissedSlots && streak.first == record.FirstMissTick && streak.last == record.LastMissTick:
			continue
		default:
			record.MissedSlots, record.FirstMissTick, record.LastMissTick = streak.missed, streak.first, streak.last
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
