package pillar

import (
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// realisticEmptyDataSend builds a fully populated empty-Data user send with
// small integer fields (low heights, a 1-unit amount), the shape for which
// the protobuf encoding (what the verifier's content byte bound sums) is
// larger than the RLP encoding plus content entry (what the RLP wire budget
// sums). The gap depends on field widths; this shape keeps the protobuf
// encoding the larger one, and the test checks that before relying on it.
func realisticEmptyDataSend(addr types.Address, height uint64) *nom.AccountBlock {
	var h types.Hash
	for i := range h {
		h[i] = byte(0x80 + i)
	}
	pk := make([]byte, 32)
	sig := make([]byte, 64)
	for i := range pk {
		pk[i] = byte(0x40 + i)
	}
	for i := range sig {
		sig[i] = byte(0x20 + i)
	}
	return &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      1,
		BlockType:            nom.BlockTypeUserSend,
		Hash:                 h,
		PreviousHash:         h,
		Height:               height,
		MomentumAcknowledged: types.HashHeight{Hash: h, Height: 1},
		Address:              addr,
		ToAddress:            addr,
		Amount:               big.NewInt(1),
		TokenStandard:        types.ZnnTokenStandard,
		FusedPlasma:          21000,
		BasePlasma:           21000,
		Nonce:                nom.Nonce{Data: [8]byte{1, 2, 3, 4, 5, 6, 7, 8}},
		PublicKey:            pk,
		Signature:            sig,
	}
}

// TestContent_ProtoBudget_RLPFillStaysUnderVerifierBound feeds the selector
// enough realistic empty-Data user sends to fill the RLP wire budget and
// checks that the selection also respects the verifier's protobuf content
// bound, which is what rawMomentumVerifier.contentByteBound enforces on the
// producer's own momentum. Without the protobuf budget in the selector the
// RLP-filling selection is over the verifier bound and GenerateMomentum
// fails with ErrMContentTooBig.
func TestContent_ProtoBudget_RLPFillStaysUnderVerifierBound(t *testing.T) {
	// Plasma budget for 30,000 base blocks, within the governance ceiling
	// (MaxBasePlasmaInMomentumUpperLimit is 100,000 base blocks), so the
	// byte budgets are the binding constraints.
	config := &definition.PlasmaVariables{MaxBasePlasmaInMomentum: 21000 * 30000}
	cs := &contentSelector{plasma: dp.NewDynamicPlasma(&nom.Momentum{NextFusionPrice: 1000, NextWorkPrice: 1000, Version: 2}, config)}
	addr1 := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	addr2 := types.ParseAddressPanic("z1qqfmjdays57w488sta69ykc2ey7r6d0q9wdvtj")

	sample := realisticEmptyDataSend(addr1, 1)
	rlpPer := blockWireSize(sample) + contentEntrySize(sample)
	protoPer := blockProtoSize(sample)
	if protoPer <= rlpPer {
		t.Fatalf("test setup: protobuf size %d not larger than RLP block+entry %d; the two budgets no longer disagree in this direction", protoPer, rlpPer)
	}

	// Enough blocks to overfill the RLP budget by a margin.
	numBlocks := (protocol.MaxDetailedMomentumWireSize/rlpPer)*11/10 + 1
	blocks := make([]*nom.AccountBlock, 0, numBlocks)
	for i := 0; i < numBlocks; i++ {
		addr := addr1
		if i%2 == 1 {
			addr = addr2
		}
		blocks = append(blocks, realisticEmptyDataSend(addr, 1+uint64(i/2)))
	}

	// Self-check: the RLP budget alone admits more protobuf bytes than the
	// verifier allows, so the protobuf budget is the binding one here.
	rlpOnly := worstCaseMomentumHeaderSize() + listPrefixAllowance
	rlpOnlyProto := 0
	for _, b := range blocks {
		if rlpOnly+rlpPer > protocol.MaxDetailedMomentumWireSize {
			break
		}
		rlpOnly += rlpPer
		rlpOnlyProto += blockProtoSize(b)
	}
	if rlpOnlyProto <= verifier.MaxMomentumContentBytes {
		t.Fatalf("test setup: an RLP-budget fill is only %d protobuf bytes, under the verifier bound %d", rlpOnlyProto, verifier.MaxMomentumContentBytes)
	}

	toCommit := cs.filterBlocksToCommit(blocks)
	if len(toCommit) == 0 {
		t.Fatal("selector committed nothing")
	}
	total := 0
	for _, b := range toCommit {
		data, err := b.Serialize()
		if err != nil {
			t.Fatalf("serialize: %v", err)
		}
		total += len(data)
	}
	t.Logf("per block: protobuf %d, RLP block+entry %d; committed %d of %d blocks, protobuf total %d, verifier bound %d, RLP-only fill would be %d protobuf bytes",
		protoPer, rlpPer, len(toCommit), len(blocks), total, verifier.MaxMomentumContentBytes, rlpOnlyProto)
	if total > verifier.MaxMomentumContentBytes {
		t.Fatalf("selected content is %d protobuf bytes, over the verifier bound %d by %d: the producer's own verifier would reject this momentum",
			total, verifier.MaxMomentumContentBytes, total-verifier.MaxMomentumContentBytes)
	}
}
