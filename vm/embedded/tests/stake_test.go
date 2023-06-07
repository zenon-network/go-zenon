package tests

import (
	"math/big"
	"testing"
	"time"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// Register an entry for User 1
// Check balance for User 1
func TestStake_SimpleStake(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="created stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
`)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.StakeContract,
		Data:          definition.ABIStake.PackMethodPanic(definition.StakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11990*g.Zexp)
}

// Check that the case where no rewards are given out is treated properly
// Register one entry in the middle of epoch 1
// Register one entry in the middle of epoch 2
// User1 received full reward for epoch 1 & 1/3 of epoch 2
// User2 receives 1/3 of epoch 2
func TestStake_NoEntries(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	ledgerApi := api.NewLedgerApi(z)
	stakeApi := embedded.NewStakeApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T02:16:40+0000 lvl=dbug msg="created stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=10000000000 weighted-amount=10000000000 duration-in-days=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11992149532 BlockReward:+9916666627 TotalReward:+21908816159 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2096666666666}" total-weight=2496666666666 self-weight=2096666666666
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1153538050 BlockReward:+9999999960 TotalReward:+11153538010 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2496666666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1153538050 BlockReward:+9999999960 TotalReward:+11153538010 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2496666666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=18000000000000 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX epoch=0 qsr-amount=1000000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T03:16:40+0000 lvl=dbug msg="created stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx amount=10000000000 weighted-amount=10000000000 duration-in-days=0
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12083646112 BlockReward:+9999999960 TotalReward:+22083646072 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2086666666666}" total-weight=2486666666666 self-weight=2086666666666
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1158176943 BlockReward:+9999999960 TotalReward:+11158176903 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2486666666666 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1158176943 BlockReward:+9999999960 TotalReward:+11158176903 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2486666666666 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=54000000000000 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX epoch=1 qsr-amount=666666666666
t=2001-09-09T03:46:50+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX epoch=1 qsr-amount=333333333333
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=1 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=2
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12077419354 BlockReward:+9999999960 TotalReward:+22077419314 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2080000000000}" total-weight=2480000000000 self-weight=2080000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1161290322 BlockReward:+9999999960 TotalReward:+11161290282 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2480000000000 self-weight=200000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1161290322 BlockReward:+9999999960 TotalReward:+11161290282 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2480000000000 self-weight=200000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=2 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000007200 end-time=1000010800
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=2 total-reward=1000000000000 cumulated-stake=72000000000000 start-time=1000007200 end-time=1000010800
t=2001-09-09T04:47:00+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX epoch=2 qsr-amount=500000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX epoch=2 qsr-amount=500000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=2 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=3
t=2001-09-09T04:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+20061600000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T04:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+182050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T04:47:20+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T04:47:20+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T05:16:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+184216666666666 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=2166666666666 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T05:17:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+185049999999999 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=833333333333 to-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
`)

	// User1 - half of Epoch1
	z.InsertMomentumsTo(30 * 6)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.StakeContract,
		Data:          definition.ABIStake.PackMethodPanic(definition.StakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(100 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11900*g.Zexp)

	// User2 - half of Epoch2
	z.InsertMomentumsTo((30 + 60) * 6)
	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     types.StakeContract,
		Data:          definition.ABIStake.PackMethodPanic(definition.StakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(100 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 7900*g.Zexp)

	// half of Epoch4
	z.InsertMomentumsTo((30 + 3*60) * 6)
	common.Json(stakeApi.GetUncollectedReward(g.User1.Address)).HideHashes().Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "0",
	"qsrAmount": "2166666666666"
}`)
	common.Json(stakeApi.GetUncollectedReward(g.User2.Address)).HideHashes().Equals(t, `
{
	"address": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"znnAmount": "0",
	"qsrAmount": "833333333333"
}`)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.StakeContract,
		Data:      definition.ABIStake.PackMethodPanic(definition.CollectRewardMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(ledgerApi.GetUnreceivedBlocksByAddress(g.User1.Address, 0, 10)).HideHashes().Equals(t, `
{
	"list": [
		{
			"version": 1,
			"chainIdentifier": 100,
			"blockType": 4,
			"hash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"previousHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"height": 14,
			"momentumAcknowledged": {
				"hash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
				"height": 1262
			},
			"address": "z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0",
			"toAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"amount": "2166666666666",
			"tokenStandard": "zts1qsrxxxxxxxxxxxxxmrhjll",
			"fromBlockHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"descendantBlocks": [],
			"data": "",
			"fusedPlasma": 0,
			"difficulty": 0,
			"nonce": "0000000000000000",
			"basePlasma": 0,
			"usedPlasma": 0,
			"changesHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"publicKey": null,
			"signature": null,
			"token": {
				"name": "QuasarCoin",
				"symbol": "QSR",
				"domain": "zenon.network",
				"totalSupply": "184216666666666",
				"decimals": 8,
				"owner": "z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62",
				"tokenStandard": "zts1qsrxxxxxxxxxxxxxmrhjll",
				"maxSupply": "4611686018427387903",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"confirmationDetail": {
				"numConfirmations": 1,
				"momentumHeight": 1263,
				"momentumHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
				"momentumTimestamp": 1000012620
			},
			"pairedAccountBlock": null
		}
	],
	"count": 1,
	"more": false
}`)
	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp+2166666666666)

	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.StakeContract,
		Data:      definition.ABIStake.PackMethodPanic(definition.CollectRewardMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(ledgerApi.GetUnreceivedBlocksByAddress(g.User2.Address, 0, 10)).HideHashes().Equals(t, `
{
	"list": [
		{
			"version": 1,
			"chainIdentifier": 100,
			"blockType": 4,
			"hash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"previousHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"height": 16,
			"momentumAcknowledged": {
				"hash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
				"height": 1265
			},
			"address": "z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0",
			"toAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"amount": "833333333333",
			"tokenStandard": "zts1qsrxxxxxxxxxxxxxmrhjll",
			"fromBlockHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"descendantBlocks": [],
			"data": "",
			"fusedPlasma": 0,
			"difficulty": 0,
			"nonce": "0000000000000000",
			"basePlasma": 0,
			"usedPlasma": 0,
			"changesHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"publicKey": null,
			"signature": null,
			"token": {
				"name": "QuasarCoin",
				"symbol": "QSR",
				"domain": "zenon.network",
				"totalSupply": "185049999999999",
				"decimals": 8,
				"owner": "z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62",
				"tokenStandard": "zts1qsrxxxxxxxxxxxxxmrhjll",
				"maxSupply": "4611686018427387903",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"confirmationDetail": {
				"numConfirmations": 1,
				"momentumHeight": 1266,
				"momentumHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
				"momentumTimestamp": 1000012650
			},
			"pairedAccountBlock": null
		}
	],
	"count": 1,
	"more": false
}`)
	autoreceive(t, z, g.User2.Address)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp+833333333333)
}

// Register an entry for 2 days
// Try to revoke it before 2 days, fail each time
// Try to revoke it from a different address
// - util.ErrDataNonExistent since the address is part of the key
func TestStake_RevokeFromInvalidAddress(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)

	stakeApi := embedded.NewStakeApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="created stake entry" module=embedded contract=stake id=5c303a6d7fe2b3390921e299f78ea9281c8f48ad564b9fe9cb1af015d5d5aed3 owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1100000000 duration-in-days=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11994438146 BlockReward:+9916666627 TotalReward:+21911104773 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099166666666}" total-weight=2499166666666 self-weight=2099166666666
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152384128 BlockReward:+9999999960 TotalReward:+11152384088 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499166666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152384128 BlockReward:+9999999960 TotalReward:+11152384088 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499166666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=3949000000000 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz id=5c303a6d7fe2b3390921e299f78ea9281c8f48ad564b9fe9cb1af015d5d5aed3 epoch=0 qsr-amount=1000000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12095078031 BlockReward:+9999999960 TotalReward:+22095077991 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099000000000}" total-weight=2499000000000 self-weight=2099000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152460984 BlockReward:+9999999960 TotalReward:+11152460944 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499000000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152460984 BlockReward:+9999999960 TotalReward:+11152460944 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499000000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=3960000000000 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz id=5c303a6d7fe2b3390921e299f78ea9281c8f48ad564b9fe9cb1af015d5d5aed3 epoch=1 qsr-amount=1000000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=1 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=2
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12095078031 BlockReward:+9999999960 TotalReward:+22095077991 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099000000000}" total-weight=2499000000000 self-weight=2099000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152460984 BlockReward:+9999999960 TotalReward:+11152460944 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499000000000 self-weight=200000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152460984 BlockReward:+9999999960 TotalReward:+11152460944 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499000000000 self-weight=200000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=2 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000007200 end-time=1000010800
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=2 total-reward=1000000000000 cumulated-stake=3960000000000 start-time=1000007200 end-time=1000010800
t=2001-09-09T04:47:00+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz id=5c303a6d7fe2b3390921e299f78ea9281c8f48ad564b9fe9cb1af015d5d5aed3 epoch=2 qsr-amount=1000000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=2 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=3
t=2001-09-09T04:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+20061600000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T04:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+182050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T04:47:20+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T04:47:20+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=1444 last-update-height=1083
t=2001-09-09T05:47:10+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=3 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12095078031 BlockReward:+9999999960 TotalReward:+22095077991 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099000000000}" total-weight=2499000000000 self-weight=2099000000000
t=2001-09-09T05:47:10+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=3 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152460984 BlockReward:+9999999960 TotalReward:+11152460944 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499000000000 self-weight=200000000000
t=2001-09-09T05:47:10+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=3 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152460984 BlockReward:+9999999960 TotalReward:+11152460944 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499000000000 self-weight=200000000000
t=2001-09-09T05:47:10+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=4
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=1444 last-update-height=1083
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=3 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000010800 end-time=1000014400
t=2001-09-09T05:47:10+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=4
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=1444 last-update-height=1083
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=3 total-reward=1000000000000 cumulated-stake=3960000000000 start-time=1000010800 end-time=1000014400
t=2001-09-09T05:47:10+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz id=5c303a6d7fe2b3390921e299f78ea9281c8f48ad564b9fe9cb1af015d5d5aed3 epoch=3 qsr-amount=1000000000000
t=2001-09-09T05:47:10+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=4
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=1444 last-update-height=1083
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=3 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T05:47:10+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=4
t=2001-09-09T05:47:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+20248800000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T05:47:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+182550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T05:47:30+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T05:47:30+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T06:46:40+0000 lvl=dbug msg="revoked stake entry" module=embedded contract=stake id=5c303a6d7fe2b3390921e299f78ea9281c8f48ad564b9fe9cb1af015d5d5aed3 owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz start-time=1000000010 revoke-time=1000018000
`)

	stakeHash := types.HexToHashPanic("5c303a6d7fe2b3390921e299f78ea9281c8f48ad564b9fe9cb1af015d5d5aed3")

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.StakeContract,
		Data:          definition.ABIStake.PackMethodPanic(definition.StakeMethodName, 2*constants.StakeTimeMinSec),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertMomentumsTo(10)
	common.Json(stakeApi.GetEntriesByAddress(g.User1.Address, 0, 10)).Equals(t, `
{
	"totalAmount": "1000000000",
	"totalWeightedAmount": "1100000000",
	"count": 1,
	"list": [
		{
			"amount": "1000000000",
			"weightedAmount": "1100000000",
			"startTimestamp": 1000000010,
			"expirationTimestamp": 1000007210,
			"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "5c303a6d7fe2b3390921e299f78ea9281c8f48ad564b9fe9cb1af015d5d5aed3"
		}
	]
}`)

	// cancel stake from different address
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.StakeContract,
		Data:      definition.ABIStake.PackMethodPanic(definition.CancelStakeMethodName, stakeHash),
	}).Error(t, constants.ErrDataNonExistent)
	z.InsertNewMomentum()

	// cancel stake while staking period is still active
	z.InsertMomentumsTo(20)

	common.Json(stakeApi.GetEntriesByAddress(g.User1.Address, 0, 10)).Equals(t, `
{
	"totalAmount": "1000000000",
	"totalWeightedAmount": "1100000000",
	"count": 1,
	"list": [
		{
			"amount": "1000000000",
			"weightedAmount": "1100000000",
			"startTimestamp": 1000000010,
			"expirationTimestamp": 1000007210,
			"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "5c303a6d7fe2b3390921e299f78ea9281c8f48ad564b9fe9cb1af015d5d5aed3"
		}
	]
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.StakeContract,
		Data:      definition.ABIStake.PackMethodPanic(definition.CancelStakeMethodName, stakeHash),
	}).Error(t, constants.RevokeNotDue)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(stakeApi.GetEntriesByAddress(g.User1.Address, 0, 10)).Equals(t, `
{
	"totalAmount": "1000000000",
	"totalWeightedAmount": "1100000000",
	"count": 1,
	"list": [
		{
			"amount": "1000000000",
			"weightedAmount": "1100000000",
			"startTimestamp": 1000000010,
			"expirationTimestamp": 1000007210,
			"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "5c303a6d7fe2b3390921e299f78ea9281c8f48ad564b9fe9cb1af015d5d5aed3"
		}
	]
}`)

	z.InsertMomentumsTo(2 * 60 * 6)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.StakeContract,
		Data:      definition.ABIStake.PackMethodPanic(definition.CancelStakeMethodName, stakeHash),
	}).Error(t, constants.RevokeNotDue)
	z.InsertNewMomentum()

	z.InsertMomentumsTo(5 * 60 * 6)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.StakeContract,
		Data:      definition.ABIStake.PackMethodPanic(definition.CancelStakeMethodName, stakeHash),
	}).Error(t, nil)
	z.InsertNewMomentum()
}

// Register 4 stake entries, 2 from address 1 and 1 from address 3 and 4
// Revoke one entry in the middle of the second epoch, receiving only 1/2 of the staking reward for that epoch
// Check address can collect reward and received back amount after revoking pillar
func TestStake_MultipleCreation(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)

	stakeApi := embedded.NewStakeApi(z)
	ledgerApi := api.NewLedgerApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="created stake entry" module=embedded contract=stake id=60be4b855f8b1871ccfbe13b348039c4106a15a902b2e983dc289c809691f545 owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T01:46:50+0000 lvl=dbug msg="created stake entry" module=embedded contract=stake id=5942e350ddaa6fdea996c5284a0de65cb7190d5b783657b763085d905a8123d5 owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1300000000 duration-in-days=0
t=2001-09-09T01:46:50+0000 lvl=dbug msg="created stake entry" module=embedded contract=stake id=1ead89d3625892c696aaa6b0602fcf95f9f79af1cb9e54b83470eacd44efaf60 owner=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac amount=10000000000 weighted-amount=15000000000 duration-in-days=0
t=2001-09-09T01:46:50+0000 lvl=dbug msg="created stake entry" module=embedded contract=stake id=66cafe4f4f5d7061c474fe8ecb0cef07495146d6e3947ce101baf63d1c350642 owner=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx amount=5000000000 weighted-amount=10000000000 duration-in-days=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12030050284 BlockReward:+9916666627 TotalReward:+21946716911 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2094166666666}" total-weight=2485833333332 self-weight=2094166666666
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1110291652 BlockReward:+9999999960 TotalReward:+11110291612 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+191666666666}" total-weight=2485833333332 self-weight=191666666666
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1158565202 BlockReward:+9999999960 TotalReward:+11158565162 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2485833333332 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=98007000000000 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz id=5942e350ddaa6fdea996c5284a0de65cb7190d5b783657b763085d905a8123d5 epoch=0 qsr-amount=47619047619
t=2001-09-09T02:46:40+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz id=60be4b855f8b1871ccfbe13b348039c4106a15a902b2e983dc289c809691f545 epoch=0 qsr-amount=36630036630
t=2001-09-09T02:46:40+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac id=1ead89d3625892c696aaa6b0602fcf95f9f79af1cb9e54b83470eacd44efaf60 epoch=0 qsr-amount=549450549450
t=2001-09-09T02:46:40+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx id=66cafe4f4f5d7061c474fe8ecb0cef07495146d6e3947ce101baf63d1c350642 epoch=0 qsr-amount=366300366300
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12138219895 BlockReward:+9999999960 TotalReward:+22138219855 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2093000000000}" total-weight=2483000000000 self-weight=2093000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1101892871 BlockReward:+9999999960 TotalReward:+11101892831 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+190000000000}" total-weight=2483000000000 self-weight=190000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1159887233 BlockReward:+9999999960 TotalReward:+11159887193 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2483000000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=98280000000000 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz id=5942e350ddaa6fdea996c5284a0de65cb7190d5b783657b763085d905a8123d5 epoch=1 qsr-amount=47619047619
t=2001-09-09T03:46:50+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz id=60be4b855f8b1871ccfbe13b348039c4106a15a902b2e983dc289c809691f545 epoch=1 qsr-amount=36630036630
t=2001-09-09T03:46:50+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac id=1ead89d3625892c696aaa6b0602fcf95f9f79af1cb9e54b83470eacd44efaf60 epoch=1 qsr-amount=549450549450
t=2001-09-09T03:46:50+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx id=66cafe4f4f5d7061c474fe8ecb0cef07495146d6e3947ce101baf63d1c350642 epoch=1 qsr-amount=366300366300
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=1 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=2
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T04:16:40+0000 lvl=dbug msg="revoked stake entry" module=embedded contract=stake id=60be4b855f8b1871ccfbe13b348039c4106a15a902b2e983dc289c809691f545 owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz start-time=1000000010 revoke-time=1000009000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12138523489 BlockReward:+9999999960 TotalReward:+22138523449 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2093333333333}" total-weight=2483333333333 self-weight=2093333333333
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1101744966 BlockReward:+9999999960 TotalReward:+11101744926 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+190000000000}" total-weight=2483333333333 self-weight=190000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1159731543 BlockReward:+9999999960 TotalReward:+11159731503 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2483333333333 self-weight=200000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=2 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000007200 end-time=1000010800
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=2 total-reward=1000000000000 cumulated-stake=96480000000000 start-time=1000007200 end-time=1000010800
t=2001-09-09T04:47:00+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz id=5942e350ddaa6fdea996c5284a0de65cb7190d5b783657b763085d905a8123d5 epoch=2 qsr-amount=48507462686
t=2001-09-09T04:47:00+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz id=60be4b855f8b1871ccfbe13b348039c4106a15a902b2e983dc289c809691f545 epoch=2 qsr-amount=18656716417
t=2001-09-09T04:47:00+0000 lvl=dbug msg="deleted stake entry" module=embedded contract=stake id=60be4b855f8b1871ccfbe13b348039c4106a15a902b2e983dc289c809691f545 revoke-time=1000009000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac id=1ead89d3625892c696aaa6b0602fcf95f9f79af1cb9e54b83470eacd44efaf60 epoch=2 qsr-amount=559701492537
t=2001-09-09T04:47:00+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx id=66cafe4f4f5d7061c474fe8ecb0cef07495146d6e3947ce101baf63d1c350642 epoch=2 qsr-amount=373134328358
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=2 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=3
t=2001-09-09T04:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+20061600000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T04:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+182050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T04:47:20+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T04:47:20+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=1444 last-update-height=1083
t=2001-09-09T05:47:10+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=3 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12139130434 BlockReward:+9999999960 TotalReward:+22139130394 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2094000000000}" total-weight=2484000000000 self-weight=2094000000000
t=2001-09-09T05:47:10+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=3 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1101449275 BlockReward:+9999999960 TotalReward:+11101449235 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+190000000000}" total-weight=2484000000000 self-weight=190000000000
t=2001-09-09T05:47:10+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=3 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1159420289 BlockReward:+9999999960 TotalReward:+11159420249 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2484000000000 self-weight=200000000000
t=2001-09-09T05:47:10+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=4
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=1444 last-update-height=1083
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=3 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000010800 end-time=1000014400
t=2001-09-09T05:47:10+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=4
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=1444 last-update-height=1083
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=3 total-reward=1000000000000 cumulated-stake=94680000000000 start-time=1000010800 end-time=1000014400
t=2001-09-09T05:47:10+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz id=5942e350ddaa6fdea996c5284a0de65cb7190d5b783657b763085d905a8123d5 epoch=3 qsr-amount=49429657794
t=2001-09-09T05:47:10+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac id=1ead89d3625892c696aaa6b0602fcf95f9f79af1cb9e54b83470eacd44efaf60 epoch=3 qsr-amount=570342205323
t=2001-09-09T05:47:10+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx id=66cafe4f4f5d7061c474fe8ecb0cef07495146d6e3947ce101baf63d1c350642 epoch=3 qsr-amount=380228136882
t=2001-09-09T05:47:10+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=4
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=1444 last-update-height=1083
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=3 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T05:47:10+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=4
t=2001-09-09T05:47:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+20248800000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T05:47:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+182550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T05:47:30+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T05:47:30+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T06:16:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+182835092005395 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=285092005395 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T06:47:20+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=1805 last-update-height=1444
t=2001-09-09T06:47:20+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=4 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12139130434 BlockReward:+9999999960 TotalReward:+22139130394 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2094000000000}" total-weight=2484000000000 self-weight=2094000000000
t=2001-09-09T06:47:20+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=4 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1101449275 BlockReward:+9999999960 TotalReward:+11101449235 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+190000000000}" total-weight=2484000000000 self-weight=190000000000
t=2001-09-09T06:47:20+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=4 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1159420289 BlockReward:+9999999960 TotalReward:+11159420249 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2484000000000 self-weight=200000000000
t=2001-09-09T06:47:20+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=5
t=2001-09-09T06:47:20+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=1805 last-update-height=1444
t=2001-09-09T06:47:20+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=4 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000014400 end-time=1000018000
t=2001-09-09T06:47:20+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=5
t=2001-09-09T06:47:20+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=1805 last-update-height=1444
t=2001-09-09T06:47:20+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=4 total-reward=1000000000000 cumulated-stake=94680000000000 start-time=1000014400 end-time=1000018000
t=2001-09-09T06:47:20+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz id=5942e350ddaa6fdea996c5284a0de65cb7190d5b783657b763085d905a8123d5 epoch=4 qsr-amount=49429657794
t=2001-09-09T06:47:20+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac id=1ead89d3625892c696aaa6b0602fcf95f9f79af1cb9e54b83470eacd44efaf60 epoch=4 qsr-amount=570342205323
t=2001-09-09T06:47:20+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx id=66cafe4f4f5d7061c474fe8ecb0cef07495146d6e3947ce101baf63d1c350642 epoch=4 qsr-amount=380228136882
t=2001-09-09T06:47:20+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=5
t=2001-09-09T06:47:20+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=1805 last-update-height=1444
t=2001-09-09T06:47:20+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=4 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T06:47:20+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=5
t=2001-09-09T06:47:30+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+20436000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T06:47:30+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+183335092005395 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T06:47:40+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T06:47:40+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T07:16:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+183384521663189 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=49429657794 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)

	stake1hash := types.HexToHashPanic("60be4b855f8b1871ccfbe13b348039c4106a15a902b2e983dc289c809691f545")
	// Epoch 0
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.StakeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
		Data:          definition.ABIStake.PackMethodPanic(definition.StakeMethodName, constants.StakeTimeMinSec),
	}).Error(t, nil)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.StakeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
		Data:          definition.ABIStake.PackMethodPanic(definition.StakeMethodName, 4*constants.StakeTimeMinSec),
	}).Error(t, nil)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     types.StakeContract,
		Data:          definition.ABIStake.PackMethodPanic(definition.StakeMethodName, 11*constants.StakeTimeMinSec),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(50 * g.Zexp),
	}).Error(t, nil)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User3.Address,
		ToAddress:     types.StakeContract,
		Data:          definition.ABIStake.PackMethodPanic(definition.StakeMethodName, 6*constants.StakeTimeMinSec),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(100 * g.Zexp),
	}).Error(t, nil)

	// Half of Epoch1
	z.InsertMomentumsTo(30 * 6)
	z.ExpectBalance(types.StakeContract, types.ZnnTokenStandard, 170*g.Zexp)
	common.Json(stakeApi.GetEntriesByAddress(g.User1.Address, 0, 10)).HideHashes().Equals(t, `
{
	"totalAmount": "2000000000",
	"totalWeightedAmount": "2300000000",
	"count": 2,
	"list": [
		{
			"amount": "1000000000",
			"weightedAmount": "1000000000",
			"startTimestamp": 1000000010,
			"expirationTimestamp": 1000003610,
			"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		},
		{
			"amount": "1000000000",
			"weightedAmount": "1300000000",
			"startTimestamp": 1000000010,
			"expirationTimestamp": 1000014410,
			"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	common.Json(stakeApi.GetEntriesByAddress(g.User5.Address, 0, 10)).HideHashes().Equals(t, `
{
	"totalAmount": "0",
	"totalWeightedAmount": "0",
	"count": 0,
	"list": []
}`)

	// Half of Epoch2
	z.InsertMomentumsTo((30 + 60) * 6)
	common.Json(stakeApi.GetUncollectedReward(g.User1.Address)).HideHashes().Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "0",
	"qsrAmount": "84249084249"
}`)

	// Half of Epoch3
	z.InsertMomentumsTo((30 + 2*60) * 6)
	// znn before cancel
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11980*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.StakeContract,
		Data:      definition.ABIStake.PackMethodPanic(definition.CancelStakeMethodName, stake1hash),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(ledgerApi.GetUnreceivedBlocksByAddress(g.User1.Address, 0, 10)).HideHashes().Equals(t, `
{
	"list": [
		{
			"version": 1,
			"chainIdentifier": 100,
			"blockType": 4,
			"hash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"previousHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"height": 7,
			"momentumAcknowledged": {
				"hash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
				"height": 901
			},
			"address": "z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62",
			"toAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"amount": "1000000000",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"fromBlockHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"descendantBlocks": [],
			"data": "",
			"fusedPlasma": 0,
			"difficulty": 0,
			"nonce": "0000000000000000",
			"basePlasma": 0,
			"usedPlasma": 0,
			"changesHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"publicKey": null,
			"signature": null,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": "19874400000000",
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": "4611686018427387903",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"confirmationDetail": {
				"numConfirmations": 1,
				"momentumHeight": 902,
				"momentumHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
				"momentumTimestamp": 1000009010
			},
			"pairedAccountBlock": null
		}
	],
	"count": 1,
	"more": false
}`)
	autoreceive(t, z, g.User1.Address)
	// znn after cancel
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11990*g.Zexp)
	// znn after cancel
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)

	// Half of Epoch5
	z.InsertMomentumsTo((30 + 4*60) * 6)
	common.Json(stakeApi.GetUncollectedReward(g.User1.Address)).HideHashes().Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "0",
	"qsrAmount": "285092005395"
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.StakeContract,
		Data:      definition.ABIStake.PackMethodPanic(definition.CollectRewardMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(stakeApi.GetUncollectedReward(g.User1.Address)).HideHashes().Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "0",
	"qsrAmount": "0"
}`)
	common.Json(ledgerApi.GetUnreceivedBlocksByAddress(g.User1.Address, 0, 10)).HideHashes().Equals(t, `
{
	"list": [
		{
			"version": 1,
			"chainIdentifier": 100,
			"blockType": 4,
			"hash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"previousHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"height": 18,
			"momentumAcknowledged": {
				"hash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
				"height": 1622
			},
			"address": "z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0",
			"toAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"amount": "285092005395",
			"tokenStandard": "zts1qsrxxxxxxxxxxxxxmrhjll",
			"fromBlockHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"descendantBlocks": [],
			"data": "",
			"fusedPlasma": 0,
			"difficulty": 0,
			"nonce": "0000000000000000",
			"basePlasma": 0,
			"usedPlasma": 0,
			"changesHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
			"publicKey": null,
			"signature": null,
			"token": {
				"name": "QuasarCoin",
				"symbol": "QSR",
				"domain": "zenon.network",
				"totalSupply": "182835092005395",
				"decimals": 8,
				"owner": "z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62",
				"tokenStandard": "zts1qsrxxxxxxxxxxxxxmrhjll",
				"maxSupply": "4611686018427387903",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"confirmationDetail": {
				"numConfirmations": 1,
				"momentumHeight": 1623,
				"momentumHash": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
				"momentumTimestamp": 1000016220
			},
			"pairedAccountBlock": null
		}
	],
	"count": 1,
	"more": false
}`)
	autoreceive(t, z, g.User1.Address)
	// qsr after collect
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp+285092005395)

	// Half of Epoch6
	z.InsertMomentumsTo((30 + 5*60) * 6)
	common.Json(stakeApi.GetUncollectedReward(g.User1.Address)).HideHashes().Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "0",
	"qsrAmount": "49429657794"
}`)
	common.Json(stakeApi.GetUncollectedReward(g.User2.Address)).HideHashes().Equals(t, `
{
	"address": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"znnAmount": "0",
	"qsrAmount": "1866191334722"
}`)
	common.Json(stakeApi.GetUncollectedReward(g.User3.Address)).HideHashes().Equals(t, `
{
	"address": "z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac",
	"znnAmount": "0",
	"qsrAmount": "2799287002083"
}`)
	// collect reward
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.StakeContract,
		Data:      definition.ABIStake.PackMethodPanic(definition.CollectRewardMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(stakeApi.GetUncollectedReward(g.User1.Address)).HideHashes().Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "0",
	"qsrAmount": "0"
}`)
	autoreceive(t, z, g.User1.Address)
	// qsr after collect
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 12334521663189)
	common.Json(stakeApi.GetUncollectedReward(g.User2.Address)).HideHashes().Equals(t, `
{
	"address": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"znnAmount": "0",
	"qsrAmount": "1866191334722"
}`)
	common.Json(stakeApi.GetUncollectedReward(g.User3.Address)).HideHashes().Equals(t, `
{
	"address": "z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac",
	"znnAmount": "0",
	"qsrAmount": "2799287002083"
}`)
}

// Register one entry in epoch 0
// Revoke this entry in the middle of the fourth epoch
// Check the reward that was not collected for the revoked entry
func TestStake_RevokeAndCollect(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)

	stakeApi := embedded.NewStakeApi(z)
	ledgerApi := api.NewLedgerApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg="created stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11994438146 BlockReward:+9916666627 TotalReward:+21911104773 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099166666666}" total-weight=2499166666666 self-weight=2099166666666
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152384128 BlockReward:+9999999960 TotalReward:+11152384088 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499166666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152384128 BlockReward:+9999999960 TotalReward:+11152384088 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499166666666 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=3590000000000 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX epoch=0 qsr-amount=1000000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12095078031 BlockReward:+9999999960 TotalReward:+22095077991 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099000000000}" total-weight=2499000000000 self-weight=2099000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152460984 BlockReward:+9999999960 TotalReward:+11152460944 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499000000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152460984 BlockReward:+9999999960 TotalReward:+11152460944 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499000000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=3600000000000 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX epoch=1 qsr-amount=1000000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=1 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=2
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12095078031 BlockReward:+9999999960 TotalReward:+22095077991 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099000000000}" total-weight=2499000000000 self-weight=2099000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152460984 BlockReward:+9999999960 TotalReward:+11152460944 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499000000000 self-weight=200000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152460984 BlockReward:+9999999960 TotalReward:+11152460944 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499000000000 self-weight=200000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=2 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000007200 end-time=1000010800
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=2 total-reward=1000000000000 cumulated-stake=3600000000000 start-time=1000007200 end-time=1000010800
t=2001-09-09T04:47:00+0000 lvl=dbug msg="giving rewards" module=embedded contract=stake address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX epoch=2 qsr-amount=1000000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=2 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=3
t=2001-09-09T04:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+20061600000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T04:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+182050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T04:47:20+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T04:47:20+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T05:16:40+0000 lvl=dbug msg="revoked stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz start-time=1000000010 revoke-time=1000012600
t=2001-09-09T05:17:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+185050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=3000000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)
	stake1hash := types.HexToHashPanic("60be4b855f8b1871ccfbe13b348039c4106a15a902b2e983dc289c809691f545")
	// Epoch 0
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.StakeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(10 * g.Zexp),
		Data:          definition.ABIStake.PackMethodPanic(definition.StakeMethodName, constants.StakeTimeMinSec),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.ExpectBalance(types.StakeContract, types.ZnnTokenStandard, 10*g.Zexp)

	// Half of Epoch4
	z.InsertMomentumsTo((30 + 3*60) * 6)
	common.Json(stakeApi.GetEntriesByAddress(g.User1.Address, 0, 10)).Equals(t, `
{
	"totalAmount": "1000000000",
	"totalWeightedAmount": "1000000000",
	"count": 1,
	"list": [
		{
			"amount": "1000000000",
			"weightedAmount": "1000000000",
			"startTimestamp": 1000000010,
			"expirationTimestamp": 1000003610,
			"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "60be4b855f8b1871ccfbe13b348039c4106a15a902b2e983dc289c809691f545"
		}
	]
}`)
	common.Json(stakeApi.GetUncollectedReward(g.User1.Address)).HideHashes().Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "0",
	"qsrAmount": "3000000000000"
}`)
	common.Json(stakeApi.GetEntriesByAddress(g.User1.Address, 0, 10)).HideHashes().Equals(t, `
{
	"totalAmount": "1000000000",
	"totalWeightedAmount": "1000000000",
	"count": 1,
	"list": [
		{
			"amount": "1000000000",
			"weightedAmount": "1000000000",
			"startTimestamp": 1000000010,
			"expirationTimestamp": 1000003610,
			"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.StakeContract,
		Data:      definition.ABIStake.PackMethodPanic(definition.CancelStakeMethodName, stake1hash),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.ExpectBalance(types.StakeContract, types.ZnnTokenStandard, 0*g.Zexp)
	common.Json(stakeApi.GetUncollectedReward(g.User1.Address)).HideHashes().Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "0",
	"qsrAmount": "3000000000000"
}`)
	common.Json(stakeApi.GetEntriesByAddress(g.User1.Address, 0, 10)).HideHashes().Equals(t, `
{
	"totalAmount": "0",
	"totalWeightedAmount": "0",
	"count": 0,
	"list": []
}`)

	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.StakeContract,
		Data:      definition.ABIStake.PackMethodPanic(definition.CollectRewardMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(ledgerApi.GetUnreceivedBlocksByAddress(g.User1.Address, 0, 10)).Equals(t, `
{
	"list": [
		{
			"version": 1,
			"chainIdentifier": 100,
			"blockType": 4,
			"hash": "24355e43a4c3f44afc39697d71cffeacb5894dae4c33f47f53c2d15dec417a5c",
			"previousHash": "d09d0839c92613b2de1854a93eb52a237ddd1d46640764285640c29bb49d00a1",
			"height": 14,
			"momentumAcknowledged": {
				"hash": "6c9ec92b5a77ec36b1119fa83c029b81258add6f59c420d4826e561788b7b322",
				"height": 1263
			},
			"address": "z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0",
			"toAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"amount": "3000000000000",
			"tokenStandard": "zts1qsrxxxxxxxxxxxxxmrhjll",
			"fromBlockHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"descendantBlocks": [],
			"data": "",
			"fusedPlasma": 0,
			"difficulty": 0,
			"nonce": "0000000000000000",
			"basePlasma": 0,
			"usedPlasma": 0,
			"changesHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"publicKey": null,
			"signature": null,
			"token": {
				"name": "QuasarCoin",
				"symbol": "QSR",
				"domain": "zenon.network",
				"totalSupply": "185050000000000",
				"decimals": 8,
				"owner": "z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62",
				"tokenStandard": "zts1qsrxxxxxxxxxxxxxmrhjll",
				"maxSupply": "4611686018427387903",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"confirmationDetail": {
				"numConfirmations": 1,
				"momentumHeight": 1264,
				"momentumHash": "12b75aeb4445db788a91e9f278a532c3e501636cf4a15836e4294ace142f9c37",
				"momentumTimestamp": 1000012630
			},
			"pairedAccountBlock": null
		},
		{
			"version": 1,
			"chainIdentifier": 100,
			"blockType": 4,
			"hash": "4041384272e029cd22d806cd1b11dadedf0a1758102f9c29cf6fa517d2c4ba44",
			"previousHash": "4f9fea7379c7d3a6d7a104809ed1f0437620bd152b7218fe437d2aa450970f81",
			"height": 5,
			"momentumAcknowledged": {
				"hash": "4048e26ad78052e277024344eb4800695769a65b8d33048d1b6e53a5c181dd27",
				"height": 1261
			},
			"address": "z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62",
			"toAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"amount": "1000000000",
			"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
			"fromBlockHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"descendantBlocks": [],
			"data": "",
			"fusedPlasma": 0,
			"difficulty": 0,
			"nonce": "0000000000000000",
			"basePlasma": 0,
			"usedPlasma": 0,
			"changesHash": "0000000000000000000000000000000000000000000000000000000000000000",
			"publicKey": null,
			"signature": null,
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": "20061600000000",
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": "4611686018427387903",
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"confirmationDetail": {
				"numConfirmations": 3,
				"momentumHeight": 1262,
				"momentumHash": "f0e02fb8b3ac7b684623ff0d16396f7e1088f76e97c9344f3a53a70caf8e9c6b",
				"momentumTimestamp": 1000012610
			},
			"pairedAccountBlock": null
		}
	],
	"count": 2,
	"more": false
}`)
	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp+3000000000000)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
}

func TestStake_CheckRpc(t *testing.T) {
	z := mock.NewMockZenon(t)
	stakeApi := embedded.NewStakeApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, ``)
	common.Json(stakeApi.GetFrontierRewardByPage(g.User1.Address, 0, 10)).Equals(t, `
{
	"count": 0,
	"list": []
}`)
	common.Json(stakeApi.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": "0",
	"qsrAmount": "0"
}`)
	common.Json(stakeApi.GetEntriesByAddress(g.User1.Address, 0, 10)).Equals(t, `
{
	"totalAmount": "0",
	"totalWeightedAmount": "0",
	"count": 0,
	"list": []
}`)
}
