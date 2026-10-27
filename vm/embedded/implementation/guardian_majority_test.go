package implementation

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	accountstore "github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// makeGuardianAddress generates a distinct valid user address for testing.
func makeGuardianAddress(i int) types.Address {
	var b [types.AddressSize]byte
	b[0] = types.UserAddrByte
	b[types.AddressSize-2] = byte((i >> 8) & 0xff)
	b[types.AddressSize-1] = byte(i & 0xff)
	return b
}

type testContractContext struct {
	store.Account
	momentum        *nom.Momentum
	dpSporkEnforced bool
}

func newTestContractContext(contract types.Address, dpSpork bool) *testContractContext {
	t := time.Unix(1000000000, 0)
	return &testContractContext{
		Account: accountstore.NewAccountStore(contract, db.NewMemDB()),
		momentum: &nom.Momentum{
			ChainIdentifier: 100,
			Timestamp:       &t,
			Height:          10,
		},
		dpSporkEnforced: dpSpork,
	}
}

func (ctx *testContractContext) CacheStore() store.Cache       { return nil }
func (ctx *testContractContext) MomentumStore() store.Momentum { return nil }
func (ctx *testContractContext) GetFrontierMomentum() (*nom.Momentum, error) {
	return ctx.momentum, nil
}
func (ctx *testContractContext) GetGenesisMomentum() *nom.Momentum { return ctx.momentum }
func (ctx *testContractContext) Save()                             {}
func (ctx *testContractContext) Reset()                            {}
func (ctx *testContractContext) Done()                             {}
func (ctx *testContractContext) AddBalance(ts *types.ZenonTokenStandard, amount *big.Int) {
}
func (ctx *testContractContext) SubBalance(ts *types.ZenonTokenStandard, amount *big.Int) {
}
func (ctx *testContractContext) IsAcceleratorSporkEnforced() bool        { return true }
func (ctx *testContractContext) IsHtlcSporkEnforced() bool               { return true }
func (ctx *testContractContext) IsBridgeAndLiquiditySporkEnforced() bool { return true }
func (ctx *testContractContext) IsDynamicPlasmaSporkEnforced() bool      { return ctx.dpSporkEnforced }
func (ctx *testContractContext) IsPtlcSporkEnforced() bool               { return false }
func (ctx *testContractContext) GetPillarWeights() (map[string]*big.Int, error) {
	return nil, nil
}
func (ctx *testContractContext) EpochTicker() common.Ticker {
	return common.NewTicker(time.Unix(0, 0), time.Second)
}
func (ctx *testContractContext) EpochStats(epoch uint64) (*api.EpochStats, error) {
	return nil, nil
}
func (ctx *testContractContext) GetPillarDelegationsByEpoch(epoch uint64) (map[string]*types.PillarDelegationDetail, error) {
	return nil, nil
}

// TestGuardian_CheckNominatedGuardians tests boundaries and duplicate checks of
// CheckNominatedGuardians.
func TestGuardian_CheckNominatedGuardians(t *testing.T) {
	// < MinGuardians (4)
	guardians4 := make([]types.Address, 4)
	for i := 0; i < 4; i++ {
		guardians4[i] = makeGuardianAddress(i + 1)
	}
	if err := CheckNominatedGuardians(guardians4); err != constants.ErrInvalidGuardians {
		t.Fatalf("expected ErrInvalidGuardians for 4 guardians, got %v", err)
	}

	// MinGuardians (5)
	guardians5 := make([]types.Address, 5)
	for i := 0; i < 5; i++ {
		guardians5[i] = makeGuardianAddress(i + 1)
	}
	if err := CheckNominatedGuardians(guardians5); err != nil {
		t.Fatalf("expected nil for 5 guardians, got %v", err)
	}

	// MaxGuardians (30)
	guardiansMax := make([]types.Address, constants.MaxGuardians)
	for i := 0; i < constants.MaxGuardians; i++ {
		guardiansMax[i] = makeGuardianAddress(i + 1)
	}
	if err := CheckNominatedGuardians(guardiansMax); err != nil {
		t.Fatalf("expected nil for MaxGuardians, got %v", err)
	}

	// MaxGuardians + 1 (31)
	guardiansMaxPlus1 := make([]types.Address, constants.MaxGuardians+1)
	for i := 0; i < constants.MaxGuardians+1; i++ {
		guardiansMaxPlus1[i] = makeGuardianAddress(i + 1)
	}
	if err := CheckNominatedGuardians(guardiansMaxPlus1); err != constants.ErrInvalidGuardians {
		t.Fatalf("expected ErrInvalidGuardians for MaxGuardians+1, got %v", err)
	}

	// Duplicates within 5 guardians
	guardiansDup := make([]types.Address, 5)
	for i := 0; i < 4; i++ {
		guardiansDup[i] = makeGuardianAddress(i + 1)
	}
	guardiansDup[4] = makeGuardianAddress(1) // duplicate of first
	if err := CheckNominatedGuardians(guardiansDup); err != constants.ErrInvalidGuardians {
		t.Fatalf("expected ErrInvalidGuardians for duplicate guardians, got %v", err)
	}
}

// TestGuardian_NominateGuardians_ReceiveBlock_SporkGating verifies that
// NominateGuardians ReceiveBlock enforces CheckNominatedGuardians post-spork
// for both bridge and liquidity, while preserving pre-spork nomination behavior.
func TestGuardian_NominateGuardians_ReceiveBlock_SporkGating(t *testing.T) {
	initialGuardians := make([]types.Address, 5)
	for i := 0; i < 5; i++ {
		initialGuardians[i] = makeGuardianAddress(i + 10)
	}

	guardiansDup := make([]types.Address, 5)
	for i := 0; i < 4; i++ {
		guardiansDup[i] = makeGuardianAddress(i + 1)
	}
	guardiansDup[4] = makeGuardianAddress(1)

	guardiansOversized := make([]types.Address, constants.MaxGuardians+1)
	for i := 0; i < len(guardiansOversized); i++ {
		guardiansOversized[i] = makeGuardianAddress(i + 1)
	}

	admin := makeGuardianAddress(999)

	// --- Bridge Pre-Spork (Legacy completion of duplicate and oversized sets) ---
	ctxBridgePre := newTestContractContext(types.BridgeContract, false)
	setupActiveBridge(t, ctxBridgePre, admin, initialGuardians)
	methodBridge := &NominateGuardiansMethod{definition.NominateGuardiansMethodName}

	sendDupBridge := &nom.AccountBlock{
		Address:   admin,
		ToAddress: types.BridgeContract,
		Data:      definition.ABIBridge.PackMethodPanic(definition.NominateGuardiansMethodName, guardiansDup),
		Amount:    big.NewInt(0),
	}
	if _, err := methodBridge.ReceiveBlock(ctxBridgePre, sendDupBridge); err != nil {
		t.Fatalf("expected pre-spork duplicate bridge nomination to start time challenge, got %v", err)
	}
	ctxBridgePre.momentum.Height += 11
	if _, err := methodBridge.ReceiveBlock(ctxBridgePre, sendDupBridge); err != nil {
		t.Fatalf("expected pre-spork duplicate bridge nomination to complete after delay, got %v", err)
	}
	secBridgePre, err := definition.GetSecurityInfoVariable(ctxBridgePre.Storage())
	if err != nil {
		t.Fatalf("failed to load bridge security info: %v", err)
	}
	if len(secBridgePre.Guardians) != len(guardiansDup) {
		t.Fatalf("expected pre-spork bridge guardians len %d, got %d", len(guardiansDup), len(secBridgePre.Guardians))
	}

	sendOverBridge := &nom.AccountBlock{
		Address:   admin,
		ToAddress: types.BridgeContract,
		Data:      definition.ABIBridge.PackMethodPanic(definition.NominateGuardiansMethodName, guardiansOversized),
		Amount:    big.NewInt(0),
	}
	if _, err := methodBridge.ReceiveBlock(ctxBridgePre, sendOverBridge); err != nil {
		t.Fatalf("expected pre-spork oversized bridge nomination to start time challenge, got %v", err)
	}
	ctxBridgePre.momentum.Height += 11
	if _, err := methodBridge.ReceiveBlock(ctxBridgePre, sendOverBridge); err != nil {
		t.Fatalf("expected pre-spork oversized bridge nomination to complete after delay, got %v", err)
	}
	secBridgePre, err = definition.GetSecurityInfoVariable(ctxBridgePre.Storage())
	if err != nil {
		t.Fatalf("failed to load bridge security info: %v", err)
	}
	if len(secBridgePre.Guardians) != len(guardiansOversized) {
		t.Fatalf("expected pre-spork bridge guardians len %d, got %d", len(guardiansOversized), len(secBridgePre.Guardians))
	}

	// --- Bridge Post-Spork (Enforced rejection, preserving guardians and time challenge) ---
	ctxBridgePost := newTestContractContext(types.BridgeContract, true)
	setupActiveBridge(t, ctxBridgePost, admin, initialGuardians)

	if _, err := methodBridge.ReceiveBlock(ctxBridgePost, sendDupBridge); err != constants.ErrInvalidGuardians {
		t.Fatalf("expected ErrInvalidGuardians for duplicate bridge guardians post-spork, got %v", err)
	}
	secBridge, err := definition.GetSecurityInfoVariable(ctxBridgePost.Storage())
	if err != nil {
		t.Fatalf("failed to load bridge security info: %v", err)
	}
	if len(secBridge.Guardians) != len(initialGuardians) {
		t.Fatalf("expected bridge guardians to remain unchanged post-spork rejection, got %v", secBridge.Guardians)
	}
	tcBridge, err := definition.GetTimeChallengeInfoVariable(ctxBridgePost.Storage(), definition.NominateGuardiansMethodName)
	if err != nil {
		t.Fatalf("failed to load bridge time challenge info: %v", err)
	}
	if tcBridge != nil {
		t.Fatalf("expected bridge time challenge to remain nil after post-spork rejection, got %+v", tcBridge)
	}

	if _, err := methodBridge.ReceiveBlock(ctxBridgePost, sendOverBridge); err != constants.ErrInvalidGuardians {
		t.Fatalf("expected ErrInvalidGuardians for oversized bridge guardians post-spork, got %v", err)
	}
	secBridge2, err := definition.GetSecurityInfoVariable(ctxBridgePost.Storage())
	if err != nil {
		t.Fatalf("failed to load bridge security info: %v", err)
	}
	if len(secBridge2.Guardians) != len(initialGuardians) {
		t.Fatalf("expected bridge guardians to remain unchanged post-spork rejection, got %v", secBridge2.Guardians)
	}
	tcBridge2, err := definition.GetTimeChallengeInfoVariable(ctxBridgePost.Storage(), definition.NominateGuardiansMethodName)
	if err != nil {
		t.Fatalf("failed to load bridge time challenge info: %v", err)
	}
	if tcBridge2 != nil {
		t.Fatalf("expected bridge time challenge to remain nil after post-spork rejection, got %+v", tcBridge2)
	}

	// --- Liquidity Pre-Spork (Legacy completion of duplicate and oversized sets) ---
	ctxLiqPre := newTestContractContext(types.LiquidityContract, false)
	setupActiveLiquidity(t, ctxLiqPre, admin, initialGuardians)
	methodLiq := &NominateGuardiansLiquidity{definition.NominateGuardiansMethodName}

	sendDupLiq := &nom.AccountBlock{
		Address:   admin,
		ToAddress: types.LiquidityContract,
		Data:      definition.ABILiquidity.PackMethodPanic(definition.NominateGuardiansMethodName, guardiansDup),
		Amount:    big.NewInt(0),
	}
	if _, err := methodLiq.ReceiveBlock(ctxLiqPre, sendDupLiq); err != nil {
		t.Fatalf("expected pre-spork duplicate liquidity nomination to start time challenge, got %v", err)
	}
	ctxLiqPre.momentum.Height += 11
	if _, err := methodLiq.ReceiveBlock(ctxLiqPre, sendDupLiq); err != nil {
		t.Fatalf("expected pre-spork duplicate liquidity nomination to complete after delay, got %v", err)
	}
	secLiqPre, err := definition.GetSecurityInfoVariable(ctxLiqPre.Storage())
	if err != nil {
		t.Fatalf("failed to load liquidity security info: %v", err)
	}
	if len(secLiqPre.Guardians) != len(guardiansDup) {
		t.Fatalf("expected pre-spork liquidity guardians len %d, got %d", len(guardiansDup), len(secLiqPre.Guardians))
	}

	sendOverLiq := &nom.AccountBlock{
		Address:   admin,
		ToAddress: types.LiquidityContract,
		Data:      definition.ABILiquidity.PackMethodPanic(definition.NominateGuardiansMethodName, guardiansOversized),
		Amount:    big.NewInt(0),
	}
	if _, err := methodLiq.ReceiveBlock(ctxLiqPre, sendOverLiq); err != nil {
		t.Fatalf("expected pre-spork oversized liquidity nomination to start time challenge, got %v", err)
	}
	ctxLiqPre.momentum.Height += 11
	if _, err := methodLiq.ReceiveBlock(ctxLiqPre, sendOverLiq); err != nil {
		t.Fatalf("expected pre-spork oversized liquidity nomination to complete after delay, got %v", err)
	}
	secLiqPre, err = definition.GetSecurityInfoVariable(ctxLiqPre.Storage())
	if err != nil {
		t.Fatalf("failed to load liquidity security info: %v", err)
	}
	if len(secLiqPre.Guardians) != len(guardiansOversized) {
		t.Fatalf("expected pre-spork liquidity guardians len %d, got %d", len(guardiansOversized), len(secLiqPre.Guardians))
	}

	// --- Liquidity Post-Spork (Enforced rejection, preserving guardians and time challenge) ---
	ctxLiqPost := newTestContractContext(types.LiquidityContract, true)
	setupActiveLiquidity(t, ctxLiqPost, admin, initialGuardians)

	if _, err := methodLiq.ReceiveBlock(ctxLiqPost, sendDupLiq); err != constants.ErrInvalidGuardians {
		t.Fatalf("expected ErrInvalidGuardians for duplicate liquidity guardians post-spork, got %v", err)
	}
	secLiq, err := definition.GetSecurityInfoVariable(ctxLiqPost.Storage())
	if err != nil {
		t.Fatalf("failed to load liquidity security info: %v", err)
	}
	if len(secLiq.Guardians) != len(initialGuardians) {
		t.Fatalf("expected liquidity guardians to remain unchanged post-spork rejection, got %v", secLiq.Guardians)
	}
	tcLiq, err := definition.GetTimeChallengeInfoVariable(ctxLiqPost.Storage(), definition.NominateGuardiansMethodName)
	if err != nil {
		t.Fatalf("failed to load liquidity time challenge info: %v", err)
	}
	if tcLiq != nil {
		t.Fatalf("expected liquidity time challenge to remain nil after post-spork rejection, got %+v", tcLiq)
	}

	if _, err := methodLiq.ReceiveBlock(ctxLiqPost, sendOverLiq); err != constants.ErrInvalidGuardians {
		t.Fatalf("expected ErrInvalidGuardians for oversized liquidity guardians post-spork, got %v", err)
	}
	secLiq2, err := definition.GetSecurityInfoVariable(ctxLiqPost.Storage())
	if err != nil {
		t.Fatalf("failed to load liquidity security info: %v", err)
	}
	if len(secLiq2.Guardians) != len(initialGuardians) {
		t.Fatalf("expected liquidity guardians to remain unchanged post-spork rejection, got %v", secLiq2.Guardians)
	}
	tcLiq2, err := definition.GetTimeChallengeInfoVariable(ctxLiqPost.Storage(), definition.NominateGuardiansMethodName)
	if err != nil {
		t.Fatalf("failed to load liquidity time challenge info: %v", err)
	}
	if tcLiq2 != nil {
		t.Fatalf("expected liquidity time challenge to remain nil after post-spork rejection, got %+v", tcLiq2)
	}
}

func setupActiveBridge(t *testing.T, ctx *testContractContext, admin types.Address, guardians []types.Address) {
	bridgeInfo := &definition.BridgeInfoVariable{
		Administrator: admin,
	}
	if err := bridgeInfo.Save(ctx.Storage()); err != nil {
		t.Fatalf("failed to save bridge info: %v", err)
	}
	votes := make([]types.Address, len(guardians))
	secInfo := &definition.SecurityInfoVariable{
		Guardians:          guardians,
		GuardiansVotes:     votes,
		AdministratorDelay: 10,
		SoftDelay:          10,
	}
	if err := secInfo.Save(ctx.Storage()); err != nil {
		t.Fatalf("failed to save security info: %v", err)
	}
}

func setupActiveLiquidity(t *testing.T, ctx *testContractContext, admin types.Address, guardians []types.Address) {
	liqInfo := &definition.LiquidityInfo{
		Administrator: admin,
		ZnnReward:     big.NewInt(0),
		QsrReward:     big.NewInt(0),
		TokenTuples:   make([]definition.TokenTuple, 0),
	}
	liqVar, err := definition.EncodeLiquidityInfo(liqInfo)
	if err != nil {
		t.Fatalf("failed to encode liquidity info: %v", err)
	}
	if err := liqVar.Save(ctx.Storage()); err != nil {
		t.Fatalf("failed to save liquidity info: %v", err)
	}
	votes := make([]types.Address, len(guardians))
	secInfo := &definition.SecurityInfoVariable{
		Guardians:          guardians,
		GuardiansVotes:     votes,
		AdministratorDelay: 10,
		SoftDelay:          10,
	}
	if err := secInfo.Save(ctx.Storage()); err != nil {
		t.Fatalf("failed to save security info: %v", err)
	}
}

// setupEmergencyBridge sets up a Bridge contract in emergency mode with
// the given guardian set.
func setupEmergencyBridge(t *testing.T, ctx *testContractContext, guardians []types.Address) {
	bridgeInfo := &definition.BridgeInfoVariable{
		Administrator: types.Address{}, // zero address == emergency mode
	}
	if err := bridgeInfo.Save(ctx.Storage()); err != nil {
		t.Fatalf("failed to save bridge info: %v", err)
	}
	votes := make([]types.Address, len(guardians))
	secInfo := &definition.SecurityInfoVariable{
		Guardians:          guardians,
		GuardiansVotes:     votes,
		AdministratorDelay: 10,
		SoftDelay:          10,
	}
	if err := secInfo.Save(ctx.Storage()); err != nil {
		t.Fatalf("failed to save security info: %v", err)
	}
}

// setupEmergencyLiquidity sets up a Liquidity contract in emergency mode with
// the given guardian set.
func setupEmergencyLiquidity(t *testing.T, ctx *testContractContext, guardians []types.Address) {
	liqInfo := &definition.LiquidityInfo{
		Administrator: types.Address{}, // zero address == emergency mode
		ZnnReward:     big.NewInt(0),
		QsrReward:     big.NewInt(0),
		TokenTuples:   make([]definition.TokenTuple, 0),
	}
	liqVar, err := definition.EncodeLiquidityInfo(liqInfo)
	if err != nil {
		t.Fatalf("failed to encode liquidity info: %v", err)
	}
	if err := liqVar.Save(ctx.Storage()); err != nil {
		t.Fatalf("failed to save liquidity info: %v", err)
	}
	votes := make([]types.Address, len(guardians))
	secInfo := &definition.SecurityInfoVariable{
		Guardians:          guardians,
		GuardiansVotes:     votes,
		AdministratorDelay: 10,
		SoftDelay:          10,
	}
	if err := secInfo.Save(ctx.Storage()); err != nil {
		t.Fatalf("failed to save security info: %v", err)
	}
}

// TestGuardian_ProposeAdministrator_ThresholdsPostSpork exercises the threshold
// arithmetic and election for sizes 5, 255, 256, 510, 511, 512, 513, and 1000.
// Verifies that:
// - threshold is len/2 exactly
// - election requires strictly more than len/2 votes (len/2 does not elect; len/2 + 1 elects)
// - counter above 255 does not wrap
func TestGuardian_ProposeAdministrator_ThresholdsPostSpork(t *testing.T) {
	testSizes := []int{5, 255, 256, 510, 511, 512, 513, 1000}
	newAdmin := makeGuardianAddress(9999)

	for _, size := range testSizes {
		t.Run(fmt.Sprintf("Bridge_Size_%d", size), func(t *testing.T) {
			ctx := newTestContractContext(types.BridgeContract, true)
			guardians := make([]types.Address, size)
			for i := 0; i < size; i++ {
				guardians[i] = makeGuardianAddress(i + 1)
			}
			setupEmergencyBridge(t, ctx, guardians)

			threshold := size / 2
			method := &ProposeAdministratorMethod{definition.ProposeAdministratorMethodName}

			// Cast exactly threshold votes: must not elect yet
			for i := 0; i < threshold; i++ {
				sendBlock := &nom.AccountBlock{
					Address:   guardians[i],
					ToAddress: types.BridgeContract,
					Data:      definition.ABIBridge.PackMethodPanic(definition.ProposeAdministratorMethodName, newAdmin),
					Amount:    big.NewInt(0),
				}
				if _, err := method.ReceiveBlock(ctx, sendBlock); err != nil {
					t.Fatalf("vote %d failed: %v", i, err)
				}
			}

			// Verify not elected yet
			bInfo, err := definition.GetBridgeInfoVariable(ctx.Storage())
			if err != nil {
				t.Fatalf("failed to load bridge info: %v", err)
			}
			if !bInfo.Administrator.IsZero() {
				t.Fatalf("size %d: administrator elected at threshold (%d votes)", size, threshold)
			}

			// Cast vote threshold + 1: must elect!
			sendElect := &nom.AccountBlock{
				Address:   guardians[threshold],
				ToAddress: types.BridgeContract,
				Data:      definition.ABIBridge.PackMethodPanic(definition.ProposeAdministratorMethodName, newAdmin),
				Amount:    big.NewInt(0),
			}
			if _, err := method.ReceiveBlock(ctx, sendElect); err != nil {
				t.Fatalf("electing vote failed: %v", err)
			}

			// Verify elected
			bInfo, err = definition.GetBridgeInfoVariable(ctx.Storage())
			if err != nil {
				t.Fatalf("failed to reload bridge info: %v", err)
			}
			if bInfo.Administrator.String() != newAdmin.String() {
				t.Fatalf("size %d: expected new admin %v, got %v", size, newAdmin, bInfo.Administrator)
			}
		})

		t.Run(fmt.Sprintf("Liquidity_Size_%d", size), func(t *testing.T) {
			ctx := newTestContractContext(types.LiquidityContract, true)
			guardians := make([]types.Address, size)
			for i := 0; i < size; i++ {
				guardians[i] = makeGuardianAddress(i + 1)
			}
			setupEmergencyLiquidity(t, ctx, guardians)

			threshold := size / 2
			method := &ProposeAdministratorLiquidity{definition.ProposeAdministratorMethodName}

			// Cast exactly threshold votes: must not elect yet
			for i := 0; i < threshold; i++ {
				sendBlock := &nom.AccountBlock{
					Address:   guardians[i],
					ToAddress: types.LiquidityContract,
					Data:      definition.ABILiquidity.PackMethodPanic(definition.ProposeAdministratorMethodName, newAdmin),
					Amount:    big.NewInt(0),
				}
				if _, err := method.ReceiveBlock(ctx, sendBlock); err != nil {
					t.Fatalf("vote %d failed: %v", i, err)
				}
			}

			// Verify not elected yet
			lInfo, err := definition.GetLiquidityInfo(ctx.Storage())
			if err != nil {
				t.Fatalf("failed to load liquidity info: %v", err)
			}
			if !lInfo.Administrator.IsZero() {
				t.Fatalf("size %d: administrator elected at threshold (%d votes)", size, threshold)
			}

			// Cast vote threshold + 1: must elect!
			sendElect := &nom.AccountBlock{
				Address:   guardians[threshold],
				ToAddress: types.LiquidityContract,
				Data:      definition.ABILiquidity.PackMethodPanic(definition.ProposeAdministratorMethodName, newAdmin),
				Amount:    big.NewInt(0),
			}
			if _, err := method.ReceiveBlock(ctx, sendElect); err != nil {
				t.Fatalf("electing vote failed: %v", err)
			}

			// Verify elected
			lInfo, err = definition.GetLiquidityInfo(ctx.Storage())
			if err != nil {
				t.Fatalf("failed to reload liquidity info: %v", err)
			}
			if lInfo.Administrator.String() != newAdmin.String() {
				t.Fatalf("size %d: expected new admin %v, got %v", size, newAdmin, lInfo.Administrator)
			}
		})
	}
}

// TestGuardian_ProposeAdministrator_PreSporkTruncation verifies that before the spork,
// 512 guardians truncates threshold to uint8(256) = 0 and elects on 1 vote for both
// Bridge and Liquidity, whereas post-spork it requires strictly > 256 votes.
func TestGuardian_ProposeAdministrator_PreSporkTruncation(t *testing.T) {
	newAdmin := makeGuardianAddress(9999)
	size := 512
	guardians := make([]types.Address, size)
	for i := 0; i < size; i++ {
		guardians[i] = makeGuardianAddress(i + 1)
	}

	t.Run("Bridge", func(t *testing.T) {
		ctxPre := newTestContractContext(types.BridgeContract, false)
		setupEmergencyBridge(t, ctxPre, guardians)
		method := &ProposeAdministratorMethod{definition.ProposeAdministratorMethodName}

		send1 := &nom.AccountBlock{
			Address:   guardians[0],
			ToAddress: types.BridgeContract,
			Data:      definition.ABIBridge.PackMethodPanic(definition.ProposeAdministratorMethodName, newAdmin),
			Amount:    big.NewInt(0),
		}
		if _, err := method.ReceiveBlock(ctxPre, send1); err != nil {
			t.Fatalf("pre-spork vote failed: %v", err)
		}

		bInfoPre, err := definition.GetBridgeInfoVariable(ctxPre.Storage())
		if err != nil {
			t.Fatalf("failed to load bridge info: %v", err)
		}
		if bInfoPre.Administrator.String() != newAdmin.String() {
			t.Fatalf("pre-spork: expected truncation bug to elect bridge admin on 1 vote")
		}

		ctxPost := newTestContractContext(types.BridgeContract, true)
		setupEmergencyBridge(t, ctxPost, guardians)

		if _, err := method.ReceiveBlock(ctxPost, send1); err != nil {
			t.Fatalf("post-spork vote failed: %v", err)
		}
		bInfoPost, err := definition.GetBridgeInfoVariable(ctxPost.Storage())
		if err != nil {
			t.Fatalf("failed to load bridge info: %v", err)
		}
		if !bInfoPost.Administrator.IsZero() {
			t.Fatalf("post-spork: 1 vote should not elect bridge admin for 512 guardians")
		}
	})

	t.Run("Liquidity", func(t *testing.T) {
		ctxPre := newTestContractContext(types.LiquidityContract, false)
		setupEmergencyLiquidity(t, ctxPre, guardians)
		method := &ProposeAdministratorLiquidity{definition.ProposeAdministratorMethodName}

		send1 := &nom.AccountBlock{
			Address:   guardians[0],
			ToAddress: types.LiquidityContract,
			Data:      definition.ABILiquidity.PackMethodPanic(definition.ProposeAdministratorMethodName, newAdmin),
			Amount:    big.NewInt(0),
		}
		if _, err := method.ReceiveBlock(ctxPre, send1); err != nil {
			t.Fatalf("pre-spork vote failed: %v", err)
		}

		lInfoPre, err := definition.GetLiquidityInfo(ctxPre.Storage())
		if err != nil {
			t.Fatalf("failed to load liquidity info: %v", err)
		}
		if lInfoPre.Administrator.String() != newAdmin.String() {
			t.Fatalf("pre-spork: expected truncation bug to elect liquidity admin on 1 vote")
		}

		ctxPost := newTestContractContext(types.LiquidityContract, true)
		setupEmergencyLiquidity(t, ctxPost, guardians)

		if _, err := method.ReceiveBlock(ctxPost, send1); err != nil {
			t.Fatalf("post-spork vote failed: %v", err)
		}
		lInfoPost, err := definition.GetLiquidityInfo(ctxPost.Storage())
		if err != nil {
			t.Fatalf("failed to load liquidity info: %v", err)
		}
		if !lInfoPost.Administrator.IsZero() {
			t.Fatalf("post-spork: 1 vote should not elect liquidity admin for 512 guardians")
		}
	})
}

// TestGuardian_ProposeAdministrator_RepeatVoteCountsOnce verifies that repeated votes
// from the same guardian update that guardian's vote without increasing the count
// for both Bridge and Liquidity.
func TestGuardian_ProposeAdministrator_RepeatVoteCountsOnce(t *testing.T) {
	guardians := make([]types.Address, 5) // threshold = 2, requires 3 votes
	for i := 0; i < 5; i++ {
		guardians[i] = makeGuardianAddress(i + 1)
	}
	candidate := makeGuardianAddress(888)

	t.Run("Bridge", func(t *testing.T) {
		ctx := newTestContractContext(types.BridgeContract, true)
		setupEmergencyBridge(t, ctx, guardians)
		method := &ProposeAdministratorMethod{definition.ProposeAdministratorMethodName}

		sendBlock := &nom.AccountBlock{
			Address:   guardians[0],
			ToAddress: types.BridgeContract,
			Data:      definition.ABIBridge.PackMethodPanic(definition.ProposeAdministratorMethodName, candidate),
			Amount:    big.NewInt(0),
		}

		for i := 0; i < 10; i++ {
			if _, err := method.ReceiveBlock(ctx, sendBlock); err != nil {
				t.Fatalf("repeat vote %d failed: %v", i, err)
			}
		}

		bInfo, err := definition.GetBridgeInfoVariable(ctx.Storage())
		if err != nil {
			t.Fatalf("failed to load bridge info: %v", err)
		}
		if !bInfo.Administrator.IsZero() {
			t.Fatalf("repeat votes by single guardian incorrectly triggered bridge election")
		}
	})

	t.Run("Liquidity", func(t *testing.T) {
		ctx := newTestContractContext(types.LiquidityContract, true)
		setupEmergencyLiquidity(t, ctx, guardians)
		method := &ProposeAdministratorLiquidity{definition.ProposeAdministratorMethodName}

		sendBlock := &nom.AccountBlock{
			Address:   guardians[0],
			ToAddress: types.LiquidityContract,
			Data:      definition.ABILiquidity.PackMethodPanic(definition.ProposeAdministratorMethodName, candidate),
			Amount:    big.NewInt(0),
		}

		for i := 0; i < 10; i++ {
			if _, err := method.ReceiveBlock(ctx, sendBlock); err != nil {
				t.Fatalf("repeat vote %d failed: %v", i, err)
			}
		}

		lInfo, err := definition.GetLiquidityInfo(ctx.Storage())
		if err != nil {
			t.Fatalf("failed to load liquidity info: %v", err)
		}
		if !lInfo.Administrator.IsZero() {
			t.Fatalf("repeat votes by single guardian incorrectly triggered liquidity election")
		}
	})
}

// TestGuardian_ProposeAdministrator_FinalVoteDecisive seeds exactly len/2
// votes for newAdmin (and non-electing votes in the remaining pre-final slots),
// then submits the final guardian's vote through ReceiveBlock so that the last
// slot is the decisive (len/2 + 1)-th vote. Verifies that the administrator is
// elected and all votes are cleared.
func TestGuardian_ProposeAdministrator_FinalVoteDecisive(t *testing.T) {
	newAdmin := makeGuardianAddress(9999)
	otherCandidate := makeGuardianAddress(8888)

	for _, size := range []int{510, 511} {
		guardians := make([]types.Address, size)
		for i := 0; i < size; i++ {
			guardians[i] = makeGuardianAddress(i + 1)
		}
		threshold := size / 2
		last := size - 1

		t.Run(fmt.Sprintf("Bridge_FinalVoteDecisive_%d", size), func(t *testing.T) {
			ctx := newTestContractContext(types.BridgeContract, true)
			setupEmergencyBridge(t, ctx, guardians)

			secInfo, err := definition.GetSecurityInfoVariable(ctx.Storage())
			if err != nil {
				t.Fatalf("failed to load security info: %v", err)
			}
			for i := 0; i < threshold; i++ {
				secInfo.GuardiansVotes[i] = newAdmin
			}
			for i := threshold; i < last; i++ {
				secInfo.GuardiansVotes[i] = otherCandidate
			}
			if err := secInfo.Save(ctx.Storage()); err != nil {
				t.Fatalf("failed to save security info: %v", err)
			}

			method := &ProposeAdministratorMethod{definition.ProposeAdministratorMethodName}
			sendFinal := &nom.AccountBlock{
				Address:   guardians[last],
				ToAddress: types.BridgeContract,
				Data:      definition.ABIBridge.PackMethodPanic(definition.ProposeAdministratorMethodName, newAdmin),
				Amount:    big.NewInt(0),
			}
			if _, err := method.ReceiveBlock(ctx, sendFinal); err != nil {
				t.Fatalf("size %d: final decisive vote failed: %v", size, err)
			}

			bInfo, err := definition.GetBridgeInfoVariable(ctx.Storage())
			if err != nil {
				t.Fatalf("failed to load bridge info: %v", err)
			}
			if bInfo.Administrator.String() != newAdmin.String() {
				t.Fatalf("size %d: expected admin %v, got %v", size, newAdmin, bInfo.Administrator)
			}

			secInfo, err = definition.GetSecurityInfoVariable(ctx.Storage())
			if err != nil {
				t.Fatalf("failed to reload security info: %v", err)
			}
			for idx, vote := range secInfo.GuardiansVotes {
				if !vote.IsZero() {
					t.Fatalf("size %d: vote %d not cleared after election", size, idx)
				}
			}
		})

		t.Run(fmt.Sprintf("Liquidity_FinalVoteDecisive_%d", size), func(t *testing.T) {
			ctx := newTestContractContext(types.LiquidityContract, true)
			setupEmergencyLiquidity(t, ctx, guardians)

			secInfo, err := definition.GetSecurityInfoVariable(ctx.Storage())
			if err != nil {
				t.Fatalf("failed to load security info: %v", err)
			}
			for i := 0; i < threshold; i++ {
				secInfo.GuardiansVotes[i] = newAdmin
			}
			for i := threshold; i < last; i++ {
				secInfo.GuardiansVotes[i] = otherCandidate
			}
			if err := secInfo.Save(ctx.Storage()); err != nil {
				t.Fatalf("failed to save security info: %v", err)
			}

			method := &ProposeAdministratorLiquidity{definition.ProposeAdministratorMethodName}
			sendFinal := &nom.AccountBlock{
				Address:   guardians[last],
				ToAddress: types.LiquidityContract,
				Data:      definition.ABILiquidity.PackMethodPanic(definition.ProposeAdministratorMethodName, newAdmin),
				Amount:    big.NewInt(0),
			}
			if _, err := method.ReceiveBlock(ctx, sendFinal); err != nil {
				t.Fatalf("size %d: final decisive vote failed: %v", size, err)
			}

			lInfo, err := definition.GetLiquidityInfo(ctx.Storage())
			if err != nil {
				t.Fatalf("failed to load liquidity info: %v", err)
			}
			if lInfo.Administrator.String() != newAdmin.String() {
				t.Fatalf("size %d: expected admin %v, got %v", size, newAdmin, lInfo.Administrator)
			}

			secInfo, err = definition.GetSecurityInfoVariable(ctx.Storage())
			if err != nil {
				t.Fatalf("failed to reload security info: %v", err)
			}
			for idx, vote := range secInfo.GuardiansVotes {
				if !vote.IsZero() {
					t.Fatalf("size %d: vote %d not cleared after election", size, idx)
				}
			}
		})
	}
}
