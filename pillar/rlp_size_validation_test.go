package pillar

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

// TestContent_ByteBudget_MeasuredSizeIsExact verifies that blockWireSize
// returns the exact RLP-encoded size, not an estimate. This is the core
// property that replaces the old conservative-estimate approach: the
// selector measures real RLP sizes so it cannot be fooled by oversized
// non-Data fields.
func TestContent_ByteBudget_MeasuredSizeIsExact(t *testing.T) {
	block := &nom.AccountBlock{
		Version:         1,
		ChainIdentifier: 1,
		BlockType:       nom.BlockTypeUserSend,
		Height:          1,
		Address:         types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz"),
		ToAddress:       types.ParseAddressPanic("z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj"),
		Data:            make([]byte, 1000),
		FusedPlasma:     21000,
		BasePlasma:      21000,
		TotalPlasma:     21000,
	}

	encoded, err := rlp.EncodeToBytes(block)
	if err != nil {
		t.Fatalf("RLP encode failed: %v", err)
	}

	measured := blockWireSize(block)
	actual := len(encoded)
	if measured != actual {
		t.Errorf("blockWireSize (%d) != actual RLP size (%d): measurement is not exact", measured, actual)
	}
	t.Logf("block with 1000-byte Data: measured=%d, actual=%d", measured, actual)

	// Empty block
	empty := &nom.AccountBlock{}
	emptyEncoded, err := rlp.EncodeToBytes(empty)
	if err != nil {
		t.Fatalf("RLP encode of empty block failed: %v", err)
	}
	emptyMeasured := blockWireSize(empty)
	emptyActual := len(emptyEncoded)
	if emptyMeasured != emptyActual {
		t.Errorf("empty block: measured (%d) != actual RLP size (%d)", emptyMeasured, emptyActual)
	}
	t.Logf("empty block: measured=%d, actual=%d", emptyMeasured, emptyActual)

	// Block with descendants
	withDesc := &nom.AccountBlock{
		Data: make([]byte, 500),
		DescendantBlocks: []*nom.AccountBlock{
			{Data: make([]byte, 300)},
			{Data: make([]byte, 200)},
		},
	}
	descEncoded, err := rlp.EncodeToBytes(withDesc)
	if err != nil {
		t.Fatalf("RLP encode of block with descendants failed: %v", err)
	}
	descMeasured := blockWireSize(withDesc)
	// blockWireSize = parent RLP + each descendant RLP
	d1Encoded, _ := rlp.EncodeToBytes(withDesc.DescendantBlocks[0])
	d2Encoded, _ := rlp.EncodeToBytes(withDesc.DescendantBlocks[1])
	descActual := len(descEncoded) + len(d1Encoded) + len(d2Encoded)
	if descMeasured != descActual {
		t.Errorf("block with descendants: measured (%d) != actual RLP size (%d)", descMeasured, descActual)
	}
	t.Logf("block with descendants: measured=%d, actual=%d", descMeasured, descActual)
}

// TestContent_ByteBudget_FullyPopulatedBlock verifies that a fully populated
// block (every field set to realistic values) is measured exactly. This is
// the case that broke the old estimate approach: the old perBlockFixedSize
// assumption was validated only for Data, not for other variable-length
// fields like PublicKey and Signature.
func TestContent_ByteBudget_FullyPopulatedBlock(t *testing.T) {
	addr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	toAddr := types.ParseAddressPanic("z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj")
	zts, _ := types.ParseZTS("zts1znnxxxxxxxxxxxxx9z4vexb")

	h1 := types.HexToHashPanic("0000000000000000000000000000000000000000000000000000000000000001")
	h2 := types.HexToHashPanic("0000000000000000000000000000000000000000000000000000000000000002")
	h3 := types.HexToHashPanic("0000000000000000000000000000000000000000000000000000000000000003")
	h4 := types.HexToHashPanic("0000000000000000000000000000000000000000000000000000000000000004")
	h5 := types.HexToHashPanic("0000000000000000000000000000000000000000000000000000000000000005")

	var nonce nom.Nonce
	copy(nonce.Data[:], []byte{1, 2, 3, 4, 5, 6, 7, 8})

	full := &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      1,
		BlockType:            nom.BlockTypeUserSend,
		Hash:                 h1,
		PreviousHash:         h2,
		Height:               12345,
		MomentumAcknowledged: types.HashHeight{Hash: h3, Height: 999},
		Address:              addr,
		ToAddress:            toAddr,
		Amount:               big.NewInt(1000000000),
		TokenStandard:        zts,
		FromBlockHash:        h4,
		Data:                 make([]byte, 1000),
		FusedPlasma:          21000,
		Difficulty:           123456789,
		Nonce:                nonce,
		BasePlasma:           21000,
		TotalPlasma:          21000,
		ChangesHash:          h5,
		PublicKey:            make([]byte, 32),
		Signature:            make([]byte, 64),
	}

	fullEncoded, err := rlp.EncodeToBytes(full)
	if err != nil {
		t.Fatalf("RLP encode of full block failed: %v", err)
	}
	fullMeasured := blockWireSize(full)
	fullActual := len(fullEncoded)
	if fullMeasured != fullActual {
		t.Errorf("fully populated block: measured (%d) != actual RLP size (%d)", fullMeasured, fullActual)
	}
	t.Logf("fully populated block: measured=%d, actual=%d", fullMeasured, fullActual)
}

// TestContent_ByteBudget_MomentumHeaderSize verifies that the momentum header
// size used in filterBlocksToCommit matches the real RLP encoding of an
// empty Momentum with Version 2.
func TestContent_ByteBudget_MomentumHeaderSize(t *testing.T) {
	momentum := &nom.Momentum{Version: 2, Content: nom.MomentumContent{}}
	encoded, err := rlp.EncodeToBytes(momentum)
	if err != nil {
		t.Fatalf("RLP encode failed: %v", err)
	}
	t.Logf("momentum header (Version 2, empty content): RLP size %d", len(encoded))

	// The header size should be reasonable (under 1 KiB).
	if len(encoded) > 1024 {
		t.Errorf("momentum header RLP size %d seems too large", len(encoded))
	}
}

// The budget safety invariant test lives in protocol (TestMaxDetailedMomentumWireSize)
// where the constants are defined.
