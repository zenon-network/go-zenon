package common

import "time"

// Clock is the process-wide time source. Production code initializes it
// to the real clock (backed by time.Now) and reads it wherever testable
// current time is needed; the mock framework in zenon/mock replaces it
// with a clock derived from the mock chain's frontier momentum so tests
// run on deterministic, chain-driven time. It is a plain package
// variable with no synchronization, so it must only be swapped during
// test setup.
var (
	Clock ClockType
)

// ClockType provides the current time. Its single implementation in
// production wraps time.Now; tests substitute their own (see Clock).
type ClockType interface {
	Now() time.Time
}

// TODO: implementing `After(d time.Duration) <-chan time.Time` would allow us to better emulate the real world in a testing environment
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func init() {
	Clock = new(realClock)
}
