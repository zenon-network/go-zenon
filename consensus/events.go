package consensus

import (
	"sync"
)

type eventManager struct {
	listeners []EventListener
	changes   sync.Mutex
}

func newEventManager() *eventManager {
	return &eventManager{
		listeners: make([]EventListener, 0),
	}
}

func (em *eventManager) broadcastNewProducerEvent(event ProducerEvent) {
	em.changes.Lock()
	defer em.changes.Unlock()

	for _, listener := range em.listeners {
		listener.NewProducerEvent(event)
	}
}
func (em *eventManager) Register(listener EventListener) {
	em.changes.Lock()
	defer em.changes.Unlock()

	em.listeners = append(em.listeners, listener)
}
func (em *eventManager) UnRegister(listener EventListener) {
	em.changes.Lock()
	defer em.changes.Unlock()

	for index, current := range em.listeners {
		if current == listener {
			em.listeners = append(em.listeners[:index], em.listeners[index+1:]...)
			break
		}
	}
}
