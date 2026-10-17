package types

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/zenon-network/go-zenon/common"
)

// AccountHeader identifies an account block globally: the address that
// owns the account chain plus the block's HashHeight within that
// chain. The embedded HashHeight flattens into the JSON object, so the
// wire form has the keys "address", "hash" and "height".
type AccountHeader struct {
	Address Address `json:"address"`
	HashHeight
}

// Identifier returns the block's chain-local identifier, i.e. the
// embedded HashHeight without the address.
func (abh *AccountHeader) Identifier() HashHeight {
	return abh.HashHeight
}

// Proto converts the header to its protobuf message.
func (abh *AccountHeader) Proto() *AccountHeaderProto {
	return &AccountHeaderProto{
		Address:    abh.Address.Proto(),
		HashHeight: abh.HashHeight.Proto(),
	}
}

// DeProtoAccountHeader is the inverse of Proto. It returns an error
// for a nil proto or when the embedded address or hash payload has the
// wrong size (via DeProtoAddress and DeProtoHash).
func DeProtoAccountHeader(pb *AccountHeaderProto) (*AccountHeader, error) {
	if pb == nil {
		return nil, fmt.Errorf("invalid DeProto - nil account header proto")
	}
	hh, err := DeProtoHashHeight(pb.HashHeight)
	if err != nil {
		return nil, err
	}
	address, err := DeProtoAddress(pb.Address)
	if err != nil {
		return nil, fmt.Errorf("invalid address: %w", err)
	}
	return &AccountHeader{
		Address:    *address,
		HashHeight: *hh,
	}, nil
}

// Serialize encodes the header as protobuf bytes.
// DeserializeAccountHeader is the inverse.
func (abh *AccountHeader) Serialize() ([]byte, error) {
	return proto.Marshal(abh.Proto())
}

// DeserializeAccountHeader decodes an AccountHeader from its protobuf
// bytes, returning an error if the data is not a valid protobuf
// message or if the address or hash payload has the wrong size (via
// DeProtoAccountHeader).
func DeserializeAccountHeader(data []byte) (*AccountHeader, error) {
	pb := new(AccountHeaderProto)
	if err := proto.Unmarshal(data, pb); err != nil {
		return nil, err
	}
	return DeProtoAccountHeader(pb)
}

// Bytes returns a fixed 60-byte encoding: the 20 address bytes, the
// height as a big-endian uint64, then the 32 hash bytes. Note the
// field order differs from HashHeight.Bytes, which puts the hash
// before the height.
func (abh *AccountHeader) Bytes() []byte {
	return common.JoinBytes(
		abh.Address.Bytes(),
		common.Uint64ToBytes(abh.Height),
		abh.Hash.Bytes())
}
