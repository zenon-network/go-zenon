// Package embedded routes account blocks sent to the embedded
// contracts — the system contracts compiled into the node at the
// fixed addresses defined in common/types — to executable Go code.
//
// Each contract pairs the ABI parsed in vm/embedded/definition with
// a map from method name to the Method (from
// vm/embedded/implementation) that executes it. The VM resolves a
// send block through GetEmbeddedMethod: the first four bytes of the
// block's Data select the ABI method by selector, and the method's
// name selects the implementation.
//
// The available method set grows with the enforced sporks. Starting
// from the genesis contracts (getOrigin), each spork that changes the
// surface — accelerator, bridge and liquidity, HTLC, Dynamic Plasma,
// PTLC — adds or replaces methods, and the registry for each
// combination is built lazily, once, on first use. GetEmbeddedMethod
// looks up the registry keyed by the sporks enforced at the context's
// momentum, so the same call is resolved against whatever method set
// is active at that height.
package embedded

import (
	"sync"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/abi"
	"github.com/zenon-network/go-zenon/vm/constants"
	cabi "github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/embedded/implementation"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

// Method defines interfaces of embedded contracts
type Method interface {
	// GetPlasma returns the required plasma to call this Method.
	// This cost includes the upfront cost for the embedded receive-block.
	GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error)

	// ValidateSendBlock is called as a static check on send-blocks.
	// All send blocks need to pass this verification before being added in the chain.
	ValidateSendBlock(block *nom.AccountBlock) error

	// ReceiveBlock is called to generate the descendant blocks and to apply the sendBlock
	// The actual receive-block is generated in the VM.
	// If an error occurred (returned err) the context is rollback and the tokens are refunded.
	ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error)
}

type embeddedImplementation struct {
	m   map[string]Method
	abi abi.ABIContract
}

func applyDynamicPlasmaDiffs(contracts map[types.Address]*embeddedImplementation) {
	contracts[types.PlasmaContract] = &embeddedImplementation{
		map[string]Method{
			cabi.FuseMethodName:         &implementation.FuseMethod{MethodName: cabi.FuseMethodName},
			cabi.CancelFuseMethodName:   &implementation.CancelFuseMethod{MethodName: cabi.CancelFuseMethodName},
			cabi.SetVariablesMethodName: &implementation.SetVariablesMethod{MethodName: cabi.SetVariablesMethodName},
		},
		cabi.ABIPlasma,
	}
}

func applyPtlcDiffs(contracts map[types.Address]*embeddedImplementation) {
	contracts[types.PtlcContract] = &embeddedImplementation{
		map[string]Method{
			cabi.CreatePtlcMethodName:      &implementation.CreatePtlcMethod{MethodName: cabi.CreatePtlcMethodName},
			cabi.ReclaimPtlcMethodName:     &implementation.ReclaimPtlcMethod{MethodName: cabi.ReclaimPtlcMethodName},
			cabi.UnlockPtlcMethodName:      &implementation.UnlockPtlcMethod{MethodName: cabi.UnlockPtlcMethodName},
			cabi.ProxyUnlockPtlcMethodName: &implementation.ProxyUnlockPtlcMethod{MethodName: cabi.ProxyUnlockPtlcMethodName},
		},
		cabi.ABIPtlc,
	}
}

func applyHtlcDiffs(contracts map[types.Address]*embeddedImplementation) {
	contracts[types.HtlcContract] = &embeddedImplementation{
		map[string]Method{
			cabi.CreateHtlcMethodName:           &implementation.CreateHtlcMethod{MethodName: cabi.CreateHtlcMethodName},
			cabi.ReclaimHtlcMethodName:          &implementation.ReclaimHtlcMethod{MethodName: cabi.ReclaimHtlcMethodName},
			cabi.UnlockHtlcMethodName:           &implementation.UnlockHtlcMethod{MethodName: cabi.UnlockHtlcMethodName},
			cabi.DenyHtlcProxyUnlockMethodName:  &implementation.DenyHtlcProxyUnlockMethod{MethodName: cabi.DenyHtlcProxyUnlockMethodName},
			cabi.AllowHtlcProxyUnlockMethodName: &implementation.AllowHtlcProxyUnlockMethod{MethodName: cabi.AllowHtlcProxyUnlockMethodName},
		},
		cabi.ABIHtlc,
	}
}

func applyBridgeAndLiquidityDiffs(contracts map[types.Address]*embeddedImplementation) {
	contracts[types.BridgeContract] = &embeddedImplementation{
		map[string]Method{
			cabi.WrapTokenMethodName:            &implementation.WrapTokenMethod{MethodName: cabi.WrapTokenMethodName},
			cabi.UpdateWrapRequestMethodName:    &implementation.UpdateWrapRequestMethod{MethodName: cabi.UpdateWrapRequestMethodName},
			cabi.RedeemUnwrapMethodName:         &implementation.RedeemMethod{MethodName: cabi.RedeemUnwrapMethodName},
			cabi.UnwrapTokenMethodName:          &implementation.UnwrapTokenMethod{MethodName: cabi.UnwrapTokenMethodName},
			cabi.RevokeUnwrapRequestMethodName:  &implementation.RevokeUnwrapRequestMethod{MethodName: cabi.RevokeUnwrapRequestMethodName},
			cabi.SetNetworkMethodName:           &implementation.SetNetworkMethod{MethodName: cabi.SetNetworkMethodName},
			cabi.RemoveNetworkMethodName:        &implementation.RemoveNetworkMethod{MethodName: cabi.RemoveNetworkMethodName},
			cabi.SetTokenPairMethod:             &implementation.SetTokenPairMethod{MethodName: cabi.SetTokenPairMethod},
			cabi.RemoveTokenPairMethodName:      &implementation.RemoveTokenPairMethod{MethodName: cabi.RemoveTokenPairMethodName},
			cabi.HaltMethodName:                 &implementation.HaltMethod{MethodName: cabi.HaltMethodName},
			cabi.NominateGuardiansMethodName:    &implementation.NominateGuardiansMethod{MethodName: cabi.NominateGuardiansMethodName},
			cabi.UnhaltMethodName:               &implementation.UnhaltMethod{MethodName: cabi.UnhaltMethodName},
			cabi.ProposeAdministratorMethodName: &implementation.ProposeAdministratorMethod{MethodName: cabi.ProposeAdministratorMethodName},
			cabi.EmergencyMethodName:            &implementation.EmergencyMethod{MethodName: cabi.EmergencyMethodName},
			cabi.ChangeTssECDSAPubKeyMethodName: &implementation.ChangeTssECDSAPubKeyMethod{MethodName: cabi.ChangeTssECDSAPubKeyMethodName},
			cabi.ChangeAdministratorMethodName:  &implementation.ChangeAdministratorMethod{MethodName: cabi.ChangeAdministratorMethodName},
			cabi.SetAllowKeygenMethodName:       &implementation.SetAllowKeygenMethod{MethodName: cabi.SetAllowKeygenMethodName},
			cabi.SetOrchestratorInfoMethodName:  &implementation.SetOrchestratorInfoMethod{MethodName: cabi.SetOrchestratorInfoMethodName},
			cabi.SetBridgeMetadataMethodName:    &implementation.SetBridgeMetadataMethod{MethodName: cabi.SetBridgeMetadataMethodName},
			cabi.SetNetworkMetadataMethodName:   &implementation.SetNetworkMetadataMethod{MethodName: cabi.SetNetworkMetadataMethodName},
		},
		cabi.ABIBridge,
	}

	contracts[types.LiquidityContract].m[cabi.SetTokenTupleMethodName] = &implementation.SetTokenTupleMethod{MethodName: cabi.SetTokenTupleMethodName}
	contracts[types.LiquidityContract].m[cabi.LiquidityStakeMethodName] = &implementation.LiquidityStakeMethod{MethodName: cabi.LiquidityStakeMethodName}
	contracts[types.LiquidityContract].m[cabi.CancelLiquidityStakeMethodName] = &implementation.CancelLiquidityStakeMethod{MethodName: cabi.CancelLiquidityStakeMethodName}
	contracts[types.LiquidityContract].m[cabi.UnlockLiquidityStakeEntriesMethodName] = &implementation.UnlockLiquidityStakeEntries{MethodName: cabi.UnlockLiquidityStakeEntriesMethodName}
	contracts[types.LiquidityContract].m[cabi.UpdateMethodName] = &implementation.UpdateRewardEmbeddedLiquidityMethod{MethodName: cabi.UpdateMethodName}
	contracts[types.LiquidityContract].m[cabi.CollectRewardMethodName] = &implementation.CollectRewardMethod{MethodName: cabi.CollectRewardMethodName, Plasma: constants.AlphanetPlasmaTable.EmbeddedWDoubleWithdraw}
	contracts[types.LiquidityContract].m[cabi.SetIsHaltedMethodName] = &implementation.SetIsHalted{MethodName: cabi.SetIsHaltedMethodName}
	contracts[types.LiquidityContract].m[cabi.SetAdditionalRewardMethodName] = &implementation.SetAdditionalReward{MethodName: cabi.SetAdditionalRewardMethodName}
	contracts[types.LiquidityContract].m[cabi.ChangeAdministratorMethodName] = &implementation.ChangeAdministratorLiquidity{MethodName: cabi.ChangeAdministratorMethodName}
	contracts[types.LiquidityContract].m[cabi.ProposeAdministratorMethodName] = &implementation.ProposeAdministratorLiquidity{MethodName: cabi.ProposeAdministratorMethodName}
	contracts[types.LiquidityContract].m[cabi.NominateGuardiansMethodName] = &implementation.NominateGuardiansLiquidity{MethodName: cabi.NominateGuardiansMethodName}
	contracts[types.LiquidityContract].m[cabi.EmergencyMethodName] = &implementation.EmergencyLiquidity{MethodName: cabi.EmergencyMethodName}
}

func applyAcceleratorDiffs(contracts map[types.Address]*embeddedImplementation) {
	contracts[types.AcceleratorContract] = &embeddedImplementation{
		map[string]Method{
			cabi.DonateMethodName:        &implementation.DonateMethod{MethodName: cabi.DonateMethodName},
			cabi.CreateProjectMethodName: &implementation.CreateProjectMethod{MethodName: cabi.CreateProjectMethodName},
			cabi.AddPhaseMethodName:      &implementation.AddPhaseMethod{MethodName: cabi.AddPhaseMethodName},
			cabi.UpdateMethodName:        &implementation.UpdateEmbeddedAcceleratorMethod{MethodName: cabi.UpdateMethodName},
			cabi.UpdatePhaseMethodName:   &implementation.UpdatePhaseMethod{MethodName: cabi.UpdatePhaseMethodName},
			// common
			cabi.VoteByNameMethodName:        &implementation.VoteByNameMethod{MethodName: cabi.VoteByNameMethodName},
			cabi.VoteByProdAddressMethodName: &implementation.VoteByProdAddressMethod{MethodName: cabi.VoteByProdAddressMethodName},
		},
		cabi.ABIAccelerator,
	}
	contracts[types.PillarContract].m[cabi.CollectRewardMethodName] = &implementation.CollectRewardMethod{MethodName: cabi.CollectRewardMethodName, Plasma: constants.AlphanetPlasmaTable.EmbeddedSimple}
	contracts[types.SentinelContract].m[cabi.CollectRewardMethodName] = &implementation.CollectRewardMethod{MethodName: cabi.CollectRewardMethodName, Plasma: constants.AlphanetPlasmaTable.EmbeddedSimple}
	contracts[types.StakeContract].m[cabi.CollectRewardMethodName] = &implementation.CollectRewardMethod{MethodName: cabi.CollectRewardMethodName, Plasma: constants.AlphanetPlasmaTable.EmbeddedSimple}
	contracts[types.LiquidityContract].m[cabi.FundMethodName] = &implementation.FundMethod{MethodName: cabi.FundMethodName}
	contracts[types.LiquidityContract].m[cabi.BurnZnnMethodName] = &implementation.BurnZnnMethod{MethodName: cabi.BurnZnnMethodName}
}

func getOrigin() map[types.Address]*embeddedImplementation {
	return map[types.Address]*embeddedImplementation{
		types.PlasmaContract: {
			map[string]Method{
				cabi.FuseMethodName:       &implementation.FuseMethod{MethodName: cabi.FuseMethodName},
				cabi.CancelFuseMethodName: &implementation.CancelFuseMethod{MethodName: cabi.CancelFuseMethodName},
			},
			cabi.ABIPlasma,
		},
		types.PillarContract: {
			map[string]Method{
				cabi.RegisterMethodName:       &implementation.RegisterMethod{MethodName: cabi.RegisterMethodName},
				cabi.LegacyRegisterMethodName: &implementation.LegacyRegisterMethod{MethodName: cabi.LegacyRegisterMethodName},
				cabi.RevokeMethodName:         &implementation.RevokeMethod{MethodName: cabi.RevokeMethodName},
				cabi.UpdatePillarMethodName:   &implementation.UpdatePillarMethod{MethodName: cabi.UpdatePillarMethodName},
				cabi.DelegateMethodName:       &implementation.DelegateMethod{MethodName: cabi.DelegateMethodName},
				cabi.UndelegateMethodName:     &implementation.UndelegateMethod{MethodName: cabi.UndelegateMethodName},
				cabi.UpdateMethodName:         &implementation.UpdateEmbeddedPillarMethod{MethodName: cabi.UpdateMethodName},
				// common
				cabi.DepositQsrMethodName:    &implementation.DepositQsrMethod{MethodName: cabi.DepositQsrMethodName},
				cabi.WithdrawQsrMethodName:   &implementation.WithdrawQsrMethod{MethodName: cabi.WithdrawQsrMethodName},
				cabi.CollectRewardMethodName: &implementation.CollectRewardMethod{MethodName: cabi.CollectRewardMethodName, Plasma: constants.AlphanetPlasmaTable.EmbeddedSimple + constants.AlphanetPlasmaTable.EmbeddedWWithdraw},
			},
			cabi.ABIPillars,
		},
		types.TokenContract: {
			map[string]Method{
				cabi.IssueMethodName:       &implementation.IssueMethod{MethodName: cabi.IssueMethodName},
				cabi.MintMethodName:        &implementation.MintMethod{MethodName: cabi.MintMethodName},
				cabi.BurnMethodName:        &implementation.BurnMethod{MethodName: cabi.BurnMethodName},
				cabi.UpdateTokenMethodName: &implementation.UpdateTokenMethod{MethodName: cabi.UpdateTokenMethodName},
			},
			cabi.ABIToken,
		},
		types.SentinelContract: {
			map[string]Method{
				cabi.RegisterSentinelMethodName: &implementation.RegisterSentinelMethod{MethodName: cabi.RegisterSentinelMethodName},
				cabi.RevokeSentinelMethodName:   &implementation.RevokeSentinelMethod{MethodName: cabi.RevokeSentinelMethodName},
				cabi.UpdateMethodName:           &implementation.UpdateEmbeddedSentinelMethod{MethodName: cabi.UpdateMethodName},
				// common
				cabi.DepositQsrMethodName:    &implementation.DepositQsrMethod{MethodName: cabi.DepositQsrMethodName},
				cabi.WithdrawQsrMethodName:   &implementation.WithdrawQsrMethod{MethodName: cabi.WithdrawQsrMethodName},
				cabi.CollectRewardMethodName: &implementation.CollectRewardMethod{MethodName: cabi.CollectRewardMethodName, Plasma: constants.AlphanetPlasmaTable.EmbeddedSimple + constants.AlphanetPlasmaTable.EmbeddedWWithdraw},
			},
			cabi.ABISentinel,
		},
		types.SwapContract: {
			map[string]Method{
				cabi.RetrieveAssetsMethodName: &implementation.SwapRetrieveAssetsMethod{MethodName: cabi.RetrieveAssetsMethodName},
			},
			cabi.ABISwap,
		},
		types.StakeContract: {
			map[string]Method{
				cabi.StakeMethodName:       &implementation.StakeMethod{MethodName: cabi.StakeMethodName},
				cabi.CancelStakeMethodName: &implementation.CancelStakeMethod{MethodName: cabi.CancelStakeMethodName},
				cabi.UpdateMethodName:      &implementation.UpdateEmbeddedStakeMethod{MethodName: cabi.UpdateMethodName},
				// common
				cabi.CollectRewardMethodName: &implementation.CollectRewardMethod{MethodName: cabi.CollectRewardMethodName, Plasma: constants.AlphanetPlasmaTable.EmbeddedSimple + constants.AlphanetPlasmaTable.EmbeddedWWithdraw},
			},
			cabi.ABIStake,
		},
		types.SporkContract: {
			m: map[string]Method{
				cabi.SporkCreateMethodName:   &implementation.CreateSporkMethod{MethodName: cabi.SporkCreateMethodName},
				cabi.SporkActivateMethodName: &implementation.ActivateSporkMethod{MethodName: cabi.SporkActivateMethodName},
			},
			abi: cabi.ABISpork,
		},
		types.LiquidityContract: {
			m: map[string]Method{
				cabi.UpdateMethodName: &implementation.UpdateEmbeddedLiquidityMethod{MethodName: cabi.UpdateMethodName},
				cabi.DonateMethodName: &implementation.DonateMethod{MethodName: cabi.DonateMethodName},
			},
			abi: cabi.ABILiquidity,
		},
		types.AcceleratorContract: {
			m: map[string]Method{
				cabi.DonateMethodName: &implementation.DonateMethod{MethodName: cabi.DonateMethodName},
			},
			abi: cabi.ABIAccelerator,
		},
	}
}

const (
	// One bit per spork GetEmbeddedMethod consults, followed by
	// embeddedVariantCombinations, which iota makes 1 << (number of bits
	// above) — the size of the precomputed variant table. Add a new spork
	// bit anywhere above that last line and the table grows with it.
	acceleratorSporkBit = 1 << iota
	bridgeAndLiquiditySporkBit
	htlcSporkBit
	dynamicPlasmaSporkBit
	ptlcSporkBit

	embeddedVariantCombinations
)

var (
	embeddedVariantsOnce sync.Once
	embeddedVariants     [embeddedVariantCombinations]map[types.Address]*embeddedImplementation
)

// buildEmbeddedVariants precomputes the merged contract map for every
// combination of the 5 sporks GetEmbeddedMethod consults, so a call
// only ever does an array lookup rather than rebuilding the map (10
// contracts, ~60 method structs) from scratch every time. Runs once,
// lazily, guarded by embeddedVariantsOnce.
func buildEmbeddedVariants() {
	for key := 0; key < embeddedVariantCombinations; key++ {
		contractsMap := getOrigin()
		if key&acceleratorSporkBit != 0 {
			applyAcceleratorDiffs(contractsMap)
		}
		if key&bridgeAndLiquiditySporkBit != 0 {
			applyBridgeAndLiquidityDiffs(contractsMap)
		}
		if key&htlcSporkBit != 0 {
			applyHtlcDiffs(contractsMap)
		}
		if key&dynamicPlasmaSporkBit != 0 {
			applyDynamicPlasmaDiffs(contractsMap)
		}
		if key&ptlcSporkBit != 0 {
			applyPtlcDiffs(contractsMap)
		}
		embeddedVariants[key] = contractsMap
	}
}

// GetEmbeddedMethod finds method instance of embedded contract by address and abiSelector
// - returns constants.ErrNotContractAddress in case address is not an embedded address (bad prefix)
// - returns constants.ErrContractDoesntExist in case the address doesn't link to a valid embedded contract
// - returns constants.ErrContractMethodNotFound if the method doesn't exist
func GetEmbeddedMethod(context vm_context.AccountVmContext, address types.Address, abiSelector []byte) (Method, error) {
	if !types.IsEmbeddedAddress(address) {
		return nil, constants.ErrNotContractAddress
	}

	embeddedVariantsOnce.Do(buildEmbeddedVariants)

	key := 0
	if context.IsAcceleratorSporkEnforced() {
		key |= acceleratorSporkBit
	}
	if context.IsBridgeAndLiquiditySporkEnforced() {
		key |= bridgeAndLiquiditySporkBit
	}
	if context.IsHtlcSporkEnforced() {
		key |= htlcSporkBit
	}
	if context.IsDynamicPlasmaSporkEnforced() {
		key |= dynamicPlasmaSporkBit
	}
	if context.IsPtlcSporkEnforced() {
		key |= ptlcSporkBit
	}
	// No change for NoPillarRegSpork
	contractsMap := embeddedVariants[key]

	// contract address must exist in map
	if p, found := contractsMap[address]; found {
		// contract must implement the method
		if method, err := p.abi.MethodById(abiSelector); err == nil {
			// method must exist in the map
			c, ok := p.m[method.Name]
			if ok {
				return c, nil
			}
		}
		return nil, constants.ErrContractMethodNotFound
	} else {
		return nil, constants.ErrContractDoesntExist
	}
}

// CheckNoTrailingBytes verifies that data contains exactly the ABI-encoded
// arguments for the embedded method it selects, with no trailing bytes past
// the canonical encoding. It resolves the abi.Method from the same spork-aware
// contract map GetEmbeddedMethod uses, decodes the arguments, re-packs them
// canonically, and compares the re-packed length (plus the 4-byte selector)
// with len(data).
//
// A nil error means either the data is clean or the lookup failed in a way
// that ValidateSendBlock will catch and report; this function only adds the
// trailing-bytes rejection on top of the normal validation path.
func CheckNoTrailingBytes(context vm_context.AccountVmContext, address types.Address, data []byte) error {
	if !types.IsEmbeddedAddress(address) {
		return nil
	}
	if len(data) <= 4 {
		return nil
	}

	embeddedVariantsOnce.Do(buildEmbeddedVariants)

	key := 0
	if context.IsAcceleratorSporkEnforced() {
		key |= acceleratorSporkBit
	}
	if context.IsBridgeAndLiquiditySporkEnforced() {
		key |= bridgeAndLiquiditySporkBit
	}
	if context.IsHtlcSporkEnforced() {
		key |= htlcSporkBit
	}
	if context.IsDynamicPlasmaSporkEnforced() {
		key |= dynamicPlasmaSporkBit
	}
	if context.IsPtlcSporkEnforced() {
		key |= ptlcSporkBit
	}
	contractsMap := embeddedVariants[key]

	p, found := contractsMap[address]
	if !found {
		return nil
	}

	method, err := p.abi.MethodById(data)
	if err != nil {
		return nil
	}

	values, err := method.Inputs.UnpackValues(data[4:])
	if err != nil {
		return nil
	}

	packed, err := method.Inputs.Pack(values...)
	if err != nil {
		return nil
	}

	if len(packed)+4 != len(data) {
		return constants.ErrABDataTrailingBytes
	}

	return nil
}
