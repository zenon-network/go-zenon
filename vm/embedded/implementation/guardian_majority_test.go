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

// TestNominateGuardians_ReceiveBlock_SporkGating verifies that
// NominateGuardians ReceiveBlock enforces CheckNominatedGuardians post-spork
// for both bridge and liquidity.
func TestGuardian_NominateGuardians_ReceiveBlock_SporkGating(t *testing.T) {
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

	// --- Bridge ---
	ctxBridgePost := newTestContractContext(types.BridgeContract, true)
	methodBridge := &NominateGuardiansMethod{definition.NominateGuardiansMethodName}

	sendDupBridge := &nom.AccountBlock{
		Address:   admin,
		ToAddress: types.BridgeContract,
		Data:      definition.ABIBridge.PackMethodPanic(definition.NominateGuardiansMethodName, guardiansDup),
		Amount:    big.NewInt(0),
	}
	if _, err := methodBridge.ReceiveBlock(ctxBridgePost, sendDupBridge); err != constants.ErrInvalidGuardians {
		t.Fatalf("expected ErrInvalidGuardians for duplicate bridge guardians post-spork, got %v", err)
	}

	sendOverBridge := &nom.AccountBlock{
		Address:   admin,
		ToAddress: types.BridgeContract,
		Data:      definition.ABIBridge.PackMethodPanic(definition.NominateGuardiansMethodName, guardiansOversized),
		Amount:    big.NewInt(0),
	}
	if _, err := methodBridge.ReceiveBlock(ctxBridgePost, sendOverBridge); err != constants.ErrInvalidGuardians {
		t.Fatalf("expected ErrInvalidGuardians for oversized bridge guardians post-spork, got %v", err)
	}

	// --- Liquidity ---
	ctxLiqPost := newTestContractContext(types.LiquidityContract, true)
	methodLiq := &NominateGuardiansLiquidity{definition.NominateGuardiansMethodName}

	sendDupLiq := &nom.AccountBlock{
		Address:   admin,
		ToAddress: types.LiquidityContract,
		Data:      definition.ABILiquidity.PackMethodPanic(definition.NominateGuardiansMethodName, guardiansDup),
		Amount:    big.NewInt(0),
	}
	if _, err := methodLiq.ReceiveBlock(ctxLiqPost, sendDupLiq); err != constants.ErrInvalidGuardians {
		t.Fatalf("expected ErrInvalidGuardians for duplicate liquidity guardians post-spork, got %v", err)
	}

	sendOverLiq := &nom.AccountBlock{
		Address:   admin,
		ToAddress: types.LiquidityContract,
		Data:      definition.ABILiquidity.PackMethodPanic(definition.NominateGuardiansMethodName, guardiansOversized),
		Amount:    big.NewInt(0),
	}
	if _, err := methodLiq.ReceiveBlock(ctxLiqPost, sendOverLiq); err != constants.ErrInvalidGuardians {
		t.Fatalf("expected ErrInvalidGuardians for oversized liquidity guardians post-spork, got %v", err)
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

// TestProposeAdministrator_ThresholdsPostSpork exercises the threshold
// arithmetic and election for sizes 5, 255, 256, 510, 511, 512, 513, and 1000.
// Verifies that:
// - threshold is len/2 exactly
// - election requires strictly more than len/2 votes (len/2 does not elect; len/2 + 1 elects)
// - counter above 255 does not wrap
// - unanimous vote on 510 and 511 succeeds
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
			bInfo, _ := definition.GetBridgeInfoVariable(ctx.Storage())
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
			bInfo, _ = definition.GetBridgeInfoVariable(ctx.Storage())
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
			lInfo, _ := definition.GetLiquidityInfo(ctx.Storage())
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
			lInfo, _ = definition.GetLiquidityInfo(ctx.Storage())
			if lInfo.Administrator.String() != newAdmin.String() {
				t.Fatalf("size %d: expected new admin %v, got %v", size, newAdmin, lInfo.Administrator)
			}
		})
	}
}

// TestProposeAdministrator_PreSporkTruncation verifies that before the spork,
// 512 guardians truncates threshold to uint8(256) = 0 and elects on 1 vote,
// whereas post-spork it requires strictly > 256 votes.
func TestGuardian_ProposeAdministrator_PreSporkTruncation(t *testing.T) {
	newAdmin := makeGuardianAddress(9999)
	size := 512
	guardians := make([]types.Address, size)
	for i := 0; i < size; i++ {
		guardians[i] = makeGuardianAddress(i + 1)
	}

	// 1. Pre-spork: 1 vote elects because threshold = uint8(256) = 0, votes[1] > 0
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

	bInfoPre, _ := definition.GetBridgeInfoVariable(ctxPre.Storage())
	if bInfoPre.Administrator.String() != newAdmin.String() {
		t.Fatalf("pre-spork: expected truncation bug to elect admin on 1 vote")
	}

	// 2. Post-spork: 1 vote does NOT elect because threshold = 256 in int
	ctxPost := newTestContractContext(types.BridgeContract, true)
	setupEmergencyBridge(t, ctxPost, guardians)

	if _, err := method.ReceiveBlock(ctxPost, send1); err != nil {
		t.Fatalf("post-spork vote failed: %v", err)
	}
	bInfoPost, _ := definition.GetBridgeInfoVariable(ctxPost.Storage())
	if !bInfoPost.Administrator.IsZero() {
		t.Fatalf("post-spork: 1 vote should not elect admin for 512 guardians!")
	}
}

// TestProposeAdministrator_RepeatVoteCountsOnce verifies that repeated votes
// from the same guardian update that guardian's vote without increasing the count.
func TestGuardian_ProposeAdministrator_RepeatVoteCountsOnce(t *testing.T) {
	ctx := newTestContractContext(types.BridgeContract, true)
	guardians := make([]types.Address, 5) // threshold = 2, requires 3 votes
	for i := 0; i < 5; i++ {
		guardians[i] = makeGuardianAddress(i + 1)
	}
	setupEmergencyBridge(t, ctx, guardians)

	candidate := makeGuardianAddress(888)
	method := &ProposeAdministratorMethod{definition.ProposeAdministratorMethodName}

	sendBlock := &nom.AccountBlock{
		Address:   guardians[0],
		ToAddress: types.BridgeContract,
		Data:      definition.ABIBridge.PackMethodPanic(definition.ProposeAdministratorMethodName, candidate),
		Amount:    big.NewInt(0),
	}

	// Guardian 0 votes 10 times for the same candidate
	for i := 0; i < 10; i++ {
		if _, err := method.ReceiveBlock(ctx, sendBlock); err != nil {
			t.Fatalf("repeat vote %d failed: %v", i, err)
		}
	}

	// Must NOT elect: guardian 0's vote counts once (1 < threshold 2)
	bInfo, _ := definition.GetBridgeInfoVariable(ctx.Storage())
	if !bInfo.Administrator.IsZero() {
		t.Fatalf("repeat votes by single guardian incorrectly triggered election!")
	}
}
