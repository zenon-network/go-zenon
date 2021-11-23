package pillar

import "github.com/pkg/errors"

var (
	ErrSyncNotDone        = errors.Errorf("sync is not done")
	ErrPillarNotDefined   = errors.Errorf("pillar has no producer address defined")
	ErrNotOurEvent        = errors.Errorf("not our event")
	ErrEventHasNotStarted = errors.Errorf("current time is before start time")
	ErrEventEnded         = errors.Errorf("current time is after the event's finish time time")
)
