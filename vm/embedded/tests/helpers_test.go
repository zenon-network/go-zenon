package tests

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// saveSporkState saves and restores all mutable spork globals.
// Call at the start of any test or helper that mutates spork state.
func saveSporkState(t *testing.T) {
	savedDP := types.DynamicPlasmaSpork.SporkId
	savedAccel := types.AcceleratorSpork.SporkId
	savedBridge := types.BridgeAndLiquiditySpork.SporkId
	savedHtlc := types.HtlcSpork.SporkId
	savedMultisig := types.MultisigSpork.SporkId
	savedMap := make(map[types.Hash]bool, len(types.ImplementedSporksMap))
	for k, v := range types.ImplementedSporksMap {
		savedMap[k] = v
	}
	t.Cleanup(func() {
		types.DynamicPlasmaSpork.SporkId = savedDP
		types.AcceleratorSpork.SporkId = savedAccel
		types.BridgeAndLiquiditySpork.SporkId = savedBridge
		types.HtlcSpork.SporkId = savedHtlc
		types.MultisigSpork.SporkId = savedMultisig
		types.ImplementedSporksMap = savedMap
	})
}

// savePlasmaDefaults saves and restores mutable plasma default globals.
func savePlasmaDefaults(t *testing.T) {
	savedMax := definition.DefaultMaxBasePlasmaInMomentum
	savedFused := definition.DefaultFusedPlasmaTarget
	savedPow := definition.DefaultPowPlasmaTarget
	t.Cleanup(func() {
		definition.DefaultMaxBasePlasmaInMomentum = savedMax
		definition.DefaultFusedPlasmaTarget = savedFused
		definition.DefaultPowPlasmaTarget = savedPow
	})
}

// saveBridgeConstants saves and restores mutable bridge/liquidity constants.
// Call at the start of any test or helper that mutates these.
func saveBridgeConstants(t *testing.T) {
	savedAdmin := constants.InitialBridgeAdministrator
	savedMinGuardians := constants.MinGuardians
	savedAdminDelay := constants.MinAdministratorDelay
	savedSoftDelay := constants.MinSoftDelay
	savedUnhalt := constants.MinUnhaltDurationInMomentums
	savedEpoch := constants.MomentumsPerEpoch
	t.Cleanup(func() {
		constants.InitialBridgeAdministrator = savedAdmin
		constants.MinGuardians = savedMinGuardians
		constants.MinAdministratorDelay = savedAdminDelay
		constants.MinSoftDelay = savedSoftDelay
		constants.MinUnhaltDurationInMomentums = savedUnhalt
		constants.MomentumsPerEpoch = savedEpoch
	})
}

// saveGovernanceAddress saves and restores types.GovernanceAddress.
func saveGovernanceAddress(t *testing.T) {
	saved := types.GovernanceAddress
	t.Cleanup(func() {
		types.GovernanceAddress = saved
	})
}

// saveChainGlobals saves and restores mutable chain globals.
func saveChainGlobals(t *testing.T) {
	savedMaxUncommitted := chain.MaxUncommittedBlocksPerAccount
	savedMaxInMomentum := chain.MaxAccountBlocksInMomentum
	t.Cleanup(func() {
		chain.MaxUncommittedBlocksPerAccount = savedMaxUncommitted
		chain.MaxAccountBlocksInMomentum = savedMaxInMomentum
	})
}

// saveCommunitySporkGlobals saves and restores community spork address and
// the validity height window.
func saveCommunitySporkGlobals(t *testing.T) {
	savedAddr := types.CommunitySporkAddress
	savedStart := definition.CommunitySporkAddressStartHeight
	savedEnd := definition.CommunitySporkAddressEndHeight
	t.Cleanup(func() {
		types.CommunitySporkAddress = savedAddr
		definition.CommunitySporkAddressStartHeight = savedStart
		definition.CommunitySporkAddressEndHeight = savedEnd
	})
}
