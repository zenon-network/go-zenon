package pillar

import (
	"errors"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/protocol"
)

type gateBroadcaster struct{ ready bool }

func (g *gateBroadcaster) SyncInfo() *protocol.SyncInfo {
	// Telemetry deliberately disagrees with the gate: production follows
	// ReadyToProduce, not the reported state.
	return &protocol.SyncInfo{State: protocol.Syncing, CurrentHeight: 1, TargetHeight: 2}
}
func (g *gateBroadcaster) ReadyToProduce() bool                                           { return g.ready }
func (g *gateBroadcaster) CreateMomentum(*nom.MomentumTransaction, *nom.DetailedMomentum) {}
func (g *gateBroadcaster) CreateAccountBlock(*nom.AccountBlockTransaction) error          { return nil }

func TestShouldProcess_FollowsReadyToProduce(t *testing.T) {
	m := &manager{broadcaster: &gateBroadcaster{ready: false}}
	if err := m.shouldProcess(consensus.ProducerEvent{}); !errors.Is(err, ErrSyncNotDone) {
		t.Fatalf("expected ErrSyncNotDone while not ready, got %v", err)
	}
	m = &manager{broadcaster: &gateBroadcaster{ready: true}}
	if err := m.shouldProcess(consensus.ProducerEvent{}); !errors.Is(err, ErrPillarNotDefined) {
		t.Fatalf("expected the gate to pass to the coinbase check, got %v", err)
	}
}
