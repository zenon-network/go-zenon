// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.31.0
// 	protoc        v4.22.3
// source: chain/nom/protobuf.proto

package nom

import (
	types "github.com/zenon-network/go-zenon/common/types"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type AccountBlockProto struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Version              uint64                 `protobuf:"varint,1,opt,name=version,proto3" json:"version,omitempty"`
	ChainIdentifier      uint64                 `protobuf:"varint,2,opt,name=chainIdentifier,proto3" json:"chainIdentifier,omitempty"`
	BlockType            uint64                 `protobuf:"varint,3,opt,name=blockType,proto3" json:"blockType,omitempty"`
	Hash                 *types.HashProto       `protobuf:"bytes,4,opt,name=hash,proto3" json:"hash,omitempty"`
	PreviousHash         *types.HashProto       `protobuf:"bytes,5,opt,name=previousHash,proto3" json:"previousHash,omitempty"`
	Height               uint64                 `protobuf:"varint,6,opt,name=height,proto3" json:"height,omitempty"`
	MomentumAcknowledged *types.HashHeightProto `protobuf:"bytes,7,opt,name=momentumAcknowledged,proto3" json:"momentumAcknowledged,omitempty"`
	Address              *types.AddressProto    `protobuf:"bytes,8,opt,name=address,proto3" json:"address,omitempty"`
	ToAddress            *types.AddressProto    `protobuf:"bytes,9,opt,name=toAddress,proto3" json:"toAddress,omitempty"`
	Amount               []byte                 `protobuf:"bytes,10,opt,name=amount,proto3" json:"amount,omitempty"`
	TokenStandard        []byte                 `protobuf:"bytes,11,opt,name=tokenStandard,proto3" json:"tokenStandard,omitempty"`
	FromBlockHash        *types.HashProto       `protobuf:"bytes,12,opt,name=fromBlockHash,proto3" json:"fromBlockHash,omitempty"`
	DescendantBlocks     []*AccountBlockProto   `protobuf:"bytes,13,rep,name=descendantBlocks,proto3" json:"descendantBlocks,omitempty"`
	Data                 []byte                 `protobuf:"bytes,14,opt,name=data,proto3" json:"data,omitempty"`
	FusedPlasma          uint64                 `protobuf:"varint,15,opt,name=fusedPlasma,proto3" json:"fusedPlasma,omitempty"`
	Difficulty           uint64                 `protobuf:"varint,17,opt,name=difficulty,proto3" json:"difficulty,omitempty"`
	Nonce                []byte                 `protobuf:"bytes,18,opt,name=nonce,proto3" json:"nonce,omitempty"`
	BasePlasma           uint64                 `protobuf:"varint,19,opt,name=basePlasma,proto3" json:"basePlasma,omitempty"`
	TotalPlasma          uint64                 `protobuf:"varint,20,opt,name=totalPlasma,proto3" json:"totalPlasma,omitempty"`
	ChangesHash          *types.HashProto       `protobuf:"bytes,21,opt,name=changesHash,proto3" json:"changesHash,omitempty"`
	PublicKey            []byte                 `protobuf:"bytes,22,opt,name=publicKey,proto3" json:"publicKey,omitempty"`
	Signature            []byte                 `protobuf:"bytes,23,opt,name=signature,proto3" json:"signature,omitempty"`
}

func (x *AccountBlockProto) Reset() {
	*x = AccountBlockProto{}
	if protoimpl.UnsafeEnabled {
		mi := &file_chain_nom_protobuf_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *AccountBlockProto) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*AccountBlockProto) ProtoMessage() {}

func (x *AccountBlockProto) ProtoReflect() protoreflect.Message {
	mi := &file_chain_nom_protobuf_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use AccountBlockProto.ProtoReflect.Descriptor instead.
func (*AccountBlockProto) Descriptor() ([]byte, []int) {
	return file_chain_nom_protobuf_proto_rawDescGZIP(), []int{0}
}

func (x *AccountBlockProto) GetVersion() uint64 {
	if x != nil {
		return x.Version
	}
	return 0
}

func (x *AccountBlockProto) GetChainIdentifier() uint64 {
	if x != nil {
		return x.ChainIdentifier
	}
	return 0
}

func (x *AccountBlockProto) GetBlockType() uint64 {
	if x != nil {
		return x.BlockType
	}
	return 0
}

func (x *AccountBlockProto) GetHash() *types.HashProto {
	if x != nil {
		return x.Hash
	}
	return nil
}

func (x *AccountBlockProto) GetPreviousHash() *types.HashProto {
	if x != nil {
		return x.PreviousHash
	}
	return nil
}

func (x *AccountBlockProto) GetHeight() uint64 {
	if x != nil {
		return x.Height
	}
	return 0
}

func (x *AccountBlockProto) GetMomentumAcknowledged() *types.HashHeightProto {
	if x != nil {
		return x.MomentumAcknowledged
	}
	return nil
}

func (x *AccountBlockProto) GetAddress() *types.AddressProto {
	if x != nil {
		return x.Address
	}
	return nil
}

func (x *AccountBlockProto) GetToAddress() *types.AddressProto {
	if x != nil {
		return x.ToAddress
	}
	return nil
}

func (x *AccountBlockProto) GetAmount() []byte {
	if x != nil {
		return x.Amount
	}
	return nil
}

func (x *AccountBlockProto) GetTokenStandard() []byte {
	if x != nil {
		return x.TokenStandard
	}
	return nil
}

func (x *AccountBlockProto) GetFromBlockHash() *types.HashProto {
	if x != nil {
		return x.FromBlockHash
	}
	return nil
}

func (x *AccountBlockProto) GetDescendantBlocks() []*AccountBlockProto {
	if x != nil {
		return x.DescendantBlocks
	}
	return nil
}

func (x *AccountBlockProto) GetData() []byte {
	if x != nil {
		return x.Data
	}
	return nil
}

func (x *AccountBlockProto) GetFusedPlasma() uint64 {
	if x != nil {
		return x.FusedPlasma
	}
	return 0
}

func (x *AccountBlockProto) GetDifficulty() uint64 {
	if x != nil {
		return x.Difficulty
	}
	return 0
}

func (x *AccountBlockProto) GetNonce() []byte {
	if x != nil {
		return x.Nonce
	}
	return nil
}

func (x *AccountBlockProto) GetBasePlasma() uint64 {
	if x != nil {
		return x.BasePlasma
	}
	return 0
}

func (x *AccountBlockProto) GetTotalPlasma() uint64 {
	if x != nil {
		return x.TotalPlasma
	}
	return 0
}

func (x *AccountBlockProto) GetChangesHash() *types.HashProto {
	if x != nil {
		return x.ChangesHash
	}
	return nil
}

func (x *AccountBlockProto) GetPublicKey() []byte {
	if x != nil {
		return x.PublicKey
	}
	return nil
}

func (x *AccountBlockProto) GetSignature() []byte {
	if x != nil {
		return x.Signature
	}
	return nil
}

type MomentumProto struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Version         uint64                      `protobuf:"varint,1,opt,name=version,proto3" json:"version,omitempty"`
	ChainIdentifier uint64                      `protobuf:"varint,2,opt,name=chainIdentifier,proto3" json:"chainIdentifier,omitempty"`
	Hash            *types.HashProto            `protobuf:"bytes,3,opt,name=hash,proto3" json:"hash,omitempty"`
	PreviousHash    *types.HashProto            `protobuf:"bytes,4,opt,name=previousHash,proto3" json:"previousHash,omitempty"`
	Height          uint64                      `protobuf:"varint,5,opt,name=height,proto3" json:"height,omitempty"`
	Timestamp       uint64                      `protobuf:"varint,6,opt,name=timestamp,proto3" json:"timestamp,omitempty"`
	Data            []byte                      `protobuf:"bytes,7,opt,name=data,proto3" json:"data,omitempty"`
	Content         []*types.AccountHeaderProto `protobuf:"bytes,8,rep,name=content,proto3" json:"content,omitempty"`
	ChangesHash     *types.HashProto            `protobuf:"bytes,9,opt,name=changesHash,proto3" json:"changesHash,omitempty"`
	PublicKey       []byte                      `protobuf:"bytes,10,opt,name=publicKey,proto3" json:"publicKey,omitempty"`
	Signature       []byte                      `protobuf:"bytes,11,opt,name=signature,proto3" json:"signature,omitempty"`
}

func (x *MomentumProto) Reset() {
	*x = MomentumProto{}
	if protoimpl.UnsafeEnabled {
		mi := &file_chain_nom_protobuf_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *MomentumProto) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*MomentumProto) ProtoMessage() {}

func (x *MomentumProto) ProtoReflect() protoreflect.Message {
	mi := &file_chain_nom_protobuf_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use MomentumProto.ProtoReflect.Descriptor instead.
func (*MomentumProto) Descriptor() ([]byte, []int) {
	return file_chain_nom_protobuf_proto_rawDescGZIP(), []int{1}
}

func (x *MomentumProto) GetVersion() uint64 {
	if x != nil {
		return x.Version
	}
	return 0
}

func (x *MomentumProto) GetChainIdentifier() uint64 {
	if x != nil {
		return x.ChainIdentifier
	}
	return 0
}

func (x *MomentumProto) GetHash() *types.HashProto {
	if x != nil {
		return x.Hash
	}
	return nil
}

func (x *MomentumProto) GetPreviousHash() *types.HashProto {
	if x != nil {
		return x.PreviousHash
	}
	return nil
}

func (x *MomentumProto) GetHeight() uint64 {
	if x != nil {
		return x.Height
	}
	return 0
}

func (x *MomentumProto) GetTimestamp() uint64 {
	if x != nil {
		return x.Timestamp
	}
	return 0
}

func (x *MomentumProto) GetData() []byte {
	if x != nil {
		return x.Data
	}
	return nil
}

func (x *MomentumProto) GetContent() []*types.AccountHeaderProto {
	if x != nil {
		return x.Content
	}
	return nil
}

func (x *MomentumProto) GetChangesHash() *types.HashProto {
	if x != nil {
		return x.ChangesHash
	}
	return nil
}

func (x *MomentumProto) GetPublicKey() []byte {
	if x != nil {
		return x.PublicKey
	}
	return nil
}

func (x *MomentumProto) GetSignature() []byte {
	if x != nil {
		return x.Signature
	}
	return nil
}

var File_chain_nom_protobuf_proto protoreflect.FileDescriptor

var file_chain_nom_protobuf_proto_rawDesc = []byte{
	0x0a, 0x18, 0x63, 0x68, 0x61, 0x69, 0x6e, 0x2f, 0x6e, 0x6f, 0x6d, 0x2f, 0x70, 0x72, 0x6f, 0x74,
	0x6f, 0x62, 0x75, 0x66, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x03, 0x6e, 0x6f, 0x6d, 0x1a,
	0x1b, 0x63, 0x6f, 0x6d, 0x6d, 0x6f, 0x6e, 0x2f, 0x74, 0x79, 0x70, 0x65, 0x73, 0x2f, 0x70, 0x72,
	0x6f, 0x74, 0x6f, 0x62, 0x75, 0x66, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x22, 0xef, 0x06, 0x0a,
	0x11, 0x41, 0x63, 0x63, 0x6f, 0x75, 0x6e, 0x74, 0x42, 0x6c, 0x6f, 0x63, 0x6b, 0x50, 0x72, 0x6f,
	0x74, 0x6f, 0x12, 0x18, 0x0a, 0x07, 0x76, 0x65, 0x72, 0x73, 0x69, 0x6f, 0x6e, 0x18, 0x01, 0x20,
	0x01, 0x28, 0x04, 0x52, 0x07, 0x76, 0x65, 0x72, 0x73, 0x69, 0x6f, 0x6e, 0x12, 0x28, 0x0a, 0x0f,
	0x63, 0x68, 0x61, 0x69, 0x6e, 0x49, 0x64, 0x65, 0x6e, 0x74, 0x69, 0x66, 0x69, 0x65, 0x72, 0x18,
	0x02, 0x20, 0x01, 0x28, 0x04, 0x52, 0x0f, 0x63, 0x68, 0x61, 0x69, 0x6e, 0x49, 0x64, 0x65, 0x6e,
	0x74, 0x69, 0x66, 0x69, 0x65, 0x72, 0x12, 0x1c, 0x0a, 0x09, 0x62, 0x6c, 0x6f, 0x63, 0x6b, 0x54,
	0x79, 0x70, 0x65, 0x18, 0x03, 0x20, 0x01, 0x28, 0x04, 0x52, 0x09, 0x62, 0x6c, 0x6f, 0x63, 0x6b,
	0x54, 0x79, 0x70, 0x65, 0x12, 0x24, 0x0a, 0x04, 0x68, 0x61, 0x73, 0x68, 0x18, 0x04, 0x20, 0x01,
	0x28, 0x0b, 0x32, 0x10, 0x2e, 0x74, 0x79, 0x70, 0x65, 0x73, 0x2e, 0x48, 0x61, 0x73, 0x68, 0x50,
	0x72, 0x6f, 0x74, 0x6f, 0x52, 0x04, 0x68, 0x61, 0x73, 0x68, 0x12, 0x34, 0x0a, 0x0c, 0x70, 0x72,
	0x65, 0x76, 0x69, 0x6f, 0x75, 0x73, 0x48, 0x61, 0x73, 0x68, 0x18, 0x05, 0x20, 0x01, 0x28, 0x0b,
	0x32, 0x10, 0x2e, 0x74, 0x79, 0x70, 0x65, 0x73, 0x2e, 0x48, 0x61, 0x73, 0x68, 0x50, 0x72, 0x6f,
	0x74, 0x6f, 0x52, 0x0c, 0x70, 0x72, 0x65, 0x76, 0x69, 0x6f, 0x75, 0x73, 0x48, 0x61, 0x73, 0x68,
	0x12, 0x16, 0x0a, 0x06, 0x68, 0x65, 0x69, 0x67, 0x68, 0x74, 0x18, 0x06, 0x20, 0x01, 0x28, 0x04,
	0x52, 0x06, 0x68, 0x65, 0x69, 0x67, 0x68, 0x74, 0x12, 0x4a, 0x0a, 0x14, 0x6d, 0x6f, 0x6d, 0x65,
	0x6e, 0x74, 0x75, 0x6d, 0x41, 0x63, 0x6b, 0x6e, 0x6f, 0x77, 0x6c, 0x65, 0x64, 0x67, 0x65, 0x64,
	0x18, 0x07, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x16, 0x2e, 0x74, 0x79, 0x70, 0x65, 0x73, 0x2e, 0x48,
	0x61, 0x73, 0x68, 0x48, 0x65, 0x69, 0x67, 0x68, 0x74, 0x50, 0x72, 0x6f, 0x74, 0x6f, 0x52, 0x14,
	0x6d, 0x6f, 0x6d, 0x65, 0x6e, 0x74, 0x75, 0x6d, 0x41, 0x63, 0x6b, 0x6e, 0x6f, 0x77, 0x6c, 0x65,
	0x64, 0x67, 0x65, 0x64, 0x12, 0x2d, 0x0a, 0x07, 0x61, 0x64, 0x64, 0x72, 0x65, 0x73, 0x73, 0x18,
	0x08, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x13, 0x2e, 0x74, 0x79, 0x70, 0x65, 0x73, 0x2e, 0x41, 0x64,
	0x64, 0x72, 0x65, 0x73, 0x73, 0x50, 0x72, 0x6f, 0x74, 0x6f, 0x52, 0x07, 0x61, 0x64, 0x64, 0x72,
	0x65, 0x73, 0x73, 0x12, 0x31, 0x0a, 0x09, 0x74, 0x6f, 0x41, 0x64, 0x64, 0x72, 0x65, 0x73, 0x73,
	0x18, 0x09, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x13, 0x2e, 0x74, 0x79, 0x70, 0x65, 0x73, 0x2e, 0x41,
	0x64, 0x64, 0x72, 0x65, 0x73, 0x73, 0x50, 0x72, 0x6f, 0x74, 0x6f, 0x52, 0x09, 0x74, 0x6f, 0x41,
	0x64, 0x64, 0x72, 0x65, 0x73, 0x73, 0x12, 0x16, 0x0a, 0x06, 0x61, 0x6d, 0x6f, 0x75, 0x6e, 0x74,
	0x18, 0x0a, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x06, 0x61, 0x6d, 0x6f, 0x75, 0x6e, 0x74, 0x12, 0x24,
	0x0a, 0x0d, 0x74, 0x6f, 0x6b, 0x65, 0x6e, 0x53, 0x74, 0x61, 0x6e, 0x64, 0x61, 0x72, 0x64, 0x18,
	0x0b, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x0d, 0x74, 0x6f, 0x6b, 0x65, 0x6e, 0x53, 0x74, 0x61, 0x6e,
	0x64, 0x61, 0x72, 0x64, 0x12, 0x36, 0x0a, 0x0d, 0x66, 0x72, 0x6f, 0x6d, 0x42, 0x6c, 0x6f, 0x63,
	0x6b, 0x48, 0x61, 0x73, 0x68, 0x18, 0x0c, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x10, 0x2e, 0x74, 0x79,
	0x70, 0x65, 0x73, 0x2e, 0x48, 0x61, 0x73, 0x68, 0x50, 0x72, 0x6f, 0x74, 0x6f, 0x52, 0x0d, 0x66,
	0x72, 0x6f, 0x6d, 0x42, 0x6c, 0x6f, 0x63, 0x6b, 0x48, 0x61, 0x73, 0x68, 0x12, 0x42, 0x0a, 0x10,
	0x64, 0x65, 0x73, 0x63, 0x65, 0x6e, 0x64, 0x61, 0x6e, 0x74, 0x42, 0x6c, 0x6f, 0x63, 0x6b, 0x73,
	0x18, 0x0d, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x16, 0x2e, 0x6e, 0x6f, 0x6d, 0x2e, 0x41, 0x63, 0x63,
	0x6f, 0x75, 0x6e, 0x74, 0x42, 0x6c, 0x6f, 0x63, 0x6b, 0x50, 0x72, 0x6f, 0x74, 0x6f, 0x52, 0x10,
	0x64, 0x65, 0x73, 0x63, 0x65, 0x6e, 0x64, 0x61, 0x6e, 0x74, 0x42, 0x6c, 0x6f, 0x63, 0x6b, 0x73,
	0x12, 0x12, 0x0a, 0x04, 0x64, 0x61, 0x74, 0x61, 0x18, 0x0e, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x04,
	0x64, 0x61, 0x74, 0x61, 0x12, 0x20, 0x0a, 0x0b, 0x66, 0x75, 0x73, 0x65, 0x64, 0x50, 0x6c, 0x61,
	0x73, 0x6d, 0x61, 0x18, 0x0f, 0x20, 0x01, 0x28, 0x04, 0x52, 0x0b, 0x66, 0x75, 0x73, 0x65, 0x64,
	0x50, 0x6c, 0x61, 0x73, 0x6d, 0x61, 0x12, 0x1e, 0x0a, 0x0a, 0x64, 0x69, 0x66, 0x66, 0x69, 0x63,
	0x75, 0x6c, 0x74, 0x79, 0x18, 0x11, 0x20, 0x01, 0x28, 0x04, 0x52, 0x0a, 0x64, 0x69, 0x66, 0x66,
	0x69, 0x63, 0x75, 0x6c, 0x74, 0x79, 0x12, 0x14, 0x0a, 0x05, 0x6e, 0x6f, 0x6e, 0x63, 0x65, 0x18,
	0x12, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x05, 0x6e, 0x6f, 0x6e, 0x63, 0x65, 0x12, 0x1e, 0x0a, 0x0a,
	0x62, 0x61, 0x73, 0x65, 0x50, 0x6c, 0x61, 0x73, 0x6d, 0x61, 0x18, 0x13, 0x20, 0x01, 0x28, 0x04,
	0x52, 0x0a, 0x62, 0x61, 0x73, 0x65, 0x50, 0x6c, 0x61, 0x73, 0x6d, 0x61, 0x12, 0x20, 0x0a, 0x0b,
	0x74, 0x6f, 0x74, 0x61, 0x6c, 0x50, 0x6c, 0x61, 0x73, 0x6d, 0x61, 0x18, 0x14, 0x20, 0x01, 0x28,
	0x04, 0x52, 0x0b, 0x74, 0x6f, 0x74, 0x61, 0x6c, 0x50, 0x6c, 0x61, 0x73, 0x6d, 0x61, 0x12, 0x32,
	0x0a, 0x0b, 0x63, 0x68, 0x61, 0x6e, 0x67, 0x65, 0x73, 0x48, 0x61, 0x73, 0x68, 0x18, 0x15, 0x20,
	0x01, 0x28, 0x0b, 0x32, 0x10, 0x2e, 0x74, 0x79, 0x70, 0x65, 0x73, 0x2e, 0x48, 0x61, 0x73, 0x68,
	0x50, 0x72, 0x6f, 0x74, 0x6f, 0x52, 0x0b, 0x63, 0x68, 0x61, 0x6e, 0x67, 0x65, 0x73, 0x48, 0x61,
	0x73, 0x68, 0x12, 0x1c, 0x0a, 0x09, 0x70, 0x75, 0x62, 0x6c, 0x69, 0x63, 0x4b, 0x65, 0x79, 0x18,
	0x16, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x09, 0x70, 0x75, 0x62, 0x6c, 0x69, 0x63, 0x4b, 0x65, 0x79,
	0x12, 0x1c, 0x0a, 0x09, 0x73, 0x69, 0x67, 0x6e, 0x61, 0x74, 0x75, 0x72, 0x65, 0x18, 0x17, 0x20,
	0x01, 0x28, 0x0c, 0x52, 0x09, 0x73, 0x69, 0x67, 0x6e, 0x61, 0x74, 0x75, 0x72, 0x65, 0x22, 0x9e,
	0x03, 0x0a, 0x0d, 0x4d, 0x6f, 0x6d, 0x65, 0x6e, 0x74, 0x75, 0x6d, 0x50, 0x72, 0x6f, 0x74, 0x6f,
	0x12, 0x18, 0x0a, 0x07, 0x76, 0x65, 0x72, 0x73, 0x69, 0x6f, 0x6e, 0x18, 0x01, 0x20, 0x01, 0x28,
	0x04, 0x52, 0x07, 0x76, 0x65, 0x72, 0x73, 0x69, 0x6f, 0x6e, 0x12, 0x28, 0x0a, 0x0f, 0x63, 0x68,
	0x61, 0x69, 0x6e, 0x49, 0x64, 0x65, 0x6e, 0x74, 0x69, 0x66, 0x69, 0x65, 0x72, 0x18, 0x02, 0x20,
	0x01, 0x28, 0x04, 0x52, 0x0f, 0x63, 0x68, 0x61, 0x69, 0x6e, 0x49, 0x64, 0x65, 0x6e, 0x74, 0x69,
	0x66, 0x69, 0x65, 0x72, 0x12, 0x24, 0x0a, 0x04, 0x68, 0x61, 0x73, 0x68, 0x18, 0x03, 0x20, 0x01,
	0x28, 0x0b, 0x32, 0x10, 0x2e, 0x74, 0x79, 0x70, 0x65, 0x73, 0x2e, 0x48, 0x61, 0x73, 0x68, 0x50,
	0x72, 0x6f, 0x74, 0x6f, 0x52, 0x04, 0x68, 0x61, 0x73, 0x68, 0x12, 0x34, 0x0a, 0x0c, 0x70, 0x72,
	0x65, 0x76, 0x69, 0x6f, 0x75, 0x73, 0x48, 0x61, 0x73, 0x68, 0x18, 0x04, 0x20, 0x01, 0x28, 0x0b,
	0x32, 0x10, 0x2e, 0x74, 0x79, 0x70, 0x65, 0x73, 0x2e, 0x48, 0x61, 0x73, 0x68, 0x50, 0x72, 0x6f,
	0x74, 0x6f, 0x52, 0x0c, 0x70, 0x72, 0x65, 0x76, 0x69, 0x6f, 0x75, 0x73, 0x48, 0x61, 0x73, 0x68,
	0x12, 0x16, 0x0a, 0x06, 0x68, 0x65, 0x69, 0x67, 0x68, 0x74, 0x18, 0x05, 0x20, 0x01, 0x28, 0x04,
	0x52, 0x06, 0x68, 0x65, 0x69, 0x67, 0x68, 0x74, 0x12, 0x1c, 0x0a, 0x09, 0x74, 0x69, 0x6d, 0x65,
	0x73, 0x74, 0x61, 0x6d, 0x70, 0x18, 0x06, 0x20, 0x01, 0x28, 0x04, 0x52, 0x09, 0x74, 0x69, 0x6d,
	0x65, 0x73, 0x74, 0x61, 0x6d, 0x70, 0x12, 0x12, 0x0a, 0x04, 0x64, 0x61, 0x74, 0x61, 0x18, 0x07,
	0x20, 0x01, 0x28, 0x0c, 0x52, 0x04, 0x64, 0x61, 0x74, 0x61, 0x12, 0x33, 0x0a, 0x07, 0x63, 0x6f,
	0x6e, 0x74, 0x65, 0x6e, 0x74, 0x18, 0x08, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x19, 0x2e, 0x74, 0x79,
	0x70, 0x65, 0x73, 0x2e, 0x41, 0x63, 0x63, 0x6f, 0x75, 0x6e, 0x74, 0x48, 0x65, 0x61, 0x64, 0x65,
	0x72, 0x50, 0x72, 0x6f, 0x74, 0x6f, 0x52, 0x07, 0x63, 0x6f, 0x6e, 0x74, 0x65, 0x6e, 0x74, 0x12,
	0x32, 0x0a, 0x0b, 0x63, 0x68, 0x61, 0x6e, 0x67, 0x65, 0x73, 0x48, 0x61, 0x73, 0x68, 0x18, 0x09,
	0x20, 0x01, 0x28, 0x0b, 0x32, 0x10, 0x2e, 0x74, 0x79, 0x70, 0x65, 0x73, 0x2e, 0x48, 0x61, 0x73,
	0x68, 0x50, 0x72, 0x6f, 0x74, 0x6f, 0x52, 0x0b, 0x63, 0x68, 0x61, 0x6e, 0x67, 0x65, 0x73, 0x48,
	0x61, 0x73, 0x68, 0x12, 0x1c, 0x0a, 0x09, 0x70, 0x75, 0x62, 0x6c, 0x69, 0x63, 0x4b, 0x65, 0x79,
	0x18, 0x0a, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x09, 0x70, 0x75, 0x62, 0x6c, 0x69, 0x63, 0x4b, 0x65,
	0x79, 0x12, 0x1c, 0x0a, 0x09, 0x73, 0x69, 0x67, 0x6e, 0x61, 0x74, 0x75, 0x72, 0x65, 0x18, 0x0b,
	0x20, 0x01, 0x28, 0x0c, 0x52, 0x09, 0x73, 0x69, 0x67, 0x6e, 0x61, 0x74, 0x75, 0x72, 0x65, 0x42,
	0x2d, 0x5a, 0x2b, 0x67, 0x69, 0x74, 0x68, 0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x7a, 0x65,
	0x6e, 0x6f, 0x6e, 0x2d, 0x6e, 0x65, 0x74, 0x77, 0x6f, 0x72, 0x6b, 0x2f, 0x67, 0x6f, 0x2d, 0x7a,
	0x65, 0x6e, 0x6f, 0x6e, 0x2f, 0x63, 0x68, 0x61, 0x69, 0x6e, 0x2f, 0x6e, 0x6f, 0x6d, 0x62, 0x06,
	0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_chain_nom_protobuf_proto_rawDescOnce sync.Once
	file_chain_nom_protobuf_proto_rawDescData = file_chain_nom_protobuf_proto_rawDesc
)

func file_chain_nom_protobuf_proto_rawDescGZIP() []byte {
	file_chain_nom_protobuf_proto_rawDescOnce.Do(func() {
		file_chain_nom_protobuf_proto_rawDescData = protoimpl.X.CompressGZIP(file_chain_nom_protobuf_proto_rawDescData)
	})
	return file_chain_nom_protobuf_proto_rawDescData
}

var file_chain_nom_protobuf_proto_msgTypes = make([]protoimpl.MessageInfo, 2)
var file_chain_nom_protobuf_proto_goTypes = []interface{}{
	(*AccountBlockProto)(nil),        // 0: nom.AccountBlockProto
	(*MomentumProto)(nil),            // 1: nom.MomentumProto
	(*types.HashProto)(nil),          // 2: types.HashProto
	(*types.HashHeightProto)(nil),    // 3: types.HashHeightProto
	(*types.AddressProto)(nil),       // 4: types.AddressProto
	(*types.AccountHeaderProto)(nil), // 5: types.AccountHeaderProto
}
var file_chain_nom_protobuf_proto_depIdxs = []int32{
	2,  // 0: nom.AccountBlockProto.hash:type_name -> types.HashProto
	2,  // 1: nom.AccountBlockProto.previousHash:type_name -> types.HashProto
	3,  // 2: nom.AccountBlockProto.momentumAcknowledged:type_name -> types.HashHeightProto
	4,  // 3: nom.AccountBlockProto.address:type_name -> types.AddressProto
	4,  // 4: nom.AccountBlockProto.toAddress:type_name -> types.AddressProto
	2,  // 5: nom.AccountBlockProto.fromBlockHash:type_name -> types.HashProto
	0,  // 6: nom.AccountBlockProto.descendantBlocks:type_name -> nom.AccountBlockProto
	2,  // 7: nom.AccountBlockProto.changesHash:type_name -> types.HashProto
	2,  // 8: nom.MomentumProto.hash:type_name -> types.HashProto
	2,  // 9: nom.MomentumProto.previousHash:type_name -> types.HashProto
	5,  // 10: nom.MomentumProto.content:type_name -> types.AccountHeaderProto
	2,  // 11: nom.MomentumProto.changesHash:type_name -> types.HashProto
	12, // [12:12] is the sub-list for method output_type
	12, // [12:12] is the sub-list for method input_type
	12, // [12:12] is the sub-list for extension type_name
	12, // [12:12] is the sub-list for extension extendee
	0,  // [0:12] is the sub-list for field type_name
}

func init() { file_chain_nom_protobuf_proto_init() }
func file_chain_nom_protobuf_proto_init() {
	if File_chain_nom_protobuf_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_chain_nom_protobuf_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*AccountBlockProto); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_chain_nom_protobuf_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*MomentumProto); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_chain_nom_protobuf_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   2,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_chain_nom_protobuf_proto_goTypes,
		DependencyIndexes: file_chain_nom_protobuf_proto_depIdxs,
		MessageInfos:      file_chain_nom_protobuf_proto_msgTypes,
	}.Build()
	File_chain_nom_protobuf_proto = out.File
	file_chain_nom_protobuf_proto_rawDesc = nil
	file_chain_nom_protobuf_proto_goTypes = nil
	file_chain_nom_protobuf_proto_depIdxs = nil
}
