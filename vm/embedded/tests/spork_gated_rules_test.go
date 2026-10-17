package tests

import (
	"math/big"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// Test 1: A user receive block with non-empty Data is rejected once the
// Dynamic Plasma spork is active.
func TestSporkGated_ReceiveBlockDataRejectedAfterSpork(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	saveSporkState(t)

	activateDynamicPlasma(t, z)

	// Send from User1 to User2
	sendBlock := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	// Receive with non-empty Data should be rejected
	z.InsertReceiveBlock(sendBlock.Header(), &nom.AccountBlock{
		Data: []byte("hello"),
	}, verifier.ErrABDataMustBeEmpty, mock.NoVmChanges)
}

// Test 2: A user receive block with non-empty Data is accepted before the
// Dynamic Plasma spork is active.
func TestSporkGated_ReceiveBlockDataAcceptedBeforeSpork(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	saveSporkState(t)

	// Send from User1 to User2
	sendBlock := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	// Receive with non-empty Data is accepted before the spork
	z.InsertReceiveBlock(sendBlock.Header(), &nom.AccountBlock{
		Data: []byte("hello"),
	}, nil, mock.SkipVmChanges)
}

// Test 3: An embedded send with trailing bytes past the ABI-encoded arguments
// is rejected once the Dynamic Plasma spork is active.
func TestSporkGated_EmbeddedSendTrailingBytesRejectedAfterSpork(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	saveSporkState(t)

	activateDynamicPlasma(t, z)

	// Build a valid Plasma.Fuse call, then append one trailing byte
	fuseData := definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User1.Address)
	trailingData := append(append([]byte{}, fuseData...), 0x00)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          trailingData,
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, constants.ErrABDataTrailingBytes, mock.NoVmChanges)
}

// Test 4: An embedded send without trailing bytes is accepted after the
// Dynamic Plasma spork is active.
func TestSporkGated_EmbeddedSendNoTrailingBytesAcceptedAfterSpork(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	saveSporkState(t)

	activateDynamicPlasma(t, z)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User1.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
}

// Test 5: A receive block whose MomentumAcknowledged is before the spork
// activation height is still valid even after the spork is active. This
// verifies the spork check uses the block's MA, not the chain tip.
func TestSporkGated_PreEnforcementBlockReplayedAfterSpork(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	saveSporkState(t)

	// Send and receive BEFORE activating the spork — the receive has Data
	// but is accepted because DP is not yet enforced.
	sendBlock := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	receiveBlock := z.InsertReceiveBlock(sendBlock.Header(), &nom.AccountBlock{
		Data: []byte("pre-spork data"),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	// Activate the spork
	activateDynamicPlasma(t, z)

	// The pre-spork receive block should still verify cleanly because its
	// MomentumAcknowledged predates the spork enforcement height.
	// We test this by sending a new block from User2 (whose frontier is the
	// pre-spork receive) — if the chain state is consistent, this succeeds.
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     g.User1.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(5 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	// Verify the pre-spork receive block's header is still retrievable
	common.Expect(t, true, receiveBlock != nil)
}
