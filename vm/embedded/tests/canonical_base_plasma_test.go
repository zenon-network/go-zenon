package tests

import (
	"math/big"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// vm.CanonicalBasePlasma must reproduce the exact value VM apply persists on
// AccountBlock.BasePlasma for each base-plasma shape, and must ignore the wire
// BasePlasma field entirely - proving the verifier's recomputation is
// bit-identical to what apply sets.
func TestCanonicalBasePlasma_MatchesAppliedValue(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	// 1. plain user send with non-empty Data to a non-contract address (data-length branch).
	// Sent to self so the later receive block (step 3) draws on User1's own genesis-fused
	// plasma rather than requiring a separate funded/plasma-holding recipient.
	dataSend := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User1.Address,
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(1 * g.Zexp),
		Data:          []byte("canonical-base-plasma-test"),
	}, nil, mock.SkipVmChanges)

	// 2. user send to an embedded method (method.GetPlasma branch)
	fuseSend := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User1.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)

	z.InsertNewMomentum() // apply and cement the two sends above

	// 3. user receive (constant branch)
	receive := z.InsertReceiveBlock(types.AccountHeader{
		Address: g.User1.Address,
		HashHeight: types.HashHeight{
			Hash:   dataSend.Hash,
			Height: dataSend.Height,
		},
	}, nil, nil, mock.SkipVmChanges)

	z.InsertNewMomentum() // apply and cement the receive block

	frontier := z.Chain().GetFrontierMomentumStore()

	stored := make([]*nom.AccountBlock, 0, 3)
	for _, sent := range []*nom.AccountBlock{dataSend, fuseSend, receive} {
		block, err := frontier.GetAccountBlockByHash(sent.Hash)
		common.FailIfErr(t, err)
		if block == nil {
			t.Fatalf("expected stored account-block for %v, got nil", sent.Identifier())
		}
		stored = append(stored, block)
	}

	// Positive assertion (bit-identity): the canonical recomputation must match
	// what apply persisted, for every base-plasma shape.
	for _, block := range stored {
		got, err := vm.CanonicalBasePlasma(z.Chain(), block)
		common.FailIfErr(t, err)
		if got != block.BasePlasma {
			t.Fatalf("canonical base plasma mismatch for %v: got %d, want %d", block.Identifier(), got, block.BasePlasma)
		}
	}

	// Negative/robustness assertion: the adapter must ignore the wire
	// BasePlasma field entirely, even when it's forged.
	tampered := stored[0]
	canonical := tampered.BasePlasma
	tampered.BasePlasma = canonical + 999999999

	got, err := vm.CanonicalBasePlasma(z.Chain(), tampered)
	common.FailIfErr(t, err)
	if got != canonical {
		t.Fatalf("expected canonical base plasma to ignore the forged wire value: got %d, want %d", got, canonical)
	}
}
