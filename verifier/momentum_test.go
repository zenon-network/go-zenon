package verifier

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// stubMomentumStore satisfies store.Momentum by embedding a nil interface and
// overriding only the methods content() exercises.
type stubMomentumStore struct {
	store.Momentum
}

func (*stubMomentumStore) GetFrontierAccountBlock(types.Address) (*nom.AccountBlock, error) {
	return nil, nil
}

func (*stubMomentumStore) GetMomentumByHash(types.Hash) (*nom.Momentum, error) {
	return &nom.Momentum{Version: 1}, nil
}

func (*stubMomentumStore) GetPlasmaVariables() (*definition.PlasmaVariables, error) {
	return &definition.PlasmaVariables{
		MaxBasePlasmaInMomentum: 1_000_000_000,
		FusedPlasmaTarget:       21000,
		PowPlasmaTarget:         21000,
		MaxPriceChangePercent:   10,
		PriceChangeDenominator:  20,
	}, nil
}

// A momentum content header with no matching entry in the prefetched
// account-blocks must be rejected with an error, not dereference a nil block.
func TestRawMomentumVerifier_Content_MissingAccountBlock_ReturnsError(t *testing.T) {
	address := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	headerHash := types.NewHash([]byte("header"))
	blockHash := types.NewHash([]byte("different-block"))

	momentum := &nom.Momentum{
		Content: nom.MomentumContent{
			{
				Address: address,
				HashHeight: types.HashHeight{
					Hash:   headerHash,
					Height: 1,
				},
			},
		},
	}
	accountBlocks := []*nom.AccountBlock{
		{
			Address: address,
			Hash:    blockHash,
			Height:  1,
		},
	}

	rmv := &rawMomentumVerifier{
		momentum:      momentum,
		accountBlocks: accountBlocks,
		momentumStore: &stubMomentumStore{},
	}

	err := rmv.content(false)
	if err == nil {
		t.Fatal("expected an error for a content header missing its account block, got nil")
	}
}

// A momentum content header must not be satisfied by more than one prefetched
// account-block, or dynamic-plasma price accounting could count it more than once.
func TestRawMomentumVerifier_Content_DuplicateAccountBlock_ReturnsError(t *testing.T) {
	address := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	blockHash := types.NewHash([]byte("block"))

	momentum := &nom.Momentum{
		Content: nom.MomentumContent{
			{
				Address: address,
				HashHeight: types.HashHeight{
					Hash:   blockHash,
					Height: 1,
				},
			},
		},
	}
	block := &nom.AccountBlock{
		Address: address,
		Hash:    blockHash,
		Height:  1,
	}
	accountBlocks := []*nom.AccountBlock{block, block}

	rmv := &rawMomentumVerifier{
		momentum:      momentum,
		accountBlocks: accountBlocks,
		momentumStore: &stubMomentumStore{},
	}

	err := rmv.content(true)
	if err == nil {
		t.Fatal("expected an error for a duplicated prefetched account-block, got nil")
	}
}

// Two content headers pointing at the same identifier must not both resolve to the
// same prefetched account-block, even when a distinct filler block keeps the
// lookup and content sizes equal.
func TestRawMomentumVerifier_Content_DuplicateContentHeader_ReturnsError(t *testing.T) {
	address := types.PlasmaContract
	blockAHash := types.NewHash([]byte("block-a"))
	blockBHash := types.NewHash([]byte("block-b"))

	header := types.AccountHeader{
		Address: address,
		HashHeight: types.HashHeight{
			Hash:   blockAHash,
			Height: 1,
		},
	}
	momentum := &nom.Momentum{
		Content: nom.MomentumContent{&header, &header},
	}
	// Batched contract sends skip chain-order validation, isolating the lookup-consumption check.
	accountBlocks := []*nom.AccountBlock{
		{BlockType: nom.BlockTypeContractSend, Address: address, Hash: blockAHash, Height: 1},
		{Address: address, Hash: blockBHash, Height: 2},
	}

	rmv := &rawMomentumVerifier{
		momentum:      momentum,
		accountBlocks: accountBlocks,
		momentumStore: &stubMomentumStore{},
	}

	err := rmv.content(false)
	if err == nil {
		t.Fatal("expected an error for duplicate content headers resolving to the same account block, got nil")
	}
}

// A content header must be rejected if its address doesn't match the address of the
// prefetched account-block sharing its hash/height identifier.
func TestRawMomentumVerifier_Content_AddressMismatch_ReturnsError(t *testing.T) {
	headerAddress := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	blockAddress := types.ParseAddressPanic("z1qqmqp40duzvtxvg7dwxph7724mq63t3mru297p")
	blockHash := types.NewHash([]byte("block"))

	momentum := &nom.Momentum{
		Content: nom.MomentumContent{
			{
				Address: headerAddress,
				HashHeight: types.HashHeight{
					Hash:   blockHash,
					Height: 1,
				},
			},
		},
	}
	accountBlocks := []*nom.AccountBlock{
		{Address: blockAddress, Hash: blockHash, Height: 1},
	}

	rmv := &rawMomentumVerifier{
		momentum:      momentum,
		accountBlocks: accountBlocks,
		momentumStore: &stubMomentumStore{},
	}

	err := rmv.content(false)
	if err == nil {
		t.Fatal("expected an error for a content header address mismatching its account block, got nil")
	}
}

// content() must recompute a block's price from the injected canonical base
// plasma rather than trusting the wire BasePlasma, so a momentum whose prices
// were derived by a byzantine producer from a forged wire value is rejected.
func TestRawMomentumVerifier_Content_ForgedBasePlasma_ReturnsError(t *testing.T) {
	const (
		canonical = uint64(21000)
		forged    = uint64(100000)
	)

	address := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	blockHash := types.NewHash([]byte("block"))

	block := &nom.AccountBlock{
		Address:     address,
		Hash:        blockHash,
		Height:      1,
		Difficulty:  0,
		BasePlasma:  forged,
		FusedPlasma: canonical,
	}

	momentum := &nom.Momentum{
		Version: 2,
		Content: nom.MomentumContent{
			{
				Address: address,
				HashHeight: types.HashHeight{
					Hash:   blockHash,
					Height: 1,
				},
			},
		},
	}

	config := &definition.PlasmaVariables{
		MaxBasePlasmaInMomentum: 1_000_000_000,
		FusedPlasmaTarget:       canonical,
		PowPlasmaTarget:         canonical,
		MaxPriceChangePercent:   10,
		PriceChangeDenominator:  20,
	}

	// The prices a byzantine producer would have derived from the forged wire
	// BasePlasma.
	forgedBlock := &nom.AccountBlock{
		Address:     address,
		Hash:        blockHash,
		Height:      1,
		Difficulty:  0,
		BasePlasma:  forged,
		FusedPlasma: canonical,
	}
	byzantinePlasma := dp.NewDynamicPlasma(&nom.Momentum{Version: 1}, config)
	byzantineBasePlasma := types.BasePlasma{Fusion: 0, Pow: 0}
	byzantineBasePlasma.Add(byzantinePlasma.ComputeBasePlasma(forgedBlock))
	momentum.NextFusionPrice = byzantinePlasma.NextFusionPrice(byzantineBasePlasma.Fusion)
	momentum.NextWorkPrice = byzantinePlasma.NextWorkPrice(byzantineBasePlasma.Pow)

	accountBlocks := []*nom.AccountBlock{block}

	rmv := &rawMomentumVerifier{
		momentum:      momentum,
		accountBlocks: accountBlocks,
		momentumStore: &stubMomentumStore{},
		chain:         nil,
		canonicalBasePlasma: func(chain.Chain, *nom.AccountBlock) (uint64, error) {
			return canonical, nil
		},
	}

	err := rmv.content(true)
	if err == nil {
		t.Fatal("expected an error for a momentum whose prices were derived from a forged wire base plasma, got nil")
	}
}

// content()'s dynamic-plasma loop must not recompute canonical base plasma for
// embedded-address blocks: ComputeBasePlasma already special-cases embedded
// addresses to zero, so the injected canonicalBasePlasma func must never be
// invoked for them.
func TestRawMomentumVerifier_Content_EmbeddedAddress_SkipsCanonicalRecompute(t *testing.T) {
	address := types.PlasmaContract
	blockHash := types.NewHash([]byte("block"))

	block := &nom.AccountBlock{
		BlockType: nom.BlockTypeContractReceive,
		Address:   address,
		Hash:      blockHash,
		Height:    1,
	}

	momentum := &nom.Momentum{
		Version:         2,
		NextFusionPrice: 1000,
		NextWorkPrice:   1000,
		Content: nom.MomentumContent{
			{
				Address: address,
				HashHeight: types.HashHeight{
					Hash:   blockHash,
					Height: 1,
				},
			},
		},
	}

	accountBlocks := []*nom.AccountBlock{block}

	rmv := &rawMomentumVerifier{
		momentum:      momentum,
		accountBlocks: accountBlocks,
		momentumStore: &stubMomentumStore{},
		chain:         nil,
		canonicalBasePlasma: func(chain.Chain, *nom.AccountBlock) (uint64, error) {
			t.Fatal("canonicalBasePlasma must not be called for an embedded-address block")
			return 0, nil
		},
	}

	if err := rmv.content(true); err != nil {
		t.Fatalf("expected no error for an untouched embedded-address block, got %v", err)
	}
}
