package common

import (
	"time"

	"github.com/pkg/errors"
)

// Ticker converts time units into ticks.
// End time of a thick is exclusive.
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

func NewTicker(startTime time.Time, interval time.Duration) Ticker {
	return &ticker{startTime: startTime, interval: interval}
}
