package protocol

import (
	"math/big"
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/chain"
	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

// contentOrderedBlocks returns n distinct user blocks and a momentum whose
// content lists them, with the blocks in content order.
func contentOrderedBlocks(n int) (*nom.Momentum, []*nom.AccountBlock) {
	blocks := make([]*nom.AccountBlock, n)
	for i := range blocks {
		blocks[i] = &nom.AccountBlock{
			Version:         1,
			ChainIdentifier: 1,
			BlockType:       nom.BlockTypeUserSend,
			Address:         g.User1.Address,
			ToAddress:       g.User2.Address,
			Height:          uint64(i + 1),
			TokenStandard:   types.ZnnTokenStandard,
			Amount:          big.NewInt(1),
		}
		blocks[i].Hash = blocks[i].ComputeHash()
	}
	momentum := &nom.Momentum{Version: 1, ChainIdentifier: 1, Height: 2, Content: nom.NewMomentumContent(blocks)}
	byIdentifier := make(map[types.HashHeight]*nom.AccountBlock, n)
	for _, block := range blocks {
		byIdentifier[block.Identifier()] = block
	}
	ordered := make([]*nom.AccountBlock, n)
	for i, header := range momentum.Content {
		ordered[i] = byIdentifier[header.Identifier()]
	}
	return momentum, ordered
}

// TestValidatePrefetchedBlocks_CountIsTheVerifiersRule pins that the preflight
// imposes no block count of its own. Before dynamic plasma a momentum holds
// at most MaxAccountBlocksInMomentum blocks; after it, a plasma budget that
// allows more. Only the momentum verifier knows which rule applies to the
// momentum in hand, so a well-formed list above the legacy count must pass
// here and be judged there.
func TestValidatePrefetchedBlocks_CountIsTheVerifiersRule(t *testing.T) {
	momentum, blocks := contentOrderedBlocks(chain.MaxAccountBlocksInMomentum + 1)
	common.FailIfErr(t, validatePrefetchedBlocks(&nom.DetailedMomentum{Momentum: momentum, AccountBlocks: blocks}))
}

func TestValidatePrefetchedBlocks_StructuralChecksStillApply(t *testing.T) {
	momentum, blocks := contentOrderedBlocks(3)

	expectError := func(name string, detailed *nom.DetailedMomentum, want string) {
		t.Helper()
		err := validatePrefetchedBlocks(detailed)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: expected an error containing %q, got %v", name, want, err)
		}
	}

	expectError("nil momentum", &nom.DetailedMomentum{}, "missing momentum")
	expectError("length mismatch", &nom.DetailedMomentum{Momentum: momentum, AccountBlocks: blocks[:2]}, "do not match momentum content")
	withNil := append([]*nom.AccountBlock{}, blocks...)
	withNil[1] = nil
	expectError("nil entry", &nom.DetailedMomentum{Momentum: momentum, AccountBlocks: withNil}, "is nil")
	swapped := append([]*nom.AccountBlock{}, blocks...)
	swapped[0], swapped[1] = swapped[1], swapped[0]
	expectError("out of content order", &nom.DetailedMomentum{Momentum: momentum, AccountBlocks: swapped}, "does not match momentum content header")

	// A header listed twice, with the block repeated to match it, is a
	// duplicate even though every index matches.
	duplicated := &nom.Momentum{Version: 1, ChainIdentifier: 1, Height: 2, Content: append(append(nom.MomentumContent{}, momentum.Content...), momentum.Content[0])}
	expectError("duplicate header", &nom.DetailedMomentum{Momentum: duplicated, AccountBlocks: append(append([]*nom.AccountBlock{}, blocks...), blocks[0])}, "duplicate momentum content header")
}
