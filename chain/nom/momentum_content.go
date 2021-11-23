package nom

import (
	"bytes"
	"sort"

	"github.com/zenon-network/go-zenon/common/types"
)

const AccountBlockHeaderRawLen = types.AddressSize + types.HashSize + 8 // (+8 from height)

type MomentumContent []*types.AccountHeader

func (mc *MomentumContent) Proto() []*types.AccountHeaderProto {
	arr := ([]*types.AccountHeader)(*mc)
	list := make([]*types.AccountHeaderProto, len(arr))
	for i := range arr {
		list[i] = arr[i].Proto()
	}
	return list
}
func DeProtoMomentumContent(content []*types.AccountHeaderProto) []*types.AccountHeader {
	list := make([]*types.AccountHeader, len(content))
	for i := range content {
		list[i] = types.DeProtoAccountHeader(content[i])
	}
	return list
}
func (mc *MomentumContent) Bytes() []byte {
	arr := ([]*types.AccountHeader)(*mc)
	source := make([]byte, 0, len(arr)*AccountBlockHeaderRawLen)
	for _, header := range arr {
		source = append(source, header.Bytes()...)
	}
	return source
}
func (mc *MomentumContent) Hash() types.Hash {
	return types.NewHash(mc.Bytes())
}

func NewMomentumContent(blocks []*AccountBlock) MomentumContent {
	content := make([]*types.AccountHeader, len(blocks))
	for i := range blocks {
		header := blocks[i].Header()
		content[i] = &header
	}
	sort.Slice(content, AccountBlockHeaderComparer(content))
	return content
}

func AccountBlockHeaderComparer(list []*types.AccountHeader) func(a, b int) bool {
	return func(a, b int) bool {
		return bytes.Compare(list[a].Bytes(), list[b].Bytes()) <= 0
	}
}
