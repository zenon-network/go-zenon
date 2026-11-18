package verifier

import (
	"errors"
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// The dynamic-plasma content verdicts added for issue #299 must come out
// of the production checks in content(), not just be recognised in
// isolation: with the pre-#299 plain errors.Errorf returns the errors do
// not wrap the sentinels, so these tests go red if the production change
// is reverted. IsValidationError is asserted alongside errors.Is because
// classification is what makes the protocol layer remember the bytes and
// quarantine the peer.

func dpVerdictBlock(addr types.Address) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       addr,
		Hash:          types.NewHash([]byte("dp-verdict-block")),
		Height:        1,
		BlockType:     nom.BlockTypeUserSend,
		FusedPlasma:   1_000_000, // well above the minimum price
		TokenStandard: types.ZeroTokenStandard,
		Amount:        big.NewInt(0),
	}
}

// dpVerdictRmv builds the verifier for a single user block priced at the
// canonical base plasma 21000: at FusedPlasmaTarget the next momentum
// holds both resource prices at the 1000 floor.
func dpVerdictRmv(block *nom.AccountBlock, momentumStore store.Momentum) *rawMomentumVerifier {
	return &rawMomentumVerifier{
		momentum: &nom.Momentum{
			Version:         2, // dynamic-plasma momentum
			NextFusionPrice: 1000,
			NextWorkPrice:   1000,
			Content: nom.MomentumContent{
				{Address: block.Address, HashHeight: types.HashHeight{Hash: block.Hash, Height: block.Height}},
			},
		},
		accountBlocks: []*nom.AccountBlock{block},
		momentumStore: momentumStore,
		canonicalBasePlasma: func(chain.Chain, *nom.AccountBlock) (uint64, error) {
			return 21000, nil
		},
	}
}

// Positive control for the mismatch tests below: the same fixture with
// the prices the block's plasma actually implies is accepted.
func TestRawMomentumVerifier_Content_DPPricesMatch_Accepted(t *testing.T) {
	addr := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	block := dpVerdictBlock(addr)

	if err := dpVerdictRmv(block, &stubMomentumStore{}).content(true); err != nil {
		t.Fatalf("prices matching the block's plasma: got %v, want nil", err)
	}
}

func TestRawMomentumVerifier_Content_FusionPriceMismatch_Rejected(t *testing.T) {
	addr := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	block := dpVerdictBlock(addr)

	rmv := dpVerdictRmv(block, &stubMomentumStore{})
	rmv.momentum.NextFusionPrice = 2000 // above the floor, so only the content check can refuse it

	err := rmv.content(true)
	if !errors.Is(err, ErrMFusionPriceMismatch) {
		t.Fatalf("fusion price one step off target: got %v, want %v", err, ErrMFusionPriceMismatch)
	}
	if !IsValidationError(err) {
		t.Fatalf("fusion price mismatch must classify as a validation verdict: %v", err)
	}
}

func TestRawMomentumVerifier_Content_WorkPriceMismatch_Rejected(t *testing.T) {
	addr := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	block := dpVerdictBlock(addr)

	rmv := dpVerdictRmv(block, &stubMomentumStore{})
	rmv.momentum.NextWorkPrice = 2000

	err := rmv.content(true)
	if !errors.Is(err, ErrMWorkPriceMismatch) {
		t.Fatalf("work price one step off target: got %v, want %v", err, ErrMWorkPriceMismatch)
	}
	if !IsValidationError(err) {
		t.Fatalf("work price mismatch must classify as a validation verdict: %v", err)
	}
}

func TestRawMomentumVerifier_Content_BlockPriceTooSmall_Rejected(t *testing.T) {
	addr := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	block := dpVerdictBlock(addr)
	block.FusedPlasma = 1 // far below the floor for 21000 units of base plasma

	err := dpVerdictRmv(block, &stubMomentumStore{}).content(true)
	if !errors.Is(err, ErrMBlockPriceTooSmall) {
		t.Fatalf("underpriced block: got %v, want %v", err, ErrMBlockPriceTooSmall)
	}
	if !IsValidationError(err) {
		t.Fatalf("underpriced block must classify as a validation verdict: %v", err)
	}
}

// contractCapMomentumStore sets MaxBasePlasmaInMomentum so the implied
// contract-block cap is exactly 2 (2*EmbeddedSimplePlasma /
// EmbeddedSimplePlasma).
type contractCapMomentumStore struct {
	stubMomentumStore
}

func (*contractCapMomentumStore) GetPlasmaVariables() (*definition.PlasmaVariables, error) {
	return &definition.PlasmaVariables{
		MaxBasePlasmaInMomentum: 2 * constants.EmbeddedSimplePlasma,
		FusedPlasmaTarget:       21000,
		PowPlasmaTarget:         21000,
		MaxPriceChangePercent:   10,
		PriceChangeDenominator:  20,
	}, nil
}

func TestRawMomentumVerifier_Content_ContractBlocksCap_Rejected(t *testing.T) {
	contract := types.ParseAddressPanic("z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae") // liquidity
	if !types.IsEmbeddedAddress(contract) {
		t.Fatal("test setup: address is not embedded")
	}

	blocks := make([]*nom.AccountBlock, 0, 3)
	content := nom.MomentumContent{}
	for i := 0; i < 3; i++ {
		block := &nom.AccountBlock{
			Address: contract,
			Hash:    types.NewHash([]byte{'e', byte('0' + i)}),
			Height:  uint64(i + 1),
		}
		blocks = append(blocks, block)
		content = append(content, &types.AccountHeader{
			Address:    contract,
			HashHeight: types.HashHeight{Hash: block.Hash, Height: block.Height},
		})
	}

	rmv := &rawMomentumVerifier{
		momentum: &nom.Momentum{
			Version:         2,
			NextFusionPrice: 1000,
			NextWorkPrice:   1000,
			Content:         content,
		},
		accountBlocks: blocks,
		momentumStore: &contractCapMomentumStore{},
		canonicalBasePlasma: func(chain.Chain, *nom.AccountBlock) (uint64, error) {
			t.Fatal("embedded blocks must not be priced")
			return 0, nil
		},
	}

	err := rmv.content(true)
	if !errors.Is(err, ErrMContractBlocksTooMany) {
		t.Fatalf("3 contract blocks with cap 2: got %v, want %v", err, ErrMContractBlocksTooMany)
	}
	if !IsValidationError(err) {
		t.Fatalf("contract-block cap must classify as a validation verdict: %v", err)
	}
}
