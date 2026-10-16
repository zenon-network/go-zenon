package subscribe

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

// TestAccountBlockEvents_Order pins the exact event sequence for the content
// layouts a momentum can carry: descendant listed before or after its parent,
// several refunds, and a descendant the content does not list at all. Listed
// blocks keep content order and appear once; an unlisted descendant is
// emitted right after its parent.
func TestAccountBlockEvents_Order(t *testing.T) {
	user := types.ParseAddressPanic("z1qqdt06lnwz57x38rwlyutcx5wgrtl0ynkfe3kv")
	hash := func(b byte) types.Hash {
		var h types.Hash
		for i := range h {
			h[i] = b
		}
		return h
	}
	send := func(id byte, height uint64) *nom.AccountBlock {
		return &nom.AccountBlock{BlockType: nom.BlockTypeContractSend, Hash: hash(id), Height: height, Address: types.PlasmaContract, ToAddress: user}
	}
	receive := func(id byte, height uint64, descendants ...*nom.AccountBlock) *nom.AccountBlock {
		return &nom.AccountBlock{BlockType: nom.BlockTypeContractReceive, Hash: hash(id), Height: height, Address: types.PlasmaContract, DescendantBlocks: descendants}
	}
	other := &nom.AccountBlock{BlockType: nom.BlockTypeUserSend, Hash: hash(0x99), Height: 1, Address: user, ToAddress: user}

	refund1, refund2 := send(0xa1, 4), send(0xa2, 5)
	parent := receive(0xb0, 6, refund1, refund2)
	unlisted := send(0xc1, 4)
	parentOfUnlisted := receive(0xc0, 5, unlisted)

	cases := []struct {
		name  string
		input []*nom.AccountBlock
		want  []types.Hash
	}{
		{"descendants before parent", []*nom.AccountBlock{refund1, refund2, parent}, []types.Hash{hash(0xa1), hash(0xa2), hash(0xb0)}},
		{"parent before descendants", []*nom.AccountBlock{parent, other, refund1, refund2}, []types.Hash{hash(0xb0), hash(0x99), hash(0xa1), hash(0xa2)}},
		{"descendant not listed", []*nom.AccountBlock{other, parentOfUnlisted}, []types.Hash{hash(0x99), hash(0xc0), hash(0xc1)}},
		{"one of two descendants listed", []*nom.AccountBlock{refund1, parent}, []types.Hash{hash(0xa1), hash(0xb0), hash(0xa2)}},
		{"no descendants", []*nom.AccountBlock{other}, []types.Hash{hash(0x99)}},
		{"empty", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events := accountBlockEvents(tc.input)
			got := make([]types.Hash, 0, len(events))
			for _, e := range events {
				got = append(got, e.Hash)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d events %v, want %d %v", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("event %d: got %v, want %v (full %v)", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}
