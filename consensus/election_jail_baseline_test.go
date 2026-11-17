package consensus

import (
	"errors"
	"testing"

	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

// With no jail record, every tick elects exactly the producers the
// algorithm selects from the full delegation list at the proof momentum.
func TestElection_NoJailRecordsMatchesPlainSelection(t *testing.T) {
	useJailTestConsensus(t)
	ch := newJailTestChain(0)
	em, _ := newJailTestElection(ch)

	for tick := uint64(0); tick <= 7; tick++ {
		got := jailTestProducers(t, jailTestElect(t, em, tick))

		proofBlock, err := getMomentumBeforeTime(ch.stores[len(ch.stores)-1], em.genProofTime(tick))
		common.FailIfErr(t, err)
		detailed, err := ch.GetMomentumStore(proofBlock.Identifier()).ComputePillarDelegations()
		common.FailIfErr(t, err)
		hashH := types.HashHeight{Hash: proofBlock.Hash, Height: proofBlock.Height}
		selected, err := em.algo.SelectProducers(NewAlgorithmContext(types.ToPillarDelegation(detailed), &hashH))
		common.FailIfErr(t, err)

		if len(got) != len(selected) {
			t.Fatalf("tick %d: got %d producers, want %d", tick, len(got), len(selected))
		}
		for i := range selected {
			if got[i] != selected[i].Producing {
				t.Fatalf("tick %d slot %d: got %v, want %v", tick, i, got[i], selected[i].Producing)
			}
		}
	}
}

// Jails apply on the first two ticks, which never fall back.
func TestElection_JailAppliesOnTicksZeroAndOne(t *testing.T) {
	useJailTestConsensus(t)
	em, _ := newJailTestElection(newJailTestChain(1, "pillar-c"))

	expectPillarC(t, em, 0, false)
	expectPillarC(t, em, 1, false)
}

type jailTestFailingStore struct {
	*jailTestStore
	err error
}

func (s *jailTestFailingStore) GetJailedPillars() (map[string]bool, error) {
	return nil, s.err
}

type jailTestFailingChain struct {
	*jailTestChain
	err error
}

func (c *jailTestFailingChain) GetMomentumStore(id types.HashHeight) store.Momentum {
	s := c.jailTestChain.GetMomentumStore(id)
	if s == nil {
		return nil
	}
	return &jailTestFailingStore{jailTestStore: s.(*jailTestStore), err: c.err}
}

// A failed jail read fails the election on a fallback tick and on a
// normal tick, and stores no result.
func TestElection_JailReadErrorFailsElection(t *testing.T) {
	useJailTestConsensus(t)
	ch := newJailTestChain(3, "pillar-c")
	failing := &jailTestFailingChain{jailTestChain: ch, err: errors.New("jail read failed")}
	em, consensusDB := newJailTestElection(ch)
	em.chain = failing

	for _, tick := range []uint64{4, 5} {
		if _, err := em.ElectionByTick(tick); err == nil {
			t.Fatalf("tick %d: election succeeded despite the jail read error", tick)
		}
	}
	stored, err := consensusDB.GetElectionResultByHash(ch.momentum(3).Hash)
	common.FailIfErr(t, err)
	if stored != nil {
		t.Fatal("a failed election stored a result")
	}
}
