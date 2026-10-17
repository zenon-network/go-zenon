package verifier

import (
	"testing"

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
