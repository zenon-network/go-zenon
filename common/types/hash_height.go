package types

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/zenon-network/go-zenon/common"
)

// HashHeight identifies a block within a chain by pairing its hash
// with its height. It is the canonical block identifier for both
// momentums and account blocks: momentum content entries, previous
// block references and store lookups all use it.
type HashHeight struct {
	Hash   Hash   `json:"hash"`
	Height uint64 `json:"height"`
}

// ZeroHashHeight is the zero HashHeight (zero hash, height 0), used as
// the identifier that precedes the first block of a chain.
var ZeroHashHeight = HashHeight{
	Hash:   ZeroHash,
	Height: 0,
}

// IsZero reports whether the identifier equals ZeroHashHeight.
func (b HashHeight) IsZero() bool {
	return b == ZeroHashHeight
}

// Bytes returns a fixed 40-byte encoding: the 32 hash bytes followed
// by the height as a big-endian uint64.
func (b *HashHeight) Bytes() []byte {
	return common.JoinBytes(
		b.Hash.Bytes(),
		common.Uint64ToBytes(b.Height),
	)
}

// Proto converts the identifier to its protobuf message.
func (b *HashHeight) Proto() *HashHeightProto {
	return &HashHeightProto{
		Hash:   b.Hash.Proto(),
		Height: b.Height,
	}
}

// DeProtoHashHeight is the inverse of Proto. It returns an error for
// a nil proto or when the embedded hash payload is not exactly
// HashSize bytes (via DeProtoHash).
func DeProtoHashHeight(pb *HashHeightProto) (*HashHeight, error) {
	if pb == nil {
		return nil, fmt.Errorf("invalid DeProto - nil hash height proto")
	}
	h, err := DeProtoHash(pb.Hash)
	if err != nil {
		return nil, err
	}
	return &HashHeight{
		Hash:   *h,
		Height: pb.Height,
	}, nil
}

// Serialize encodes the identifier as protobuf bytes, panicking on
// marshalling failure. DeserializeHashHeight is the inverse.
func (b *HashHeight) Serialize() []byte {
	data, err := proto.Marshal(b.Proto())
	common.DealWithErr(err)
	return data
}

// DeserializeHashHeight decodes a HashHeight from its protobuf bytes,
// returning an error if the data is not a valid protobuf message or if
// its hash payload is not exactly HashSize bytes (via
// DeProtoHashHeight).
func DeserializeHashHeight(data []byte) (*HashHeight, error) {
	pb := &HashHeightProto{}
	if err := proto.Unmarshal(data, pb); err != nil {
		return nil, err
	}
	return DeProtoHashHeight(pb)
}
