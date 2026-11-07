package tests

import (
	"math/big"
	"testing"
	"time"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// Issue #271: when a contract-to-contract send fails, the refund back to
// the sending contract carries no data. Before EmbeddedRefundSpork the
// refund's method lookup fails, the receive is never generated and the
// send stays at the head of the receiving contract's queue. After the
// spork the receive finalizes as failed and the sending contract takes
// the refund back as a deposit.
//
// The trigger is the live one from the issue: a bridge administrator
// registers an Owned pair for a token the bridge does not own and that
// is not burnable, so the Burn the Bridge sends to the Token contract on
// every wrap fails with ErrPermissionDenied.

const (
	refundNetworkClass = uint32(2) // evm
	refundChainId      = uint32(123)
	refundWrapAmount   = int64(10000)
	refundFeePercent   = uint32(15)
)

// activateEmbeddedRefundSpork creates and activates a spork on the mock
// chain, points EmbeddedRefundSpork at it and advances to its enforcement
// height. The spork id and the implemented-spork map are restored when
// the test ends.
func activateEmbeddedRefundSpork(t *testing.T, z mock.MockZenon) {
	saveSporkState(t)
	savedId := types.EmbeddedRefundSpork.SporkId
	t.Cleanup(func() { types.EmbeddedRefundSpork.SporkId = savedId })

	createSend := z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"embedded-refund",                          // name
			"Refund failed contract-to-contract sends", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	id := createSend.Hash

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data:      definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName, id),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.EmbeddedRefundSpork.SporkId = id
	types.ImplementedSporksMap[id] = true

	sporkList, err := embedded.NewSporkApi(z).GetAll(0, 10)
	common.FailIfErr(t, err)
	var enforcementHeight uint64
	for _, s := range sporkList.List {
		if s.Id == id {
			enforcementHeight = s.EnforcementHeight
		}
	}
	if enforcementHeight == 0 {
		t.Fatal("embedded refund spork not found in spork list")
	}
	z.InsertMomentumsTo(enforcementHeight + 1)

	active, err := z.Chain().GetFrontierMomentumStore().IsSporkActive(types.EmbeddedRefundSpork)
	common.FailIfErr(t, err)
	if !active {
		t.Fatal("embedded refund spork is not active after its enforcement height")
	}
}

// setupUnburnableOwnedPair initializes the bridge, issues a token owned by
// User1 that is mintable but not burnable, and registers it as an Owned
// pair. Every wrap of it makes the Bridge send a Burn the Token contract
// rejects.
func setupUnburnableOwnedPair(t *testing.T, z mock.MockZenon) types.ZenonTokenStandard {
	activateBridgeStep1(t, z)
	securityInfo, err := embedded.NewBridgeApi(z).GetSecurityInfo()
	common.FailIfErr(t, err)

	defer z.CallContract(addNetwork(g.User5.Address, refundNetworkClass, refundChainId, "Ethereum", "0x323b5d4c32345ced77393b3530b1eed0f346429d", "{}")).
		Error(t, nil)
	insertMomentums(z, 2)

	guardians := []types.Address{g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User5.Address}
	nominateGuardians(t, z, g.User5.Address, guardians, securityInfo.AdministratorDelay)
	changeTssWithAdministrator(t, z, g.User5.Address, "AsAQx1M3LVXCuozDOqO5b9adj/PItYgwZFG/xTDBiZzT", securityInfo.SoftDelay)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.TokenContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.TokenIssueAmount,
		Data: definition.ABIToken.PackMethodPanic(definition.IssueMethodName,
			"unburnable",        // TokenName
			"UNBURN",            // TokenSymbol
			"",                  // TokenDomain
			big.NewInt(100000),  // TotalSupply
			big.NewInt(1000000), // MaxSupply
			uint8(1),            // Decimals
			true,                // IsMintable
			false,               // IsBurnable
			false,               // IsUtility
		),
	}).Error(t, nil)
	insertMomentums(z, 2)

	tokenList, err := embedded.NewTokenApi(z).GetByOwner(g.User1.Address, 0, 10)
	common.FailIfErr(t, err)
	if len(tokenList.List) != 1 {
		t.Fatalf("expected one token owned by User1, got %d", len(tokenList.List))
	}
	zts := tokenList.List[0].ZenonTokenStandard
	autoreceive(t, z, g.User1.Address)
	insertMomentums(z, 2)
	z.ExpectBalance(g.User1.Address, zts, 100000)

	setTokenPair(t, z, g.User5.Address, securityInfo.SoftDelay, refundNetworkClass, refundChainId, zts, "0x5fbdb2315678afecb367f032d93f642f64180aa3",
		true, true, true, big.NewInt(100), refundFeePercent, uint32(20), "{}")
	return zts
}

// wrapUnburnable wraps refundWrapAmount of zts and returns the Burn send
// the Bridge emits towards the Token contract.
func wrapUnburnable(t *testing.T, z mock.MockZenon, zts types.ZenonTokenStandard) *nom.AccountBlock {
	defer z.CallContract(wrapToken(zts, big.NewInt(refundWrapAmount), refundNetworkClass, refundChainId, "0xb794f5ea0ba39494ce839613fffba74279579268")).
		Error(t, nil)
	insertMomentums(z, 2)

	burn := lastSend(t, z, types.BridgeContract, types.TokenContract)
	if burn == nil || burn.TokenStandard != zts {
		t.Fatalf("no Burn of %v from the Bridge to the Token contract", zts)
	}
	return burn
}

// lastSend walks address's chain back from its frontier and returns the
// latest contract send to toAddress, or nil.
func lastSend(t *testing.T, z mock.MockZenon, address, toAddress types.Address) *nom.AccountBlock {
	t.Helper()
	store := z.Chain().GetFrontierAccountStore(address)
	frontier, err := store.Frontier()
	common.FailIfErr(t, err)
	for h := frontier.Height; h > 0; h-- {
		block, err := store.ByHeight(h)
		common.FailIfErr(t, err)
		if block.BlockType == nom.BlockTypeContractSend && block.ToAddress == toAddress {
			return block
		}
	}
	return nil
}

// issueAfterBurn sends an unrelated Issue to the Token contract, queued
// behind the Bridge's Burn.
func issueAfterBurn(z mock.MockZenon) *nom.AccountBlock {
	return z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     types.TokenContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.TokenIssueAmount,
		Data: definition.ABIToken.PackMethodPanic(definition.IssueMethodName,
			"queued", "QUEUED", "", big.NewInt(100), big.NewInt(100), uint8(1), false, true, false),
	}, nil, mock.SkipVmChanges)
}

// expectReceived checks whether a confirmed receive block on address
// consumed send. Embedded contracts consume their queue through the
// sequencer and do not mark sends as received in the account store, so
// the check reads the momentum store's receive mapping.
func expectReceived(t *testing.T, z mock.MockZenon, address types.Address, send *nom.AccountBlock, expected bool) {
	t.Helper()
	receive, err := z.Chain().GetFrontierMomentumStore().GetBlockWhichReceives(send.Hash)
	if err != nil && err != constants.ErrDataNonExistent {
		common.FailIfErr(t, err)
	}
	got := receive != nil
	if got && receive.Address != address {
		t.Fatalf("send %v was received by %v, want %v", send.Hash, receive.Address, address)
	}
	if got != expected {
		t.Fatalf("send %v received by %v = %v, want %v", send.Hash, address, got, expected)
	}
}

func expectTotalSupply(t *testing.T, z mock.MockZenon, zts types.ZenonTokenStandard, expected int64) {
	t.Helper()
	token, err := embedded.NewTokenApi(z).GetByZts(zts)
	common.FailIfErr(t, err)
	if token.TotalSupply.Cmp(big.NewInt(expected)) != 0 {
		t.Fatalf("total supply of %v = %v, want %v", zts, token.TotalSupply, expected)
	}
}

// Pins the behaviour before the spork: the failed Burn cannot be refunded,
// so the Token contract never receives it and its queue stops there.
func TestEmbeddedRefund_BeforeSporkStallsQueue(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()

	zts := setupUnburnableOwnedPair(t, z)
	burn := wrapUnburnable(t, z, zts)
	fee := refundWrapAmount * int64(refundFeePercent) / int64(constants.MaximumFee)
	if burn.Amount.Int64() != refundWrapAmount-fee {
		t.Fatalf("burn amount = %v, want %v", burn.Amount, refundWrapAmount-fee)
	}

	queued := issueAfterBurn(z)
	insertMomentums(z, 10)

	expectReceived(t, z, types.TokenContract, burn, false)
	expectReceived(t, z, types.TokenContract, queued, false)

	tokenStore := z.Chain().GetFrontierAccountStore(types.TokenContract)
	head, err := tokenStore.SequencerFront(z.Chain().GetFrontierMomentumStore().GetAccountMailbox(types.TokenContract))
	common.FailIfErr(t, err)
	if head == nil || head.Hash != burn.Hash {
		t.Fatalf("Token contract queue head = %v, want the Bridge's Burn %v", head, burn.Hash)
	}

	// The burn amount sits in the cemented, unreceived Burn send.
	z.ExpectBalance(types.BridgeContract, zts, fee)
	z.ExpectBalance(types.TokenContract, zts, 0)
	expectTotalSupply(t, z, zts, 100000)
}

// After the spork the Token contract's receive finalizes as failed, the
// refund returns to the Bridge as a deposit and the queue advances.
func TestEmbeddedRefund_AfterSporkRefundsAndAdvances(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()

	zts := setupUnburnableOwnedPair(t, z)
	activateEmbeddedRefundSpork(t, z)

	burn := wrapUnburnable(t, z, zts)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     types.TokenContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.TokenIssueAmount,
		Data: definition.ABIToken.PackMethodPanic(definition.IssueMethodName,
			"queued", "QUEUED", "", big.NewInt(100), big.NewInt(100), uint8(1), false, true, false),
	}).Error(t, nil)
	insertMomentums(z, 6)

	expectReceived(t, z, types.TokenContract, burn, true)

	// The Token contract's chain ends with the refund of the failed Burn,
	// followed by the queued Issue's receive and its mint.
	refund := lastSend(t, z, types.TokenContract, types.BridgeContract)
	if refund == nil {
		t.Fatal("no refund from the Token contract to the Bridge")
	}
	if refund.TokenStandard != zts || refund.Amount.Cmp(burn.Amount) != 0 || len(refund.Data) != 0 {
		t.Fatalf("refund = %v %v data %x, want %v %v and no data", refund.Amount, refund.TokenStandard, refund.Data, burn.Amount, zts)
	}

	// The Bridge accepts the refund as a deposit: it holds the whole wrap
	// amount, the Token contract holds nothing and no supply was burned.
	expectReceived(t, z, types.BridgeContract, refund, true)
	z.ExpectBalance(types.BridgeContract, zts, refundWrapAmount)
	z.ExpectBalance(types.TokenContract, zts, 0)
	expectTotalSupply(t, z, zts, 100000)
}
