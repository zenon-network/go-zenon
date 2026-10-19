package tests

import (
	"math"
	"math/big"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

func znnBalanceOf(t *testing.T, z mock.MockZenon, addr types.Address) *big.Int {
	b, err := z.Chain().GetFrontierAccountStore(addr).GetBalance(types.ZnnTokenStandard)
	common.FailIfErr(t, err)
	return b
}

// On a chain that is not mainnet (the mock genesis uses chain identifier
// 100) a send is consumable only by the account it was addressed to, from
// the first momentum: a receive by any other account is rejected and credits
// nothing, and the rightful recipient's receive is accepted.
func TestReceive_OnlyTheRecipientCanReceive_FromGenesis(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	amount := big.NewInt(100 * g.Zexp)
	send := z.InsertSendBlock(&nom.AccountBlock{
		Address: g.User1.Address, ToAddress: g.User2.Address,
		TokenStandard: types.ZnnTokenStandard, Amount: amount,
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	common.Expect(t, true, z.Chain().GetFrontierMomentumStore().Identifier().Height < verifier.ReceiverMismatchEnforcementHeight)

	user2Before := znnBalanceOf(t, z, g.User2.Address)
	user3Before := znnBalanceOf(t, z, g.User3.Address)

	z.InsertReceiveBlock(send.Header(), &nom.AccountBlock{Address: g.User3.Address}, verifier.ErrABFromBlockReceiverMismatch, mock.SkipVmChanges)
	z.InsertNewMomentum()
	common.Expect(t, 0, new(big.Int).Sub(znnBalanceOf(t, z, g.User3.Address), user3Before).Sign())

	z.InsertReceiveBlock(send.Header(), nil, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	common.Expect(t, 0, new(big.Int).Sub(znnBalanceOf(t, z, g.User2.Address), user2Before).Cmp(amount))

	// The recipient cannot receive the same send twice either.
	z.InsertReceiveBlock(send.Header(), nil, verifier.ErrABFromBlockAlreadyReceived, mock.SkipVmChanges)
}

// The rejection on a non-mainnet chain comes from the chain identity, not
// from the frontier having reached the enforcement height: with the height
// pushed out of reach the wrong receiver is still rejected.
func TestReceive_OtherChainRejectsWrongRecipient_RegardlessOfHeight(t *testing.T) {
	saved := verifier.ReceiverMismatchEnforcementHeight
	verifier.ReceiverMismatchEnforcementHeight = math.MaxUint64
	defer func() { verifier.ReceiverMismatchEnforcementHeight = saved }()

	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	send := z.InsertSendBlock(&nom.AccountBlock{
		Address: g.User1.Address, ToAddress: g.User2.Address,
		TokenStandard: types.ZnnTokenStandard, Amount: big.NewInt(100 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	user3Before := znnBalanceOf(t, z, g.User3.Address)
	z.InsertReceiveBlock(send.Header(), &nom.AccountBlock{Address: g.User3.Address}, verifier.ErrABFromBlockReceiverMismatch, mock.SkipVmChanges)
	z.InsertNewMomentum()
	common.Expect(t, 0, new(big.Int).Sub(znnBalanceOf(t, z, g.User3.Address), user3Before).Sign())
}

// On mainnet's chain identifier the historical relaxation still applies: a
// receive by an account other than the recipient is accepted while the
// frontier is below the enforcement height and rejected from that height
// on, so a sync of mainnet's history keeps replaying the earlier blocks.
func TestReceive_MainnetIdentifier_RelaxedOnlyBelowTheHeight(t *testing.T) {
	savedHeight := verifier.ReceiverMismatchEnforcementHeight
	savedChain := g.EmbeddedGenesis.ChainIdentifier
	verifier.ReceiverMismatchEnforcementHeight = 10
	g.EmbeddedGenesis.ChainIdentifier = 1
	defer func() {
		verifier.ReceiverMismatchEnforcementHeight = savedHeight
		g.EmbeddedGenesis.ChainIdentifier = savedChain
	}()

	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	common.Expect(t, uint64(1), z.Chain().ChainIdentifier())

	amount := big.NewInt(100 * g.Zexp)
	send := z.InsertSendBlock(&nom.AccountBlock{
		Address: g.User1.Address, ToAddress: g.User2.Address,
		TokenStandard: types.ZnnTokenStandard, Amount: amount,
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	acknowledged := z.Chain().GetFrontierMomentumStore().Identifier()
	common.Expect(t, true, acknowledged.Height < verifier.ReceiverMismatchEnforcementHeight)

	// Below the height: the historical exception admits the wrong receiver.
	user3Before := znnBalanceOf(t, z, g.User3.Address)
	z.InsertReceiveBlock(send.Header(), &nom.AccountBlock{Address: g.User3.Address, MomentumAcknowledged: acknowledged}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	common.Expect(t, 0, new(big.Int).Sub(znnBalanceOf(t, z, g.User3.Address), user3Before).Cmp(amount))

	// From the height on: rejected, even with an acknowledgement from
	// before the height.
	z.InsertMomentumsTo(verifier.ReceiverMismatchEnforcementHeight)
	common.Expect(t, true, z.Chain().GetFrontierMomentumStore().Identifier().Height >= verifier.ReceiverMismatchEnforcementHeight)
	z.InsertReceiveBlock(send.Header(), &nom.AccountBlock{Address: g.User4.Address, MomentumAcknowledged: acknowledged}, verifier.ErrABFromBlockReceiverMismatch, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertReceiveBlock(send.Header(), &nom.AccountBlock{Address: g.User4.Address}, verifier.ErrABFromBlockReceiverMismatch, mock.SkipVmChanges)

	// The rightful recipient is unaffected either way.
	z.InsertReceiveBlock(send.Header(), nil, nil, mock.SkipVmChanges)
}
