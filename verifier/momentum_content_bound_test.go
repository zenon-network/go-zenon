package verifier

import (
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

// makeLargeBlock returns an AccountBlock whose serialized size is at least
// sizeHint bytes, achieved by padding the Data field.
func makeLargeBlock(address types.Address, sizeHint int) *nom.AccountBlock {
	// AccountBlock overhead is small; Data field adds directly.
	overhead := 200
	dataLen := sizeHint - overhead
	if dataLen < 0 {
		dataLen = 0
	}
	return &nom.AccountBlock{
		Address:   address,
		Hash:      types.NewHash([]byte("large-block")),
		Height:    1,
		Data:      make([]byte, dataLen),
		BlockType: nom.BlockTypeUserSend,
	}
}

func TestRawMomentumVerifier_ContentByteBound_WithinLimit(t *testing.T) {
	addr := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	block := makeLargeBlock(addr, 1024) // 1 KiB, well under the 8 MiB bound

	rmv := &rawMomentumVerifier{
		accountBlocks: []*nom.AccountBlock{block},
	}
	if err := rmv.contentByteBound(); err != nil {
		t.Errorf("1 KiB block: got %v, want nil", err)
	}
}

func TestRawMomentumVerifier_ContentByteBound_ExceedsLimit(t *testing.T) {
	addr := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")

	// Two 5 MiB blocks = 10 MiB total, exceeding the 8 MiB bound.
	block1 := makeLargeBlock(addr, 5*1024*1024)
	block2 := makeLargeBlock(addr, 5*1024*1024)

	rmv := &rawMomentumVerifier{
		accountBlocks: []*nom.AccountBlock{block1, block2},
	}
	err := rmv.contentByteBound()
	if err != ErrMContentTooBig {
		t.Errorf("10 MiB total: got %v, want %v", err, ErrMContentTooBig)
	}
}

func TestRawMomentumVerifier_ContentByteBound_SkipsNilBlocks(t *testing.T) {
	addr := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	block := makeLargeBlock(addr, 1024)

	rmv := &rawMomentumVerifier{
		accountBlocks: []*nom.AccountBlock{nil, block, nil},
	}
	if err := rmv.contentByteBound(); err != nil {
		t.Errorf("with nil blocks: got %v, want nil", err)
	}
}

// TestRawMomentumVerifier_Content_DPActive_ByteBound exercises the
// content() gate that calls contentByteBound only when DP is active.
func TestRawMomentumVerifier_Content_DPActive_ByteBound(t *testing.T) {
	addr := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")

	// A single block just over the 8 MiB bound.
	bigBlock := makeLargeBlock(addr, MaxMomentumContentBytes+1024)

	rmv := &rawMomentumVerifier{
		momentum: &nom.Momentum{
			Content: nom.MomentumContent{
				{Address: addr, HashHeight: types.HashHeight{Hash: bigBlock.Hash, Height: 1}},
			},
		},
		accountBlocks: []*nom.AccountBlock{bigBlock},
		momentumStore: &stubMomentumStore{},
	}

	// With DP active, the byte bound fires before any block matching.
	err := rmv.content(true)
	if err != ErrMContentTooBig {
		t.Errorf("DP active, oversized block: got %v, want %v", err, ErrMContentTooBig)
	}

	// With DP inactive, the byte bound is skipped (the count bound still applies
	// but a single block is within the count limit).
	err = rmv.content(false)
	if err == ErrMContentTooBig {
		t.Errorf("DP inactive: contentByteBound should not fire")
	}
}

// makeExactSizeBlock returns an AccountBlock whose protobuf-serialized size is
// exactly targetSize bytes. It binary-searches the Data length because the
// protobuf varint encoding of the Data length prefix changes size at 128-byte
// boundaries, making the overhead non-linear. The block carries enough
// FusedPlasma to pass the dynamic-plasma price check in content().
func makeExactSizeBlock(address types.Address, targetSize int) *nom.AccountBlock {
	block := &nom.AccountBlock{
		Address:       address,
		Hash:          types.NewHash([]byte("exact-size-block")),
		Height:        1,
		BlockType:     nom.BlockTypeUserSend,
		FusedPlasma:   1_000_000, // well above the minimum price
		TokenStandard: types.ZeroTokenStandard,
		Amount:        big.NewInt(0),
	}
	// Binary search for the Data length that yields exactly targetSize.
	lo, hi := 0, targetSize
	for lo < hi {
		mid := (lo + hi + 1) / 2
		block.Data = make([]byte, mid)
		serialized, err := block.Serialize()
		if err != nil {
			panic(err)
		}
		if len(serialized) <= targetSize {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	block.Data = make([]byte, lo)
	return block
}

// TestRawMomentumVerifier_Content_ExactBound_Accepted pins the enforcement
// transition at exactly MaxMomentumContentBytes: a momentum whose blocks
// serialize to exactly the limit must pass the content() byte bound.
func TestRawMomentumVerifier_Content_ExactBound_Accepted(t *testing.T) {
	addr := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")

	block := makeExactSizeBlock(addr, MaxMomentumContentBytes)
	serialized, err := block.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if len(serialized) > MaxMomentumContentBytes {
		t.Fatalf("test setup: block size %d exceeds target %d", len(serialized), MaxMomentumContentBytes)
	}

	rmv := &rawMomentumVerifier{
		momentum: &nom.Momentum{
			Version:         2, // dynamic-plasma momentum
			NextFusionPrice: 1000,
			NextWorkPrice:   1000,
			Content: nom.MomentumContent{
				{Address: addr, HashHeight: types.HashHeight{Hash: block.Hash, Height: 1}},
			},
		},
		accountBlocks: []*nom.AccountBlock{block},
		momentumStore: &stubMomentumStore{},
		canonicalBasePlasma: func(chain.Chain, *nom.AccountBlock) (uint64, error) {
			return 21000, nil // matches stubMomentumStore's FusedPlasmaTarget
		},
	}

	// content(true) runs contentByteBound + matching + frontier checks.
	// The byte bound must NOT fire at exactly MaxMomentumContentBytes.
	if err := rmv.content(true); err != nil {
		t.Errorf("exactly %d bytes: got %v, want nil", MaxMomentumContentBytes, err)
	}
}

// TestRawMomentumVerifier_Content_OneByteOverBound_Rejected pins the
// enforcement transition at MaxMomentumContentBytes+1: a momentum whose
// blocks exceed the limit by a single byte must be rejected by content().
func TestRawMomentumVerifier_Content_OneByteOverBound_Rejected(t *testing.T) {
	addr := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")

	// Build a block that serializes to exactly MaxMomentumContentBytes, then
	// add one more byte to its Data field.
	block := makeExactSizeBlock(addr, MaxMomentumContentBytes)
	block.Data = append(block.Data, 0x00)
	serialized, err := block.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if len(serialized) <= MaxMomentumContentBytes {
		t.Fatalf("test setup: block size %d should exceed %d", len(serialized), MaxMomentumContentBytes)
	}

	rmv := &rawMomentumVerifier{
		momentum: &nom.Momentum{
			Content: nom.MomentumContent{
				{Address: addr, HashHeight: types.HashHeight{Hash: block.Hash, Height: 1}},
			},
		},
		accountBlocks: []*nom.AccountBlock{block},
		momentumStore: &stubMomentumStore{},
	}

	err = rmv.content(true)
	if err != ErrMContentTooBig {
		t.Errorf("%d bytes (1 over): got %v, want %v", len(serialized), err, ErrMContentTooBig)
	}
}
