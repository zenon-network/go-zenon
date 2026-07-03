package chain

import (
	"sync"

	"github.com/zenon-network/go-zenon/chain/nom"
)

// momentumEventManager implements MomentumEventManager: a
// mutex-guarded listener list to which the momentum pool broadcasts
// Insert/DeleteMomentum events synchronously, in registration order.
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

// Register appends listener to the broadcast list; registering the
// same listener twice delivers events twice.
func (em *momentumEventManager) Register(listener MomentumEventListener) {
	em.changes.Lock()
	defer em.changes.Unlock()

	em.listeners = append(em.listeners, listener)
}

// UnRegister removes the first registration of listener, if any.
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
