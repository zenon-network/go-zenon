package consensus

import (
	"math/rand"
	"sort"

	"github.com/zenon-network/go-zenon/common/types"
)

// AlgorithmConfig is the input of one producer selection: the pillar
// delegations computed at the election's proof momentum and that
// momentum's hash-height, whose height seeds the deterministic PRNG.
type AlgorithmConfig struct {
	delegations []*types.PillarDelegation
	hashH       *types.HashHeight
}

// NewAlgorithmContext bundles the delegations and proof momentum
// identifier into the AlgorithmConfig consumed by SelectProducers.
func NewAlgorithmContext(delegations []*types.PillarDelegation, hashH *types.HashHeight) *AlgorithmConfig {
	return &AlgorithmConfig{
		delegations: delegations,
		hashH:       hashH,
	}
}

// ElectionAlgorithm fills the producer slots of one election tick:
// SelectProducers picks NodeCount pillars from the delegations in the
// context and returns them in slot order. The selection is a pure
// function of its inputs, so every node computes the same schedule.
type ElectionAlgorithm interface {
	SelectProducers(context *AlgorithmConfig) []*types.PillarDelegation
}

// electionAlgorithm implements ElectionAlgorithm. All randomness
// comes from math/rand generators seeded with the proof momentum's
// height. With the delegations sorted by descending weight:
//
//   - if there are fewer than NodeCount pillars, the whole set is
//     repeated in a fixed pseudo-random order until all slots are
//     filled;
//   - otherwise NodeCount - RandCount slots go to pillars drawn from
//     the NodeCount highest-weighted (group A), and RandCount slots
//     to pillars drawn (with the seed offset by one) from the
//     remainder plus the group-A pillars left unselected (group B).
//
// The chosen producers are then shuffled once more to fix the order
// in which the slots are assigned.
type electionAlgorithm struct {
	group *Context
}

// NewElectionAlgorithm returns the production election algorithm,
// parameterized by the consensus context's NodeCount and RandCount.
func NewElectionAlgorithm(group *Context) *electionAlgorithm {
	return &electionAlgorithm{
		group: group,
	}
}

// findSeed derives the deterministic PRNG seed for the election: the
// height of the proof momentum.
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

// shuffleOrder permutes the selected producers based on the seed,
// fixing the order in which the tick's slots are assigned.
func (ea *electionAlgorithm) shuffleOrder(producers []*types.PillarDelegation, context *AlgorithmConfig) (result []*types.PillarDelegation) {
	random := rand.New(rand.NewSource(ea.findSeed(context)))
	perm := random.Perm(len(producers))

	for _, v := range perm {
		result = append(result, producers[v])
	}

	return result
}

// filterByWeight splits the delegations into the NodeCount
// highest-weighted (groupA) and the remainder (groupB), sorting by
// descending weight only when a split is needed; with NodeCount
// pillars or fewer, the slice is returned unsorted as groupA
// (filterRandom sorts groupA again before use either way).
func (ea *electionAlgorithm) filterByWeight(context *AlgorithmConfig) (groupA []*types.PillarDelegation, groupB []*types.PillarDelegation) {
	if len(context.delegations) <= int(ea.group.NodeCount) {
		return context.delegations, groupB
	}

	sort.Sort(types.SortPDByWeight(context.delegations))
	groupA = context.delegations[0:ea.group.NodeCount]
	groupB = context.delegations[ea.group.NodeCount:]

	return groupA, groupB
}

// filterRandom selects the NodeCount producers: NodeCount - RandCount
// drawn from groupA and RandCount drawn from groupB plus the groupA
// pillars left unselected. When fewer than NodeCount pillars exist,
// groupA is repeated in a fixed pseudo-random order until all slots
// are filled.
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

	// Insert unselected pillars in groupB for a second chance at
	// being selected.
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
