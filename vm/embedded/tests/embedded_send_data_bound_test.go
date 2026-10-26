package tests

import (
	"encoding/base64"
	"math/big"
	"strings"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// Embedded-contract sends carry ABI-encoded arguments in Data. Before the
// Dynamic Plasma spork the base plasma of such a send is the method tier
// alone, whatever the argument size, and no size bound applies. Once the
// spork is enforced the argument bytes are priced and bounded like the Data
// of a send to a user address (vm.EmbeddedSendBasePlasma), and the methods
// whose arguments have a known maximum useful size reject longer values at
// send time (HTLC Unlock preimage, bridge signature strings).

func unlockSend(from types.Address, preimage []byte) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:   from,
		ToAddress: types.HtlcContract,
		Data:      definition.ABIHtlc.PackMethodPanic(definition.UnlockHtlcMethodName, types.Hash{1}, preimage),
	}
}

// Pre-spork documentation: a 1 MiB Unlock preimage is accepted at the fixed
// method tier and cemented, while a user send one byte over MaxDataLength is
// rejected. This is the behaviour the spork-gated rules below replace.
func TestEmbeddedSendData_BeforeSpork_ArgumentBytesUnpricedAndUnbounded(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activateHtlc(t, z)

	sendBlock := z.InsertSendBlock(unlockSend(g.User1.Address, make([]byte, 1<<20)), nil, mock.SkipVmChanges)
	if sendBlock == nil {
		t.Fatal("oversized Unlock send was not accepted before the spork")
	}
	if got, want := len(sendBlock.Data), 1048676; got != want {
		t.Fatalf("canonical data length: got %d want %d", got, want)
	}
	if tier := constants.AlphanetPlasmaTable.EmbeddedWWithdraw; sendBlock.BasePlasma != tier {
		t.Fatalf("base plasma before the spork: got %d want the fixed tier %d", sendBlock.BasePlasma, tier)
	}

	z.InsertNewMomentum()
	cemented, err := z.Chain().GetFrontierMomentumStore().GetAccountBlockByHash(sendBlock.Hash)
	common.FailIfErr(t, err)
	if cemented == nil || len(cemented.Data) != len(sendBlock.Data) {
		t.Fatalf("send with %d data bytes was not cemented unchanged", len(sendBlock.Data))
	}

	// control: the user-send rule is unchanged by the spork state
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: g.User2.Address,
		Data:      make([]byte, constants.MaxDataLength+1),
	}, verifier.ErrABDataTooBig, mock.SkipVmChanges)

	// A block acknowledging a pre-spork momentum keeps its pre-spork price
	// when the verifier recomputes it after activation: the rule reads the
	// spork state at MomentumAcknowledged, not at the frontier.
	activateDynamicPlasmaPastHeight(t, z)
	got, err := vm.CanonicalBasePlasma(z.Chain(), cemented)
	common.FailIfErr(t, err)
	if got != cemented.BasePlasma {
		t.Fatalf("canonical base plasma of the pre-spork send after activation: got %d want %d", got, cemented.BasePlasma)
	}
}

// After the spork an embedded send whose Data exceeds MaxDataLength is
// rejected with the same error as a user send, and the rejection happens at
// plasma computation, before any method-specific rule.
func TestEmbeddedSendData_AfterSpork_OversizedArgumentsRejected(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activateHtlc(t, z)
	activateDynamicPlasmaPastHeight(t, z)

	z.InsertSendBlock(unlockSend(g.User1.Address, make([]byte, constants.MaxDataLength)), verifier.ErrABDataTooBig, mock.SkipVmChanges)

	// control: the user-send rule and price are unchanged
	plain := z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: g.User2.Address,
		Data:      make([]byte, 100),
	}, nil, mock.SkipVmChanges)
	if want := uint64(constants.AccountBlockBasePlasma + 100*constants.ABByteDataPlasma); plain.BasePlasma != want {
		t.Fatalf("user send base plasma: got %d want %d", plain.BasePlasma, want)
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: g.User2.Address,
		Data:      make([]byte, constants.MaxDataLength+1),
	}, verifier.ErrABDataTooBig, mock.SkipVmChanges)
}

// After the spork the base plasma of an embedded send is the method tier
// plus ABByteDataPlasma per argument byte past the selector, so it scales
// with the argument size; a zero-argument call still pays the tier alone.
// The plasma RPC quote reports the same figure.
func TestEmbeddedSendData_AfterSpork_ArgumentBytesPriced(t *testing.T) {
	z := mock.NewMockZenon(t)
	plasmaApi := embedded.NewPlasmaApi(z)
	defer z.StopPanic()
	activateHtlc(t, z)
	activateDynamicPlasmaPastHeight(t, z)
	// the quote branches on the frontier momentum version, so land one
	// momentum produced under the spork first
	z.InsertNewMomentum()

	tier := constants.AlphanetPlasmaTable.EmbeddedWWithdraw
	small := z.InsertSendBlock(unlockSend(g.User1.Address, make([]byte, 32)), nil, mock.SkipVmChanges)
	large := z.InsertSendBlock(unlockSend(g.User1.Address, make([]byte, 128)), nil, mock.SkipVmChanges)
	for _, b := range []*nom.AccountBlock{small, large} {
		want := tier + uint64(len(b.Data)-4)*constants.ABByteDataPlasma
		if b.BasePlasma != want {
			t.Fatalf("base plasma for %d data bytes: got %d want %d", len(b.Data), b.BasePlasma, want)
		}
	}
	if diff, want := large.BasePlasma-small.BasePlasma, uint64(len(large.Data)-len(small.Data))*constants.ABByteDataPlasma; diff != want {
		t.Fatalf("price difference between 128- and 32-byte preimages: got %d want %d", diff, want)
	}

	quote, err := plasmaApi.GetRequiredPoWForAccountBlock(embedded.GetRequiredParam{
		BlockType: nom.BlockTypeUserSend,
		SelfAddr:  g.User1.Address,
		ToAddr:    &types.HtlcContract,
		Data:      large.Data,
	})
	common.FailIfErr(t, err)
	if quote.BasePlasma != large.BasePlasma {
		t.Fatalf("plasma quote: got basePlasma %d, the applied block paid %d", quote.BasePlasma, large.BasePlasma)
	}

	// zero-argument call: selector only, tier alone. The mock chain runs
	// the accelerator variant, where the stake CollectReward tier is
	// EmbeddedSimple + EmbeddedWWithdraw (vm/embedded/embedded.go).
	collectTier := constants.AlphanetPlasmaTable.EmbeddedSimple + constants.AlphanetPlasmaTable.EmbeddedWWithdraw
	collect := z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.StakeContract,
		Data:      definition.ABIStake.PackMethodPanic(definition.CollectRewardMethodName),
	}, nil, mock.SkipVmChanges)
	if len(collect.Data) != 4 {
		t.Fatalf("CollectReward data length: got %d want 4", len(collect.Data))
	}
	if collect.BasePlasma != collectTier {
		t.Fatalf("zero-argument call base plasma: got %d want the tier %d", collect.BasePlasma, collectTier)
	}
}

// After the spork an Unlock send whose preimage is longer than any entry
// can accept (KeyMaxSize is a uint8) is rejected at send time; one at the
// bound is accepted. Before the spork both are accepted.
func TestEmbeddedSendData_UnlockPreimageBound(t *testing.T) {
	t.Run("before spork", func(t *testing.T) {
		z := mock.NewMockZenon(t)
		defer z.StopPanic()
		activateHtlc(t, z)
		z.InsertSendBlock(unlockSend(g.User1.Address, make([]byte, constants.HtlcPreimageMaxLength+1)), nil, mock.SkipVmChanges)
	})
	t.Run("after spork", func(t *testing.T) {
		z := mock.NewMockZenon(t)
		defer z.StopPanic()
		activateHtlc(t, z)
		activateDynamicPlasmaPastHeight(t, z)
		z.InsertSendBlock(unlockSend(g.User1.Address, make([]byte, constants.HtlcPreimageMaxLength+1)), constants.ErrInvalidPreimage, mock.SkipVmChanges)
		z.InsertSendBlock(unlockSend(g.User1.Address, make([]byte, constants.HtlcPreimageMaxLength)), nil, mock.SkipVmChanges)
	})
}

// After the spork the bridge's UpdateWrapRequest and UnwrapToken sends
// reject a signature string longer than the base64 form of a signature;
// one at the bound passes the send-time rule. Before the spork both lengths
// are accepted at send time (the receive fails later on the signature).
func TestEmbeddedSendData_BridgeSignatureStringBound(t *testing.T) {
	atBound := strings.Repeat("A", constants.ECDSASignatureStringMaxLength)
	overBound := atBound + "A"
	// a well-formed 65-byte signature encoding, and the same bytes encoded
	// with a line break, which the decoder skips but the length rule rejects
	wellFormed := base64.StdEncoding.EncodeToString(make([]byte, constants.ECDSASignatureLength))
	withLineBreak := wellFormed[:44] + "\n" + wellFormed[44:]
	if len(wellFormed) != constants.ECDSASignatureStringMaxLength || len(withLineBreak) != constants.ECDSASignatureStringMaxLength+1 {
		t.Fatalf("fixture lengths %d / %d", len(wellFormed), len(withLineBreak))
	}
	if decoded, err := base64.StdEncoding.DecodeString(withLineBreak); err != nil || len(decoded) != constants.ECDSASignatureLength {
		t.Fatalf("line-break-bearing string should still decode to %d bytes: %d %v", constants.ECDSASignatureLength, len(decoded), err)
	}
	tokenAddress := "0x5fbdb2315678afecb367f032d93f642f64180aa3"
	txHash := types.HexToHashPanic("0123456789012345678901234567890123456789012345678901234567890123")

	t.Run("before spork", func(t *testing.T) {
		z := mock.NewMockZenon(t)
		defer z.StopPanic()
		activateBridge(t, z)
		z.InsertMomentumsTo(20)
		z.InsertSendBlock(updateWrapToken(types.Hash{1}, overBound), nil, mock.SkipVmChanges)
		z.InsertSendBlock(updateWrapToken(types.Hash{1}, withLineBreak), nil, mock.SkipVmChanges)
		z.InsertSendBlock(unwrapToken(2, 123, txHash, 0, tokenAddress, big.NewInt(100), overBound), nil, mock.SkipVmChanges)
	})
	t.Run("after spork", func(t *testing.T) {
		z := mock.NewMockZenon(t)
		defer z.StopPanic()
		activateBridge(t, z)
		activateDynamicPlasmaPastHeight(t, z)
		z.InsertSendBlock(updateWrapToken(types.Hash{1}, overBound), constants.ErrInvalidECDSASignature, mock.SkipVmChanges)
		z.InsertSendBlock(updateWrapToken(types.Hash{1}, atBound), nil, mock.SkipVmChanges)
		z.InsertSendBlock(updateWrapToken(types.Hash{1}, wellFormed), nil, mock.SkipVmChanges)
		z.InsertSendBlock(updateWrapToken(types.Hash{1}, withLineBreak), constants.ErrInvalidECDSASignature, mock.SkipVmChanges)
		z.InsertSendBlock(unwrapToken(2, 123, txHash, 0, tokenAddress, big.NewInt(100), overBound), constants.ErrInvalidECDSASignature, mock.SkipVmChanges)
		z.InsertSendBlock(unwrapToken(2, 123, txHash, 0, tokenAddress, big.NewInt(100), atBound), nil, mock.SkipVmChanges)
	})
}

// The verifier's recomputation of a priced embedded send matches what apply
// persisted and ignores a forged wire BasePlasma.
func TestEmbeddedSendData_AfterSpork_CanonicalRecomputeMatchesApplied(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activateHtlc(t, z)
	activateDynamicPlasmaPastHeight(t, z)

	sent := z.InsertSendBlock(unlockSend(g.User1.Address, make([]byte, 128)), nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	stored, err := z.Chain().GetFrontierMomentumStore().GetAccountBlockByHash(sent.Hash)
	common.FailIfErr(t, err)
	if stored == nil {
		t.Fatal("priced send was not cemented")
	}
	want := constants.AlphanetPlasmaTable.EmbeddedWWithdraw + uint64(len(stored.Data)-4)*constants.ABByteDataPlasma
	if stored.BasePlasma != want {
		t.Fatalf("applied base plasma: got %d want %d", stored.BasePlasma, want)
	}
	got, err := vm.CanonicalBasePlasma(z.Chain(), stored)
	common.FailIfErr(t, err)
	if got != want {
		t.Fatalf("canonical base plasma: got %d want %d", got, want)
	}
	stored.BasePlasma = constants.AlphanetPlasmaTable.EmbeddedWWithdraw // forged: the tier alone
	got, err = vm.CanonicalBasePlasma(z.Chain(), stored)
	common.FailIfErr(t, err)
	if got != want {
		t.Fatalf("canonical base plasma with a forged wire value: got %d want %d", got, want)
	}
}

// At the enforcement momentum, before the first version-2 momentum, the
// quote must already apply the spork's capacity rules: a priced Unlock
// send for an address with no fused plasma is above the legacy
// proof-of-work cap but within the Dynamic Plasma one, and gets a
// difficulty instead of an error. One momentum later the priced branch
// takes over.
func TestEmbeddedSendData_QuoteAtEnforcementMomentum(t *testing.T) {
	z := mock.NewMockZenon(t)
	plasmaApi := embedded.NewPlasmaApi(z)
	defer z.StopPanic()
	activateHtlc(t, z)
	activateDynamicPlasmaPastHeight(t, z)

	frontier, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.FailIfErr(t, err)
	if frontier.Version >= nom.DynamicPlasmaMomentumVersion {
		t.Fatalf("test assumes the enforcement momentum %d is still version %d, got %d", frontier.Height, nom.DynamicPlasmaMomentumVersion-1, frontier.Version)
	}

	send := unlockSend(g.User6.Address, make([]byte, constants.HtlcPreimageMaxLength))
	basePlasma := constants.AlphanetPlasmaTable.EmbeddedWWithdraw + uint64(len(send.Data)-4)*constants.ABByteDataPlasma
	legacyPoWCap := uint64(constants.MaxPoWPlasmaForAccountBlock)
	if basePlasma <= legacyPoWCap {
		t.Fatalf("test needs a price above the legacy proof-of-work cap %d, got %d", legacyPoWCap, basePlasma)
	}
	quote, err := plasmaApi.GetRequiredPoWForAccountBlock(embedded.GetRequiredParam{
		BlockType: nom.BlockTypeUserSend,
		SelfAddr:  g.User6.Address,
		ToAddr:    &types.HtlcContract,
		Data:      send.Data,
	})
	common.FailIfErr(t, err)
	wantDifficulty, err := dp.GetDifficultyForPlasma(basePlasma)
	common.FailIfErr(t, err)
	if quote.AvailablePlasma != 0 || quote.BasePlasma != basePlasma || quote.RequiredDifficulty != wantDifficulty {
		t.Fatalf("quote at enforcement: got %+v want available 0, base %d, difficulty %d", quote, basePlasma, wantDifficulty)
	}
	// The VM's enoughPlasma applies the same spork-driven cap for a block
	// acknowledging this momentum; admitting one here would need a mined
	// nonce for that difficulty, which the mock cannot produce in a test.
}

// The spork-gated send rules read the spork state at the block's
// MomentumAcknowledged, the convention every spork gate in the VM follows
// (vm_context/spork.go) and the one the receive-data rule adopted in #179.
// A block that acknowledges a momentum before enforcement is therefore
// priced and bounded under the pre-spork rules even when it is created and
// cemented after activation, and the verifier's recomputation agrees; a
// block acknowledging the frontier gets the new rules. The acknowledgement
// anchor itself is a chain-wide design question, not part of this change.
func TestEmbeddedSendData_RulesFollowAcknowledgedMomentum(t *testing.T) {
	t.Run("htlc unlock cap and preimage bound", func(t *testing.T) {
		z := mock.NewMockZenon(t)
		defer z.StopPanic()
		activateHtlc(t, z)
		preSpork, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
		common.FailIfErr(t, err)
		activateDynamicPlasmaPastHeight(t, z)

		tier := constants.AlphanetPlasmaTable.EmbeddedWWithdraw
		stale := unlockSend(g.User2.Address, make([]byte, constants.MaxDataLength))
		stale.MomentumAcknowledged = preSpork.Identifier()
		staleSent := z.InsertSendBlock(stale, nil, mock.SkipVmChanges)
		if staleSent.BasePlasma != tier {
			t.Fatalf("base plasma of a send acknowledging a pre-spork momentum: got %d want the tier %d", staleSent.BasePlasma, tier)
		}
		stale = unlockSend(g.User2.Address, make([]byte, constants.HtlcPreimageMaxLength+1))
		stale.MomentumAcknowledged = preSpork.Identifier()
		z.InsertSendBlock(stale, nil, mock.SkipVmChanges)

		z.InsertNewMomentum()
		cemented, err := z.Chain().GetFrontierMomentumStore().GetAccountBlockByHash(staleSent.Hash)
		common.FailIfErr(t, err)
		if cemented == nil {
			t.Fatal("send acknowledging a pre-spork momentum was not cemented after activation")
		}
		got, err := vm.CanonicalBasePlasma(z.Chain(), cemented)
		common.FailIfErr(t, err)
		if got != tier {
			t.Fatalf("canonical base plasma of the send acknowledging a pre-spork momentum: got %d want %d", got, tier)
		}

		// the same account, acknowledging the frontier, gets the new rules
		z.InsertSendBlock(unlockSend(g.User2.Address, make([]byte, constants.MaxDataLength)), verifier.ErrABDataTooBig, mock.SkipVmChanges)
		z.InsertSendBlock(unlockSend(g.User2.Address, make([]byte, constants.HtlcPreimageMaxLength+1)), constants.ErrInvalidPreimage, mock.SkipVmChanges)
	})
	t.Run("bridge signature string bound", func(t *testing.T) {
		z := mock.NewMockZenon(t)
		defer z.StopPanic()
		activateBridge(t, z)
		z.InsertMomentumsTo(20)
		preSpork, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
		common.FailIfErr(t, err)
		activateDynamicPlasmaPastHeight(t, z)

		overBound := strings.Repeat("A", constants.ECDSASignatureStringMaxLength+1)
		stale := updateWrapToken(types.Hash{1}, overBound)
		stale.MomentumAcknowledged = preSpork.Identifier()
		z.InsertSendBlock(stale, nil, mock.SkipVmChanges)
		z.InsertSendBlock(updateWrapToken(types.Hash{1}, overBound), constants.ErrInvalidECDSASignature, mock.SkipVmChanges)
	})
}

// The cap applies to the whole Data. Canonical ABI data of an embedded send
// is 4 selector bytes plus 32-byte words, so its length is always 4 mod 32:
// the longest canonical Unlock send under the cap is 16,356 bytes and the
// shortest over it 16,388. Exactly 16,384 and 16,385 bytes are reachable
// only with trailing bytes, which the spork's trailing-bytes rule rejects
// once the cap has passed. At the boundary the error changes from the cap's
// to the next rule's, and the quote prices the longest canonical payload
// below the cap (which the preimage rule then rejects).
func TestEmbeddedSendData_AfterSpork_CapBoundaryWithAbiOverhead(t *testing.T) {
	z := mock.NewMockZenon(t)
	plasmaApi := embedded.NewPlasmaApi(z)
	defer z.StopPanic()
	activateHtlc(t, z)
	activateDynamicPlasmaPastHeight(t, z)
	z.InsertNewMomentum()

	// 100 fixed bytes (selector, hash, offset, length) + preimage padded to 32
	longestUnder := unlockSend(g.User1.Address, make([]byte, 16256))
	shortestOver := unlockSend(g.User1.Address, make([]byte, 16288))
	if len(longestUnder.Data) != constants.MaxDataLength-28 || len(shortestOver.Data) != constants.MaxDataLength+4 {
		t.Fatalf("fixture lengths %d / %d", len(longestUnder.Data), len(shortestOver.Data))
	}
	exactlyAtCap := unlockSend(g.User1.Address, make([]byte, 16256))
	exactlyAtCap.Data = append(exactlyAtCap.Data, make([]byte, 28)...)
	oneOverCap := unlockSend(g.User1.Address, make([]byte, 16256))
	oneOverCap.Data = append(oneOverCap.Data, make([]byte, 29)...)
	if len(exactlyAtCap.Data) != constants.MaxDataLength || len(oneOverCap.Data) != constants.MaxDataLength+1 {
		t.Fatalf("fixture lengths %d / %d", len(exactlyAtCap.Data), len(oneOverCap.Data))
	}

	// under the cap the method's own rule is what rejects the preimage
	z.InsertSendBlock(longestUnder, constants.ErrInvalidPreimage, mock.SkipVmChanges)
	z.InsertSendBlock(shortestOver, verifier.ErrABDataTooBig, mock.SkipVmChanges)
	z.InsertSendBlock(exactlyAtCap, constants.ErrABDataTrailingBytes, mock.SkipVmChanges)
	z.InsertSendBlock(oneOverCap, verifier.ErrABDataTooBig, mock.SkipVmChanges)

	quote, err := plasmaApi.GetRequiredPoWForAccountBlock(embedded.GetRequiredParam{
		BlockType: nom.BlockTypeUserSend,
		SelfAddr:  g.User1.Address,
		ToAddr:    &types.HtlcContract,
		Data:      longestUnder.Data,
	})
	common.FailIfErr(t, err)
	if want := constants.AlphanetPlasmaTable.EmbeddedWWithdraw + uint64(len(longestUnder.Data)-4)*constants.ABByteDataPlasma; quote.BasePlasma != want {
		t.Fatalf("quote for the longest canonical payload below the cap: got %d want %d", quote.BasePlasma, want)
	}
	if _, err := plasmaApi.GetRequiredPoWForAccountBlock(embedded.GetRequiredParam{
		BlockType: nom.BlockTypeUserSend,
		SelfAddr:  g.User1.Address,
		ToAddr:    &types.HtlcContract,
		Data:      shortestOver.Data,
	}); err != verifier.ErrABDataTooBig {
		t.Fatalf("quote for the shortest payload over the cap: got %v want %v", err, verifier.ErrABDataTooBig)
	}
}

// A cryptographically valid unwrap signature that carries a line break
// decodes to the same 65 bytes, so before the spork the request is accepted
// end to end; after the spork the send-time length rule rejects the string
// with the line break while the plain encoding of the same signature is
// accepted end to end.
func TestEmbeddedSendData_BridgeSignatureLineBreakEndToEnd(t *testing.T) {
	z := mock.NewMockZenon(t)
	bridgeAPI := embedded.NewBridgeApi(z)
	defer z.StopPanic()
	activateBridgeStep6(t, z)

	networkClass := uint32(2) // evm
	chainId := uint32(123)
	tokenAddress := "0x5fbdb2315678afecb367f032d93f642f64180aa3"
	withLineBreak := func(signature string) string {
		if len(signature) != constants.ECDSASignatureStringMaxLength {
			t.Fatalf("signature length %d", len(signature))
		}
		return signature[:44] + "\n" + signature[44:]
	}

	before := types.HexToHashPanic("0a23456789012345678901234567890123456789012345678901234567890123")
	beforeSignature := getUnwrapTokenSignature(t, networkClass, chainId, before, 200, tokenAddress, big.NewInt(100*g.Zexp), networkClass)
	func() {
		defer z.CallContract(unwrapToken(networkClass, chainId, before, 200, tokenAddress, big.NewInt(100*g.Zexp), withLineBreak(beforeSignature))).
			Error(t, nil)
		insertMomentums(z, 2)
	}()
	request, err := bridgeAPI.GetUnwrapTokenRequestByHashAndLog(before, 200)
	common.FailIfErr(t, err)
	if request == nil || request.Signature != withLineBreak(beforeSignature) {
		t.Fatalf("pre-spork unwrap with a line-break-bearing signature was not registered: %+v", request)
	}

	activateDynamicPlasmaPastHeight(t, z)
	after := types.HexToHashPanic("0b23456789012345678901234567890123456789012345678901234567890123")
	afterSignature := getUnwrapTokenSignature(t, networkClass, chainId, after, 200, tokenAddress, big.NewInt(100*g.Zexp), networkClass)
	z.InsertSendBlock(unwrapToken(networkClass, chainId, after, 200, tokenAddress, big.NewInt(100*g.Zexp), withLineBreak(afterSignature)), constants.ErrInvalidECDSASignature, mock.SkipVmChanges)
	func() {
		defer z.CallContract(unwrapToken(networkClass, chainId, after, 200, tokenAddress, big.NewInt(100*g.Zexp), afterSignature)).
			Error(t, nil)
		insertMomentums(z, 2)
	}()
	request, err = bridgeAPI.GetUnwrapTokenRequestByHashAndLog(after, 200)
	common.FailIfErr(t, err)
	if request == nil || request.Signature != afterSignature {
		t.Fatalf("post-spork unwrap with the plain signature was not registered: %+v", request)
	}
}
