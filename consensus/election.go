package consensus

import (
	"time"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/storage"
)

var (
	ErrElectionBeforeGenesis = errors.New("election time/tick before genesis timestamp")
)

func getMomentumBeforeTime(chain chain.Chain, t time.Time) (*nom.Momentum, error) {
	block, err := chain.GetFrontierMomentumStore().GetMomentumBeforeTime(&t)
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
type ElectionReader interface {
	common.Ticker
	ElectionByTime(t time.Time) (*electionResult, error)
	ElectionByTick(tick uint64) (*electionResult, error)
	DelegationsByTick(tick uint64) ([]*types.PillarDelegationDetail, error)
}

func (em *electionManager) ElectionByTime(t time.Time) (*electionResult, error) {
	if t.Before(em.GenesisTime) {
		return nil, ErrElectionBeforeGenesis
	}
	tick := em.ToTick(t)
	return em.ElectionByTick(tick)
}
func (em *electionManager) ElectionByTick(tick uint64) (*electionResult, error) {
	if int64(tick) < 0 {
		return nil, ErrElectionBeforeGenesis
	}
	proofTime := em.genProofTime(tick)
	proofBlock, err := getMomentumBeforeTime(em.chain, proofTime)
	if err != nil {
		em.log.Error("GetMomentumBeforeTime failed", "reason", err)
		return nil, err
	}

	em.log.Debug("election", "tick", tick, "hash", proofBlock.Hash, "time", proofTime)

	data, err := em.generateProducers(proofBlock)
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
	proofTime := em.genProofTime(tick)
	proofBlock, err := getMomentumBeforeTime(em.chain, proofTime)
	if err != nil {
		em.log.Error("GetMomentumBeforeTime failed", "reason", err)
		return nil, err
	}
	store := em.chain.GetMomentumStore(proofBlock.Identifier())

	return store.ComputePillarDelegations()
}
func (em *electionManager) genProofTime(tick uint64) time.Time {
	if tick < 2 {
		return em.GenesisTime.Add(time.Second)
	}
	_, endTime := em.ToTime(tick - 2)
	return endTime
}

func (em *electionManager) generateProducers(proofBlock *nom.Momentum) (*storage.ElectionData, error) {
	hashH := types.HashHeight{Hash: proofBlock.Hash, Height: proofBlock.Height}
	store := em.chain.GetMomentumStore(proofBlock.Identifier())
	// load from cache
	cached, err := em.db.GetElectionResultByHash(hashH.Hash)
	if err != nil {
		return nil, err
	}
	if cached != nil {
		em.log.Debug("hit cache for compute producers", "hash", hashH.Height)
		return cached, nil
	}

	// get delegations
	delegationsDetailed, err := store.ComputePillarDelegations()
	if err != nil {
		return nil, err
	}
	delegations := types.ToPillarDelegation(delegationsDetailed)

	context := NewAlgorithmContext(delegations, &hashH)
	finalProducers := em.algo.SelectProducers(context)
	producers := make([]types.Address, 0, len(finalProducers))
	for _, v := range finalProducers {
		producers = append(producers, v.Producing)
	}

	em.log.Info("computed producers", "proof-hash", hashH.Hash, "proof-height", hashH.Height, "delegations", delegations, "producers", producers)

	// update cache
	electionData := storage.GenElectionData(producers, delegations)
	err = em.db.StoreElectionResultByHash(hashH.Hash, electionData)
	if err != nil {
		return nil, err
	}
	return electionData, nil
}

// InsertMomentum pre-computes electionData when a tick is completed
func (em *electionManager) InsertMomentum(detailed *nom.DetailedMomentum) {
	block := detailed.Momentum

	tick := em.ToTick(*block.Timestamp)
	if tick == 0 {
		return
	}

	// tick - 1 is completed - cache election results for later use
	_, eTime := em.ToTime(tick - 1)
	header, err := em.chain.GetFrontierMomentumStore().GetMomentumBeforeTime(&eTime)

	if err != nil {
		em.log.Error("failed to GetMomentumBeforeTime", "reason", err)
		return
	}

	_, err = em.generateProducers(header)
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
