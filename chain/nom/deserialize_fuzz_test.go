package nom

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/zenon-network/go-zenon/common/types"
)

// TestDeserializeAccountBlock_MalformedHash verifies that
// DeserializeAccountBlock returns an error instead of panicking when the
// protobuf has a nil hash field. Previously this caused a nil-pointer
// panic in DeProtoHash.
func TestDeserializeAccountBlock_MalformedHash(t *testing.T) {
	// AccountBlockProto with hash field absent
	pb := &AccountBlockProto{Version: 1, BlockType: BlockTypeUserSend, Height: 2}
	data, err := proto.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DeserializeAccountBlock(data)
	if err == nil {
		t.Fatal("expected error for AccountBlockProto with nil Hash, got nil")
	}
}

// TestDeserializeMomentum_MalformedHash verifies that DeserializeMomentum
// returns an error instead of panicking when the protobuf has a wrong-length
// hash field. Previously this caused a nil-pointer panic in DeProtoHash.
func TestDeserializeMomentum_MalformedHash(t *testing.T) {
	// Build a valid momentum, then corrupt only the PreviousHash field.
	valid := &Momentum{
		Version:         1,
		ChainIdentifier: 1,
		Hash:            types.Hash{1},
		PreviousHash:    types.Hash{2},
		Height:          42,
		TimestampUnix:   1000000,
		Content:         MomentumContent{},
		ChangesHash:     types.Hash{3},
	}
	pb := valid.Proto()
	pb.PreviousHash = &types.HashProto{Hash: []byte{1, 2, 3, 4, 5}} // wrong length
	data, err := proto.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DeserializeMomentum(data)
	if err == nil {
		t.Fatal("expected error for MomentumProto with wrong-length PreviousHash, got nil")
	}
	if !strings.Contains(err.Error(), "invalid previous hash") {
		t.Fatalf("expected error to mention 'invalid previous hash', got: %v", err)
	}
}
