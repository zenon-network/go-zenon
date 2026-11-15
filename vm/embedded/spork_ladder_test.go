package embedded

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	cabi "github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/embedded/implementation"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

// sporkLadderContext answers only the five spork questions sporkVariantKey
// asks. Every other AccountVmContext method panics on the nil embedded
// interface, which is what these tests want: the method lookup must not
// read anything else.
type sporkLadderContext struct {
	vm_context.AccountVmContext
	accelerator, bridgeAndLiquidity, htlc, dynamicPlasma, ptlc bool
}

func (c *sporkLadderContext) IsAcceleratorSporkEnforced() bool        { return c.accelerator }
func (c *sporkLadderContext) IsBridgeAndLiquiditySporkEnforced() bool { return c.bridgeAndLiquidity }
func (c *sporkLadderContext) IsHtlcSporkEnforced() bool               { return c.htlc }
func (c *sporkLadderContext) IsDynamicPlasmaSporkEnforced() bool      { return c.dynamicPlasma }
func (c *sporkLadderContext) IsPtlcSporkEnforced() bool               { return c.ptlc }

// releasedMethodTable names the table the released lookup (v0.0.9,
// bb39e31) selects for a legacy spork state. That lookup is a ladder,
//
//	if Htlc { htlc } else if BridgeAndLiquidity { bridgeAndLiquidity } else if Accelerator { accelerator } else { origin }
//
// and each table is built on top of the one below it, so the HTLC spork
// alone brings the Bridge contract and the Accelerator methods with it.
func releasedMethodTable(accelerator, bridgeAndLiquidity, htlc bool) string {
	switch {
	case htlc:
		return "htlc"
	case bridgeAndLiquidity:
		return "bridgeAndLiquidity"
	case accelerator:
		return "accelerator"
	default:
		return "origin"
	}
}

// dumpMethodTable lists a contract map one line per method, as
// "<contract> <method name> <method type> <method value>", sorted.
func dumpMethodTable(contracts map[types.Address]*embeddedImplementation) []string {
	lines := make([]string, 0)
	for address, contract := range contracts {
		for name, method := range contract.m {
			lines = append(lines, fmt.Sprintf("%v %v %T %+v", address, name, method, method))
		}
	}
	sort.Strings(lines)
	return lines
}

// TestSporkVariantKey_LegacySporksFormTheReleasedLadder checks the
// contract map selected for every combination of the five sporks
// against the tables of the released lookup. The Dynamic Plasma and
// PTLC sporks postdate that lookup and stay independent switches: each
// adds exactly its own methods on top of the legacy table.
func TestSporkVariantKey_LegacySporksFormTheReleasedLadder(t *testing.T) {
	embeddedVariantsOnce.Do(buildEmbeddedVariants)

	setVariables := fmt.Sprintf("%v %v ", types.PlasmaContract, cabi.SetVariablesMethodName)
	ptlcContract := fmt.Sprintf("%v ", types.PtlcContract)

	for key := 0; key < 32; key++ {
		context := &sporkLadderContext{
			accelerator:        key&1 != 0,
			bridgeAndLiquidity: key&2 != 0,
			htlc:               key&4 != 0,
			dynamicPlasma:      key&8 != 0,
			ptlc:               key&16 != 0,
		}
		name := fmt.Sprintf("accelerator=%v,bridgeAndLiquidity=%v,htlc=%v,dynamicPlasma=%v,ptlc=%v",
			context.accelerator, context.bridgeAndLiquidity, context.htlc, context.dynamicPlasma, context.ptlc)
		t.Run(name, func(t *testing.T) {
			legacy := make([]string, 0)
			hasSetVariables, ptlcMethods := false, 0
			for _, line := range dumpMethodTable(embeddedVariants[sporkVariantKey(context)]) {
				switch {
				case strings.HasPrefix(line, setVariables):
					hasSetVariables = true
				case strings.HasPrefix(line, ptlcContract):
					ptlcMethods++
				default:
					legacy = append(legacy, line)
				}
			}

			table := releasedMethodTable(context.accelerator, context.bridgeAndLiquidity, context.htlc)
			expected := strings.Split(strings.TrimSpace(releasedMethodTables[table]), "\n")
			if got, want := strings.Join(legacy, "\n"), strings.Join(expected, "\n"); got != want {
				t.Errorf("legacy methods differ from the released %q table\ngot:\n%v\nwant:\n%v", table, got, want)
			}
			if hasSetVariables != context.dynamicPlasma {
				t.Errorf("Plasma %v present = %v, want %v", cabi.SetVariablesMethodName, hasSetVariables, context.dynamicPlasma)
			}
			wantPtlcMethods := 0
			if context.ptlc {
				wantPtlcMethods = 6
			}
			if ptlcMethods != wantPtlcMethods {
				t.Errorf("PTLC contract has %v methods, want %v", ptlcMethods, wantPtlcMethods)
			}
		})
	}
}

// TestGetEmbeddedMethod_HtlcSporkBringsBridgeAndLiquidity pins the
// state between the two mainnet enforcement heights, HTLC enforced and
// BridgeAndLiquidity not yet, through the exported lookups. The
// released lookup resolves the Bridge contract and the newer Liquidity
// methods there; a send that calls one is valid on released nodes and
// has to stay valid here.
func TestGetEmbeddedMethod_HtlcSporkBringsBridgeAndLiquidity(t *testing.T) {
	context := &sporkLadderContext{accelerator: true, htlc: true}

	method, err := GetEmbeddedMethod(context, types.LiquidityContract, cabi.ABILiquidity.PackMethodPanic(cabi.CollectRewardMethodName))
	if err != nil {
		t.Fatalf("Liquidity %v: %v", cabi.CollectRewardMethodName, err)
	}
	collect, ok := method.(*implementation.CollectRewardMethod)
	if !ok {
		t.Fatalf("Liquidity %v resolved to %T", cabi.CollectRewardMethodName, method)
	}
	if collect.Plasma != constants.AlphanetPlasmaTable.EmbeddedWDoubleWithdraw {
		t.Fatalf("Liquidity %v plasma = %v, want %v", cabi.CollectRewardMethodName, collect.Plasma, constants.AlphanetPlasmaTable.EmbeddedWDoubleWithdraw)
	}

	method, err = GetEmbeddedMethod(context, types.BridgeContract, cabi.ABIBridge.PackMethodPanic(cabi.EmergencyMethodName))
	if err != nil {
		t.Fatalf("Bridge %v: %v", cabi.EmergencyMethodName, err)
	}
	if _, ok := method.(*implementation.EmergencyMethod); !ok {
		t.Fatalf("Bridge %v resolved to %T", cabi.EmergencyMethodName, method)
	}

	method, err = GetEmbeddedMethod(context, types.LiquidityContract, cabi.ABILiquidity.PackMethodPanic(cabi.UpdateMethodName))
	if err != nil {
		t.Fatalf("Liquidity %v: %v", cabi.UpdateMethodName, err)
	}
	if _, ok := method.(*implementation.UpdateRewardEmbeddedLiquidityMethod); !ok {
		t.Fatalf("Liquidity %v resolved to %T", cabi.UpdateMethodName, method)
	}

	// The HTLC spork alone, without the Accelerator spork reported either,
	// selects the same table: the ladder's first rung decides.
	method, err = GetEmbeddedMethod(&sporkLadderContext{htlc: true}, types.AcceleratorContract, cabi.ABIAccelerator.PackMethodPanic(cabi.UpdateMethodName))
	if err != nil {
		t.Fatalf("Accelerator %v: %v", cabi.UpdateMethodName, err)
	}
	if _, ok := method.(*implementation.UpdateEmbeddedAcceleratorMethod); !ok {
		t.Fatalf("Accelerator %v resolved to %T", cabi.UpdateMethodName, method)
	}

	// CheckNoTrailingBytes reads the same table. A Bridge call with a
	// trailing byte is only detected if the Bridge contract is found.
	data := append(cabi.ABIBridge.PackMethodPanic(cabi.EmergencyMethodName), 0)
	if err := CheckNoTrailingBytes(context, types.BridgeContract, data); err != constants.ErrABDataTrailingBytes {
		t.Fatalf("CheckNoTrailingBytes = %v, want %v", err, constants.ErrABDataTrailingBytes)
	}
}

// releasedMethodTables holds the four method tables of the released
// lookup, dumped with dumpMethodTable from originEmbedded,
// acceleratorEmbedded, bridgeAndLiquidityEmbedded and htlcEmbedded in
// vm/embedded/embedded.go at bb39e31 (v0.0.9). They are data, not
// derived from the code under test: a change to a legacy table has to
// show up here as a diff.
var releasedMethodTables = map[string]string{
	"origin": `
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 Donate *implementation.DonateMethod &{MethodName:Donate}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae Donate *implementation.DonateMethod &{MethodName:Donate}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae Update *implementation.UpdateEmbeddedLiquidityMethod &{MethodName:Update}
z1qxemdeddedxplasmaxxxxxxxxxxxxxxxxsctrp CancelFuse *implementation.CancelFuseMethod &{MethodName:CancelFuse}
z1qxemdeddedxplasmaxxxxxxxxxxxxxxxxsctrp Fuse *implementation.FuseMethod &{MethodName:Fuse}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg CollectReward *implementation.CollectRewardMethod &{MethodName:CollectReward Plasma:126000}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg Delegate *implementation.DelegateMethod &{MethodName:Delegate}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg DepositQsr *implementation.DepositQsrMethod &{MethodName:DepositQsr}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg Register *implementation.RegisterMethod &{MethodName:Register}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg RegisterLegacy *implementation.LegacyRegisterMethod &{MethodName:RegisterLegacy}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg Revoke *implementation.RevokeMethod &{MethodName:Revoke}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg Undelegate *implementation.UndelegateMethod &{MethodName:Undelegate}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg Update *implementation.UpdateEmbeddedPillarMethod &{MethodName:Update}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg UpdatePillar *implementation.UpdatePillarMethod &{MethodName:UpdatePillar}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg WithdrawQsr *implementation.WithdrawQsrMethod &{MethodName:WithdrawQsr}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r CollectReward *implementation.CollectRewardMethod &{MethodName:CollectReward Plasma:126000}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r DepositQsr *implementation.DepositQsrMethod &{MethodName:DepositQsr}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r Register *implementation.RegisterSentinelMethod &{MethodName:Register}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r Revoke *implementation.RevokeSentinelMethod &{MethodName:Revoke}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r Update *implementation.UpdateEmbeddedSentinelMethod &{MethodName:Update}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r WithdrawQsr *implementation.WithdrawQsrMethod &{MethodName:WithdrawQsr}
z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48 ActivateSpork *implementation.ActivateSporkMethod &{MethodName:ActivateSpork}
z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48 CreateSpork *implementation.CreateSporkMethod &{MethodName:CreateSpork}
z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 Cancel *implementation.CancelStakeMethod &{MethodName:Cancel}
z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 CollectReward *implementation.CollectRewardMethod &{MethodName:CollectReward Plasma:126000}
z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 Stake *implementation.StakeMethod &{MethodName:Stake}
z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 Update *implementation.UpdateEmbeddedStakeMethod &{MethodName:Update}
z1qxemdeddedxswapxxxxxxxxxxxxxxxxxxl4yww RetrieveAssets *implementation.SwapRetrieveAssetsMethod &{MethodName:RetrieveAssets}
z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 Burn *implementation.BurnMethod &{MethodName:Burn}
z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 IssueToken *implementation.IssueMethod &{MethodName:IssueToken}
z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 Mint *implementation.MintMethod &{MethodName:Mint}
z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 UpdateToken *implementation.UpdateTokenMethod &{MethodName:UpdateToken}
`,
	"accelerator": `
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 AddPhase *implementation.AddPhaseMethod &{MethodName:AddPhase}
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 CreateProject *implementation.CreateProjectMethod &{MethodName:CreateProject}
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 Donate *implementation.DonateMethod &{MethodName:Donate}
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 Update *implementation.UpdateEmbeddedAcceleratorMethod &{MethodName:Update}
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 UpdatePhase *implementation.UpdatePhaseMethod &{MethodName:UpdatePhase}
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 VoteByName *implementation.VoteByNameMethod &{MethodName:VoteByName}
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 VoteByProdAddress *implementation.VoteByProdAddressMethod &{MethodName:VoteByProdAddress}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae BurnZnn *implementation.BurnZnnMethod &{MethodName:BurnZnn}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae Donate *implementation.DonateMethod &{MethodName:Donate}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae Fund *implementation.FundMethod &{MethodName:Fund}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae Update *implementation.UpdateEmbeddedLiquidityMethod &{MethodName:Update}
z1qxemdeddedxplasmaxxxxxxxxxxxxxxxxsctrp CancelFuse *implementation.CancelFuseMethod &{MethodName:CancelFuse}
z1qxemdeddedxplasmaxxxxxxxxxxxxxxxxsctrp Fuse *implementation.FuseMethod &{MethodName:Fuse}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg CollectReward *implementation.CollectRewardMethod &{MethodName:CollectReward Plasma:52500}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg Delegate *implementation.DelegateMethod &{MethodName:Delegate}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg DepositQsr *implementation.DepositQsrMethod &{MethodName:DepositQsr}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg Register *implementation.RegisterMethod &{MethodName:Register}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg RegisterLegacy *implementation.LegacyRegisterMethod &{MethodName:RegisterLegacy}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg Revoke *implementation.RevokeMethod &{MethodName:Revoke}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg Undelegate *implementation.UndelegateMethod &{MethodName:Undelegate}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg Update *implementation.UpdateEmbeddedPillarMethod &{MethodName:Update}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg UpdatePillar *implementation.UpdatePillarMethod &{MethodName:UpdatePillar}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg WithdrawQsr *implementation.WithdrawQsrMethod &{MethodName:WithdrawQsr}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r CollectReward *implementation.CollectRewardMethod &{MethodName:CollectReward Plasma:52500}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r DepositQsr *implementation.DepositQsrMethod &{MethodName:DepositQsr}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r Register *implementation.RegisterSentinelMethod &{MethodName:Register}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r Revoke *implementation.RevokeSentinelMethod &{MethodName:Revoke}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r Update *implementation.UpdateEmbeddedSentinelMethod &{MethodName:Update}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r WithdrawQsr *implementation.WithdrawQsrMethod &{MethodName:WithdrawQsr}
z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48 ActivateSpork *implementation.ActivateSporkMethod &{MethodName:ActivateSpork}
z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48 CreateSpork *implementation.CreateSporkMethod &{MethodName:CreateSpork}
z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 Cancel *implementation.CancelStakeMethod &{MethodName:Cancel}
z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 CollectReward *implementation.CollectRewardMethod &{MethodName:CollectReward Plasma:52500}
z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 Stake *implementation.StakeMethod &{MethodName:Stake}
z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 Update *implementation.UpdateEmbeddedStakeMethod &{MethodName:Update}
z1qxemdeddedxswapxxxxxxxxxxxxxxxxxxl4yww RetrieveAssets *implementation.SwapRetrieveAssetsMethod &{MethodName:RetrieveAssets}
z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 Burn *implementation.BurnMethod &{MethodName:Burn}
z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 IssueToken *implementation.IssueMethod &{MethodName:IssueToken}
z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 Mint *implementation.MintMethod &{MethodName:Mint}
z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 UpdateToken *implementation.UpdateTokenMethod &{MethodName:UpdateToken}
`,
	"bridgeAndLiquidity": `
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 AddPhase *implementation.AddPhaseMethod &{MethodName:AddPhase}
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 CreateProject *implementation.CreateProjectMethod &{MethodName:CreateProject}
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 Donate *implementation.DonateMethod &{MethodName:Donate}
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 Update *implementation.UpdateEmbeddedAcceleratorMethod &{MethodName:Update}
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 UpdatePhase *implementation.UpdatePhaseMethod &{MethodName:UpdatePhase}
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 VoteByName *implementation.VoteByNameMethod &{MethodName:VoteByName}
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 VoteByProdAddress *implementation.VoteByProdAddressMethod &{MethodName:VoteByProdAddress}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d ChangeAdministrator *implementation.ChangeAdministratorMethod &{MethodName:ChangeAdministrator}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d ChangeTssECDSAPubKey *implementation.ChangeTssECDSAPubKeyMethod &{MethodName:ChangeTssECDSAPubKey}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d Emergency *implementation.EmergencyMethod &{MethodName:Emergency}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d Halt *implementation.HaltMethod &{MethodName:Halt}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d NominateGuardians *implementation.NominateGuardiansMethod &{MethodName:NominateGuardians}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d ProposeAdministrator *implementation.ProposeAdministratorMethod &{MethodName:ProposeAdministrator}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d Redeem *implementation.RedeemMethod &{MethodName:Redeem}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d RemoveNetwork *implementation.RemoveNetworkMethod &{MethodName:RemoveNetwork}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d RemoveTokenPair *implementation.RemoveTokenPairMethod &{MethodName:RemoveTokenPair}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d RevokeUnwrapRequest *implementation.RevokeUnwrapRequestMethod &{MethodName:RevokeUnwrapRequest}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d SetAllowKeyGen *implementation.SetAllowKeygenMethod &{MethodName:SetAllowKeyGen}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d SetBridgeMetadata *implementation.SetBridgeMetadataMethod &{MethodName:SetBridgeMetadata}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d SetNetwork *implementation.SetNetworkMethod &{MethodName:SetNetwork}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d SetNetworkMetadata *implementation.SetNetworkMetadataMethod &{MethodName:SetNetworkMetadata}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d SetOrchestratorInfo *implementation.SetOrchestratorInfoMethod &{MethodName:SetOrchestratorInfo}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d SetTokenPair *implementation.SetTokenPairMethod &{MethodName:SetTokenPair}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d Unhalt *implementation.UnhaltMethod &{MethodName:Unhalt}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d UnwrapToken *implementation.UnwrapTokenMethod &{MethodName:UnwrapToken}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d UpdateWrapRequest *implementation.UpdateWrapRequestMethod &{MethodName:UpdateWrapRequest}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d WrapToken *implementation.WrapTokenMethod &{MethodName:WrapToken}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae BurnZnn *implementation.BurnZnnMethod &{MethodName:BurnZnn}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae CancelLiquidityStake *implementation.CancelLiquidityStakeMethod &{MethodName:CancelLiquidityStake}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae ChangeAdministrator *implementation.ChangeAdministratorLiquidity &{MethodName:ChangeAdministrator}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae CollectReward *implementation.CollectRewardMethod &{MethodName:CollectReward Plasma:94500}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae Donate *implementation.DonateMethod &{MethodName:Donate}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae Emergency *implementation.EmergencyLiquidity &{MethodName:Emergency}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae Fund *implementation.FundMethod &{MethodName:Fund}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae LiquidityStake *implementation.LiquidityStakeMethod &{MethodName:LiquidityStake}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae NominateGuardians *implementation.NominateGuardiansLiquidity &{MethodName:NominateGuardians}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae ProposeAdministrator *implementation.ProposeAdministratorLiquidity &{MethodName:ProposeAdministrator}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae SetAdditionalReward *implementation.SetAdditionalReward &{MethodName:SetAdditionalReward}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae SetIsHalted *implementation.SetIsHalted &{MethodName:SetIsHalted}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae SetTokenTuple *implementation.SetTokenTupleMethod &{MethodName:SetTokenTuple}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae UnlockLiquidityStakeEntries *implementation.UnlockLiquidityStakeEntries &{MethodName:UnlockLiquidityStakeEntries}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae Update *implementation.UpdateRewardEmbeddedLiquidityMethod &{MethodName:Update}
z1qxemdeddedxplasmaxxxxxxxxxxxxxxxxsctrp CancelFuse *implementation.CancelFuseMethod &{MethodName:CancelFuse}
z1qxemdeddedxplasmaxxxxxxxxxxxxxxxxsctrp Fuse *implementation.FuseMethod &{MethodName:Fuse}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg CollectReward *implementation.CollectRewardMethod &{MethodName:CollectReward Plasma:52500}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg Delegate *implementation.DelegateMethod &{MethodName:Delegate}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg DepositQsr *implementation.DepositQsrMethod &{MethodName:DepositQsr}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg Register *implementation.RegisterMethod &{MethodName:Register}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg RegisterLegacy *implementation.LegacyRegisterMethod &{MethodName:RegisterLegacy}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg Revoke *implementation.RevokeMethod &{MethodName:Revoke}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg Undelegate *implementation.UndelegateMethod &{MethodName:Undelegate}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg Update *implementation.UpdateEmbeddedPillarMethod &{MethodName:Update}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg UpdatePillar *implementation.UpdatePillarMethod &{MethodName:UpdatePillar}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg WithdrawQsr *implementation.WithdrawQsrMethod &{MethodName:WithdrawQsr}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r CollectReward *implementation.CollectRewardMethod &{MethodName:CollectReward Plasma:52500}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r DepositQsr *implementation.DepositQsrMethod &{MethodName:DepositQsr}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r Register *implementation.RegisterSentinelMethod &{MethodName:Register}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r Revoke *implementation.RevokeSentinelMethod &{MethodName:Revoke}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r Update *implementation.UpdateEmbeddedSentinelMethod &{MethodName:Update}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r WithdrawQsr *implementation.WithdrawQsrMethod &{MethodName:WithdrawQsr}
z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48 ActivateSpork *implementation.ActivateSporkMethod &{MethodName:ActivateSpork}
z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48 CreateSpork *implementation.CreateSporkMethod &{MethodName:CreateSpork}
z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 Cancel *implementation.CancelStakeMethod &{MethodName:Cancel}
z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 CollectReward *implementation.CollectRewardMethod &{MethodName:CollectReward Plasma:52500}
z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 Stake *implementation.StakeMethod &{MethodName:Stake}
z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 Update *implementation.UpdateEmbeddedStakeMethod &{MethodName:Update}
z1qxemdeddedxswapxxxxxxxxxxxxxxxxxxl4yww RetrieveAssets *implementation.SwapRetrieveAssetsMethod &{MethodName:RetrieveAssets}
z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 Burn *implementation.BurnMethod &{MethodName:Burn}
z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 IssueToken *implementation.IssueMethod &{MethodName:IssueToken}
z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 Mint *implementation.MintMethod &{MethodName:Mint}
z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 UpdateToken *implementation.UpdateTokenMethod &{MethodName:UpdateToken}
`,
	"htlc": `
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 AddPhase *implementation.AddPhaseMethod &{MethodName:AddPhase}
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 CreateProject *implementation.CreateProjectMethod &{MethodName:CreateProject}
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 Donate *implementation.DonateMethod &{MethodName:Donate}
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 Update *implementation.UpdateEmbeddedAcceleratorMethod &{MethodName:Update}
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 UpdatePhase *implementation.UpdatePhaseMethod &{MethodName:UpdatePhase}
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 VoteByName *implementation.VoteByNameMethod &{MethodName:VoteByName}
z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 VoteByProdAddress *implementation.VoteByProdAddressMethod &{MethodName:VoteByProdAddress}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d ChangeAdministrator *implementation.ChangeAdministratorMethod &{MethodName:ChangeAdministrator}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d ChangeTssECDSAPubKey *implementation.ChangeTssECDSAPubKeyMethod &{MethodName:ChangeTssECDSAPubKey}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d Emergency *implementation.EmergencyMethod &{MethodName:Emergency}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d Halt *implementation.HaltMethod &{MethodName:Halt}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d NominateGuardians *implementation.NominateGuardiansMethod &{MethodName:NominateGuardians}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d ProposeAdministrator *implementation.ProposeAdministratorMethod &{MethodName:ProposeAdministrator}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d Redeem *implementation.RedeemMethod &{MethodName:Redeem}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d RemoveNetwork *implementation.RemoveNetworkMethod &{MethodName:RemoveNetwork}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d RemoveTokenPair *implementation.RemoveTokenPairMethod &{MethodName:RemoveTokenPair}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d RevokeUnwrapRequest *implementation.RevokeUnwrapRequestMethod &{MethodName:RevokeUnwrapRequest}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d SetAllowKeyGen *implementation.SetAllowKeygenMethod &{MethodName:SetAllowKeyGen}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d SetBridgeMetadata *implementation.SetBridgeMetadataMethod &{MethodName:SetBridgeMetadata}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d SetNetwork *implementation.SetNetworkMethod &{MethodName:SetNetwork}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d SetNetworkMetadata *implementation.SetNetworkMetadataMethod &{MethodName:SetNetworkMetadata}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d SetOrchestratorInfo *implementation.SetOrchestratorInfoMethod &{MethodName:SetOrchestratorInfo}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d SetTokenPair *implementation.SetTokenPairMethod &{MethodName:SetTokenPair}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d Unhalt *implementation.UnhaltMethod &{MethodName:Unhalt}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d UnwrapToken *implementation.UnwrapTokenMethod &{MethodName:UnwrapToken}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d UpdateWrapRequest *implementation.UpdateWrapRequestMethod &{MethodName:UpdateWrapRequest}
z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d WrapToken *implementation.WrapTokenMethod &{MethodName:WrapToken}
z1qxemdeddedxhtlcxxxxxxxxxxxxxxxxxygecvw AllowProxyUnlock *implementation.AllowHtlcProxyUnlockMethod &{MethodName:AllowProxyUnlock}
z1qxemdeddedxhtlcxxxxxxxxxxxxxxxxxygecvw Create *implementation.CreateHtlcMethod &{MethodName:Create}
z1qxemdeddedxhtlcxxxxxxxxxxxxxxxxxygecvw DenyProxyUnlock *implementation.DenyHtlcProxyUnlockMethod &{MethodName:DenyProxyUnlock}
z1qxemdeddedxhtlcxxxxxxxxxxxxxxxxxygecvw Reclaim *implementation.ReclaimHtlcMethod &{MethodName:Reclaim}
z1qxemdeddedxhtlcxxxxxxxxxxxxxxxxxygecvw Unlock *implementation.UnlockHtlcMethod &{MethodName:Unlock}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae BurnZnn *implementation.BurnZnnMethod &{MethodName:BurnZnn}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae CancelLiquidityStake *implementation.CancelLiquidityStakeMethod &{MethodName:CancelLiquidityStake}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae ChangeAdministrator *implementation.ChangeAdministratorLiquidity &{MethodName:ChangeAdministrator}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae CollectReward *implementation.CollectRewardMethod &{MethodName:CollectReward Plasma:94500}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae Donate *implementation.DonateMethod &{MethodName:Donate}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae Emergency *implementation.EmergencyLiquidity &{MethodName:Emergency}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae Fund *implementation.FundMethod &{MethodName:Fund}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae LiquidityStake *implementation.LiquidityStakeMethod &{MethodName:LiquidityStake}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae NominateGuardians *implementation.NominateGuardiansLiquidity &{MethodName:NominateGuardians}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae ProposeAdministrator *implementation.ProposeAdministratorLiquidity &{MethodName:ProposeAdministrator}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae SetAdditionalReward *implementation.SetAdditionalReward &{MethodName:SetAdditionalReward}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae SetIsHalted *implementation.SetIsHalted &{MethodName:SetIsHalted}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae SetTokenTuple *implementation.SetTokenTupleMethod &{MethodName:SetTokenTuple}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae UnlockLiquidityStakeEntries *implementation.UnlockLiquidityStakeEntries &{MethodName:UnlockLiquidityStakeEntries}
z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae Update *implementation.UpdateRewardEmbeddedLiquidityMethod &{MethodName:Update}
z1qxemdeddedxplasmaxxxxxxxxxxxxxxxxsctrp CancelFuse *implementation.CancelFuseMethod &{MethodName:CancelFuse}
z1qxemdeddedxplasmaxxxxxxxxxxxxxxxxsctrp Fuse *implementation.FuseMethod &{MethodName:Fuse}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg CollectReward *implementation.CollectRewardMethod &{MethodName:CollectReward Plasma:52500}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg Delegate *implementation.DelegateMethod &{MethodName:Delegate}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg DepositQsr *implementation.DepositQsrMethod &{MethodName:DepositQsr}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg Register *implementation.RegisterMethod &{MethodName:Register}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg RegisterLegacy *implementation.LegacyRegisterMethod &{MethodName:RegisterLegacy}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg Revoke *implementation.RevokeMethod &{MethodName:Revoke}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg Undelegate *implementation.UndelegateMethod &{MethodName:Undelegate}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg Update *implementation.UpdateEmbeddedPillarMethod &{MethodName:Update}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg UpdatePillar *implementation.UpdatePillarMethod &{MethodName:UpdatePillar}
z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg WithdrawQsr *implementation.WithdrawQsrMethod &{MethodName:WithdrawQsr}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r CollectReward *implementation.CollectRewardMethod &{MethodName:CollectReward Plasma:52500}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r DepositQsr *implementation.DepositQsrMethod &{MethodName:DepositQsr}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r Register *implementation.RegisterSentinelMethod &{MethodName:Register}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r Revoke *implementation.RevokeSentinelMethod &{MethodName:Revoke}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r Update *implementation.UpdateEmbeddedSentinelMethod &{MethodName:Update}
z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r WithdrawQsr *implementation.WithdrawQsrMethod &{MethodName:WithdrawQsr}
z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48 ActivateSpork *implementation.ActivateSporkMethod &{MethodName:ActivateSpork}
z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48 CreateSpork *implementation.CreateSporkMethod &{MethodName:CreateSpork}
z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 Cancel *implementation.CancelStakeMethod &{MethodName:Cancel}
z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 CollectReward *implementation.CollectRewardMethod &{MethodName:CollectReward Plasma:52500}
z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 Stake *implementation.StakeMethod &{MethodName:Stake}
z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 Update *implementation.UpdateEmbeddedStakeMethod &{MethodName:Update}
z1qxemdeddedxswapxxxxxxxxxxxxxxxxxxl4yww RetrieveAssets *implementation.SwapRetrieveAssetsMethod &{MethodName:RetrieveAssets}
z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 Burn *implementation.BurnMethod &{MethodName:Burn}
z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 IssueToken *implementation.IssueMethod &{MethodName:IssueToken}
z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 Mint *implementation.MintMethod &{MethodName:Mint}
z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 UpdateToken *implementation.UpdateTokenMethod &{MethodName:UpdateToken}
`,
}
