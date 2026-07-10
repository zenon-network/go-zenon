package tests

import (
	"math/big"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// A receive referencing a send confirmed after the receive's own
// acknowledged momentum must be rejected.
func TestAccountBlock_ReceiveSendConfirmedAfterAcknowledged(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	z.InsertMomentumsTo(10)

	send := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(1 * g.Zexp),
	}, nil, mock.SkipVmChanges)

	// the send is now confirmed at momentum height 11
	z.InsertNewMomentum()

	m10, err := z.Chain().GetFrontierMomentumStore().GetMomentumByHeight(10)
	if err != nil {
		t.Fatal(err)
	}

	z.InsertReceiveBlock(send.Header(), &nom.AccountBlock{
		Address:              g.User2.Address,
		FromBlockHash:        send.Hash,
		MomentumAcknowledged: m10.Identifier(),
	}, verifier.ErrABFromBlockMissing, mock.SkipVmChanges)
}
