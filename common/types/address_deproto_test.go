package types

import (
	"strings"
	"testing"
)

// TestDeProtoAddress_NilProto verifies that DeProtoAddress returns an error
// instead of panicking when given a nil proto.
func TestDeProtoAddress_NilProto(t *testing.T) {
	_, err := DeProtoAddress(nil)
	if err == nil {
		t.Fatal("expected error for nil AddressProto, got nil")
	}
}

// TestDeProtoAddress_WrongLength verifies that DeProtoAddress returns an error
// instead of panicking when given a proto with wrong-length address.
func TestDeProtoAddress_WrongLength(t *testing.T) {
	_, err := DeProtoAddress(&AddressProto{Address: []byte{1, 2, 3, 4, 5}})
	if err == nil {
		t.Fatal("expected error for 5-byte address, got nil")
	}
}

// TestDeProtoAccountHeader_MalformedAddress verifies that
// DeProtoAccountHeader returns an error instead of panicking when the
// embedded address field is malformed.
func TestDeProtoAccountHeader_MalformedAddress(t *testing.T) {
	valid := &AccountHeader{
		Address: Address{1, 2, 3},
		HashHeight: HashHeight{
			Hash:   Hash{4, 5, 6},
			Height: 42,
		},
	}
	pb := valid.Proto()
	pb.Address = &AddressProto{Address: []byte{1, 2, 3, 4, 5}} // wrong length
	_, err := DeProtoAccountHeader(pb)
	if err == nil {
		t.Fatal("expected error for AccountHeaderProto with wrong-length Address, got nil")
	}
	// DeProtoAccountHeader wraps the DeProtoAddress error with "invalid address".
	if !strings.Contains(err.Error(), "invalid address") {
		t.Fatalf("expected address error from DeProtoAddress, got: %v", err)
	}
}
