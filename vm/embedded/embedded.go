package embedded

import (
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

var (
	originEmbedded             = getOrigin()
	acceleratorEmbedded        = getAccelerator()
	htlcEmbedded               = getHtlc()
	bridgeAndLiquidityEmbedded = getBridgeAndLiquidity()
)

func getHtlc() map[types.Address]*embeddedImplementation {
	contracts := getBridgeAndLiquidity()
	contracts[types.HtlcContract] = &embeddedImplementation{
		map[string]Method{
			cabi.CreateHtlcMethodName:           &implementation.CreateHtlcMethod{cabi.CreateHtlcMethodName},
			cabi.ReclaimHtlcMethodName:          &implementation.ReclaimHtlcMethod{cabi.ReclaimHtlcMethodName},
			cabi.UnlockHtlcMethodName:           &implementation.UnlockHtlcMethod{cabi.UnlockHtlcMethodName},
			cabi.DenyHtlcProxyUnlockMethodName:  &implementation.DenyHtlcProxyUnlockMethod{cabi.DenyHtlcProxyUnlockMethodName},
			cabi.AllowHtlcProxyUnlockMethodName: &implementation.AllowHtlcProxyUnlockMethod{cabi.AllowHtlcProxyUnlockMethodName},
		},
		cabi.ABIHtlc,
	}
	return contracts
}

func getBridgeAndLiquidity() map[types.Address]*embeddedImplementation {
	contracts := getAccelerator()
	contracts[types.BridgeContract] = &embeddedImplementation{
		map[string]Method{
			cabi.WrapTokenMethodName:            &implementation.WrapTokenMethod{cabi.WrapTokenMethodName},
			cabi.UpdateWrapRequestMethodName:    &implementation.UpdateWrapRequestMethod{cabi.UpdateWrapRequestMethodName},
			cabi.RedeemUnwrapMethodName:         &implementation.RedeemMethod{cabi.RedeemUnwrapMethodName},
			cabi.UnwrapTokenMethodName:          &implementation.UnwrapTokenMethod{cabi.UnwrapTokenMethodName},
			cabi.RevokeUnwrapRequestMethodName:  &implementation.RevokeUnwrapRequestMethod{cabi.RevokeUnwrapRequestMethodName},
			cabi.SetNetworkMethodName:           &implementation.SetNetworkMethod{cabi.SetNetworkMethodName},
			cabi.RemoveNetworkMethodName:        &implementation.RemoveNetworkMethod{cabi.RemoveNetworkMethodName},
			cabi.SetTokenPairMethod:             &implementation.SetTokenPairMethod{cabi.SetTokenPairMethod},
			cabi.RemoveTokenPairMethodName:      &implementation.RemoveTokenPairMethod{cabi.RemoveTokenPairMethodName},
			cabi.HaltMethodName:                 &implementation.HaltMethod{cabi.HaltMethodName},
			cabi.NominateGuardiansMethodName:    &implementation.NominateGuardiansMethod{cabi.NominateGuardiansMethodName},
			cabi.UnhaltMethodName:               &implementation.UnhaltMethod{cabi.UnhaltMethodName},
			cabi.ProposeAdministratorMethodName: &implementation.ProposeAdministratorMethod{cabi.ProposeAdministratorMethodName},
			cabi.EmergencyMethodName:            &implementation.EmergencyMethod{cabi.EmergencyMethodName},
			cabi.ChangeTssECDSAPubKeyMethodName: &implementation.ChangeTssECDSAPubKeyMethod{cabi.ChangeTssECDSAPubKeyMethodName},
			cabi.ChangeAdministratorMethodName:  &implementation.ChangeAdministratorMethod{cabi.ChangeAdministratorMethodName},
			cabi.SetAllowKeygenMethodName:       &implementation.SetAllowKeygenMethod{cabi.SetAllowKeygenMethodName},
			cabi.SetOrchestratorInfoMethodName:  &implementation.SetOrchestratorInfoMethod{cabi.SetOrchestratorInfoMethodName},
			cabi.SetBridgeMetadataMethodName:    &implementation.SetBridgeMetadataMethod{cabi.SetBridgeMetadataMethodName},
			cabi.SetNetworkMetadataMethodName:   &implementation.SetNetworkMetadataMethod{cabi.SetNetworkMetadataMethodName},
		},
		cabi.ABIBridge,
	}

	contracts[types.LiquidityContract].m[cabi.SetTokenTupleMethodName] = &implementation.SetTokenTupleMethod{cabi.SetTokenTupleMethodName}
	contracts[types.LiquidityContract].m[cabi.LiquidityStakeMethodName] = &implementation.LiquidityStakeMethod{cabi.LiquidityStakeMethodName}
	contracts[types.LiquidityContract].m[cabi.CancelLiquidityStakeMethodName] = &implementation.CancelLiquidityStakeMethod{cabi.CancelLiquidityStakeMethodName}
	contracts[types.LiquidityContract].m[cabi.UnlockLiquidityStakeEntriesMethodName] = &implementation.UnlockLiquidityStakeEntries{cabi.UnlockLiquidityStakeEntriesMethodName}
	contracts[types.LiquidityContract].m[cabi.UpdateMethodName] = &implementation.UpdateRewardEmbeddedLiquidityMethod{cabi.UpdateMethodName}
	contracts[types.LiquidityContract].m[cabi.CollectRewardMethodName] = &implementation.CollectRewardMethod{cabi.CollectRewardMethodName, constants.AlphanetPlasmaTable.EmbeddedWDoubleWithdraw}
	contracts[types.LiquidityContract].m[cabi.SetIsHaltedMethodName] = &implementation.SetIsHalted{cabi.SetIsHaltedMethodName}
	contracts[types.LiquidityContract].m[cabi.SetAdditionalRewardMethodName] = &implementation.SetAdditionalReward{cabi.SetAdditionalRewardMethodName}
	contracts[types.LiquidityContract].m[cabi.ChangeAdministratorMethodName] = &implementation.ChangeAdministratorLiquidity{cabi.ChangeAdministratorMethodName}
	contracts[types.LiquidityContract].m[cabi.ProposeAdministratorMethodName] = &implementation.ProposeAdministratorLiquidity{cabi.ProposeAdministratorMethodName}
	contracts[types.LiquidityContract].m[cabi.NominateGuardiansMethodName] = &implementation.NominateGuardiansLiquidity{cabi.NominateGuardiansMethodName}
	contracts[types.LiquidityContract].m[cabi.EmergencyMethodName] = &implementation.EmergencyLiquidity{cabi.EmergencyMethodName}

	return contracts
}

func getAccelerator() map[types.Address]*embeddedImplementation {
	contracts := getOrigin()
	contracts[types.AcceleratorContract] = &embeddedImplementation{
		map[string]Method{
			cabi.DonateMethodName:        &implementation.DonateMethod{cabi.DonateMethodName},
			cabi.CreateProjectMethodName: &implementation.CreateProjectMethod{cabi.CreateProjectMethodName},
			cabi.AddPhaseMethodName:      &implementation.AddPhaseMethod{cabi.AddPhaseMethodName},
			cabi.UpdateMethodName:        &implementation.UpdateEmbeddedAcceleratorMethod{cabi.UpdateMethodName},
			cabi.UpdatePhaseMethodName:   &implementation.UpdatePhaseMethod{cabi.UpdatePhaseMethodName},
			// common
			cabi.VoteByNameMethodName:        &implementation.VoteByNameMethod{cabi.VoteByNameMethodName},
			cabi.VoteByProdAddressMethodName: &implementation.VoteByProdAddressMethod{cabi.VoteByProdAddressMethodName},
		},
		cabi.ABIAccelerator,
	}
	contracts[types.PillarContract].m[cabi.CollectRewardMethodName] = &implementation.CollectRewardMethod{cabi.CollectRewardMethodName, constants.AlphanetPlasmaTable.EmbeddedSimple}
	contracts[types.SentinelContract].m[cabi.CollectRewardMethodName] = &implementation.CollectRewardMethod{cabi.CollectRewardMethodName, constants.AlphanetPlasmaTable.EmbeddedSimple}
	contracts[types.StakeContract].m[cabi.CollectRewardMethodName] = &implementation.CollectRewardMethod{cabi.CollectRewardMethodName, constants.AlphanetPlasmaTable.EmbeddedSimple}
	contracts[types.LiquidityContract].m[cabi.FundMethodName] = &implementation.FundMethod{cabi.FundMethodName}
	contracts[types.LiquidityContract].m[cabi.BurnZnnMethodName] = &implementation.BurnZnnMethod{cabi.BurnZnnMethodName}

	return contracts
}

func getOrigin() map[types.Address]*embeddedImplementation {
	return map[types.Address]*embeddedImplementation{
		types.PlasmaContract: {
			map[string]Method{
				cabi.FuseMethodName:       &implementation.FuseMethod{cabi.FuseMethodName},
				cabi.CancelFuseMethodName: &implementation.CancelFuseMethod{cabi.CancelFuseMethodName},
			},
			cabi.ABIPlasma,
		},
		types.PillarContract: {
			map[string]Method{
				cabi.RegisterMethodName:       &implementation.RegisterMethod{cabi.RegisterMethodName},
				cabi.LegacyRegisterMethodName: &implementation.LegacyRegisterMethod{cabi.LegacyRegisterMethodName},
				cabi.RevokeMethodName:         &implementation.RevokeMethod{cabi.RevokeMethodName},
				cabi.UpdatePillarMethodName:   &implementation.UpdatePillarMethod{cabi.UpdatePillarMethodName},
				cabi.DelegateMethodName:       &implementation.DelegateMethod{cabi.DelegateMethodName},
				cabi.UndelegateMethodName:     &implementation.UndelegateMethod{cabi.UndelegateMethodName},
				cabi.UpdateMethodName:         &implementation.UpdateEmbeddedPillarMethod{cabi.UpdateMethodName},
				// common
				cabi.DepositQsrMethodName:    &implementation.DepositQsrMethod{cabi.DepositQsrMethodName},
				cabi.WithdrawQsrMethodName:   &implementation.WithdrawQsrMethod{cabi.WithdrawQsrMethodName},
				cabi.CollectRewardMethodName: &implementation.CollectRewardMethod{cabi.CollectRewardMethodName, constants.AlphanetPlasmaTable.EmbeddedSimple + constants.AlphanetPlasmaTable.EmbeddedWWithdraw},
			},
			cabi.ABIPillars,
		},
		types.TokenContract: {
			map[string]Method{
				cabi.IssueMethodName:       &implementation.IssueMethod{cabi.IssueMethodName},
				cabi.MintMethodName:        &implementation.MintMethod{cabi.MintMethodName},
				cabi.BurnMethodName:        &implementation.BurnMethod{cabi.BurnMethodName},
				cabi.UpdateTokenMethodName: &implementation.UpdateTokenMethod{cabi.UpdateTokenMethodName},
			},
			cabi.ABIToken,
		},
		types.SentinelContract: {
			map[string]Method{
				cabi.RegisterSentinelMethodName: &implementation.RegisterSentinelMethod{cabi.RegisterSentinelMethodName},
				cabi.RevokeSentinelMethodName:   &implementation.RevokeSentinelMethod{cabi.RevokeSentinelMethodName},
				cabi.UpdateMethodName:           &implementation.UpdateEmbeddedSentinelMethod{cabi.UpdateMethodName},
				// common
				cabi.DepositQsrMethodName:    &implementation.DepositQsrMethod{cabi.DepositQsrMethodName},
				cabi.WithdrawQsrMethodName:   &implementation.WithdrawQsrMethod{cabi.WithdrawQsrMethodName},
				cabi.CollectRewardMethodName: &implementation.CollectRewardMethod{cabi.CollectRewardMethodName, constants.AlphanetPlasmaTable.EmbeddedSimple + constants.AlphanetPlasmaTable.EmbeddedWWithdraw},
			},
			cabi.ABISentinel,
		},
		types.SwapContract: {
			map[string]Method{
				cabi.RetrieveAssetsMethodName: &implementation.SwapRetrieveAssetsMethod{cabi.RetrieveAssetsMethodName},
			},
			cabi.ABISwap,
		},
		types.StakeContract: {
			map[string]Method{
				cabi.StakeMethodName:       &implementation.StakeMethod{cabi.StakeMethodName},
				cabi.CancelStakeMethodName: &implementation.CancelStakeMethod{cabi.CancelStakeMethodName},
				cabi.UpdateMethodName:      &implementation.UpdateEmbeddedStakeMethod{cabi.UpdateMethodName},
				// common
				cabi.CollectRewardMethodName: &implementation.CollectRewardMethod{cabi.CollectRewardMethodName, constants.AlphanetPlasmaTable.EmbeddedSimple + constants.AlphanetPlasmaTable.EmbeddedWWithdraw},
			},
			cabi.ABIStake,
		},
		types.SporkContract: {
			m: map[string]Method{
				cabi.SporkCreateMethodName:   &implementation.CreateSporkMethod{cabi.SporkCreateMethodName},
				cabi.SporkActivateMethodName: &implementation.ActivateSporkMethod{cabi.SporkActivateMethodName},
			},
			abi: cabi.ABISpork,
		},
		types.LiquidityContract: {
			m: map[string]Method{
				cabi.UpdateMethodName: &implementation.UpdateEmbeddedLiquidityMethod{cabi.UpdateMethodName},
				cabi.DonateMethodName: &implementation.DonateMethod{cabi.DonateMethodName},
			},
			abi: cabi.ABILiquidity,
		},
		types.AcceleratorContract: {
			m: map[string]Method{
				cabi.DonateMethodName: &implementation.DonateMethod{cabi.DonateMethodName},
			},
			abi: cabi.ABIAccelerator,
		},
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

	var contractsMap map[types.Address]*embeddedImplementation

	if context.IsHtlcSporkEnforced() {
		contractsMap = htlcEmbedded
	} else if context.IsBridgeAndLiquiditySporkEnforced() {
		contractsMap = bridgeAndLiquidityEmbedded
	} else if context.IsAcceleratorSporkEnforced() {
		contractsMap = acceleratorEmbedded
	} else {
		contractsMap = originEmbedded
	}

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
