package tests

import (
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// createAndActivateSpork creates the spork with the given name, activates
// it, and returns its record as the spork contract stores it after the
// activation, EnforcementHeight included. The caller binds the id to an
// implemented spork.
func createAndActivateSpork(t *testing.T, z mock.MockZenon, name string) *definition.Spork {
	t.Helper()
	sporkAPI := embedded.NewSporkApi(z)
	find := func() *definition.Spork {
		list, err := sporkAPI.GetAll(0, 10)
		common.FailIfErr(t, err)
		for _, spork := range list.List {
			if spork.Name == name {
				return spork
			}
		}
		t.Fatalf("spork %q not found", name)
		return nil
	}

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data:      definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName, name, "spork ladder history test"),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data:      definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName, find().Id),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	spork := find()
	if !spork.Activated {
		t.Fatalf("spork %q is not activated", name)
	}
	return spork
}

// enforcedSporksAt lists, in a fixed order, the implemented sporks the
// chain cache reports as enforced at the given momentum.
func enforcedSporksAt(t *testing.T, z mock.MockZenon, momentum types.HashHeight) string {
	t.Helper()
	cacheStore := z.Chain().GetCacheStore(momentum)
	if cacheStore == nil {
		t.Fatalf("no cache store at %v", momentum)
	}
	context := vm_context.NewAccountContext(nil, nil, cacheStore, nil)
	enforced := ""
	for _, spork := range []struct {
		name     string
		enforced bool
	}{
		{"accelerator", context.IsAcceleratorSporkEnforced()},
		{"bridgeAndLiquidity", context.IsBridgeAndLiquiditySporkEnforced()},
		{"htlc", context.IsHtlcSporkEnforced()},
		{"dynamicPlasma", context.IsDynamicPlasmaSporkEnforced()},
		{"ptlc", context.IsPtlcSporkEnforced()},
	} {
		if spork.enforced {
			enforced += spork.name + " "
		}
	}
	return enforced
}

func liquidityCollectRewardSend(acknowledged types.HashHeight) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:              g.User1.Address,
		ToAddress:            types.LiquidityContract,
		Data:                 definition.ABILiquidity.PackMethodPanic(definition.CollectRewardMethodName),
		MomentumAcknowledged: acknowledged,
	}
}

func bridgeEmergencySend(acknowledged types.HashHeight) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:              g.User1.Address,
		ToAddress:            types.BridgeContract,
		Data:                 definition.ABIBridge.PackMethodPanic(definition.EmergencyMethodName),
		MomentumAcknowledged: acknowledged,
	}
}

func stakeCollectRewardSend(acknowledged types.HashHeight) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:              g.User1.Address,
		ToAddress:            types.StakeContract,
		Data:                 definition.ABIStake.PackMethodPanic(definition.CollectRewardMethodName),
		MomentumAcknowledged: acknowledged,
	}
}

// The HTLC spork was enforced on mainnet a few momentums before the
// BridgeAndLiquidity spork. A send is checked and priced against the
// sporks enforced at the momentum it acknowledges, so a send acknowledging
// a momentum between the two heights runs with HTLC enforced and
// BridgeAndLiquidity not. The released lookup selects the HTLC table there,
// which holds the Bridge contract, the newer Liquidity methods and the
// Accelerator reward tiers (sporkVariantKey, vm/embedded/embedded.go).
//
// The test builds that history on a chain with real spork state and
// checks, for a Liquidity, a Bridge and a Stake send:
//   - acknowledging a momentum from before HTLC, the Liquidity and Bridge
//     sends are rejected, and so is the recomputation of their base plasma;
//   - acknowledging a momentum in the window they are admitted at the
//     released plasma tier, and they are confirmed and received before the
//     BridgeAndLiquidity height, the receive running with the same sporks;
//   - once the frontier is past the BridgeAndLiquidity height, the base
//     plasma recomputed for the confirmed sends is unchanged, a new send
//     that still acknowledges the window momentum is admitted the same
//     way, and the sends acknowledging a momentum from before HTLC are
//     still rejected. The table at the frontier holds their methods, so
//     the last check fails if the context is read at the frontier.
//
// Dynamic Plasma and PTLC stay inactive. The Accelerator spork is enforced
// first in one case, as on mainnet, and never in the other.
func TestSporkLadder_SendAcknowledgingTheHtlcOnlyWindow(t *testing.T) {
	for _, tc := range []struct {
		name        string
		accelerator bool
		before      string
		window      string
		after       string
	}{
		{
			name:        "accelerator enforced first",
			accelerator: true,
			before:      "accelerator ",
			window:      "accelerator htlc ",
			after:       "accelerator bridgeAndLiquidity htlc ",
		},
		{
			name:   "accelerator never enforced",
			before: "",
			window: "htlc ",
			after:  "bridgeAndLiquidity htlc ",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			z := mock.NewMockZenon(t)
			defer z.StopPanic()
			saveSporkState(t)

			if tc.accelerator {
				accelerator := createAndActivateSpork(t, z, "spork-accelerator")
				types.AcceleratorSpork.SporkId = accelerator.Id
				types.ImplementedSporksMap[accelerator.Id] = true
				z.InsertMomentumsTo(accelerator.EnforcementHeight)
			}
			z.InsertNewMomentum()
			before := frontierMomentum(t, z).Identifier()

			htlc := createAndActivateSpork(t, z, "spork-htlc")
			types.HtlcSpork.SporkId = htlc.Id
			types.ImplementedSporksMap[htlc.Id] = true
			// The window needs three momentums: one to acknowledge, one to
			// confirm the sends and one to confirm their receives.
			for i := 0; i < 3; i++ {
				z.InsertNewMomentum()
			}
			bridge := createAndActivateSpork(t, z, "spork-bridge")
			types.BridgeAndLiquiditySpork.SporkId = bridge.Id
			types.ImplementedSporksMap[bridge.Id] = true
			if bridge.EnforcementHeight < htlc.EnforcementHeight+3 {
				t.Fatalf("enforcement heights too close: htlc %d, bridgeAndLiquidity %d", htlc.EnforcementHeight, bridge.EnforcementHeight)
			}
			if before.Height >= htlc.EnforcementHeight {
				t.Fatalf("the control momentum %d is not before the htlc enforcement height %d", before.Height, htlc.EnforcementHeight)
			}
			if got := enforcedSporksAt(t, z, before); got != tc.before {
				t.Fatalf("sporks enforced at the control momentum: got %q want %q", got, tc.before)
			}

			// Before HTLC the Liquidity contract has no CollectReward and
			// there is no Bridge contract. The send is rejected when it is
			// generated, and the base plasma recomputed for a block with the
			// same acknowledgement fails the same way.
			rejectControls := func() {
				t.Helper()
				for _, control := range []struct {
					name string
					send func(types.HashHeight) *nom.AccountBlock
					err  error
				}{
					{"Liquidity CollectReward", liquidityCollectRewardSend, constants.ErrContractMethodNotFound},
					{"Bridge Emergency", bridgeEmergencySend, constants.ErrContractDoesntExist},
				} {
					z.InsertSendBlock(control.send(before), control.err, mock.SkipVmChanges)
					block := control.send(before)
					block.BlockType = nom.BlockTypeUserSend
					if _, err := vm.CanonicalBasePlasma(z.Chain(), block); err != control.err {
						t.Fatalf("%v acknowledging the control momentum: canonical base plasma error %v, want %v", control.name, err, control.err)
					}
				}
			}
			rejectControls()

			// In the window.
			z.InsertMomentumsTo(htlc.EnforcementHeight)
			window := frontierMomentum(t, z).Identifier()
			if got := enforcedSporksAt(t, z, window); got != tc.window {
				t.Fatalf("sporks enforced at the window momentum: got %q want %q", got, tc.window)
			}
			type priced struct {
				name  string
				block *nom.AccountBlock
				tier  uint64
			}
			// checkSend requires a send that acknowledges the window
			// momentum to be confirmed at the released tier and received
			// by its contract. With inWindow set, the send and its receive
			// must both be confirmed before the BridgeAndLiquidity height,
			// and the receive must have run with the window's sporks.
			checkSend := func(send priced, inWindow bool) {
				t.Helper()
				frontier := z.Chain().GetFrontierMomentumStore()
				if send.block.MomentumAcknowledged != window {
					t.Fatalf("%v: acknowledges %v, want the window momentum %v", send.name, send.block.MomentumAcknowledged, window)
				}
				if send.block.BasePlasma != send.tier {
					t.Fatalf("%v: applied base plasma %d, want the released tier %d", send.name, send.block.BasePlasma, send.tier)
				}
				stored, err := frontier.GetAccountBlockByHash(send.block.Hash)
				common.FailIfErr(t, err)
				if stored == nil {
					t.Fatalf("%v: not confirmed", send.name)
				}
				canonical, err := vm.CanonicalBasePlasma(z.Chain(), stored)
				common.FailIfErr(t, err)
				if canonical != send.tier {
					t.Fatalf("%v: canonical base plasma %d at frontier %d, want the released tier %d", send.name, canonical, frontier.Identifier().Height, send.tier)
				}
				receive, err := frontier.GetBlockWhichReceives(send.block.Hash)
				common.FailIfErr(t, err)
				if receive == nil {
					t.Fatalf("%v: not received by its contract", send.name)
				}
				if !inWindow {
					return
				}
				for _, confirmed := range []*nom.AccountBlock{stored, receive} {
					height, err := frontier.GetBlockConfirmationHeight(confirmed.Hash)
					common.FailIfErr(t, err)
					if height <= window.Height || height >= bridge.EnforcementHeight {
						t.Fatalf("%v: %v confirmed at %d, outside the window (%d, %d)", send.name, confirmed.Header(), height, window.Height, bridge.EnforcementHeight)
					}
				}
				if got := enforcedSporksAt(t, z, receive.MomentumAcknowledged); got != tc.window {
					t.Fatalf("%v: sporks enforced at the momentum its receive acknowledges: got %q want %q", send.name, got, tc.window)
				}
			}
			sends := []priced{
				{"Liquidity CollectReward", z.InsertSendBlock(liquidityCollectRewardSend(window), nil, mock.SkipVmChanges), constants.AlphanetPlasmaTable.EmbeddedWDoubleWithdraw},
				{"Bridge Emergency", z.InsertSendBlock(bridgeEmergencySend(window), nil, mock.SkipVmChanges), constants.AlphanetPlasmaTable.EmbeddedSimple},
				{"Stake CollectReward", z.InsertSendBlock(stakeCollectRewardSend(window), nil, mock.SkipVmChanges), constants.AlphanetPlasmaTable.EmbeddedSimple},
			}
			// One momentum confirms the sends and the next confirms their
			// receives.
			z.InsertNewMomentum()
			z.InsertNewMomentum()
			for _, send := range sends {
				checkSend(send, true)
			}

			// Past the BridgeAndLiquidity height.
			z.InsertMomentumsTo(bridge.EnforcementHeight + 1)
			if got := enforcedSporksAt(t, z, frontierMomentum(t, z).Identifier()); got != tc.after {
				t.Fatalf("sporks enforced at the frontier: got %q want %q", got, tc.after)
			}
			// The confirmed sends are priced as before.
			for _, send := range sends {
				checkSend(send, true)
			}
			// A new send that still acknowledges the window momentum is
			// admitted at the same tier.
			kept := priced{"Liquidity CollectReward, acknowledgement kept", z.InsertSendBlock(liquidityCollectRewardSend(window), nil, mock.SkipVmChanges), constants.AlphanetPlasmaTable.EmbeddedWDoubleWithdraw}
			z.InsertNewMomentum()
			z.InsertNewMomentum()
			checkSend(kept, false)

			// A send acknowledging a momentum from before HTLC is rejected
			// as it was then, whatever the frontier enforces. The table at
			// the frontier holds both methods, so this fails if the
			// context is read there.
			rejectControls()
		})
	}
}
