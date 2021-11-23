package common

import "time"

var (
	Clock ClockType
)

type ClockType interface {
	Now() time.Time
}

// TODO: implementing `After(d time.Duration) <-chan time.Time` would allow us to better emulate the real world in a testing environment
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func init() {
	Clock = new(realClock)
}
