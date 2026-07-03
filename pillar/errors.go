package pillar

import "github.com/pkg/errors"

// Errors returned by the producer when it declines to process a
// ProducerEvent. They are informational: the event is skipped rather than
// failing the node.
var (
	// ErrSyncNotDone is returned when the node has not finished syncing and
	// so must not produce momentums yet.
	ErrSyncNotDone        = errors.Errorf("sync is not done")
	ErrPillarNotDefined   = errors.Errorf("pillar has no producer address defined")
	ErrNotOurEvent        = errors.Errorf("not our event")
	ErrEventHasNotStarted = errors.Errorf("current time is before start time")
	ErrEventEnded         = errors.Errorf("current time is after the event's finish time time")
)
