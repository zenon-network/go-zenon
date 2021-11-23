package common

import (
	"sync"
	"time"
)

type Task interface {
	Wait()
	Finished() chan struct{}
	ForceStop()
}
type TaskResolver interface {
	ShouldStop() bool
}

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
