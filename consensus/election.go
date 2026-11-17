package consensus

import (
	"time"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/storage"
)

var (
	// ErrElectionBeforeGenesis is returned when an election is
	// requested for a time or tick before the genesis timestamp.
	ErrElectionBeforeGenesis = errors.New("election time/tick before genesis timestamp")
)

func getMomentumBeforeTime(momentumStore store.Momentum, t time.Time) (*nom.Momentum, error) {
	block, err := momentumStore.GetMomentumBeforeTime(&t)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, errors.Errorf("no block before time %v", t.String())
	}
	return block, nil
}

type electionResult struct {
	STime       time.Time
	ETime       time.Time
	Producers   []*ProducerEvent
	Delegations []*types.PillarDelegation
	Tick        uint64
}

func generateProducers(info *Context, tick uint64, producerAddresses []types.Address) []*ProducerEvent {
	sTime, _ := info.ToTime(tick)
	var producers []*ProducerEvent

	if len(producerAddresses) != int(info.NodeCount) {
		return nil
	}

	for _, address := range producerAddresses {
		etime := sTime.Add(time.Duration(info.BlockTime) * time.Second)
		producers = append(producers, &ProducerEvent{
			StartTime: sTime,
			EndTime:   etime,
			Producer:  address,
		})
		sTime = etime
	}

	return producers
}
func genElectionResult(info *Context, tick uint64, data *storage.ElectionData) *electionResult {
	result := &electionResult{
		Tick:        tick,
		Delegations: data.Delegations,
		Producers:   generateProducers(info, tick, data.Producers),
	}
	result.STime, result.ETime = info.ToTime(tick)
	return result
}

type electionManager struct {
	log common.Logger
	Context

	chain chain.Chain
	algo  ElectionAlgorithm
	db    *storage.DB
}

// ElectionReader exposes election results on the election-tick
// schedule and is itself the Ticker of that schedule (one tick =
// BlockTime * NodeCount seconds from genesis).
//
// Every tick's election is derived from a proof momentum: the latest
// momentum before the end of tick-2 (one second past genesis for
// ticks 0 and 1). Sampling two ticks back makes the input final
// before the tick begins, so all nodes elect the same producers.
//
//   - ElectionByTime and ElectionByTick return the tick's producer
//     schedule and the delegations it was computed from.
//   - DelegationsByTick returns the detailed per-backer delegations
//     at the tick's proof momentum.
//
// Producers are drawn from the active pillars minus those jailed at the
// proof momentum, except when every active pillar is jailed or no
// momentum of tick-2 came from a pillar the election would keep (see
// jailFallback). The delegations always list every active pillar.
//
// The exported methods resolve the proof momentum on the frontier; the
// At variants resolve it on the momentum store they are given, so a
// caller pinned to one store (a period point, a FixedPillarReader) gets
// the proof momentum of that store's chain.
type ElectionReader interface {
	common.Ticker
	ElectionByTime(t time.Time) (*electionResult, error)
	ElectionByTick(tick uint64) (*electionResult, error)
	DelegationsByTick(tick uint64) ([]*types.PillarDelegationDetail, error)

	electionByTickAt(momentumStore store.Momentum, tick uint64) (*electionResult, error)
	delegationsByTickAt(momentumStore store.Momentum, tick uint64) ([]*types.PillarDelegationDetail, error)
	jailedAtProof(momentumStore store.Momentum, tick uint64) (map[string]bool, error)
}

func (em *electionManager) ElectionByTime(t time.Time) (*electionResult, error) {
	return em.electionByTimeAt(em.chain.GetFrontierMomentumStore(), t)
}
func (em *electionManager) ElectionByTick(tick uint64) (*electionResult, error) {
	return em.electionByTickAt(em.chain.GetFrontierMomentumStore(), tick)
}

// electionByTimeAt computes the election for the tick containing t as a
// chain ending at momentumStore's frontier sees it: the proof block is the
// last momentum of that chain before the tick's proof time.
func (em *electionManager) electionByTimeAt(momentumStore store.Momentum, t time.Time) (*electionResult, error) {
	if t.Before(em.GenesisTime) {
		return nil, ErrElectionBeforeGenesis
	}
	tick := em.ToTick(t)
	return em.electionByTickAt(momentumStore, tick)
}
func (em *electionManager) electionByTickAt(momentumStore store.Momentum, tick uint64) (*electionResult, error) {
	if int64(tick) < 0 {
		return nil, ErrElectionBeforeGenesis
	}
	if momentumStore == nil {
		return nil, errors.Errorf("no momentum store to read the election of tick %v from", tick)
	}
	proofTime := em.genProofTime(tick)
	proofBlock, err := getMomentumBeforeTime(momentumStore, proofTime)
	if err != nil {
		em.log.Error("GetMomentumBeforeTime failed", "reason", err)
		return nil, err
	}

	em.log.Debug("election", "tick", tick, "hash", proofBlock.Hash, "time", proofTime)

	data, err := em.generateProducers(proofBlock, tick)
	if err != nil {
		em.log.Error("generateProducers failed", "reason", err)
		return nil, err
	}

	result := genElectionResult(&em.Context, tick, data)

	// Set name to plan members
	registerMap := make(map[types.Address]string)
	for _, v := range data.Delegations {
		registerMap[v.Producing] = v.Name
	}
	for _, p := range result.Producers {
		name, ok := registerMap[p.Producer]
		if ok {
			p.Name = name
		} else {
			em.log.Error("pillar name-lookup failed", "reason", "can't find name for address", "producing-address", p.Producer)
			return nil, errors.Errorf("pillar name-lookup failed. reason: can't find name for producing-address %v", p.Producer)
		}
	}

	return result, nil
}
func (em *electionManager) DelegationsByTick(tick uint64) ([]*types.PillarDelegationDetail, error) {
	return em.delegationsByTickAt(em.chain.GetFrontierMomentumStore(), tick)
}

// delegationsByTickAt returns the detailed delegations at the proof
// momentum of tick as momentumStore's chain sees it.
func (em *electionManager) delegationsByTickAt(momentumStore store.Momentum, tick uint64) ([]*types.PillarDelegationDetail, error) {
	if momentumStore == nil {
		return nil, errors.Errorf("no momentum store to read the delegations of tick %v from", tick)
	}
	proofTime := em.genProofTime(tick)
	proofBlock, err := getMomentumBeforeTime(momentumStore, proofTime)
	if err != nil {
		em.log.Error("GetMomentumBeforeTime failed", "reason", err)
		return nil, err
	}
	proofStore := em.chain.GetMomentumStore(proofBlock.Identifier())
	if proofStore == nil {
		return nil, errors.Errorf("no momentum store at proof momentum %v of tick %v", proofBlock.Identifier(), tick)
	}

	return proofStore.ComputePillarDelegations()
}
func (em *electionManager) genProofTime(tick uint64) time.Time {
	if tick < 2 {
		return em.GenesisTime.Add(time.Second)
	}
	_, endTime := em.ToTime(tick - 2)
	return endTime
}

// excludeJailed returns delegations minus the jailed names, in the
// same order, in a newly allocated slice. It never writes to the
// backing array of delegations. It returns delegations itself when
// no entry is jailed or every entry is jailed.
func excludeJailed(delegations []*types.PillarDelegation, jailed map[string]bool) []*types.PillarDelegation {
	kept := make([]*types.PillarDelegation, 0, len(delegations))
	for _, delegation := range delegations {
		if !jailed[delegation.Name] {
			kept = append(kept, delegation)
		}
	}
	if len(kept) == 0 || len(kept) == len(delegations) {
		return delegations
	}
	return kept
}

// jailFallback reports whether the election of tick, proved by
// proofBlock, ignores pillar jails. It does when a pillar is jailed at
// the proof and no momentum of tick-2 up to the proof was produced by a
// pillar that is active and not jailed there: the pillars the election
// would keep have shown no sign of producing. With every active pillar
// jailed no pillar is kept, so the election always falls back. A
// fallback election keeps producing through the jailed pillars until
// one of the kept pillars produces again. Ticks 0 and 1 never fall
// back. The answer depends only on the chain up to proofBlock.
func (em *electionManager) jailFallback(proofStore store.Momentum, tick uint64, proofBlock *nom.Momentum, jailed map[string]bool) (bool, error) {
	if tick < 2 || len(jailed) == 0 {
		return false, nil
	}
	pillars, err := proofStore.GetActivePillars()
	if err != nil {
		return false, err
	}
	kept := make(map[types.Address]bool, len(pillars))
	for _, pillar := range pillars {
		if !jailed[pillar.Name] {
			kept[pillar.BlockProducingAddress] = true
		}
	}

	start, _ := em.ToTime(tick - 2)
	momentum := proofBlock
	for steps := 0; steps <= int(em.NodeCount); steps++ {
		if momentum.Timestamp.Before(start) {
			return true, nil
		}
		if kept[momentum.Producer()] {
			return false, nil
		}
		if momentum.Height <= 1 {
			return true, nil
		}
		momentum, err = proofStore.GetMomentumByHash(momentum.PreviousHash)
		if err != nil {
			return false, err
		}
		if momentum == nil {
			return false, errors.Errorf("missing momentum before %v while checking the fallback of tick %v", proofBlock.Identifier(), tick)
		}
	}
	return true, nil
}

// generateProducers returns the election of tick computed from
// proofBlock. Delegations always list every active pillar.
//
// Normally producers are selected from the active pillars minus those
// jailed at proofBlock, and the result is cached under the proof hash. When jailFallback holds,
// producers are selected from every active pillar with the seed offset
// by the ticks between the proof and tick, so a proof that stops
// moving draws a new schedule every tick. That result is cached under
// the proof hash and tick in its own namespace.
func (em *electionManager) generateProducers(proofBlock *nom.Momentum, tick uint64) (*storage.ElectionData, error) {
	hashH := types.HashHeight{Hash: proofBlock.Hash, Height: proofBlock.Height}

	proofStore := em.chain.GetMomentumStore(proofBlock.Identifier())
	if proofStore == nil {
		return nil, errors.Errorf("no momentum store at proof momentum %v", proofBlock.Identifier())
	}
	jailed, err := proofStore.GetJailedPillars()
	if err != nil {
		return nil, err
	}
	fallback, err := em.jailFallback(proofStore, tick, proofBlock, jailed)
	if err != nil {
		return nil, err
	}

	var cached *storage.ElectionData
	if fallback {
		cached, err = em.db.GetFallbackElectionResult(hashH.Hash, tick)
	} else {
		cached, err = em.db.GetElectionResultByHash(hashH.Hash)
	}
	if err != nil {
		return nil, err
	}
	if cached != nil {
		em.log.Debug("hit cache for compute producers", "hash", hashH.Height, "fallback", fallback)
		return cached, nil
	}

	delegationsDetailed, err := proofStore.ComputePillarDelegations()
	if err != nil {
		return nil, err
	}
	delegations := types.ToPillarDelegation(delegationsDetailed)

	var context *AlgorithmConfig
	if fallback {
		context = newFallbackAlgorithmContext(delegations, &hashH, tick-em.ToTick(*proofBlock.Timestamp))
	} else {
		context = NewAlgorithmContext(excludeJailed(delegations, jailed), &hashH)
	}
	finalProducers, err := em.algo.SelectProducers(context)
	if err != nil {
		return nil, err
	}
	producers := make([]types.Address, 0, len(finalProducers))
	for _, v := range finalProducers {
		producers = append(producers, v.Producing)
	}

	em.log.Info("computed producers", "proof-hash", hashH.Hash, "proof-height", hashH.Height, "fallback", fallback, "delegations", delegations, "producers", producers)

	electionData := storage.GenElectionData(producers, delegations)
	if fallback {
		err = em.db.StoreFallbackElectionResult(hashH.Hash, tick, electionData)
	} else {
		err = em.db.StoreElectionResultByHash(hashH.Hash, electionData)
	}
	if err != nil {
		return nil, err
	}
	return electionData, nil
}

// jailedAtProof returns the pillars jailed at the proof momentum of
// tick as momentumStore's chain sees it: the pillars the election of
// tick excluded unless it fell back.
func (em *electionManager) jailedAtProof(momentumStore store.Momentum, tick uint64) (map[string]bool, error) {
	if momentumStore == nil {
		return nil, errors.Errorf("no momentum store to read the jailed pillars of tick %v from", tick)
	}
	proofBlock, err := getMomentumBeforeTime(momentumStore, em.genProofTime(tick))
	if err != nil {
		return nil, err
	}
	proofStore := em.chain.GetMomentumStore(proofBlock.Identifier())
	if proofStore == nil {
		return nil, errors.Errorf("no momentum store at proof momentum %v of tick %v", proofBlock.Identifier(), tick)
	}
	return proofStore.GetJailedPillars()
}

// InsertMomentum pre-computes electionData when a tick is completed
func (em *electionManager) InsertMomentum(detailed *nom.DetailedMomentum) {
	block := detailed.Momentum

	tick := em.ToTick(*block.Timestamp)
	if tick == 0 {
		return
	}

	// tick - 1 is completed - cache the election of tick + 1, whose
	// proof is the last momentum of tick - 1
	_, eTime := em.ToTime(tick - 1)
	header, err := em.chain.GetFrontierMomentumStore().GetMomentumBeforeTime(&eTime)

	if err != nil {
		em.log.Error("failed to GetMomentumBeforeTime", "reason", err)
		return
	}

	_, err = em.generateProducers(header, tick+1)
	if err != nil {
		em.log.Error("failed to generateProducers", "reason", err)
		return
	}
}
func (em *electionManager) DeleteMomentum(*nom.DetailedMomentum) {
	// No need to worry about deleted momentums since electionData uses the proofBlock hash as a key
	return
}

func newElectionManager(chain chain.Chain, db *storage.DB) *electionManager {
	context := NewConsensusContext(*chain.GetGenesisMomentum().Timestamp)
	return &electionManager{
		Context: *context,
		chain:   chain,
		algo:    NewElectionAlgorithm(context),
		db:      db,
		log:     common.ConsensusLogger.New("submodule", "election-manager"),
	}
}
