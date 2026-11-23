//go:build !race

package tests

// raceDetectorEnabled reports whether the test binary was built with -race.
// The race detector slows execution several times over, so wall-clock budgets
// measured without it do not hold.
const raceDetectorEnabled = false
