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

// newTestVerifier returns an AccountBlockVerifier backed by the mock chain.
func newTestVerifier(z mock.MockZenon) verifier.AccountBlockVerifier {
	return verifier.NewAccountBlockVerifier(z.Chain(), z.Consensus())
}

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

	// Send from User1 to User3 BEFORE activating the spork.
	sendBlock := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User3.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	// Capture the pre-spork momentum identifier for later use as MA.
	frontierStore := z.Chain().GetFrontierMomentumStore()
	preSporkMomentum, err := frontierStore.GetFrontierMomentum()
	common.FailIfErr(t, err)

	// Activate the spork
	activateDynamicPlasma(t, z)

	// Construct a receive block for User3 with the PRE-SPORK MA and non-empty
	// Data. The verifier must accept it because receiveData() looks up the
	// spork state at the block's MA, which predates enforcement.
	user3Store := z.Chain().GetFrontierAccountStore(g.User3.Address)
	user3Frontier, err := user3Store.Frontier()
	common.FailIfErr(t, err)

	receiveBlock := &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      z.Chain().ChainIdentifier(),
		BlockType:            nom.BlockTypeUserReceive,
		Address:              g.User3.Address,
		ToAddress:            types.ZeroAddress,
		Amount:               big.NewInt(0),
		TokenStandard:        types.ZeroTokenStandard,
		Height:               user3Frontier.Height + 1,
		PreviousHash:         user3Frontier.Hash,
		MomentumAcknowledged: preSporkMomentum.Identifier(),
		FromBlockHash:        sendBlock.Hash,
		Data:                 []byte("pre-spork data"),
	}
	receiveBlock.Hash = receiveBlock.ComputeHash()
	signature, _, publicKey, err := g.User3.Signer(receiveBlock.Hash.Bytes())
	common.FailIfErr(t, err)
	receiveBlock.Signature = signature
	receiveBlock.PublicKey = publicKey

	v := newTestVerifier(z)
	if err := v.AccountBlock(receiveBlock); err != nil {
		t.Fatalf("pre-spork MA receive with Data rejected after spork: %v", err)
	}
}

// Test 6: A pre-spork embedded send (created before the spork) is
// auto-received by the contract during momentum production that happens after
// enforcement begins. The contract auto-receive must succeed because
// receiveData() skips embedded addresses.
func TestSporkGated_PreSporkEmbeddedSendReceivedAfterSpork(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	saveSporkState(t)

	// Snapshot the Plasma contract frontier before the send so we can prove
	// the auto-receive advanced it (Height >= 1 holds from genesis, so a
	// bare >= 1 assertion would pass even if the fuse was never processed).
	plasmaStoreBefore := z.Chain().GetFrontierAccountStore(types.PlasmaContract)
	plasmaFrontierBefore, err := plasmaStoreBefore.Frontier()
	common.FailIfErr(t, err)

	// Create a Plasma.Fuse embedded send BEFORE the spork.
	fuseData := definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User1.Address)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          fuseData,
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)

	// Activate the spork — the momentums created during activation pick up the
	// pending fuse send and the Plasma contract auto-receives it. The
	// auto-receive is a ContractReceive (embedded address), so receiveData()
	// skips it and the fusion is processed normally.
	activateDynamicPlasma(t, z)

	// Verify the Plasma contract processed the fuse: its account chain grew
	// beyond the pre-send snapshot.
	plasmaStoreAfter := z.Chain().GetFrontierAccountStore(types.PlasmaContract)
	plasmaFrontierAfter, err := plasmaStoreAfter.Frontier()
	common.FailIfErr(t, err)
	common.Expect(t, true, plasmaFrontierAfter.Height > plasmaFrontierBefore.Height)
}

// Test 7: A pre-signed external block with non-empty Data on a receive is
// rejected by the verifier when the spork is active. The block is constructed
// and signed independently (not via GenerateFromTemplate), then passed through
// verifier.AccountBlock() — the admission path for external blocks.
func TestSporkGated_ExternalSignedReceiveWithDataRejectedAfterSpork(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	saveSporkState(t)

	activateDynamicPlasma(t, z)

	// Insert a send so there is something to receive
	sendBlock := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	// Construct the receive block manually (simulating an external sender),
	// set all required fields, sign it, then submit through the verifier.
	frontierMomentum := z.Chain().GetFrontierMomentumStore()
	fm, err := frontierMomentum.GetFrontierMomentum()
	common.FailIfErr(t, err)

	user2Store := z.Chain().GetFrontierAccountStore(g.User2.Address)
	user2Frontier, err := user2Store.Frontier()
	common.FailIfErr(t, err)

	receiveBlock := &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      z.Chain().ChainIdentifier(),
		BlockType:            nom.BlockTypeUserReceive,
		Address:              g.User2.Address,
		ToAddress:            types.ZeroAddress,
		Amount:               big.NewInt(0),
		TokenStandard:        types.ZeroTokenStandard,
		Height:               user2Frontier.Height + 1,
		PreviousHash:         user2Frontier.Hash,
		MomentumAcknowledged: fm.Identifier(),
		FromBlockHash:        sendBlock.Hash,
		Data:                 []byte("external data"),
	}
	receiveBlock.Hash = receiveBlock.ComputeHash()
	signature, _, publicKey, err := g.User2.Signer(receiveBlock.Hash.Bytes())
	common.FailIfErr(t, err)
	receiveBlock.Signature = signature
	receiveBlock.PublicKey = publicKey

	// The verifier must reject this block
	v := newTestVerifier(z)
	err = v.AccountBlock(receiveBlock)
	common.ExpectError(t, err, verifier.ErrABDataMustBeEmpty)

	// Pool/chain state must be unchanged: User2's frontier is still the same
	unchangedFrontier, err := z.Chain().GetFrontierAccountStore(g.User2.Address).Frontier()
	common.FailIfErr(t, err)
	common.Expect(t, user2Frontier.Identifier(), unchangedFrontier.Identifier())
}

// Test 8: A pre-signed external block with empty Data on a receive is
// accepted by the verifier when the spork is active. The block is constructed
// and signed independently, then passed through verifier.AccountBlock().
func TestSporkGated_ExternalSignedReceiveEmptyDataAcceptedAfterSpork(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	saveSporkState(t)

	activateDynamicPlasma(t, z)

	// Insert a send so there is something to receive
	sendBlock := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	// Construct the receive block manually
	frontierMomentum := z.Chain().GetFrontierMomentumStore()
	fm, err := frontierMomentum.GetFrontierMomentum()
	common.FailIfErr(t, err)

	user2Store := z.Chain().GetFrontierAccountStore(g.User2.Address)
	user2Frontier, err := user2Store.Frontier()
	common.FailIfErr(t, err)

	receiveBlock := &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      z.Chain().ChainIdentifier(),
		BlockType:            nom.BlockTypeUserReceive,
		Address:              g.User2.Address,
		ToAddress:            types.ZeroAddress,
		Amount:               big.NewInt(0),
		TokenStandard:        types.ZeroTokenStandard,
		Height:               user2Frontier.Height + 1,
		PreviousHash:         user2Frontier.Hash,
		MomentumAcknowledged: fm.Identifier(),
		FromBlockHash:        sendBlock.Hash,
		Data:                 nil,
	}
	receiveBlock.Hash = receiveBlock.ComputeHash()
	signature, _, publicKey, err := g.User2.Signer(receiveBlock.Hash.Bytes())
	common.FailIfErr(t, err)
	receiveBlock.Signature = signature
	receiveBlock.PublicKey = publicKey

	// The verifier must accept this block
	v := newTestVerifier(z)
	if err := v.AccountBlock(receiveBlock); err != nil {
		t.Fatalf("external signed receive with empty Data rejected: %v", err)
	}
}

// TestSporkGated_NominateGuardiansSendRejectedAfterSpork verifies that an
// embedded send to NominateGuardians (Bridge and Liquidity) with duplicate
// or oversized guardian sets is rejected once DynamicPlasmaSpork is active.
func TestSporkGated_NominateGuardiansSendRejectedAfterSpork(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	saveSporkState(t)

	activateBridge(t, z)
	activateDynamicPlasmaPastHeight(t, z)

	guardiansDup := []types.Address{
		g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User1.Address,
	}

	// Bridge duplicate send rejected post-spork
	sendDupBridge := &nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABIBridge.PackMethodPanic(definition.NominateGuardiansMethodName, guardiansDup),
	}
	z.InsertSendBlock(sendDupBridge, constants.ErrInvalidGuardians, mock.SkipVmChanges)

	// Liquidity duplicate send rejected post-spork
	sendDupLiq := &nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABILiquidity.PackMethodPanic(definition.NominateGuardiansMethodName, guardiansDup),
	}
	z.InsertSendBlock(sendDupLiq, constants.ErrInvalidGuardians, mock.SkipVmChanges)
}

// TestSporkGated_NominateGuardiansSendAcceptedBeforeSpork verifies that an
// embedded send to NominateGuardians with duplicate guardians is admitted
// before the Dynamic Plasma spork is active.
func TestSporkGated_NominateGuardiansSendAcceptedBeforeSpork(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	saveSporkState(t)

	activateBridge(t, z)
	z.InsertMomentumsTo(10)

	guardiansDup := []types.Address{
		g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User1.Address,
	}

	sendDupBridge := &nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABIBridge.PackMethodPanic(definition.NominateGuardiansMethodName, guardiansDup),
	}
	z.InsertSendBlock(sendDupBridge, nil, mock.SkipVmChanges)
}
