package types

import (
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

// DeProtoAccountHeader is the inverse of Proto. It panics (via
// DeProtoAddress and DeProtoHash) if the embedded address or hash
// payload has the wrong size.
func DeProtoAccountHeader(pb *AccountHeaderProto) *AccountHeader {
	return &AccountHeader{
		Address:    *DeProtoAddress(pb.Address),
		HashHeight: *DeProtoHashHeight(pb.HashHeight),
	}
}

// Serialize encodes the header as protobuf bytes.
// DeserializeAccountHeader is the inverse.
func (abh *AccountHeader) Serialize() ([]byte, error) {
	return proto.Marshal(abh.Proto())
}

// DeserializeAccountHeader decodes an AccountHeader from its protobuf
// bytes, returning an error if the data is not a valid protobuf
// message. It panics (via DeProtoAccountHeader) if the message decodes
// but the address or hash payload has the wrong size.
func DeserializeAccountHeader(data []byte) (*AccountHeader, error) {
	pb := new(AccountHeaderProto)
	if err := proto.Unmarshal(data, pb); err != nil {
		return nil, err
	}
	return DeProtoAccountHeader(pb), nil
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
