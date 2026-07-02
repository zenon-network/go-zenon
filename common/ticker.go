package common

import (
	"time"

	"github.com/pkg/errors"
)

// Ticker maps wall-clock time onto consecutive fixed-length intervals
// (ticks) counted from a start time, with tick 0 covering the first
// interval. The consensus layer builds its election-tick and epoch
// schedules as tickers anchored at the genesis timestamp. Each tick
// spans [startTime, endTime), end exclusive.
//
// ToTime returns the start and end time of the given tick. ToTick is
// the inverse, returning the tick whose interval contains t; t must not
// precede the ticker's start time and the interval arithmetic operates
// at whole-second granularity. TickMultiplier returns how many of this
// ticker's ticks make up one tick of bigger; it errors if the two
// tickers have different start times, if this ticker's interval is the
// longer one, or if the bigger interval is not an exact multiple of
// this one.
type Ticker interface {
	// ToTime returns [startTime, endTime) for tick
	ToTime(tick uint64) (time.Time, time.Time)
	ToTick(t time.Time) uint64
	TickMultiplier(bigger Ticker) (uint64, error)
}

type ticker struct {
	startTime time.Time
	interval  time.Duration
}

func (t ticker) ToTime(tick uint64) (time.Time, time.Time) {
	sTime := t.startTime.Add(t.interval * time.Duration(tick))
	eTime := t.startTime.Add(t.interval * time.Duration(tick+1))
	return sTime, eTime
}
func (t ticker) ToTick(time time.Time) uint64 {
	subSec := int64(time.Sub(t.startTime).Seconds())
	i := uint64(subSec) / uint64(t.interval.Seconds())
	return i
}
func (t ticker) TickMultiplier(bigger Ticker) (uint64, error) {
	cStart, cEnd := t.ToTime(0)
	bStart, bEnd := bigger.ToTime(0)
	if cStart != bStart {
		return 0, errors.Errorf("ticker error - start time is different - can't convert ticks")
	}

	cDuration := cEnd.UnixNano() - cStart.UnixNano()
	bDuration := bEnd.UnixNano() - bStart.UnixNano()

	if cDuration > bDuration {
		return 0, errors.Errorf("ticker error - callee has bigger thick duration than argument")
	}

	if bDuration%cDuration != 0 {
		return 0, errors.Errorf("ticker error - can't convert - small duration(ns) %v - big duration(ns) %v", cDuration, bDuration)
	}

	return uint64(bDuration / cDuration), nil
}

// NewTicker returns a Ticker whose tick 0 starts at startTime and whose
// ticks are interval long. The interval must be a whole number of
// seconds, at least one, for ToTick to divide correctly.
func NewTicker(startTime time.Time, interval time.Duration) Ticker {
	return &ticker{startTime: startTime, interval: interval}
}
