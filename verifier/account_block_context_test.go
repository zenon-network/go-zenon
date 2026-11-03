package verifier_test

import (
	"math/big"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// noChainBlocks returns, for an address that has no account chain at all,
// a send block above height 1 for each of a few heights, acknowledging the
// frontier momentum.
func noChainBlocks(t *testing.T, z mock.MockZenon) []*nom.AccountBlock {
	t.Helper()
	address := types.PubKeyToAddress([]byte("address-without-an-account-chain"))
	frontier, err := z.Chain().GetFrontierAccountStore(address).Frontier()
	common.FailIfErr(t, err)
	if frontier != nil {
		t.Fatalf("test address %v unexpectedly has an account chain", address)
	}
	momentum, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.FailIfErr(t, err)

	var blocks []*nom.AccountBlock
	for _, height := range []uint64{2, 3, 1000} {
		blocks = append(blocks, &nom.AccountBlock{
			Version:              1,
			BlockType:            nom.BlockTypeUserSend,
			Address:              address,
			ToAddress:            address,
			Height:               height,
			PreviousHash:         types.NewHash([]byte("previous")),
			MomentumAcknowledged: momentum.Identifier(),
			Amount:               common.Big0,
			TokenStandard:        types.ZnnTokenStandard,
		})
	}
	return blocks
}

// A block above height 1 for an address that has no account chain at all
// must be rejected the same way as a block whose previous block is missing.
// Before the guard in getContext the frontier lookup for such an address
// returned nil and the verifier dereferenced it.
func TestAccountBlockVerifier_NoAccountChain_ReturnsPreviousMissing(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(5)

	v := verifier.NewAccountBlockVerifier(z.Chain(), z.Consensus())
	for _, block := range noChainBlocks(t, z) {
		if err := v.AccountBlock(block); err != verifier.ErrABPreviousMissing {
			t.Errorf("height %d: got %v, want %v", block.Height, err, verifier.ErrABPreviousMissing)
		}
	}
}

// AccountBlockTransaction reaches the same getContext as AccountBlock, so a
// transaction whose block is for an address without an account chain is
// rejected the same way.
func TestAccountBlockVerifier_NoAccountChain_TransactionReturnsPreviousMissing(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(5)

	v := verifier.NewAccountBlockVerifier(z.Chain(), z.Consensus())
	for _, block := range noChainBlocks(t, z) {
		if err := v.AccountBlockTransaction(&nom.AccountBlockTransaction{Block: block}); err != verifier.ErrABPreviousMissing {
			t.Errorf("height %d: got %v, want %v", block.Height, err, verifier.ErrABPreviousMissing)
		}
	}
}

// The guard only applies above height 1: the first block of a new account
// chain, at height 1 with a zero previous hash, is still accepted. User10
// has no account chain in the mock genesis; it gets plasma from a fusion
// and then receives a send as its first block. The mock generates the
// block through the supervisor, which runs both AccountBlock and
// AccountBlockTransaction on it before it is inserted.
func TestAccountBlockVerifier_FirstBlockOfANewAccountChainIsAccepted(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(5)

	frontier, err := z.Chain().GetFrontierAccountStore(g.User10.Address).Frontier()
	common.FailIfErr(t, err)
	if frontier != nil {
		t.Fatalf("User10 unexpectedly has an account chain at height %d", frontier.Height)
	}

	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User10.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	send := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User10.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(100),
	}, nil, mock.SkipVmChanges)
	z.InsertMomentumsTo(8) // the send, the fusion and its contract receive

	receive := z.InsertReceiveBlock(send.Header(), nil, nil, mock.SkipVmChanges)
	if receive.Height != 1 || !receive.PreviousHash.IsZero() {
		t.Fatalf("first block of User10 at height %d with previous %v, want height 1 and a zero previous hash", receive.Height, receive.PreviousHash)
	}
	frontier, err = z.Chain().GetFrontierAccountStore(g.User10.Address).Frontier()
	common.FailIfErr(t, err)
	if frontier == nil || frontier.Hash != receive.Hash {
		t.Fatalf("User10's account chain does not start with the inserted block: frontier %v", frontier)
	}
}
