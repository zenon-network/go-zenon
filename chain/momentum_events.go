package chain

import (
	"sync"

	"github.com/zenon-network/go-zenon/chain/nom"
)

type momentumEventManager struct {
	listeners []MomentumEventListener
	changes   sync.Mutex
}

func newMomentumEventManager() *momentumEventManager {
	return &momentumEventManager{
		listeners: make([]MomentumEventListener, 0),
	}
}

func (em *momentumEventManager) broadcastInsertMomentum(detailed *nom.DetailedMomentum) {
	em.changes.Lock()
	defer em.changes.Unlock()

	for _, listener := range em.listeners {
		listener.InsertMomentum(detailed)
	}
}
func (em *momentumEventManager) broadcastDeleteMomentum(detailed *nom.DetailedMomentum) {
	em.changes.Lock()
	defer em.changes.Unlock()

	for _, listener := range em.listeners {
		listener.DeleteMomentum(detailed)
	}
}

func (em *momentumEventManager) Register(listener MomentumEventListener) {
	em.changes.Lock()
	defer em.changes.Unlock()

	em.listeners = append(em.listeners, listener)
}
func (em *momentumEventManager) UnRegister(listener MomentumEventListener) {
	em.changes.Lock()
	defer em.changes.Unlock()

	for index, current := range em.listeners {
		if current == listener {
			em.listeners = append(em.listeners[:index], em.listeners[index+1:]...)
			break
		}
	}
}
