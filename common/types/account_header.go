package types

import (
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
func DeProtoAccountHeader(pb *AccountHeaderProto) *AccountHeader {
	return &AccountHeader{
		Address:    *DeProtoAddress(pb.Address),
		HashHeight: *DeProtoHashHeight(pb.HashHeight),
	}
}
func (abh *AccountHeader) Serialize() ([]byte, error) {
	return proto.Marshal(abh.Proto())
}
func DeserializeAccountHeader(data []byte) (*AccountHeader, error) {
	pb := new(AccountHeaderProto)
	if err := proto.Unmarshal(data, pb); err != nil {
		return nil, err
	}
	return DeProtoAccountHeader(pb), nil
}

func (abh *AccountHeader) Bytes() []byte {
	return common.JoinBytes(
		abh.Address.Bytes(),
		common.Uint64ToBytes(abh.Height),
		abh.Hash.Bytes())
}
