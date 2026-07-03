package common

import (
	"sync"
	"time"
)

// Task is the caller-side handle of a background action started with
// NewTask. Wait blocks until the action has returned. Finished returns
// the channel that is closed when the action returns, for use in select
// statements. ForceStop requests cooperative cancellation: it signals
// the action's TaskResolver and returns immediately without waiting for
// the action to notice; it is safe to call multiple times and from
// multiple goroutines.
type Task interface {
	Wait()
	Finished() chan struct{}
	ForceStop()
}

// TaskResolver is the action-side view of a task. The action is expected
// to poll ShouldStop at convenient points — typically between expensive
// steps — and return early once it reports true, which happens after
// ForceStop has been called on the task. Cancellation is purely
// cooperative; nothing interrupts an action that never checks.
type TaskResolver interface {
	ShouldStop() bool
}

// NewTask starts action in a new goroutine and returns a handle for
// waiting on it or requesting that it stop. The task is finished when
// action returns; NewTask itself installs no panic recovery, so an
// action that panics without recovering crashes the process, and a
// recover-then-repanic helper such as RecoverStack only adds logging on
// the way down.
func NewTask(action func(TaskResolver)) Task {
	t := &task{
		forceClosed: make(chan struct{}),
		closed:      make(chan struct{}),
	}

	go func() {
		action(t)
		t.finish()
	}()

	return t
}

type task struct {
	forceClosed chan struct{}
	closed      chan struct{}
	changes     sync.Mutex
}

func (t *task) Wait() {
	for {
		select {
		case <-t.closed:
			return
		case <-time.After(time.Millisecond * 100):
		}
	}
}
func (t *task) Finished() chan struct{} {
	return t.closed
}
func (t *task) ForceStop() {
	t.changes.Lock()
	defer t.changes.Unlock()
	select {
	case <-t.forceClosed:
	default:
		close(t.forceClosed)
	}
}

func (t *task) ShouldStop() bool {
	select {
	case <-t.forceClosed:
		return true
	default:
		return false
	}
}
func (t *task) finish() {
	t.changes.Lock()
	defer t.changes.Unlock()
	close(t.closed)
}
