package nom

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/zenon-network/go-zenon/common/types"
)

// Decode-path coverage for the three gaps left after #141 and #154 (#159):
// a malformed descendant names its index, a malformed momentum-content
// header names its index through DeserializeMomentum, and input nested
// past the protobuf decoder's default limit is rejected by the decoder
// before DeProtoAccountBlock recurses into it.

// TestDeserializeAccountBlock_MalformedDescendantNamesIndex: a descendant
// with a wrong-length address fails the whole block, and the error names
// the failing descendant's position and the field inside it.
func TestDeserializeAccountBlock_MalformedDescendantNamesIndex(t *testing.T) {
	bad := validAccountBlockProto()
	bad.Address = &types.AddressProto{Address: make([]byte, 10)} // wrong length
	pb := validAccountBlockProto()
	pb.DescendantBlocks = []*AccountBlockProto{validAccountBlockProto(), bad}

	data, err := proto.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DeserializeAccountBlock(data)
	if err == nil {
		t.Fatal("expected error for a malformed descendant, got nil")
	}
	if !strings.Contains(err.Error(), "invalid descendant block at index 1:") {
		t.Fatalf("expected the error to name descendant index 1, got: %v", err)
	}
	if strings.Contains(err.Error(), "index 0") {
		t.Fatalf("the valid descendant at index 0 must not be blamed, got: %v", err)
	}
	if !strings.Contains(err.Error(), "invalid address") {
		t.Fatalf("expected the error to name the descendant's address field, got: %v", err)
	}
}

// TestDeserializeAccountBlock_MalformedNestedDescendantNamesPath: the
// index context accumulates through each level of descendants, so a
// failure two levels down reads outer index, then inner index.
func TestDeserializeAccountBlock_MalformedNestedDescendantNamesPath(t *testing.T) {
	bad := validAccountBlockProto()
	bad.Nonce = make([]byte, 3) // wrong length
	inner := validAccountBlockProto()
	inner.DescendantBlocks = []*AccountBlockProto{validAccountBlockProto(), validAccountBlockProto(), bad}
	pb := validAccountBlockProto()
	pb.DescendantBlocks = []*AccountBlockProto{inner}

	data, err := proto.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DeserializeAccountBlock(data)
	if err == nil {
		t.Fatal("expected error for a malformed nested descendant, got nil")
	}
	want := "invalid descendant block at index 0: invalid descendant block at index 2: invalid nonce"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error to contain %q, got: %v", want, err)
	}
}

// TestDeserializeAccountBlock_ValidDescendantsRoundTrip is the control:
// well-formed descendants decode in order with their own fields intact.
func TestDeserializeAccountBlock_ValidDescendantsRoundTrip(t *testing.T) {
	first := validAccountBlockProto()
	first.Height = 7
	second := validAccountBlockProto()
	second.Height = 8
	pb := validAccountBlockProto()
	pb.DescendantBlocks = []*AccountBlockProto{first, second}

	data, err := proto.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	ab, err := DeserializeAccountBlock(data)
	if err != nil {
		t.Fatalf("expected valid descendants to decode, got: %v", err)
	}
	if len(ab.DescendantBlocks) != 2 {
		t.Fatalf("expected 2 descendants, got %d", len(ab.DescendantBlocks))
	}
	if ab.DescendantBlocks[0].Height != 7 || ab.DescendantBlocks[1].Height != 8 {
		t.Fatalf("descendant order or fields lost: heights %d, %d", ab.DescendantBlocks[0].Height, ab.DescendantBlocks[1].Height)
	}
	if ab.DescendantBlocks[1].Address != types.Address(second.Address.Address) {
		t.Fatalf("descendant address mismatch: got %x", ab.DescendantBlocks[1].Address)
	}
}

// validMomentumProtoWithContent returns a well-formed MomentumProto whose
// content lists n headers with distinct addresses and heights.
func validMomentumProtoWithContent(n int) *MomentumProto {
	content := make(MomentumContent, 0, n)
	for i := 0; i < n; i++ {
		var addr types.Address
		for j := range addr {
			addr[j] = byte(10*i + j + 1)
		}
		content = append(content, &types.AccountHeader{
			Address:    addr,
			HashHeight: types.HashHeight{Hash: types.Hash{byte(i + 1)}, Height: uint64(100 + i)},
		})
	}
	m := &Momentum{
		Version:         1,
		ChainIdentifier: 1,
		Hash:            types.Hash{1},
		PreviousHash:    types.Hash{2},
		Height:          42,
		TimestampUnix:   1000000,
		Content:         content,
		ChangesHash:     types.Hash{3},
	}
	return m.Proto()
}

// TestDeserializeMomentum_MalformedContentAddressNamesIndex: a content
// header with a wrong-length address fails DeserializeMomentum, and the
// error carries the content index and the field.
func TestDeserializeMomentum_MalformedContentAddressNamesIndex(t *testing.T) {
	pb := validMomentumProtoWithContent(3)
	pb.Content[2].Address = &types.AddressProto{Address: make([]byte, 7)} // wrong length

	data, err := proto.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DeserializeMomentum(data)
	if err == nil {
		t.Fatal("expected error for a malformed content address, got nil")
	}
	want := "invalid content: invalid momentum content at index 2: invalid address"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error to contain %q, got: %v", want, err)
	}
	if strings.Contains(err.Error(), "index 0") || strings.Contains(err.Error(), "index 1") {
		t.Fatalf("the valid headers must not be blamed, got: %v", err)
	}
}

// TestDeserializeMomentum_MalformedContentHashNamesIndex: the same path
// for the header's hash payload, at a different index.
func TestDeserializeMomentum_MalformedContentHashNamesIndex(t *testing.T) {
	pb := validMomentumProtoWithContent(2)
	pb.Content[0].HashHeight.Hash = &types.HashProto{Hash: make([]byte, 31)} // wrong length

	data, err := proto.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DeserializeMomentum(data)
	if err == nil {
		t.Fatal("expected error for a malformed content hash, got nil")
	}
	if !strings.Contains(err.Error(), "invalid content: invalid momentum content at index 0:") {
		t.Fatalf("expected the error to name content index 0, got: %v", err)
	}
	if !strings.Contains(err.Error(), "wanted hash size") {
		t.Fatalf("expected the error to name the hash size, got: %v", err)
	}
}

// TestDeserializeMomentum_ValidContentRoundTrip is the control: the
// headers come back in order with address and height intact.
func TestDeserializeMomentum_ValidContentRoundTrip(t *testing.T) {
	pb := validMomentumProtoWithContent(3)
	data, err := proto.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	m, err := DeserializeMomentum(data)
	if err != nil {
		t.Fatalf("expected valid content to decode, got: %v", err)
	}
	if len(m.Content) != 3 {
		t.Fatalf("expected 3 headers, got %d", len(m.Content))
	}
	for i, h := range m.Content {
		if h.Height != uint64(100+i) {
			t.Fatalf("header %d: height %d, want %d", i, h.Height, 100+i)
		}
		if h.Address != types.Address(pb.Content[i].Address.Address) {
			t.Fatalf("header %d: address mismatch", i)
		}
	}
}

// nestDescendant wraps payload (the wire encoding of an AccountBlockProto)
// depth times: each level is the wire encoding of a well-formed block
// (wrapper) followed by the previous level as its single descendantBlocks
// entry (field 13). Protobuf accepts fields in any order, so every level
// is a valid block. The sizes are computed first and the bytes written
// outside-in into one pre-sized buffer, so the builder is linear in the
// output and never recurses.
func nestDescendant(wrapper, payload []byte, depth int) []byte {
	tag := protowire.AppendTag(nil, 13, protowire.BytesType)
	sizes := make([]int, depth+1) // sizes[k] = encoded size of the level with k wrappers
	sizes[0] = len(payload)
	for k := 1; k <= depth; k++ {
		sizes[k] = len(wrapper) + len(tag) + protowire.SizeVarint(uint64(sizes[k-1])) + sizes[k-1]
	}
	out := make([]byte, 0, sizes[depth])
	for k := depth; k >= 1; k-- {
		out = append(out, wrapper...)
		out = append(out, tag...)
		out = protowire.AppendVarint(out, uint64(sizes[k-1]))
	}
	return append(out, payload...)
}

// TestDeserializeAccountBlock_NestedPastDecoderLimitIsRejected: bytes
// nested past the protobuf decoder's default recursion limit come back as
// a decoder error. The fixture has DefaultRecursionLimit+2 block messages
// (the root, the wrappers and the leaf); each block's hash fields add
// embedded depth of their own, so the deepest message sits a few levels
// past the limit rather than exactly one. DeProtoAccountBlock has no depth guard of
// its own; this pins the assumption that the decoder stops such input
// first, so the recursion in DeProtoAccountBlock is never reached.
func TestDeserializeAccountBlock_NestedPastDecoderLimitIsRejected(t *testing.T) {
	wrapper, err := proto.Marshal(validAccountBlockProto())
	if err != nil {
		t.Fatal(err)
	}
	data := nestDescendant(wrapper, wrapper, protowire.DefaultRecursionLimit+1)

	_, err = DeserializeAccountBlock(data)
	if err == nil {
		t.Fatal("expected the decoder to reject input nested past its recursion limit, got nil")
	}
	if !strings.Contains(err.Error(), "recursion depth") {
		t.Fatalf("expected a decoder recursion-depth error, got: %v", err)
	}
	if strings.Contains(err.Error(), "invalid descendant block") {
		t.Fatalf("DeProtoAccountBlock ran on over-limit input: %v", err)
	}
}

// TestDeserializeAccountBlock_NestedBelowDecoderLimitDecodes is the control
// for the fixture and the limit: the same wrapping, well below the limit,
// decodes through DeserializeAccountBlock to a descendant chain of exactly
// that depth ending in the leaf.
func TestDeserializeAccountBlock_NestedBelowDecoderLimitDecodes(t *testing.T) {
	wrapper, err := proto.Marshal(validAccountBlockProto())
	if err != nil {
		t.Fatal(err)
	}
	leafProto := validAccountBlockProto()
	leafProto.Height = 99
	leaf, err := proto.Marshal(leafProto)
	if err != nil {
		t.Fatal(err)
	}
	const depth = 50
	data := nestDescendant(wrapper, leaf, depth)

	ab, err := DeserializeAccountBlock(data)
	if err != nil {
		t.Fatalf("expected input well below the decoder limit to decode, got: %v", err)
	}
	cur := ab
	for i := 0; i < depth; i++ {
		if len(cur.DescendantBlocks) != 1 {
			t.Fatalf("level %d: expected 1 descendant, got %d", i, len(cur.DescendantBlocks))
		}
		if cur.Height != 42 {
			t.Fatalf("level %d: wrapper fields lost, height %d", i, cur.Height)
		}
		cur = cur.DescendantBlocks[0]
	}
	if cur.Height != 99 || len(cur.DescendantBlocks) != 0 {
		t.Fatalf("leaf mismatch: height %d, %d descendants", cur.Height, len(cur.DescendantBlocks))
	}
}
