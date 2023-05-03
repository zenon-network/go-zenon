package tests

import (
	"math/big"
	"math/rand"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain"
	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

func simpleSendSetup(t *testing.T, z mock.MockZenon) {
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(100 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum() // cemented send

	autoreceive(t, z, g.User2.Address)
	z.InsertNewMomentum() // cemented receive
}

// send 1500 znn from user1 to user2
// - check that one can receive block only if it's in a momentum
func TestSimple_NeedsToBeCemented(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	// send money
	block := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(1500 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 10500*g.Zexp)

	// can't receive until it's been added to a momentum
	z.InsertReceiveBlock(block.Header(), nil, verifier.ErrABFromBlockMissing, mock.NoVmChanges)
	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
}

// - send 1500 znn from user1 to user2
// - check that balances change when received
// - check ledger.GetUnreceivedBlocksByAddress RPC call returns unreceived block
func TestSimple_UnreceivedRPC(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	// verify balances
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)

	// send money
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(1500 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 10500*g.Zexp)

	z.InsertMomentumsTo(10)

	autoreceive(t, z, g.User2.Address)
	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 9500*g.Zexp)
}

func TestSimple_UnreceivedDisappears(t *testing.T) {
	z := mock.NewMockZenon(t)
	ledgerApi := api.NewLedgerApi(z)
	defer z.StopPanic()

	simpleSendSetup(t, z)

	// check that the block disappears from unreceived
	common.Json(ledgerApi.GetUnreceivedBlocksByAddress(g.User2.Address, 0, 10)).Equals(t, `
{
	"list": [],
	"count": 0,
	"more": false
}`)
}

func TestSimple_ContractCall(t *testing.T) {
	z := mock.NewMockZenon(t)
	pillarApi := embedded.NewPillarApi(z, true)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, ``)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PillarContract,
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(1500 * g.Zexp),
		Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
	}).Error(t, nil)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.StakeContract,
		Data:      definition.ABIStake.PackMethodPanic(definition.CancelStakeMethodName, types.ZeroHash),
	}).Error(t, constants.ErrDataNonExistent)

	// Doesn't get added to any momentum since the send-block is invalid
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PillarContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(1500 * g.Zexp),
		Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
	}, constants.ErrInvalidTokenOrAmount, mock.NoVmChanges)

	z.InsertNewMomentum() // cemented send blocks
	z.InsertNewMomentum() // cemented pillar receive-blocks

	common.Json(pillarApi.GetDepositedQsr(g.User1.Address)).Equals(t, `"150000000000"`)
	common.Json(pillarApi.GetDelegatedPillar(g.User1.Address)).Equals(t, `
{
	"name": "TEST-pillar-1",
	"status": 1,
	"weight": "1200000000000"
}`)

	z.InsertMomentumsTo(60)
	common.Json(pillarApi.GetAll(0, 10)).SubJson(ListOf(func() interface{} {
		return new(struct {
			Weight string `json:"weight"`
		})
	})).Equals(t, `
{
	"count": 3,
	"list": [
		{
			"weight": "2100000000000"
		},
		{
			"weight": "200000000000"
		},
		{
			"weight": "200000000000"
		}
	]
}`)
}

func TestSimple_ContractMultiCall(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.ZenonLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:40+0000 lvl=info msg="inserted block" module=zenon identifier="{Address:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:2}}"
t=2001-09-09T01:46:50+0000 lvl=info msg="added block to momentum" module=zenon momentum-height=2 identifier="&{Address:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:2}}"
t=2001-09-09T01:46:50+0000 lvl=info msg="inserted block" module=zenon identifier="{Address:z1qxemdeddedxswapxxxxxxxxxxxxxxxxxxl4yww HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:4}}"
t=2001-09-09T01:47:00+0000 lvl=info msg="added block to momentum" module=zenon momentum-height=3 identifier="&{Address:z1qxemdeddedxswapxxxxxxxxxxxxxxxxxxl4yww HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:2}}"
t=2001-09-09T01:47:00+0000 lvl=info msg="added block to momentum" module=zenon momentum-height=3 identifier="&{Address:z1qxemdeddedxswapxxxxxxxxxxxxxxxxxxl4yww HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:3}}"
t=2001-09-09T01:47:00+0000 lvl=info msg="added block to momentum" module=zenon momentum-height=3 identifier="&{Address:z1qxemdeddedxswapxxxxxxxxxxxxxxxxxxl4yww HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:4}}"
t=2001-09-09T01:47:00+0000 lvl=info msg="inserted block" module=zenon identifier="{Address:z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:3}}"
t=2001-09-09T01:47:00+0000 lvl=info msg="inserted block" module=zenon identifier="{Address:z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:5}}"
t=2001-09-09T01:47:10+0000 lvl=info msg="added block to momentum" module=zenon momentum-height=4 identifier="&{Address:z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:2}}"
t=2001-09-09T01:47:10+0000 lvl=info msg="added block to momentum" module=zenon momentum-height=4 identifier="&{Address:z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:3}}"
t=2001-09-09T01:47:10+0000 lvl=info msg="added block to momentum" module=zenon momentum-height=4 identifier="&{Address:z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:4}}"
t=2001-09-09T01:47:10+0000 lvl=info msg="added block to momentum" module=zenon momentum-height=4 identifier="&{Address:z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:5}}"
t=2001-09-09T01:47:10+0000 lvl=info msg="inserted block" module=zenon identifier="{Address:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:3}}"
t=2001-09-09T01:47:10+0000 lvl=info msg="inserted block" module=zenon identifier="{Address:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:4}}"
t=2001-09-09T01:47:20+0000 lvl=info msg="added block to momentum" module=zenon momentum-height=5 identifier="&{Address:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:3}}"
t=2001-09-09T01:47:20+0000 lvl=info msg="added block to momentum" module=zenon momentum-height=5 identifier="&{Address:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:4}}"
`)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.SwapContract,
		Data: definition.ABISwap.PackMethodPanic(
			definition.RetrieveAssetsMethodName,
			g.Secp1PubKeyB64,
			signRetrieveAssetsMessage(t, g.User1.Address, g.Secp1PrvKey, g.Secp1PubKeyB64)),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented swap receive-blocks
	z.InsertNewMomentum() // cemented token receive-blocks

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)

	autoreceive(t, z, g.User1.Address)
	z.InsertNewMomentum() // cemented user1 receive blocks

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 27000*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 270000*g.Zexp)
}

func TestSimple_MomentumInsertionBenchmark(b *testing.T) {
	z := mock.NewMockZenon(b)
	defer z.StopPanic()

	start := time.Now().UnixNano()
	// 1 hours, expect to last less than 1,5 seconds
	z.InsertMomentumsTo(1 * 60 * 60 / 10)
	end := time.Now().UnixNano()

	diff := (end - start) / 1000000
	if diff > 1500 {
		b.Fatalf("Test took too much. Expected to be less than 1500 ms but it took %v", diff)
	}
}

func TestSimple_InsufficientBalance(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, ``)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.StakeContract,
		Data:          definition.ABIStake.PackMethodPanic(definition.StakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(13000 * g.Zexp),
	}, constants.ErrInsufficientBalance, mock.NoVmChanges)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
}

func TestSimple_InvalidToken(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, ``)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.StakeContract,
		Data:          definition.ABIStake.PackMethodPanic(definition.StakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(100 * g.Zexp),
	}, constants.ErrInvalidTokenOrAmount, mock.NoVmChanges)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)

	zts, err := types.ParseZTS("zts1s7q09quzmnc5ypd6uf5vha")
	common.DealWithErr(err)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.StakeContract,
		Data:          definition.ABIStake.PackMethodPanic(definition.StakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: zts,
		Amount:        big.NewInt(100 * g.Zexp),
	}, constants.ErrInvalidTokenOrAmount, mock.NoVmChanges)
}

// - test that you can call embedded.pillar.getAll without returning an error
func TestSimple_CanCallConsensusCache(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	pillarApi := embedded.NewPillarApi(z, true)

	common.Json(pillarApi.GetAll(0, 10)).Error(t, nil)
}

// - test that it's not possible to have 2 transaction which don't have the momentum-ack in decreasing order
// * creates 2 send blocks in momentum 2
// * tries to receive the 2 blocks manually with faulty momentum-ack
func TestSimple_MomentumAcknowledgedIncreasing(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	ledgerApi := api.NewLedgerApi(z)

	// 2 send blocks to user 2
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(100 * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(200 * g.Zexp),
	}, nil, mock.SkipVmChanges)

	z.InsertMomentumsTo(5)

	momentums, err := ledgerApi.GetMomentumsByHeight(3, 2)
	common.FailIfErr(t, err)
	unreceived, err := ledgerApi.GetUnreceivedBlocksByAddress(g.User2.Address, 0, 2)
	common.FailIfErr(t, err)
	common.Expect(t, unreceived.Count, 2)

	z.InsertReceiveBlock(unreceived.List[0].Header(), &nom.AccountBlock{
		MomentumAcknowledged: momentums.List[1].Identifier(),
	}, nil, mock.SkipVmChanges)
	z.InsertReceiveBlock(unreceived.List[1].Header(), &nom.AccountBlock{
		MomentumAcknowledged: momentums.List[0].Identifier(),
	}, verifier.ErrABMAGap, mock.NoVmChanges)
}

// - test that it's possible to receive blocks which are not on the frontier of the other account-block
// * creates 10 send blocks in momentum 2
// * creates 10 receive blocks in momentum 3, which receive in random order
// * test that all 10 blocks have been received successfully
func TestSimple_BatchedBlocks(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	ledgerApi := api.NewLedgerApi(z)

	// 10 send blocks
	for i := 0; i < 10; i += 1 {
		z.InsertSendBlock(&nom.AccountBlock{
			Address:       g.User1.Address,
			ToAddress:     g.User2.Address,
			TokenStandard: types.ZnnTokenStandard,
			Amount:        big.NewInt(100 * g.Zexp),
		}, nil, mock.SkipVmChanges)
	}
	z.InsertNewMomentum()

	// initial statement, account-block has height 1 with 10 unreceived blocks
	frontierAccBlock, err := ledgerApi.GetFrontierAccountBlock(g.User2.Address)
	common.FailIfErr(t, err)
	common.Expect(t, frontierAccBlock.Height, 1)
	unreceived, err := ledgerApi.GetUnreceivedBlocksByAddress(g.User2.Address, 0, 10)
	common.FailIfErr(t, err)
	common.Expect(t, unreceived.Count, 10)

	// shuffle the order in which we receive the blocks
	rand.Shuffle(len(unreceived.List), func(i, j int) { unreceived.List[i], unreceived.List[j] = unreceived.List[j], unreceived.List[i] })
	for _, block := range unreceived.List {
		z.InsertReceiveBlock(block.Header(), nil, nil, mock.SkipVmChanges)
	}
	z.InsertNewMomentum()

	// final statement, account-block has height 11 with 0 unreceived blocks
	frontierAccBlock, err = ledgerApi.GetFrontierAccountBlock(g.User2.Address)
	common.FailIfErr(t, err)
	common.Expect(t, frontierAccBlock.Height, 11)
	unreceived, err = ledgerApi.GetUnreceivedBlocksByAddress(g.User2.Address, 0, 10)
	common.FailIfErr(t, err)
	common.Expect(t, unreceived.Count, 0)
}

// The purpose of this test is to limit the max number of account blocks in a momentum
// and afterwards do some random stuff in order to try to break the system.
func TestSimple_MomentumContent(t *testing.T) {
	currentMaxAccountBlocksInMomentum := chain.MaxAccountBlocksInMomentum
	chain.MaxAccountBlocksInMomentum = 5
	defer func() {
		chain.MaxAccountBlocksInMomentum = currentMaxAccountBlocksInMomentum
	}()

	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	addresses := []types.Address{
		g.User1.Address, g.User2.Address, g.User3.Address, g.User4.Address, g.User5.Address,
	}

	for i := 1; i < 5; i += 1 {
		for j := 0; j < i; j += 1 {
			autoreceive(t, z, addresses[j])
			z.CallContract(&nom.AccountBlock{
				Address:       addresses[j],
				ToAddress:     types.PillarContract,
				TokenStandard: types.QsrTokenStandard,
				Amount:        big.NewInt(10 * g.Zexp),
				Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
			})
			z.CallContract(&nom.AccountBlock{
				Address:   addresses[j],
				ToAddress: types.PillarContract,
				Data:      definition.ABIPillars.PackMethodPanic(definition.WithdrawQsrMethodName),
			})
		}

		z.InsertNewMomentum()
	}

	for i := 5; i < 20; i += 1 {
		autoreceive(t, z, addresses[i%5])
		z.InsertNewMomentum()
	}
}
