package consensus

import (
	"math/rand"
	"sort"

	"github.com/zenon-network/go-zenon/common/types"
)

type AlgorithmConfig struct {
	delegations []*types.PillarDelegation
	hashH       *types.HashHeight
}

func NewAlgorithmContext(delegations []*types.PillarDelegation, hashH *types.HashHeight) *AlgorithmConfig {
	return &AlgorithmConfig{
		delegations: delegations,
		hashH:       hashH,
	}
}

type ElectionAlgorithm interface {
	SelectProducers(context *AlgorithmConfig) []*types.PillarDelegation
}

type electionAlgorithm struct {
	group *Context
}

func NewElectionAlgorithm(group *Context) *electionAlgorithm {
	return &electionAlgorithm{
		group: group,
	}
}

// Generates a deterministic seed based on the context
// formula depends on seed, weights and momentumHeight
func (ea *electionAlgorithm) findSeed(context *AlgorithmConfig) int64 {
	return int64(context.hashH.Height)
}

func (ea *electionAlgorithm) SelectProducers(context *AlgorithmConfig) []*types.PillarDelegation {
	// Split into groups based on weight
	groupA, groupB := ea.filterByWeight(context)

	producers := ea.filterRandom(groupA, groupB, context)
	producers = ea.shuffleOrder(producers, context)

	return producers
}

// Shuffles the order in which momentums are produced, based on seed
func (ea *electionAlgorithm) shuffleOrder(producers []*types.PillarDelegation, context *AlgorithmConfig) (result []*types.PillarDelegation) {
	random := rand.New(rand.NewSource(ea.findSeed(context)))
	perm := random.Perm(len(producers))

	for _, v := range perm {
		result = append(result, producers[v])
	}

	return result
}

// Splits into 2 groups
func (ea *electionAlgorithm) filterByWeight(context *AlgorithmConfig) (groupA []*types.PillarDelegation, groupB []*types.PillarDelegation) {
	if len(context.delegations) <= int(ea.group.NodeCount) {
		return context.delegations, groupB
	}

	sort.Sort(types.SortPDByWeight(context.delegations))
	groupA = context.delegations[0:ea.group.NodeCount]
	groupB = context.delegations[ea.group.NodeCount:]

	return groupA, groupB
}

// Applies RandCount rules
func (ea *electionAlgorithm) filterRandom(groupA, groupB []*types.PillarDelegation, context *AlgorithmConfig) []*types.PillarDelegation {
	var result []*types.PillarDelegation
	total := int(ea.group.NodeCount)
	sort.Sort(types.SortPDByWeight(groupA))
	sort.Sort(types.SortPDByWeight(groupB))

	seed := ea.findSeed(context)
	// Number of active pillars is lower that the number of nodes in the consensus group.
	// Fill up result as many times as needed so there are no empty spots.
	if total != len(groupA) {
		for len(result) < total {
			random1 := rand.New(rand.NewSource(seed))
			arr := random1.Perm(len(groupA))
			for _, index := range arr {
				result = append(result, groupA[index])
			}
		}
		return result[:total]
	}

	// Select top pillars
	topTotal := total - int(ea.group.RandCount)
	topIndex := rand.New(rand.NewSource(seed)).Perm(len(groupA))

	for index := 0; index < topTotal; index += 1 {
		result = append(result, groupA[topIndex[index]])
	}

	// Insert unselected pillars in groupB for a second chx at becoming pillars.
	for index := topTotal; index < total; index += 1 {
		groupB = append(groupB, groupA[topIndex[index]])
	}

	// Select random pillars.
	randomIndex := rand.New(rand.NewSource(seed + 1)).Perm(len(groupB))[:ea.group.RandCount]
	for _, v := range randomIndex {
		promotion := groupB[v]
		result = append(result, promotion)
	}

	return result
}
