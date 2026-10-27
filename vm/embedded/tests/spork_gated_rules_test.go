package tests

import (
	"math/big"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
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

// makeTestGuardianSet returns n distinct valid user addresses for guardian tests.
func makeTestGuardianSet(n int) []types.Address {
	out := make([]types.Address, n)
	for i := 0; i < n; i++ {
		var b [types.AddressSize]byte
		b[0] = types.UserAddrByte
		b[types.AddressSize-2] = byte(((i + 1) >> 8) & 0xff)
		b[types.AddressSize-1] = byte((i + 1) & 0xff)
		out[i] = b
	}
	return out
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
	guardiansOversized := makeTestGuardianSet(constants.MaxGuardians + 1)

	// Bridge duplicate send rejected post-spork
	sendDupBridge := &nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABIBridge.PackMethodPanic(definition.NominateGuardiansMethodName, guardiansDup),
	}
	z.InsertSendBlock(sendDupBridge, constants.ErrInvalidGuardians, mock.SkipVmChanges)

	// Bridge oversized send rejected post-spork
	sendOverBridge := &nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABIBridge.PackMethodPanic(definition.NominateGuardiansMethodName, guardiansOversized),
	}
	z.InsertSendBlock(sendOverBridge, constants.ErrInvalidGuardians, mock.SkipVmChanges)

	// Liquidity duplicate send rejected post-spork
	sendDupLiq := &nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABILiquidity.PackMethodPanic(definition.NominateGuardiansMethodName, guardiansDup),
	}
	z.InsertSendBlock(sendDupLiq, constants.ErrInvalidGuardians, mock.SkipVmChanges)

	// Liquidity oversized send rejected post-spork
	sendOverLiq := &nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABILiquidity.PackMethodPanic(definition.NominateGuardiansMethodName, guardiansOversized),
	}
	z.InsertSendBlock(sendOverLiq, constants.ErrInvalidGuardians, mock.SkipVmChanges)
}

// TestSporkGated_NominateGuardiansSendAcceptedBeforeSpork verifies that an
// embedded send to NominateGuardians (Bridge and Liquidity) with duplicate or
// oversized guardians is admitted before the Dynamic Plasma spork is active.
func TestSporkGated_NominateGuardiansSendAcceptedBeforeSpork(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	saveSporkState(t)

	activateBridge(t, z)
	z.InsertMomentumsTo(10)

	guardiansDup := []types.Address{
		g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User1.Address,
	}
	guardiansOversized := makeTestGuardianSet(constants.MaxGuardians + 1)

	sendDupBridge := &nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABIBridge.PackMethodPanic(definition.NominateGuardiansMethodName, guardiansDup),
	}
	z.InsertSendBlock(sendDupBridge, nil, mock.SkipVmChanges)

	sendOverBridge := &nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABIBridge.PackMethodPanic(definition.NominateGuardiansMethodName, guardiansOversized),
	}
	z.InsertSendBlock(sendOverBridge, nil, mock.SkipVmChanges)

	sendDupLiq := &nom.AccountBlock{
		Address:       g.User3.Address,
		ToAddress:     types.LiquidityContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABILiquidity.PackMethodPanic(definition.NominateGuardiansMethodName, guardiansDup),
	}
	z.InsertSendBlock(sendDupLiq, nil, mock.SkipVmChanges)

	sendOverLiq := &nom.AccountBlock{
		Address:       g.User4.Address,
		ToAddress:     types.LiquidityContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data:          definition.ABILiquidity.PackMethodPanic(definition.NominateGuardiansMethodName, guardiansOversized),
	}
	z.InsertSendBlock(sendOverLiq, nil, mock.SkipVmChanges)
}

// TestSporkGated_NominateGuardiansValidSetAcceptedAfterSpork verifies that a
// valid guardian set (MinGuardians <= len <= MaxGuardians, no duplicates) can
// be nominated and installed on both Bridge and Liquidity after
// DynamicPlasmaSpork activation.
func TestSporkGated_NominateGuardiansValidSetAcceptedAfterSpork(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	saveSporkState(t)
	saveBridgeConstants(t)

	constants.InitialBridgeAdministrator = g.User5.Address
	constants.MinGuardians = 5
	constants.MinAdministratorDelay = 5
	constants.MinSoftDelay = 5
	constants.MinUnhaltDurationInMomentums = 5

	activateBridge(t, z)
	z.InsertMomentumsTo(10)
	activateDynamicPlasmaPastHeight(t, z)

	validGuardians := []types.Address{
		g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User5.Address,
	}

	bridgeAPI := embedded.NewBridgeApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)

	bridgeSec, err := bridgeAPI.GetSecurityInfo()
	common.FailIfErr(t, err)
	nominateGuardians(t, z, g.User5.Address, validGuardians, bridgeSec.AdministratorDelay)

	liqSec, err := liquidityAPI.GetSecurityInfo()
	common.FailIfErr(t, err)
	nominateGuardiansLiq(t, z, g.User5.Address, validGuardians, liqSec.AdministratorDelay)

	bridgeSecAfter, err := bridgeAPI.GetSecurityInfo()
	common.FailIfErr(t, err)
	common.Expect(t, len(validGuardians), len(bridgeSecAfter.Guardians))

	liqSecAfter, err := liquidityAPI.GetSecurityInfo()
	common.FailIfErr(t, err)
	common.Expect(t, len(validGuardians), len(liqSecAfter.Guardians))
}

// TestSporkGated_PreSporkNominateGuardiansReceivedAfterSpork verifies that a
// NominateGuardians send with a duplicate or oversized guardian set admitted
// before DynamicPlasmaSpork enforcement is auto-received after activation and
// fails at the receive, leaving the installed guardian set on both Bridge and
// Liquidity untouched.
func TestSporkGated_PreSporkNominateGuardiansReceivedAfterSpork(t *testing.T) {
	cases := []struct {
		name      string
		guardians []types.Address
	}{
		{
			name: "Duplicate",
			guardians: []types.Address{
				g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User1.Address,
			},
		},
		{
			name:      "Oversized",
			guardians: makeTestGuardianSet(constants.MaxGuardians + 1),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			z := mock.NewMockZenon(t)
			defer z.StopPanic()
			saveSporkState(t)
			saveBridgeConstants(t)

			constants.InitialBridgeAdministrator = g.User5.Address
			constants.MinGuardians = 5
			constants.MinAdministratorDelay = 5
			constants.MinSoftDelay = 5
			constants.MinUnhaltDurationInMomentums = 5

			activateBridge(t, z)
			z.InsertMomentumsTo(10)

			initialGuardians := []types.Address{
				g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User5.Address,
			}
			bridgeAPI := embedded.NewBridgeApi(z)
			liquidityAPI := embedded.NewLiquidityApi(z)

			nominateGuardians(t, z, g.User5.Address, initialGuardians, 5)
			nominateGuardiansLiq(t, z, g.User5.Address, initialGuardians, 5)

			// Prime the TimeChallenge for tc.guardians pre-spork and advance past
			// AdministratorDelay so that a second receive would immediately install
			// tc.guardians if not rejected on the receive path.
			defer z.CallContract(nominateGuardiansStep(g.User5.Address, tc.guardians)).Error(t, nil)
			defer z.CallContract(nominateGuardiansLiqStep(g.User5.Address, tc.guardians)).Error(t, nil)
			insertMomentums(z, 2)
			frMom, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
			common.FailIfErr(t, err)
			z.InsertMomentumsTo(frMom.Height + 5 + 2)

			bridgeSecBefore, err := bridgeAPI.GetSecurityInfo()
			common.FailIfErr(t, err)
			liqSecBefore, err := liquidityAPI.GetSecurityInfo()
			common.FailIfErr(t, err)
			common.Expect(t, len(initialGuardians), len(bridgeSecBefore.Guardians))
			common.Expect(t, len(initialGuardians), len(liqSecBefore.Guardians))

			// Schedule DynamicPlasmaSpork and advance to enforcementHeight - 1
			// (the last pre-spork momentum height).
			createSend := z.InsertSendBlock(&nom.AccountBlock{
				Address:   g.Spork.Address,
				ToAddress: types.SporkContract,
				Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
					"dynamic-plasma",
					"Activates Dynamic Plasma",
				),
			}, nil, mock.SkipVmChanges)
			z.InsertNewMomentum()
			id := createSend.Hash

			z.InsertSendBlock(&nom.AccountBlock{
				Address:   g.Spork.Address,
				ToAddress: types.SporkContract,
				Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
					id,
				),
			}, nil, mock.SkipVmChanges)
			z.InsertNewMomentum()
			types.DynamicPlasmaSpork.SporkId = id
			types.ImplementedSporksMap[id] = true

			sporkAPI := embedded.NewSporkApi(z)
			sporkList, err := sporkAPI.GetAll(0, 10)
			common.FailIfErr(t, err)
			var enforcementHeight uint64
			for _, s := range sporkList.List {
				if s.Id == id {
					enforcementHeight = s.EnforcementHeight
					break
				}
			}
			if enforcementHeight == 0 {
				t.Fatal("dynamic plasma spork not found in spork list")
			}

			z.InsertMomentumsTo(enforcementHeight - 1)
			activeBefore, err := z.Chain().GetFrontierMomentumStore().IsSporkActive(types.DynamicPlasmaSpork)
			common.FailIfErr(t, err)
			common.Expect(t, false, activeBefore)

			// Admit the confirming NominateGuardians sends pre-spork (at enforcementHeight - 1);
			// they will be cemented at enforcementHeight and auto-received post-spork.
			defer z.CallContract(nominateGuardiansStep(g.User5.Address, tc.guardians)).Error(t, constants.ErrInvalidGuardians)
			defer z.CallContract(nominateGuardiansLiqStep(g.User5.Address, tc.guardians)).Error(t, constants.ErrInvalidGuardians)

			z.InsertMomentumsTo(enforcementHeight + 1)

			activeAfter, err := z.Chain().GetFrontierMomentumStore().IsSporkActive(types.DynamicPlasmaSpork)
			common.FailIfErr(t, err)
			common.Expect(t, true, activeAfter)

			// Verify the installed guardian sets on both contracts remain untouched.
			bridgeSecAfter, err := bridgeAPI.GetSecurityInfo()
			common.FailIfErr(t, err)
			liqSecAfter, err := liquidityAPI.GetSecurityInfo()
			common.FailIfErr(t, err)
			common.Expect(t, len(initialGuardians), len(bridgeSecAfter.Guardians))
			common.Expect(t, len(initialGuardians), len(liqSecAfter.Guardians))
			for i := range initialGuardians {
				common.Expect(t, bridgeSecBefore.Guardians[i], bridgeSecAfter.Guardians[i])
				common.Expect(t, liqSecBefore.Guardians[i], liqSecAfter.Guardians[i])
			}
		})
	}
}
