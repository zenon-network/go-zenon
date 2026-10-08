package protocol_test

import (
	"math/big"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// byteDifferentCopy returns a copy of block that keeps the same identifier and
// signature but serializes to different bytes. ChangesHash is stored on user
// blocks, excluded from the hash and never checked for them, so changing it is
// the cheapest way to build such a copy. In the field the differing field was
// the signature, which is also outside the hash; the code under test compares
// whole serialized blocks, so it does not matter which field differs.
func byteDifferentCopy(t *testing.T, block *nom.AccountBlock) *nom.AccountBlock {
	t.Helper()
	copied := block.Copy()
	copied.ChangesHash = types.HexToHashPanic("1111111111111111111111111111111111111111111111111111111111111111")
	if copied.ChangesHash == block.ChangesHash {
		t.Fatal("changes-hash already had the substitute value")
	}
	if copied.Identifier() != block.Identifier() || copied.EqualBytes(block) {
		t.Fatal("copy must keep the identifier and differ in bytes")
	}
	return copied
}

func TestInsertChain_MomentumBytesReplacePoolCopyOfSameBlock(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	bridge := protocol.NewChainBridge(z.Chain(), z.Consensus(), z.Verifier(), supervisor)

	// User1 sends one raw ZNN unit to User2 and the send gets committed.
	send := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(1),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	// User2 receives it. This copy, with the deterministic signature, is the one
	// the momentum is built from.
	momentumCopy, err := supervisor.GenerateFromTemplate(&nom.AccountBlock{
		BlockType:     nom.BlockTypeUserReceive,
		Address:       g.User2.Address,
		FromBlockHash: send.Hash,
	}, g.User2.Signer)
	common.FailIfErr(t, err)
	z.Broadcaster().CreateAccountBlock(momentumCopy)
	z.InsertNewMomentum()

	store := z.Chain().GetFrontierMomentumStore()
	momentum, err := store.GetFrontierMomentum()
	common.FailIfErr(t, err)
	detailed, err := store.PrefetchMomentum(momentum)
	common.FailIfErr(t, err)
	common.Expect(t, len(detailed.AccountBlocks), 1)
	common.Expect(t, detailed.AccountBlocks[0].Identifier(), momentumCopy.Block.Identifier())

	// Roll back so the momentum can be replayed against a pool that holds a
	// byte-different copy of the same block.
	insert := z.Chain().AcquireInsert("test rollback")
	common.FailIfErr(t, z.Chain().RollbackTo(insert, momentum.Previous()))
	insert.Unlock()

	poolCopy := byteDifferentCopy(t, momentumCopy.Block)
	common.FailIfErr(t, bridge.AddAccountBlocks([]*nom.AccountBlock{poolCopy}))

	_, err = bridge.InsertChain([]*nom.DetailedMomentum{detailed})
	common.FailIfErr(t, err)

	frontier := z.Chain().GetFrontierMomentumStore()
	common.Expect(t, frontier.Identifier(), momentum.Identifier())
	committed, err := frontier.GetAccountBlock(momentumCopy.Block.Header())
	common.FailIfErr(t, err)
	if !committed.EqualBytes(momentumCopy.Block) {
		t.Fatal("committed block does not match the momentum's copy")
	}
}

// TestInsertChain_RejectsEmptyAndNilInput pins that malformed input is
// answered with an error before anything is indexed or dereferenced. The
// wire decoder never produces these shapes, so this is API hardening: a
// caller mistake must not take the node down from the import goroutine.
func TestInsertChain_RejectsEmptyAndNilInput(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	bridge := protocol.NewChainBridge(z.Chain(), z.Consensus(), z.Verifier(), vm.NewSupervisor(z.Chain(), z.Consensus()))

	frontier := z.Chain().GetFrontierMomentumStore().Identifier()
	valid := func() *nom.DetailedMomentum {
		momentum, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
		common.FailIfErr(t, err)
		detailed, err := z.Chain().GetFrontierMomentumStore().PrefetchMomentum(momentum)
		common.FailIfErr(t, err)
		return detailed
	}
	cases := []struct {
		name  string
		input []*nom.DetailedMomentum
	}{
		{"nil slice", nil},
		{"empty slice", []*nom.DetailedMomentum{}},
		{"nil first entry", []*nom.DetailedMomentum{nil}},
		{"nil last entry", []*nom.DetailedMomentum{valid(), nil}},
		{"nil momentum", []*nom.DetailedMomentum{{Momentum: nil}}},
		{"nil momentum after a valid one", []*nom.DetailedMomentum{valid(), {Momentum: nil}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := bridge.InsertChain(tc.input); err == nil {
				t.Fatalf("expected an error for %s", tc.name)
			}
			common.Expect(t, z.Chain().GetFrontierMomentumStore().Identifier(), frontier)
		})
	}
}
