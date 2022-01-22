package consensus

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
)

// Returns pillar name for the ith pillar
func pillarName(i int) string {
	return fmt.Sprintf("pillar_%d", i)
}

// Generates delegations for numPillars. `pillar_0` has the highest weight
func generateDelegationInfo(numPillars int) (delegations []*types.PillarDelegation) {
	for i := 0; i < numPillars; i++ {
		delegations = append(delegations, &types.PillarDelegation{
			Name:   pillarName(i),
			Weight: big.NewInt(1000 - int64(i)),
		})
	}
	return delegations
}

// Merges produced momentums in this chains (tmp) into merger map
func mergeProducedNum(merged map[string]int, tmp []*types.PillarDelegation) {
	for _, v := range tmp {
		merged[v.Name] = merged[v.Name] + 1
	}
}

// Given 2 maps of produced momentums, throws error if min(a,b)/min(a,b) < chx
func checkExpectedResults(t *testing.T, a, b map[string]int, chx float64) {
	for name := range a {
		numA := a[name]
		numB := b[name]
		if numA > numB {
			numA, numB = numB, numA
		}

		if float64(numA)/float64(numB) < chx {
			t.Errorf("Too much discrepancy between expected %v and actual %v", a[name], b[name])
		}
	}
}

// Checks whenever 2 slices are equal
func checkUnequalOrder(t *testing.T, a, b []*types.PillarDelegation) {
	if len(a) != len(b) {
		return
	}

	for i := range a {
		if a[i].Name != b[i].Name {
			return
		}
	}
	t.Errorf("Expected 2 different orders. Error")
}

// Checks that
// 1. There are always Context.NodeCount nodes returned by the election algorithm even when there are less of them.
// 2. The produced blocks are distributed uniformly among them.
func TestAlgo_fillPillars(t *testing.T) {
	constants.ConsensusConfig = &constants.Consensus{
		BlockTime:   1,
		NodeCount:   5,
		RandCount:   2,
		CountingZTS: types.ZnnTokenStandard,
	}
	smallCG := NewConsensusContext(time.Unix(2000000000, 0))
	for numPillars := 1; numPillars <= 5; numPillars++ {
		delegations := generateDelegationInfo(numPillars)
		numIterations := 12000

		ag := NewElectionAlgorithm(smallCG)
		merged := make(map[string]int)
		for j := 0; j < numIterations; j++ {
			tmp := ag.SelectProducers(NewAlgorithmContext(delegations, &types.HashHeight{Height: uint64(j)}))
			mergeProducedNum(merged, tmp)
		}

		totalBlocks := 5 * numIterations

		var expected = map[string]int{}
		for j := 0; j < numPillars; j++ {
			expected[pillarName(j)] = totalBlocks / numPillars
		}
		checkExpectedResults(t, expected, merged, 0.98)
	}
}

func TestAlgo_seed(t *testing.T) {
	constants.ConsensusConfig = &constants.Consensus{
		BlockTime:   1,
		NodeCount:   5,
		RandCount:   2,
		CountingZTS: types.ZnnTokenStandard,
	}
	smallCG := NewConsensusContext(time.Unix(2000000000, 0))
	ag := NewElectionAlgorithm(smallCG)

	numPillars := 3
	delegations := generateDelegationInfo(numPillars)
	// Check seed for height
	{
		tmp_1 := ag.SelectProducers(NewAlgorithmContext(delegations, &types.HashHeight{Height: uint64(1)}))
		tmp_2 := ag.SelectProducers(NewAlgorithmContext(delegations, &types.HashHeight{Height: uint64(2)}))
		checkUnequalOrder(t, tmp_1, tmp_2)
	}
}

func pillarBlockProductionChx(cg *Context, numPillars int) (topPillarChx, restChx float64) {
	topPercentage := float64(cg.NodeCount-cg.RandCount) / float64(cg.NodeCount)
	restChx = float64(cg.RandCount) / float64(numPillars-int(cg.NodeCount-cg.RandCount))
	topPillarChx = topPercentage + (1.0-topPercentage)*restChx
	return
}

// Checks distribution of momentum production on multiple pillars
func TestAlgo_bigDistribution(t *testing.T) {
	constants.ConsensusConfig = &constants.Consensus{
		BlockTime:   1,
		NodeCount:   30,
		RandCount:   15,
		CountingZTS: types.ZnnTokenStandard,
	}
	cg := NewConsensusContext(time.Unix(2000000000, 0))

	for _, numPillars := range []int{31, 50, 75, 100, 150} {
		delegations := generateDelegationInfo(numPillars)
		numIterations := 12000

		ag := NewElectionAlgorithm(cg)
		merged := make(map[string]int)
		for j := 0; j < numIterations; j++ {
			tmp := ag.SelectProducers(NewAlgorithmContext(delegations, &types.HashHeight{Height: uint64(j)}))
			mergeProducedNum(merged, tmp)
		}

		top, rest := pillarBlockProductionChx(cg, numPillars)

		var expected = map[string]int{}
		for j := 0; j < 30; j++ {
			expected[pillarName(j)] = int(float64(numIterations) * top)
		}
		for j := 30; j < numPillars; j++ {
			expected[pillarName(j)] = int(float64(numIterations) * rest)
		}
		checkExpectedResults(t, expected, merged, 0.9)
	}
}
