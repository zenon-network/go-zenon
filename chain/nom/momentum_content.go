package nom

import (
	"bytes"
	"sort"

	"github.com/zenon-network/go-zenon/common/types"
)

// AccountBlockHeaderRawLen is the length of one serialized account
// header (types.AccountHeader.Bytes), laid out as 20 address bytes,
// 8 big-endian height bytes, then 32 hash bytes — 60 in total.
const AccountBlockHeaderRawLen = types.AddressSize + types.HashSize + 8 // (+8 from height)

// MomentumContent is the list of account headers a momentum confirms.
// NewMomentumContent produces it sorted by the headers' raw byte
// encoding; its hash is committed in the momentum hash.
type MomentumContent []*types.AccountHeader

// Proto converts the content to its protobuf form, one message per
// header, preserving order.
func (mc *MomentumContent) Proto() []*types.AccountHeaderProto {
	arr := ([]*types.AccountHeader)(*mc)
	list := make([]*types.AccountHeaderProto, len(arr))
	for i := range arr {
		list[i] = arr[i].Proto()
	}
	return list
}

// DeProtoMomentumContent is the inverse of MomentumContent.Proto. It
// panics (via types.DeProtoAccountHeader) on malformed address or
// hash payloads.
func DeProtoMomentumContent(content []*types.AccountHeaderProto) []*types.AccountHeader {
	list := make([]*types.AccountHeader, len(content))
	for i := range content {
		list[i] = types.DeProtoAccountHeader(content[i])
	}
	return list
}

// Bytes returns the concatenated raw encodings of the headers
// (types.AccountHeader.Bytes), AccountBlockHeaderRawLen bytes each,
// in list order.
func (mc *MomentumContent) Bytes() []byte {
	arr := ([]*types.AccountHeader)(*mc)
	source := make([]byte, 0, len(arr)*AccountBlockHeaderRawLen)
	for _, header := range arr {
		source = append(source, header.Bytes()...)
	}
	return source
}

// Hash returns the SHA3-256 digest of Bytes; this is the content
// digest committed by Momentum.ComputeHash.
func (mc *MomentumContent) Hash() types.Hash {
	return types.NewHash(mc.Bytes())
}

// NewMomentumContent builds the content for a momentum confirming the
// given account blocks: one header per block, sorted by the headers'
// raw byte encoding (see AccountBlockHeaderComparer). Both the pillar
// and the genesis builder produce content through this function.
func NewMomentumContent(blocks []*AccountBlock) MomentumContent {
	content := make([]*types.AccountHeader, len(blocks))
	for i := range blocks {
		header := blocks[i].Header()
		content[i] = &header
	}
	sort.Slice(content, AccountBlockHeaderComparer(content))
	return content
}

// AccountBlockHeaderComparer returns a less function over list,
// ordering headers by bytes.Compare of their raw encodings — address
// first, then height, then hash. Note it returns true for equal
// elements (<= 0), so it is not a strict less; this is harmless for
// sorting since headers in a momentum are unique.
func AccountBlockHeaderComparer(list []*types.AccountHeader) func(a, b int) bool {
	return func(a, b int) bool {
		return bytes.Compare(list[a].Bytes(), list[b].Bytes()) <= 0
	}
}
