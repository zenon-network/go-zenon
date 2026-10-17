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

// TestDeserializeAccountBlock_MalformedNonce verifies that
// DeserializeAccountBlock returns an error instead of panicking when the
// protobuf has a wrong-length nonce field. Previously this caused a panic
// in DeSerializeNonce.
func TestDeserializeAccountBlock_MalformedNonce(t *testing.T) {
	pb := validAccountBlockProto()
	pb.Nonce = make([]byte, 4) // wrong length, should be 8
	data, err := proto.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DeserializeAccountBlock(data)
	if err == nil {
		t.Fatal("expected error for AccountBlockProto with wrong-length Nonce, got nil")
	}
	if !strings.Contains(err.Error(), "invalid nonce") {
		t.Fatalf("expected error to mention 'invalid nonce', got: %v", err)
	}
}

// TestDeserializeAccountBlock_MalformedAddress verifies that
// DeserializeAccountBlock returns an error instead of panicking when the
// protobuf has a wrong-length address field. Previously this caused a panic
// in DeProtoAddress.
func TestDeserializeAccountBlock_MalformedAddress(t *testing.T) {
	pb := validAccountBlockProto()
	pb.Address = &types.AddressProto{Address: make([]byte, 10)} // wrong length
	data, err := proto.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DeserializeAccountBlock(data)
	if err == nil {
		t.Fatal("expected error for AccountBlockProto with wrong-length Address, got nil")
	}
	if !strings.Contains(err.Error(), "invalid address") {
		t.Fatalf("expected error to mention 'invalid address', got: %v", err)
	}
}

// TestDeserializeAccountBlock_MalformedTokenStandard verifies that
// DeserializeAccountBlock returns an error instead of panicking when the
// protobuf has a wrong-length token standard field. Previously this caused
// a panic in BytesToZTSPanic.
func TestDeserializeAccountBlock_MalformedTokenStandard(t *testing.T) {
	pb := validAccountBlockProto()
	pb.TokenStandard = make([]byte, 5) // wrong length
	data, err := proto.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DeserializeAccountBlock(data)
	if err == nil {
		t.Fatal("expected error for AccountBlockProto with wrong-length TokenStandard, got nil")
	}
	if !strings.Contains(err.Error(), "invalid token standard") {
		t.Fatalf("expected error to mention 'invalid token standard', got: %v", err)
	}
}

// TestDeserializeAccountBlock_MalformedToAddress verifies that
// DeserializeAccountBlock returns an error instead of panicking when the
// protobuf has a wrong-length to-address field.
func TestDeserializeAccountBlock_MalformedToAddress(t *testing.T) {
	pb := validAccountBlockProto()
	pb.ToAddress = &types.AddressProto{Address: make([]byte, 15)} // wrong length
	data, err := proto.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DeserializeAccountBlock(data)
	if err == nil {
		t.Fatal("expected error for AccountBlockProto with wrong-length ToAddress, got nil")
	}
	if !strings.Contains(err.Error(), "invalid to address") {
		t.Fatalf("expected error to mention 'invalid to address', got: %v", err)
	}
}

// TestDeserializeAccountBlock_ValidRoundTrip verifies that a valid
// AccountBlock still round-trips correctly after the error-handling change.
// Address, ToAddress, TokenStandard and Nonce carry distinct non-zero bytes
// so that a field swap or mis-assignment cannot go undetected.
func TestDeserializeAccountBlock_ValidRoundTrip(t *testing.T) {
	pb := validAccountBlockProto()
	data, err := proto.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	ab, err := DeserializeAccountBlock(data)
	if err != nil {
		t.Fatalf("expected valid AccountBlockProto to deserialize, got: %v", err)
	}
	if ab.Version != 1 {
		t.Fatalf("expected Version 1, got %d", ab.Version)
	}
	if ab.Height != 42 {
		t.Fatalf("expected Height 42, got %d", ab.Height)
	}
	if ab.Address != types.Address(pb.Address.Address) {
		t.Errorf("Address mismatch: got %x, want %x", ab.Address, pb.Address.Address)
	}
	if ab.ToAddress != types.Address(pb.ToAddress.Address) {
		t.Errorf("ToAddress mismatch: got %x, want %x", ab.ToAddress, pb.ToAddress.Address)
	}
	if ab.TokenStandard != types.BytesToZTSPanic(pb.TokenStandard) {
		t.Errorf("TokenStandard mismatch: got %x, want %x", ab.TokenStandard, pb.TokenStandard)
	}
	if string(ab.Nonce.Serialize()) != string(pb.Nonce) {
		t.Errorf("Nonce mismatch: got %x, want %x", ab.Nonce.Serialize(), pb.Nonce)
	}
}

// validAccountBlockProto returns a minimal but valid AccountBlockProto for
// use in regression tests. Address, ToAddress, TokenStandard and Nonce are
// filled with distinct non-zero bytes so that field-swap bugs are caught.
func validAccountBlockProto() *AccountBlockProto {
	addrBytes := make([]byte, types.AddressSize)
	for i := range addrBytes {
		addrBytes[i] = byte(i + 1) // 1,2,…,20
	}
	toAddrBytes := make([]byte, types.AddressSize)
	for i := range toAddrBytes {
		toAddrBytes[i] = byte(100 + i) // 100,101,…,119
	}
	tsBytes := make([]byte, types.ZenonTokenStandardSize)
	for i := range tsBytes {
		tsBytes[i] = byte(200 + i) // 200,201,…,209
	}
	nonceBytes := make([]byte, 8)
	for i := range nonceBytes {
		nonceBytes[i] = byte(50 + i) // 50,51,…,57
	}
	return &AccountBlockProto{
		Version:              1,
		ChainIdentifier:      1,
		BlockType:            uint64(BlockTypeUserSend),
		Hash:                 &types.HashProto{Hash: make([]byte, 32)},
		PreviousHash:         &types.HashProto{Hash: make([]byte, 32)},
		Height:               42,
		MomentumAcknowledged: &types.HashHeightProto{Hash: &types.HashProto{Hash: make([]byte, 32)}, Height: 1},
		FromBlockHash:        &types.HashProto{Hash: make([]byte, 32)},
		Address:              &types.AddressProto{Address: addrBytes},
		ToAddress:            &types.AddressProto{Address: toAddrBytes},
		TokenStandard:        tsBytes,
		Nonce:                nonceBytes,
		ChangesHash:          &types.HashProto{Hash: make([]byte, 32)},
	}
}
