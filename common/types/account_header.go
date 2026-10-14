package types

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/zenon-network/go-zenon/common"
)

type AccountHeader struct {
	Address Address `json:"address"`
	HashHeight
}

func (abh *AccountHeader) Identifier() HashHeight {
	return abh.HashHeight
}

func (abh *AccountHeader) Proto() *AccountHeaderProto {
	return &AccountHeaderProto{
		Address:    abh.Address.Proto(),
		HashHeight: abh.HashHeight.Proto(),
	}
}
func DeProtoAccountHeader(pb *AccountHeaderProto) (*AccountHeader, error) {
	if pb == nil {
		return nil, fmt.Errorf("invalid DeProto - nil account header proto")
	}
	hh, err := DeProtoHashHeight(pb.HashHeight)
	if err != nil {
		return nil, err
	}
	return &AccountHeader{
		Address:    *DeProtoAddress(pb.Address),
		HashHeight: *hh,
	}, nil
}
func (abh *AccountHeader) Serialize() ([]byte, error) {
	return proto.Marshal(abh.Proto())
}
func DeserializeAccountHeader(data []byte) (*AccountHeader, error) {
	pb := new(AccountHeaderProto)
	if err := proto.Unmarshal(data, pb); err != nil {
		return nil, err
	}
	return DeProtoAccountHeader(pb)
}

func (abh *AccountHeader) Bytes() []byte {
	return common.JoinBytes(
		abh.Address.Bytes(),
		common.Uint64ToBytes(abh.Height),
		abh.Hash.Bytes())
}
