package nom

import (
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
	// MomentumProto with previousHash of wrong length (5 bytes)
	pb := &MomentumProto{
		Version:      1,
		Height:       42,
		PreviousHash: &types.HashProto{Hash: []byte{1, 2, 3, 4, 5}},
	}
	data, err := proto.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DeserializeMomentum(data)
	if err == nil {
		t.Fatal("expected error for MomentumProto with wrong-length PreviousHash, got nil")
	}
}
