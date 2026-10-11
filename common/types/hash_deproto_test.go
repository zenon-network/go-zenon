package types

import (
	"testing"

	"google.golang.org/protobuf/proto"
)

// TestDeProtoHash_NilProto verifies that DeProtoHash returns an error
// instead of panicking when given a nil proto.
func TestDeProtoHash_NilProto(t *testing.T) {
	_, err := DeProtoHash(nil)
	if err == nil {
		t.Fatal("expected error for nil HashProto, got nil")
	}
}

// TestDeProtoHash_WrongLength verifies that DeProtoHash returns an error
// instead of panicking when given a proto with wrong-length hash.
func TestDeProtoHash_WrongLength(t *testing.T) {
	_, err := DeProtoHash(&HashProto{Hash: []byte{1, 2, 3, 4, 5}})
	if err == nil {
		t.Fatal("expected error for 5-byte hash, got nil")
	}
}

// TestDeProtoHash_Valid verifies that DeProtoHash succeeds on valid input.
func TestDeProtoHash_Valid(t *testing.T) {
	h := Hash{1, 2, 3}
	pb := h.Proto()
	got, err := DeProtoHash(pb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *got != h {
		t.Errorf("got %v, want %v", got, h)
	}
}

// TestDeserializeHashHeight_Malformed verifies that DeserializeHashHeight
// returns an error instead of panicking on malformed input.
func TestDeserializeHashHeight_Malformed(t *testing.T) {
	// Valid protobuf with HashHeightProto containing a nil Hash field
	pb := &HashHeightProto{Height: 42}
	data, err := proto.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DeserializeHashHeight(data)
	if err == nil {
		t.Fatal("expected error for HashHeightProto with nil Hash, got nil")
	}
}

// TestDeserializeAccountHeader_Malformed verifies that DeserializeAccountHeader
// returns an error instead of panicking on malformed input.
func TestDeserializeAccountHeader_Malformed(t *testing.T) {
	// Valid protobuf with AccountHeaderProto containing a nil HashHeight
	pb := &AccountHeaderProto{}
	data, err := proto.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DeserializeAccountHeader(data)
	if err == nil {
		t.Fatal("expected error for AccountHeaderProto with nil HashHeight, got nil")
	}
}
