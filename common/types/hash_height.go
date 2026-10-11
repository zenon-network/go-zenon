package types

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/zenon-network/go-zenon/common"
)

type HashHeight struct {
	Hash   Hash   `json:"hash"`
	Height uint64 `json:"height"`
}

var ZeroHashHeight = HashHeight{
	Hash:   ZeroHash,
	Height: 0,
}

func (b HashHeight) IsZero() bool {
	return b == ZeroHashHeight
}
func (b *HashHeight) Bytes() []byte {
	return common.JoinBytes(
		b.Hash.Bytes(),
		common.Uint64ToBytes(b.Height),
	)
}

func (b *HashHeight) Proto() *HashHeightProto {
	return &HashHeightProto{
		Hash:   b.Hash.Proto(),
		Height: b.Height,
	}
}
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
func (b *HashHeight) Serialize() []byte {
	data, err := proto.Marshal(b.Proto())
	common.DealWithErr(err)
	return data
}
func DeserializeHashHeight(data []byte) (*HashHeight, error) {
	pb := &HashHeightProto{}
	if err := proto.Unmarshal(data, pb); err != nil {
		return nil, err
	}
	return DeProtoHashHeight(pb)
}
