package tests

import (
	"fmt"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"math/big"
	"testing"
	"time"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

var (
	tokens []types.ZenonTokenStandard
)

func issueMultipleTokensSetup(t *testing.T, z mock.MockZenon) {
	tokenAPI := embedded.NewTokenApi(z)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.TokenContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.TokenIssueAmount,
		Data: definition.ABIToken.PackMethodPanic(definition.IssueMethodName,
			"test.tok3n_liquidity-1", //param.TokenName
			"LIQ1",                   //param.TokenSymbol
			"",                       //param.TokenDomain
			big.NewInt(100*g.Zexp),   //param.TotalSupply
			big.NewInt(1000*g.Zexp),  //param.MaxSupply
			uint8(6),                 //param.Decimals
			true,                     //param.IsMintable
			true,                     //param.IsBurnable
			false,                    //param.IsUtility
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.TokenContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.TokenIssueAmount,
		Data: definition.ABIToken.PackMethodPanic(definition.IssueMethodName,
			"test.tok3n_liquidity-2", //param.TokenName
			"LIQ2",                   //param.TokenSymbol
			"",                       //param.TokenDomain
			big.NewInt(200*g.Zexp),   //param.TotalSupply
			big.NewInt(2000*g.Zexp),  //param.MaxSupply
			uint8(6),                 //param.Decimals
			true,                     //param.IsMintable
			true,                     //param.IsBurnable
			false,                    //param.IsUtility
		),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block

	tokenList, err := tokenAPI.GetByOwner(g.User1.Address, 0, 10)
	common.FailIfErr(t, err)

	tokens = make([]types.ZenonTokenStandard, 0)
	for _, zts := range tokenList.List {
		tokens = append(tokens, zts.ZenonTokenStandard)
	}

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, tokens[0], big.NewInt(100*g.Zexp), g.User1.Address),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, tokens[1], big.NewInt(200*g.Zexp), g.User1.Address),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	autoreceive(t, z, g.User1.Address)
}

func TestLiquidity(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995200000 BlockReward:+9916666627 TotalReward:+21911866627 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2100000000000}" total-weight=2500000000000 self-weight=2100000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152000000 BlockReward:+9999999960 TotalReward:+11151999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152000000 BlockReward:+9999999960 TotalReward:+11151999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12096000000 BlockReward:+9999999960 TotalReward:+22095999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2100000000000}" total-weight=2500000000000 self-weight=2100000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152000000 BlockReward:+9999999960 TotalReward:+11151999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152000000 BlockReward:+9999999960 TotalReward:+11151999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=1 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=2
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
`)
	ledgerApi := api.NewLedgerApi(z)

	z.InsertMomentumsTo(1000)
	common.Json(ledgerApi.GetAccountInfoByAddress(types.LiquidityContract)).Equals(t, `
{
	"address": "z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae",
	"accountHeight": 10,
	"balanceInfoMap": {
		"zts1qsrxxxxxxxxxxxxxmrhjll": {
			"token": {
				"name": "QuasarCoin",
				"symbol": "QSR",
				"domain": "zenon.network",
				"totalSupply": 181550000000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62",
				"tokenStandard": "zts1qsrxxxxxxxxxxxxxmrhjll",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"balance": 1000000000000
		},
		"zts1znnxxxxxxxxxxxxx9z4ulx": {
			"token": {
				"name": "Zenon Coin",
				"symbol": "ZNN",
				"domain": "zenon.network",
				"totalSupply": 19874400000000,
				"decimals": 8,
				"owner": "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg",
				"tokenStandard": "zts1znnxxxxxxxxxxxxx9z4ulx",
				"maxSupply": 4611686018427387903,
				"isBurnable": true,
				"isMintable": true,
				"isUtility": true
			},
			"balance": 374400000000
		}
	}
}`)
}

func TestLiquidity_SendRewardFromNonSporkAddress(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
`)
	activateAccelerator(z)

	// Try to send reward using User1
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.FundMethodName,
			common.Big1, // znnReward
			common.Big1, // qsrReward
		),
	}, constants.ErrPermissionDenied, mock.SkipVmChanges)
	z.InsertNewMomentum()

}

func TestLiquidity_SendInvalidReward(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="invalid send reward - not enough funds" module=embedded contract=liquidity`)
	activateAccelerator(z)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 0)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 0)
	// Try to send reward using Spork Address
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.FundMethodName,
			common.Big100, // znnReward
			common.Big100, // qsrReward
		),
	}).Error(t, constants.ErrInvalidTokenOrAmount)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
}

func TestLiquidity_SimpleSendReward(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="invalid send reward - not enough funds" module=embedded contract=liquidity
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995200000 BlockReward:+9916666627 TotalReward:+21911866627 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2100000000000}" total-weight=2500000000000 self-weight=2100000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152000000 BlockReward:+9999999960 TotalReward:+11151999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152000000 BlockReward:+9999999960 TotalReward:+11151999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T02:47:20+0000 lvl=dbug msg="donate reward to accelerator" module=embedded contract=liquidity znn-amount=1 qsr-amount=1
t=2001-09-09T02:47:30+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 from-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=1
t=2001-09-09T02:47:30+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 from-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=1
`)
	activateAccelerator(z)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.FundMethodName,
			common.Big1, // znnReward
			common.Big1, // qsrReward
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.InsertMomentumsTo(60*6 + 4)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 187200000000)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 500000000000)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.FundMethodName,
			common.Big1, // znnReward
			common.Big1, // qsrReward
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.ExpectBalance(types.AcceleratorContract, types.ZnnTokenStandard, 1)
	z.ExpectBalance(types.AcceleratorContract, types.QsrTokenStandard, 1)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 187200000000-1)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 500000000000-1)
}

func TestLiquidity_BurnFromNonSporkAddress(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
`)
	activateAccelerator(z)

	// Try to burn znn using User1
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.BurnZnnMethodName,
			common.Big1, // burnAmount
		),
	}, constants.ErrPermissionDenied, mock.SkipVmChanges)
	z.InsertNewMomentum()

}

func TestLiquidity_BurnInvalidAmount(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="invalid burn ZNN - not enough funds" module=embedded contract=liquidity`)
	activateAccelerator(z)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 0)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 0)
	// Try to burn znn using Spork Address
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.BurnZnnMethodName,
			common.Big100, // burnAmount
		),
	}).Error(t, constants.ErrInvalidTokenOrAmount)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
}

func TestLiquidity_SimpleBurn(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:34d8229bd07586c243c6e74122a18d6d2002694c72964a7186111026a9cec6ab Name:spork-accelerator Description:activate spork for accelerator Activated:true EnforcementHeight:9}"
t=2001-09-09T01:50:00+0000 lvl=dbug msg="invalid send reward - not enough funds" module=embedded contract=liquidity
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995200000 BlockReward:+9916666627 TotalReward:+21911866627 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2100000000000}" total-weight=2500000000000 self-weight=2100000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152000000 BlockReward:+9999999960 TotalReward:+11151999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152000000 BlockReward:+9999999960 TotalReward:+11151999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T02:47:20+0000 lvl=dbug msg="burn ZNN" module=embedded contract=liquidity znn-amount=1
t=2001-09-09T02:47:30+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687199999999 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=1
`)
	activateAccelerator(z)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.FundMethodName,
			common.Big1, // znnReward
			common.Big1, // qsrReward
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.InsertMomentumsTo(60*6 + 4)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 187200000000)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 500000000000)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.BurnZnnMethodName,
			common.Big1, // burnAmount
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 187200000000-1)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 500000000000)
}

func TestLiquidity_SetTokenTuples(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}"
t=2001-09-09T01:49:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+30000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:49:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+30000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)
	sporkAPI := embedded.NewSporkApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address
	issueMultipleTokensSetup(t, z)
	customZts := make([]string, 0)
	for _, zts := range tokens {
		customZts = append(customZts, zts.String())
	}
	percentages := []uint32{
		uint32(5000),
		uint32(5000),
	}
	minAmounts := []*big.Int{
		big.NewInt(1000),
		big.NewInt(2000),
	}
	fmt.Println(customZts)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			percentages,
			percentages,
			minAmounts,
		),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"isHalted": false,
	"znnReward": 0,
	"qsrReward": 0,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 2000
		}
	]
}`)
}

func TestLiquidity_SimpleLiquidityStake(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}"
t=2001-09-09T01:49:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+30000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:49:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+30000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:50:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T01:50:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1100000000 weighted-amount=1100000000 duration-in-days=0
`)
	sporkAPI := embedded.NewSporkApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address
	issueMultipleTokensSetup(t, z)
	customZts := make([]string, 0)
	for _, zts := range tokens {
		customZts = append(customZts, zts.String())
	}
	percentages := []uint32{
		uint32(5000),
		uint32(5000),
	}
	minAmounts := []*big.Int{
		big.NewInt(1000),
		big.NewInt(2000),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			percentages,
			percentages,
			minAmounts,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"isHalted": false,
	"znnReward": 0,
	"qsrReward": 0,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 2000
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000003800,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, tokens[1], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[1],
		Amount:        big.NewInt(11 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[1], 289*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 2100000000,
	"totalWeightedAmount": 2100000000,
	"count": 2,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000003800,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "96a6918463995f5f38314a15bb6b3a96d60d83509ac325f6873aafed6ec015f6"
		},
		{
			"amount": 1100000000,
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"weightedAmount": 1100000000,
			"startTime": 1000000220,
			"revokeTime": 0,
			"expirationTime": 1000003820,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "cb905be998a7a5c80e0d2e753dd87dc1020961bf5062a0413b41e75c1cc2fdfe"
		}
	]
}`)
}

func TestLiquidity_CancelLiquidityStake(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}"
t=2001-09-09T01:49:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+30000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:49:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+30000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:50:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T01:50:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1100000000 weighted-amount=1100000000 duration-in-days=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995047669 BlockReward:+9916666627 TotalReward:+21911714296 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099833333333}" total-weight=2499833333333 self-weight=2099833333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=93600000000 qsr-amount=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1a2ukxhyameh230e58m5x8t znn-amount=93600000000 qsr-amount=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T03:10:00+0000 lvl=dbug msg="revoked liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz start-time=1000000200 revoke-time=1000005000
`)
	sporkAPI := embedded.NewSporkApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address
	issueMultipleTokensSetup(t, z)
	customZts := make([]string, 0)
	for _, zts := range tokens {
		customZts = append(customZts, zts.String())
	}
	percentages := []uint32{
		uint32(5000),
		uint32(5000),
	}
	minAmounts := []*big.Int{
		big.NewInt(1000),
		big.NewInt(2000),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			percentages,
			percentages,
			minAmounts,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"isHalted": false,
	"znnReward": 0,
	"qsrReward": 0,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 2000
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000003800,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[1],
		Amount:        big.NewInt(11 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 2100000000,
	"totalWeightedAmount": 2100000000,
	"count": 2,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000003800,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "96a6918463995f5f38314a15bb6b3a96d60d83509ac325f6873aafed6ec015f6"
		},
		{
			"amount": 1100000000,
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"weightedAmount": 1100000000,
			"startTime": 1000000220,
			"revokeTime": 0,
			"expirationTime": 1000003820,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "cb905be998a7a5c80e0d2e753dd87dc1020961bf5062a0413b41e75c1cc2fdfe"
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, tokens[1], 289*g.Zexp)
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	hash := types.HexToHashPanic("96a6918463995f5f38314a15bb6b3a96d60d83509ac325f6873aafed6ec015f6")
	z.InsertMomentumsTo(500)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.CancelLiquidityStakeMethodName, hash),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, tokens[0], 300*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 1100000000,
	"totalWeightedAmount": 1100000000,
	"count": 1,
	"list": [
		{
			"amount": 1100000000,
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"weightedAmount": 1100000000,
			"startTime": 1000000220,
			"revokeTime": 0,
			"expirationTime": 1000003820,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "cb905be998a7a5c80e0d2e753dd87dc1020961bf5062a0413b41e75c1cc2fdfe"
		}
	]
}`)
}

// Add LIQ1 token tuple (min: 1000, percentage: 50% znn, 50% qsr)
// Add LIQ2 token tuple (min: 2000, percentage: 50% znn, 50% qsr)
// Register an entry for User 1 (token: LIQ1, amount: 10*10^8)
// Register an entry for User 1 (token: LIQ2, amount: 10*10^8)
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8)
// Rewards for znn: 50% * 1872*10^8 = 936*10^8 znn, 50% * 5000*10^8 = 2500*10^8 qsr
// Rewards for qsr: 50% * 1872*10^8 = 936*10^8 znn, 50% * 5000*10^8 = 2500*10^8 qsr
// Rewards for first entry (token: LIQ1, amount: 10*10^8) -> 100% * 936*10^8 znn, 100% * 2500*10^8 qsr
// Rewards for second entry (token: LIQ2, amount: 10*10^8) -> 100% * 936*10^8 znn, 100% * 2500*10^8 qsr
// Total rewards -> 936 * 2 * 10^8 znn = 1872 * 10^8 znn, 2 * 2500 * 10^8 = 5000 * 10^8 qsr
func TestLiquidity_LiquidityStakeAndUpdate1(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}"
t=2001-09-09T01:49:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+30000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:49:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+30000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:50:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T01:50:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=2000000000 weighted-amount=2000000000 duration-in-days=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995047669 BlockReward:+9916666627 TotalReward:+21911714296 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099833333333}" total-weight=2499833333333 self-weight=2099833333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=93600000000 qsr-amount=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1a2ukxhyameh230e58m5x8t znn-amount=93600000000 qsr-amount=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
`)
	sporkAPI := embedded.NewSporkApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address
	issueMultipleTokensSetup(t, z)
	customZts := make([]string, 0)
	for _, zts := range tokens {
		customZts = append(customZts, zts.String())
	}
	percentages := []uint32{
		uint32(5000),
		uint32(5000),
	}
	minAmounts := []*big.Int{
		big.NewInt(1000),
		big.NewInt(2000),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			percentages,
			percentages,
			minAmounts,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"isHalted": false,
	"znnReward": 0,
	"qsrReward": 0,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 2000
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000003800,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "96a6918463995f5f38314a15bb6b3a96d60d83509ac325f6873aafed6ec015f6"
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, tokens[1], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[1],
		Amount:        big.NewInt(20 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[1], 280*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 3000000000,
	"totalWeightedAmount": 3000000000,
	"count": 2,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000003800,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "96a6918463995f5f38314a15bb6b3a96d60d83509ac325f6873aafed6ec015f6"
		},
		{
			"amount": 2000000000,
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"weightedAmount": 2000000000,
			"startTime": 1000000220,
			"revokeTime": 0,
			"expirationTime": 1000003820,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "e8921d45cd03aa6431b5d3b1d821370007db450ab2970fe7a3ad180481853134"
		}
	]
}`)
	z.InsertMomentumsTo(60*6 + 2)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 187200000000,
	"qsrAmount": 500000000000
}`)
}

// Add LIQ1 token tuple (min: 1000, percentage: 70% znn, 30% qsr)
// Add LIQ2 token tuple (min: 2000, percentage: 30% znn, 70% qsr)
// Register an entry for User 1 (token: LIQ1, amount: 10*10^8)
// Register an entry for User 1 (token: LIQ2, amount: 10*10^8)
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8)
// Rewards for znn: 70% * 1872*10^8 = 13104*10^7 znn, 30% * 5000*10^8 = 1500*10^8 qsr
// Rewards for qsr: 30% * 1872*10^8 = 5616*10^7 znn, 70% * 5000*10^8 = 3500*10^8 qsr
// Rewards for first entry (token: LIQ1, amount: 10*10^8) -> 100% * 13104*10^7 znn, 100% * 1500*10^8 qsr
// Rewards for second entry (token: LIQ2, amount: 10*10^8) -> 100% * 5616*10^7 znn, 100% * 3500*10^8 qsr
// Total rewards -> (13104 + 5616) * 10^7 znn, (1500 + 3500) * 10^8 qsr
func TestLiquidity_LiquidityStakeAndUpdate2(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}"
t=2001-09-09T01:49:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+30000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:49:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+30000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:50:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T01:50:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995047669 BlockReward:+9916666627 TotalReward:+21911714296 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099833333333}" total-weight=2499833333333 self-weight=2099833333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=7000 qsr-percentage=3000 znn-rewards=131040000000 qsr-rewards=150000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=3000 qsr-percentage=7000 znn-rewards=56160000000 qsr-rewards=350000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1a2ukxhyameh230e58m5x8t znn-amount=56160000000 qsr-amount=350000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=131040000000 qsr-amount=150000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
`)
	sporkAPI := embedded.NewSporkApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address
	issueMultipleTokensSetup(t, z)
	customZts := make([]string, 0)
	for _, zts := range tokens {
		customZts = append(customZts, zts.String())
	}
	znnPercentages := []uint32{
		uint32(7000),
		uint32(3000),
	}
	qsrPercentages := []uint32{
		uint32(3000),
		uint32(7000),
	}
	minAmounts := []*big.Int{
		big.NewInt(1000),
		big.NewInt(2000),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			znnPercentages,
			qsrPercentages,
			minAmounts,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"isHalted": false,
	"znnReward": 0,
	"qsrReward": 0,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 7000,
			"qsrPercentage": 3000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 3000,
			"qsrPercentage": 7000,
			"minAmount": 2000
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000003800,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[1],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": 2000000000,
	"totalWeightedAmount": 2000000000,
	"count": 2,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000003800,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		},
		{
			"amount": 1000000000,
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"weightedAmount": 1000000000,
			"startTime": 1000000220,
			"revokeTime": 0,
			"expirationTime": 1000003820,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	z.InsertMomentumsTo(60*6 + 2)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 187200000000,
	"qsrAmount": 500000000000
}`)
}

// Add LIQ1 token tuple (min: 1000, percentage: 70% znn, 30% qsr)
// Add LIQ2 token tuple (min: 2000, percentage: 30% znn, 70% qsr)
// Register an entry for User 1 (token: LIQ1, amount: 10*10^8)
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8)
// Rewards for znn: 70% * 1872*10^8 = 13104*10^7 znn, 30% * 5000*10^8 = 1500*10^8 qsr
// Rewards for qsr: 30% * 1872*10^8 = 5616*10^7 znn, 70% * 5000*10^8 = 3500*10^8 qsr
// Rewards for first entry (token: LIQ1, amount: 10*10^8) -> 100% * 13104*10^7 znn, 100% * 1500*10^8 qsr
// Minted ZTS (token: znn, amount: 5616*10^7)
// Minted ZTS (token: qsr, amount: 3500*10^8)
func TestLiquidity_LiquidityStakeAndUpdate3(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}"
t=2001-09-09T01:49:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+30000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:49:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+30000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:50:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995047669 BlockReward:+9916666627 TotalReward:+21911714296 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099833333333}" total-weight=2499833333333 self-weight=2099833333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=7000 qsr-percentage=3000 znn-rewards=131040000000 qsr-rewards=150000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=3000 qsr-percentage=7000 znn-rewards=56160000000 qsr-rewards=350000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=131040000000 qsr-amount=150000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=56160000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=350000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19556160000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=56160000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+180900000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=350000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
`)
	sporkAPI := embedded.NewSporkApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address
	issueMultipleTokensSetup(t, z)
	customZts := make([]string, 0)
	for _, zts := range tokens {
		customZts = append(customZts, zts.String())
	}
	znnPercentages := []uint32{
		uint32(7000),
		uint32(3000),
	}
	qsrPercentages := []uint32{
		uint32(3000),
		uint32(7000),
	}
	minAmounts := []*big.Int{
		big.NewInt(1000),
		big.NewInt(2000),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			znnPercentages,
			qsrPercentages,
			minAmounts,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"isHalted": false,
	"znnReward": 0,
	"qsrReward": 0,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 7000,
			"qsrPercentage": 3000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 3000,
			"qsrPercentage": 7000,
			"minAmount": 2000
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000003800,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	z.InsertMomentumsTo(60*6 + 2)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 131040000000,
	"qsrAmount": 150000000000
}`)
}

// Add LIQ1 token tuple (min: 1000, percentage: 70% znn, 30% qsr)
// Add LIQ2 token tuple (min: 2000, percentage: 30% znn, 70% qsr)
// Register an entry for User 1 (token: LIQ1, amount: 10*10^8)
// Register an entry for User 1 (token: LIQ1, amount: 10*10^8)
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8)
// Rewards for znn: 70% * 1872*10^8 = 13104*10^7 znn, 30% * 5000*10^8 = 1500*10^8 qsr
// Rewards for qsr: 30% * 1872*10^8 = 5616*10^7 znn, 70% * 5000*10^8 = 3500*10^8 qsr
// Rewards for first entry (token: LIQ1, amount: 10*10^8) -> 50% * 13104*10^7 = 6552 * 10^7 znn, 50% * 1500*10^8 = 750 * 10^8 qsr
// Rewards for second entry (token: LIQ1, amount: 10*10^8) -> 50% * 13104*10^7 = 6552 * 10^7 znn, 50% * 1500*10^8 = 750 * 10^8 qsr
// Minted ZTS (token: znn, amount: 5616*10^7)
// Minted ZTS (token: qsr, amount: 3500*10^8)
// Total rewards: 131039999999 + 56160000001 = 1872 * 10^8 znn, 149999999999 + 350000000001 = 5000 * 10^8 qsr
func TestLiquidity_LiquidityStakeAndUpdate4(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}"
t=2001-09-09T01:49:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+30000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:49:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+30000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:50:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T01:50:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995047669 BlockReward:+9916666627 TotalReward:+21911714296 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099833333333}" total-weight=2499833333333 self-weight=2099833333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=7000 qsr-percentage=3000 znn-rewards=131040000000 qsr-rewards=150000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=3000 qsr-percentage=7000 znn-rewards=56160000000 qsr-rewards=350000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=65326725663 qsr-amount=74778761061
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=65713274336 qsr-amount=75221238938
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=56160000001
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=350000000001
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19556160000001 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=56160000001 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+180900000000001 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=350000000001 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=56160000001
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=350000000001
`)
	sporkAPI := embedded.NewSporkApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address
	issueMultipleTokensSetup(t, z)
	customZts := make([]string, 0)
	for _, zts := range tokens {
		customZts = append(customZts, zts.String())
	}
	znnPercentages := []uint32{
		uint32(7000),
		uint32(3000),
	}
	qsrPercentages := []uint32{
		uint32(3000),
		uint32(7000),
	}
	minAmounts := []*big.Int{
		big.NewInt(1000),
		big.NewInt(2000),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			znnPercentages,
			qsrPercentages,
			minAmounts,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"isHalted": false,
	"znnReward": 0,
	"qsrReward": 0,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 7000,
			"qsrPercentage": 3000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 3000,
			"qsrPercentage": 7000,
			"minAmount": 2000
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000003800,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 280*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": 2000000000,
	"totalWeightedAmount": 2000000000,
	"count": 2,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000003800,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		},
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000220,
			"revokeTime": 0,
			"expirationTime": 1000003820,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	z.InsertMomentumsTo(60*6 + 2)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 131039999999,
	"qsrAmount": 149999999999
}`)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 56160000001)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 350000000001)
}

// Add LIQ1 token tuple (min: 1000, percentage: 70% znn, 30% qsr)
// Add LIQ2 token tuple (min: 2000, percentage: 30% znn, 70% qsr)
// Register an entry for User 1 (token: LIQ1, amount: 10*10^8)
// Register an entry for User 1 (token: LIQ1, amount: 30*10^8)
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8)
// Rewards for znn: 70% * 1872*10^8 = 13104*10^7 znn, 30% * 5000*10^8 = 1500*10^8 qsr
// Rewards for qsr: 30% * 1872*10^8 = 5616*10^7 znn, 70% * 5000*10^8 = 3500*10^8 qsr
// Rewards for first entry (token: LIQ1, amount: 10*10^8, id: fa43edc03a892a5192a55ad8311a382b861a0f7cce8e0bc769b0f821ee65a84e) -> 25% * 13104*10^7 = 3276 * 10^7 znn, 25% * 1500*10^8 = 375 * 10^8 qsr
// Rewards for second entry (token: LIQ1, amount: 30*10^8, id: 021393b4e4e76738bfc5b5534b04e4a185b1cd80ac7f58d48d00250ae6e3d4c1) -> 75% * 13104*10^7 = 9828 * 10^7 znn, 75% * 1500*10^8 = 1125 * 10^8 qsr
// Minted ZTS (token: znn, amount: 5616*10^7)
// Minted ZTS (token: qsr, amount: 3500*10^8)
// Total rewards: 131039999999 + 56160000001 = 1872 * 10^8 znn, 149999999999 + 350000000001 = 5000 * 10^8 qsr
func TestLiquidity_LiquidityStakeAndUpdate5(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}"
t=2001-09-09T01:49:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+30000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:49:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+30000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:50:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=fd7301e6f38322fe0a2de52729b0d4515e5750bf515e14a08854423c3866d777 owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T01:50:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=260b38a0c7a2899d59baa6c19b81abae0eaa05b074789de8af293022e2b3772a owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=3000000000 weighted-amount=3000000000 duration-in-days=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995047669 BlockReward:+9916666627 TotalReward:+21911714296 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099833333333}" total-weight=2499833333333 self-weight=2099833333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=7000 qsr-percentage=3000 znn-rewards=131040000000 qsr-rewards=150000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=3000 qsr-percentage=7000 znn-rewards=56160000000 qsr-rewards=350000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=260b38a0c7a2899d59baa6c19b81abae0eaa05b074789de8af293022e2b3772a stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=98134830132 qsr-amount=112333825701
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=fd7301e6f38322fe0a2de52729b0d4515e5750bf515e14a08854423c3866d777 stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=32905169867 qsr-amount=37666174298
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=56160000001
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=350000000001
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19556160000001 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=56160000001 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+180900000000001 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=350000000001 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=56160000001
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=350000000001
`)
	sporkAPI := embedded.NewSporkApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address
	issueMultipleTokensSetup(t, z)
	customZts := make([]string, 0)
	for _, zts := range tokens {
		customZts = append(customZts, zts.String())
	}
	znnPercentages := []uint32{
		uint32(7000),
		uint32(3000),
	}
	qsrPercentages := []uint32{
		uint32(3000),
		uint32(7000),
	}
	minAmounts := []*big.Int{
		big.NewInt(1000),
		big.NewInt(2000),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			znnPercentages,
			qsrPercentages,
			minAmounts,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"isHalted": false,
	"znnReward": 0,
	"qsrReward": 0,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 7000,
			"qsrPercentage": 3000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 3000,
			"qsrPercentage": 7000,
			"minAmount": 2000
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000003800,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(30 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 260*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": 4000000000,
	"totalWeightedAmount": 4000000000,
	"count": 2,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000003800,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		},
		{
			"amount": 3000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 3000000000,
			"startTime": 1000000220,
			"revokeTime": 0,
			"expirationTime": 1000003820,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	z.InsertMomentumsTo(60*6 + 2)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 131039999999,
	"qsrAmount": 149999999999
}`)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 56160000001)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 350000000001)
}

// Add LIQ1 token tuple (min: 1000, percentage: 70% znn, 30% qsr)
// Add LIQ2 token tuple (min: 2000, percentage: 30% znn, 70% qsr)
// Register an entry for User 1 (token: LIQ1, amount: 10*10^8)
// Register an entry for User 2 (token: LIQ1, amount: 10*10^8)
// Register an entry for User 3 (token: LIQ1, amount: 10*10^8)
// Register an entry for User 4 (token: LIQ1, amount: 10*10^8)
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8)
// Rewards for znn: 70% * 1872*10^8 = 13104*10^7 znn, 30% * 5000*10^8 = 1500*10^8 qsr
// Rewards for qsr: 30% * 1872*10^8 = 5616*10^7 znn, 70% * 5000*10^8 = 3500*10^8 qsr
// Rewards for first entry (token: LIQ1, amount: 10*10^8, User1) -> 25% * 13104*10^7 = 3276 * 10^7 znn, 25% * 1500*10^8 = 375 * 10^8 qsr
// Rewards for first entry (token: LIQ1, amount: 10*10^8, User2) -> 25% * 13104*10^7 = 3276 * 10^7 znn, 25% * 1500*10^8 = 375 * 10^8 qsr
// Rewards for first entry (token: LIQ1, amount: 10*10^8, User3) -> 25% * 13104*10^7 = 3276 * 10^7 znn, 25% * 1500*10^8 = 375 * 10^8 qsr
// Rewards for first entry (token: LIQ1, amount: 10*10^8, User4) -> 25% * 13104*10^7 = 3276 * 10^7 znn, 25% * 1500*10^8 = 375 * 10^8 qsr
// Minted ZTS (token: znn, amount: 5616*10^7)
// Minted ZTS (token: qsr, amount: 3500*10^8)
// Total rewards: 33498947368 + 32710736842 + 32513684210 + 32316631578 + 56160000002 = 1872 * 10^8 znn,
//				  38345864661 + 37443609022 + 37218045112 + 36992481203 + 350000000002 = 5000 * 10^8 qsr
func TestLiquidity_LiquidityStakeAndUpdate6(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:c6a597f757168bd5c9fddf52b16b3bf38e2ef781fb8edeea1bf2ae0d3225230d Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}"
t=2001-09-09T01:49:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+30000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:49:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+30000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:50:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=fd7301e6f38322fe0a2de52729b0d4515e5750bf515e14a08854423c3866d777 owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T01:50:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+50000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=20000000000 to-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:50:40+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+80000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=30000000000 to-address=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac
t=2001-09-09T01:51:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+100000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=20000000000 to-address=z1qraz4ermhhua89a0h0gxxan4lnzrfutgs6xxe2
t=2001-09-09T01:51:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=7b4fbe44af9d8312cb86794f4fa8f286d929e0670a4c0a5965fc3355d69969d7 owner=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T01:51:40+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=3226640df606e476aed56fa5b4f663634ed4cd119759d798e8fa39b4c2ce7394 owner=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T01:52:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=03af96dc4fa5cbf17865bbf613ddf0151b6092c5c1c402db409648eb2d2301eb owner=z1qraz4ermhhua89a0h0gxxan4lnzrfutgs6xxe2 amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995047669 BlockReward:+9916666627 TotalReward:+21911714296 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099833333333}" total-weight=2499833333333 self-weight=2099833333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=7000 qsr-percentage=3000 znn-rewards=131040000000 qsr-rewards=150000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=3000 qsr-percentage=7000 znn-rewards=56160000000 qsr-rewards=350000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=fd7301e6f38322fe0a2de52729b0d4515e5750bf515e14a08854423c3866d777 stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=33498947368 qsr-amount=38345864661
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=3226640df606e476aed56fa5b4f663634ed4cd119759d798e8fa39b4c2ce7394 stake-address=z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=32513684210 qsr-amount=37218045112
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=7b4fbe44af9d8312cb86794f4fa8f286d929e0670a4c0a5965fc3355d69969d7 stake-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=32710736842 qsr-amount=37443609022
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=03af96dc4fa5cbf17865bbf613ddf0151b6092c5c1c402db409648eb2d2301eb stake-address=z1qraz4ermhhua89a0h0gxxan4lnzrfutgs6xxe2 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=32316631578 qsr-amount=36992481203
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=56160000002
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=350000000002
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19556160000002 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=56160000002 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+180900000000002 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=350000000002 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=56160000002
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=350000000002
`)
	sporkAPI := embedded.NewSporkApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address
	issueMultipleTokensSetup(t, z)
	customZts := make([]string, 0)
	for _, zts := range tokens {
		customZts = append(customZts, zts.String())
	}
	znnPercentages := []uint32{
		uint32(7000),
		uint32(3000),
	}
	qsrPercentages := []uint32{
		uint32(3000),
		uint32(7000),
	}
	minAmounts := []*big.Int{
		big.NewInt(1000),
		big.NewInt(2000),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			znnPercentages,
			qsrPercentages,
			minAmounts,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"isHalted": false,
	"znnReward": 0,
	"qsrReward": 0,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 7000,
			"qsrPercentage": 3000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 3000,
			"qsrPercentage": 7000,
			"minAmount": 2000
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000003800,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, tokens[0], big.NewInt(200*g.Zexp), g.User2.Address),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, tokens[0], big.NewInt(300*g.Zexp), g.User3.Address),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	autoreceive(t, z, g.User2.Address)
	autoreceive(t, z, g.User3.Address)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, tokens[0], big.NewInt(200*g.Zexp), g.User4.Address),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	autoreceive(t, z, g.User4.Address)

	z.ExpectBalance(g.User2.Address, tokens[0], 200*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User2.Address, tokens[0], 190*g.Zexp)

	z.ExpectBalance(g.User3.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User3.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User3.Address, tokens[0], 290*g.Zexp)

	z.ExpectBalance(g.User4.Address, tokens[0], 200*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User4.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User4.Address, tokens[0], 190*g.Zexp)

	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000003800,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User2.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000280,
			"revokeTime": 0,
			"expirationTime": 1000003880,
			"stakeAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User3.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000300,
			"revokeTime": 0,
			"expirationTime": 1000003900,
			"stakeAddress": "z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User4.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000320,
			"revokeTime": 0,
			"expirationTime": 1000003920,
			"stakeAddress": "z1qraz4ermhhua89a0h0gxxan4lnzrfutgs6xxe2",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	z.InsertMomentumsTo(60*6 + 2)

	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 33498947368,
	"qsrAmount": 38345864661
}`)
	common.Json(liquidityAPI.GetUncollectedReward(g.User2.Address)).Equals(t, `
{
	"address": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"znnAmount": 32710736842,
	"qsrAmount": 37443609022
}`)
	common.Json(liquidityAPI.GetUncollectedReward(g.User3.Address)).Equals(t, `
{
	"address": "z1qrs2lpccnsneglhnnfwvlsj0qncnxjnwlfmjac",
	"znnAmount": 32513684210,
	"qsrAmount": 37218045112
}`)
	common.Json(liquidityAPI.GetUncollectedReward(g.User4.Address)).Equals(t, `
{
	"address": "z1qraz4ermhhua89a0h0gxxan4lnzrfutgs6xxe2",
	"znnAmount": 32316631578,
	"qsrAmount": 36992481203
}`)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 56160000002)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 350000000002)
}

// Add Znn token tuple (min: 1000, percentage: 70% znn, 30% qsr)
// Add Qsr token tuple (min: 2000, percentage: 30% znn, 70% qsr)
// Register an entry for User 1 (token: znn, amount: 10*10^8)
// Register an entry for User 1 (token: znn, amount: 10*10^8)
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8)
// Rewards for znn: 70% * 1872*10^8 = 13104*10^7 znn, 30% * 5000*10^8 = 1500*10^8 qsr
// Rewards for qsr: 30% * 1872*10^8 = 5616*10^7 znn, 70% * 5000*10^8 = 3500*10^8 qsr
// Rewards for first entry (token: znn, amount: 10*10^8) -> 50% * 13104*10^7 = 6552 * 10^7 znn, 50% * 1500*10^8 = 750 * 10^8 qsr
// Rewards for second entry (token: znn, amount: 10*10^8) -> 50% * 13104*10^7 = 6552 * 10^7 znn, 50% * 1500*10^8 = 750 * 10^8 qsr
// Update for epoch 1 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8)
// Rewards for znn: 70% * 1872*10^8 = 13104*10^7 znn, 30% * 5000*10^8 = 1500*10^8 qsr
// Rewards for qsr: 30% * 1872*10^8 = 5616*10^7 znn, 70% * 5000*10^8 = 3500*10^8 qsr
// Rewards for first entry (token: znn, amount: 10*10^8) -> 50% * 13104*10^7 = 6552 * 10^7 znn, 50% * 1500*10^8 = 750 * 10^8 qsr
// Rewards for second entry (token: znn, amount: 10*10^8) -> 50% * 13104*10^7 = 6552 * 10^7 znn, 50% * 1500*10^8 = 750 * 10^8 qsr
func TestLiquidity_LiquidityStakeAndDoubleUpdate1(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}"
t=2001-09-09T01:49:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+30000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:49:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+30000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:50:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T01:50:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995047669 BlockReward:+9916666627 TotalReward:+21911714296 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099833333333}" total-weight=2499833333333 self-weight=2099833333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=7000 qsr-percentage=3000 znn-rewards=131040000000 qsr-rewards=150000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=3000 qsr-percentage=7000 znn-rewards=56160000000 qsr-rewards=350000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=65326725663 qsr-amount=74778761061
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=65713274336 qsr-amount=75221238938
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=56160000001
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=350000000001
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19556160000001 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=56160000001 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+180900000000001 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=350000000001 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=56160000001
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=350000000001
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12095815665 BlockReward:+9999999960 TotalReward:+22095815625 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099800000000}" total-weight=2499800000000 self-weight=2099800000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152092167 BlockReward:+9999999960 TotalReward:+11152092127 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499800000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152092167 BlockReward:+9999999960 TotalReward:+11152092127 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499800000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=1 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=7000 qsr-percentage=3000 znn-rewards=131040000000 qsr-rewards=150000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=3000 qsr-percentage=7000 znn-rewards=56160000000 qsr-rewards=350000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=65520000000 qsr-amount=75000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=65520000000 qsr-amount=75000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=1 znnReward=56160000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=1 qsrReward=350000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=722 last-update-height=361
`)
	sporkAPI := embedded.NewSporkApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address
	issueMultipleTokensSetup(t, z)
	customZts := make([]string, 0)
	for _, zts := range tokens {
		customZts = append(customZts, zts.String())
	}
	znnPercentages := []uint32{
		uint32(7000),
		uint32(3000),
	}
	qsrPercentages := []uint32{
		uint32(3000),
		uint32(7000),
	}
	minAmounts := []*big.Int{
		big.NewInt(1000),
		big.NewInt(2000),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			znnPercentages,
			qsrPercentages,
			minAmounts,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"isHalted": false,
	"znnReward": 0,
	"qsrReward": 0,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 7000,
			"qsrPercentage": 3000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 3000,
			"qsrPercentage": 7000,
			"minAmount": 2000
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000003800,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 280*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).HideHashes().Equals(t, `
{
	"totalAmount": 2000000000,
	"totalWeightedAmount": 2000000000,
	"count": 2,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000003800,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		},
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000220,
			"revokeTime": 0,
			"expirationTime": 1000003820,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
		}
	]
}`)
	z.InsertMomentumsTo(60*6 + 2)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 131039999999,
	"qsrAmount": 149999999999
}`)
	z.InsertMomentumsTo(60*6*2 + 2)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 262079999999,
	"qsrAmount": 299999999999
}`)
}

// Add Znn token tuple (min: 1000, percentage: 70% znn, 30% qsr)
// Add Qsr token tuple (min: 2000, percentage: 30% znn, 70% qsr)
// Register an entry for User 1 (token: znn, amount: 10*10^8)
// Register an entry for User 2 (token: znn, amount: 10*10^8)
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8)
// Rewards for znn: 70% * 1872*10^8 = 13104*10^7 znn, 30% * 5000*10^8 = 1500*10^8 qsr
// Rewards for qsr: 30% * 1872*10^8 = 5616*10^7 znn, 70% * 5000*10^8 = 3500*10^8 qsr
// Rewards for first entry (token: znn, amount: 10*10^8) -> 50% * 13104*10^7 = 6552 * 10^7 znn, 50% * 1500*10^8 = 750 * 10^8 qsr
// Rewards for second entry (token: znn, amount: 10*10^8) -> 50% * 13104*10^7 = 6552 * 10^7 znn, 50% * 1500*10^8 = 750 * 10^8 qsr
// Update for epoch 1 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8)
// Rewards for znn: 70% * 1872*10^8 = 13104*10^7 znn, 30% * 5000*10^8 = 1500*10^8 qsr
// Rewards for qsr: 30% * 1872*10^8 = 5616*10^7 znn, 70% * 5000*10^8 = 3500*10^8 qsr
// Rewards for first entry (token: znn, amount: 10*10^8) -> 50% * 13104*10^7 = 6552 * 10^7 znn, 50% * 1500*10^8 = 750 * 10^8 qsr
// Rewards for second entry (token: znn, amount: 10*10^8) -> 50% * 13104*10^7 = 6552 * 10^7 znn, 50% * 1500*10^8 = 750 * 10^8 qsr
func TestLiquidity_LiquidityStakeAndDoubleUpdate2(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}"
t=2001-09-09T01:49:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+30000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:49:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+30000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:50:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T01:50:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+50000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=20000000000 to-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:50:40+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995047669 BlockReward:+9916666627 TotalReward:+21911714296 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099833333333}" total-weight=2499833333333 self-weight=2099833333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=7000 qsr-percentage=3000 znn-rewards=131040000000 qsr-rewards=150000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=3000 qsr-percentage=7000 znn-rewards=56160000000 qsr-rewards=350000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=65907692307 qsr-amount=75443786982
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=65132307692 qsr-amount=74556213017
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=56160000001
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=350000000001
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19556160000001 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=56160000001 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+180900000000001 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=350000000001 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=56160000001
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=350000000001
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12095815665 BlockReward:+9999999960 TotalReward:+22095815625 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099800000000}" total-weight=2499800000000 self-weight=2099800000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152092167 BlockReward:+9999999960 TotalReward:+11152092127 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499800000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152092167 BlockReward:+9999999960 TotalReward:+11152092127 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499800000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=1 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=7000 qsr-percentage=3000 znn-rewards=131040000000 qsr-rewards=150000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=3000 qsr-percentage=7000 znn-rewards=56160000000 qsr-rewards=350000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=65520000000 qsr-amount=75000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=65520000000 qsr-amount=75000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=1 znnReward=56160000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=1 qsrReward=350000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=722 last-update-height=361
`)
	sporkAPI := embedded.NewSporkApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address
	issueMultipleTokensSetup(t, z)
	customZts := make([]string, 0)
	for _, zts := range tokens {
		customZts = append(customZts, zts.String())
	}
	znnPercentages := []uint32{
		uint32(7000),
		uint32(3000),
	}
	qsrPercentages := []uint32{
		uint32(3000),
		uint32(7000),
	}
	minAmounts := []*big.Int{
		big.NewInt(1000),
		big.NewInt(2000),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			znnPercentages,
			qsrPercentages,
			minAmounts,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"isHalted": false,
	"znnReward": 0,
	"qsrReward": 0,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 7000,
			"qsrPercentage": 3000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 3000,
			"qsrPercentage": 7000,
			"minAmount": 2000
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000003800,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "fd7301e6f38322fe0a2de52729b0d4515e5750bf515e14a08854423c3866d777"
		}
	]
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, tokens[0], big.NewInt(200*g.Zexp), g.User2.Address),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	autoreceive(t, z, g.User2.Address)
	z.ExpectBalance(g.User2.Address, tokens[0], 200*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User2.Address, tokens[0], 190*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User2.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000240,
			"revokeTime": 0,
			"expirationTime": 1000003840,
			"stakeAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"id": "bf7f7ecf9e00b842a37966817801be9f6dac42b8e19da2631136b3d3ea30ad3a"
		}
	]
}`)
	z.InsertMomentumsTo(60*6 + 2)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 65907692307,
	"qsrAmount": 75443786982
}`)
	common.Json(liquidityAPI.GetUncollectedReward(g.User2.Address)).Equals(t, `
{
	"address": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"znnAmount": 65132307692,
	"qsrAmount": 74556213017
}`)
	z.InsertMomentumsTo(60*6*2 + 2)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 131427692307,
	"qsrAmount": 150443786982
}`)
	common.Json(liquidityAPI.GetUncollectedReward(g.User2.Address)).Equals(t, `
{
	"address": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"znnAmount": 130652307692,
	"qsrAmount": 149556213017
}`)
}

// Add Znn token tuple (min: 1000, percentage: 50% znn, 50% qsr)
// Add Qsr token tuple (min: 2000, percentage: 50% znn, 50% qsr)
// Register an entry for User 1 (token: znn, amount: 10*10^8, period: 2 months)
// Register an entry for User 1 (token: qsr, amount: 10*10^8, period: 2 months)
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8)
// Rewards for znn: 50% * 1872*10^8 = 936*10^8 znn, 50% * 5000*10^8 = 2500*10^8 qsr
// Rewards for qsr: 50% * 1872*10^8 = 936*10^8 znn, 50% * 5000*10^8 = 2500*10^8 qsr
// Rewards for first entry (token: znn, amount: 10*10^8) -> 100% * 936*10^8 znn, 100% * 2500*10^8 qsr
// Rewards for second entry (token: qsr, amount: 10*10^8) -> 100% * 936*10^8 znn, 100% * 2500*10^8 qsr
func TestLiquidity_LiquidityStakeForTwoMonthsAndUpdate1(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}"
t=2001-09-09T01:49:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+30000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:49:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+30000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:50:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=2000000000 duration-in-days=0
t=2001-09-09T01:50:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=2000000000 duration-in-days=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995047669 BlockReward:+9916666627 TotalReward:+21911714296 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099833333333}" total-weight=2499833333333 self-weight=2099833333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1a2ukxhyameh230e58m5x8t znn-amount=93600000000 qsr-amount=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=93600000000 qsr-amount=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
`)
	sporkAPI := embedded.NewSporkApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address
	issueMultipleTokensSetup(t, z)
	customZts := make([]string, 0)
	for _, zts := range tokens {
		customZts = append(customZts, zts.String())
	}
	percentages := []uint32{
		uint32(5000),
		uint32(5000),
	}
	minAmounts := []*big.Int{
		big.NewInt(1000),
		big.NewInt(2000),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			percentages,
			percentages,
			minAmounts,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"isHalted": false,
	"znnReward": 0,
	"qsrReward": 0,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 2000
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, 2*constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 2000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 2000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000007400,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "d96af3fff15f3582487cdbd34a4d0b33350c2db56b875c51460821add2b64e8a"
		}
	]
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, 2*constants.StakeTimeMinSec),
		TokenStandard: tokens[1],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 2000000000,
	"totalWeightedAmount": 4000000000,
	"count": 2,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 2000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000007400,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "d96af3fff15f3582487cdbd34a4d0b33350c2db56b875c51460821add2b64e8a"
		},
		{
			"amount": 1000000000,
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"weightedAmount": 2000000000,
			"startTime": 1000000220,
			"revokeTime": 0,
			"expirationTime": 1000007420,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "33127b933f1cde3370922bbb6afc04ec8ebb830c7dcd4d56509717df2a434d93"
		}
	]
}`)
	z.InsertMomentumsTo(60*6 + 2)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 187200000000,
	"qsrAmount": 500000000000
}`)
}

// Add Znn token tuple (min: 1000, percentage: 50% znn, 50% qsr)
// Add Qsr token tuple (min: 2000, percentage: 50% znn, 50% qsr)
// Register an entry for User 1 (token: znn, amount: 10*10^8, period: 2 months)
// Register an entry for User 1 (token: qsr, amount: 10*10^8, period: 1 months)
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8)
// Rewards for znn: 50% * 1872*10^8 = 936*10^8 znn, 50% * 5000*10^8 = 2500*10^8 qsr
// Rewards for qsr: 50% * 1872*10^8 = 936*10^8 znn, 50% * 5000*10^8 = 2500*10^8 qsr
// Rewards for first entry (token: znn, amount: 10*10^8) -> 100% * 936*10^8 znn, 100% * 2500*10^8 qsr
// Rewards for second entry (token: qsr, amount: 10*10^8) -> 100% * 936*10^8 znn, 100% * 2500*10^8 qsr
func TestLiquidity_LiquidityStakeForTwoMonthsAndUpdate2(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}"
t=2001-09-09T01:49:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+30000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:49:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+30000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:50:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=2000000000 duration-in-days=0
t=2001-09-09T01:50:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995047669 BlockReward:+9916666627 TotalReward:+21911714296 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099833333333}" total-weight=2499833333333 self-weight=2099833333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1a2ukxhyameh230e58m5x8t znn-amount=93600000000 qsr-amount=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=93600000000 qsr-amount=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
`)
	sporkAPI := embedded.NewSporkApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address
	issueMultipleTokensSetup(t, z)
	customZts := make([]string, 0)
	for _, zts := range tokens {
		customZts = append(customZts, zts.String())
	}
	percentages := []uint32{
		uint32(5000),
		uint32(5000),
	}
	minAmounts := []*big.Int{
		big.NewInt(1000),
		big.NewInt(2000),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			percentages,
			percentages,
			minAmounts,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"isHalted": false,
	"znnReward": 0,
	"qsrReward": 0,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 2000
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, 2*constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 2000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 2000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000007400,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "d96af3fff15f3582487cdbd34a4d0b33350c2db56b875c51460821add2b64e8a"
		}
	]
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, 1*constants.StakeTimeMinSec),
		TokenStandard: tokens[1],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 2000000000,
	"totalWeightedAmount": 3000000000,
	"count": 2,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"weightedAmount": 1000000000,
			"startTime": 1000000220,
			"revokeTime": 0,
			"expirationTime": 1000003820,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "4619d3dd21feba3b6e5eedaba2c9b2e5f857d9b3cd4ef8bfc62709dbc3ff78e2"
		},
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 2000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000007400,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "d96af3fff15f3582487cdbd34a4d0b33350c2db56b875c51460821add2b64e8a"
		}
	]
}`)
	z.InsertMomentumsTo(60*6 + 2)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 187200000000,
	"qsrAmount": 500000000000
}`)
}

// Add Znn token tuple (min: 1000, percentage: 50% znn, 50% qsr)
// Add Qsr token tuple (min: 2000, percentage: 50% znn, 50% qsr)
// Register an entry for User 1 (token: znn, amount: 10*10^8, period: 2 months)
// Register an entry for User 1 (token: qsr, amount: 10*10^8, period: 1 month)
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8)
// Rewards for znn: 50% * 1872*10^8 = 936*10^8 znn, 50% * 5000*10^8 = 2500*10^8 qsr
// Rewards for qsr: 50% * 1872*10^8 = 936*10^8 znn, 50% * 5000*10^8 = 2500*10^8 qsr
// Rewards for first entry (token: znn, amount: 10*10^8) -> 100% * 936*10^8 znn, 100% * 2500*10^8 qsr
// Rewards for second entry (token: qsr, amount: 10*10^8) -> 100% * 936*10^8 znn, 100% * 2500*10^8 qsr
func TestLiquidity_LiquidityStakeForTwoMonthsAndUpdate3(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}"
t=2001-09-09T01:49:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+30000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:49:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+30000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:50:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=2000000000 duration-in-days=0
t=2001-09-09T01:50:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995047669 BlockReward:+9916666627 TotalReward:+21911714296 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099833333333}" total-weight=2499833333333 self-weight=2099833333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1a2ukxhyameh230e58m5x8t znn-amount=93600000000 qsr-amount=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=93600000000 qsr-amount=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
`)
	sporkAPI := embedded.NewSporkApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address
	issueMultipleTokensSetup(t, z)
	customZts := make([]string, 0)
	for _, zts := range tokens {
		customZts = append(customZts, zts.String())
	}
	percentages := []uint32{
		uint32(5000),
		uint32(5000),
	}
	minAmounts := []*big.Int{
		big.NewInt(1000),
		big.NewInt(2000),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			percentages,
			percentages,
			minAmounts,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"isHalted": false,
	"znnReward": 0,
	"qsrReward": 0,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 2000
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, 2*constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 2000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 2000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000007400,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "d96af3fff15f3582487cdbd34a4d0b33350c2db56b875c51460821add2b64e8a"
		}
	]
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, 1*constants.StakeTimeMinSec),
		TokenStandard: tokens[1],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 2000000000,
	"totalWeightedAmount": 3000000000,
	"count": 2,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"weightedAmount": 1000000000,
			"startTime": 1000000220,
			"revokeTime": 0,
			"expirationTime": 1000003820,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "4619d3dd21feba3b6e5eedaba2c9b2e5f857d9b3cd4ef8bfc62709dbc3ff78e2"
		},
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 2000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000007400,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "d96af3fff15f3582487cdbd34a4d0b33350c2db56b875c51460821add2b64e8a"
		}
	]
}`)
	z.InsertMomentumsTo(60*6 + 2)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 187200000000,
	"qsrAmount": 500000000000
}`)
}

// Add Znn token tuple (min: 1000, percentage: 50% znn, 50% qsr)
// Add Qsr token tuple (min: 2000, percentage: 50% znn, 50% qsr)
// Register an entry for User 1 (token: znn, amount: 10*10^8, period: 3 months)
// Register an entry for User 2 (token: znn, amount: 10*10^8, period: 1 month)
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8)
// Rewards for znn: 50% * 1872*10^8 = 936*10^8 znn, 50% * 5000*10^8 = 2500*10^8 qsr
// Rewards for qsr: 50% * 1872*10^8 = 936*10^8 znn, 50% * 5000*10^8 = 2500*10^8 qsr
// Rewards for first entry (token: znn, amount: 10*10^8, weightedAmount: 3*10*10^8) -> 75% * 936*10^8 znn, 75% * 2500*10^8 qsr
// Rewards for second entry (token: znn, amount: 10*10^8, weightedAmount: 10*10^8) -> 25% * 936*10^8 znn, 25% * 2500*10^8 qsr
// Mint ZTS (token: znn, amount: 93600000001)
// Mint ZTS (token qsr, amount: 250000000001)
func TestLiquidity_LiquidityStakeForMultipleMonthsAndUpdate1(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}"
t=2001-09-09T01:49:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+30000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:49:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+30000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:50:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=3000000000 duration-in-days=0
t=2001-09-09T01:50:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+50000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=20000000000 to-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T01:50:40+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995047669 BlockReward:+9916666627 TotalReward:+21911714296 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099833333333}" total-weight=2499833333333 self-weight=2099833333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=70407079646 qsr-amount=188053097345
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=23192920353 qsr-amount=61946902654
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=93600000001
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=250000000001
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19593600000001 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=93600000001 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+180800000000001 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=250000000001 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
`)
	sporkAPI := embedded.NewSporkApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address
	issueMultipleTokensSetup(t, z)
	customZts := make([]string, 0)
	for _, zts := range tokens {
		customZts = append(customZts, zts.String())
	}
	percentages := []uint32{
		uint32(5000),
		uint32(5000),
	}
	minAmounts := []*big.Int{
		big.NewInt(1000),
		big.NewInt(2000),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			percentages,
			percentages,
			minAmounts,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"isHalted": false,
	"znnReward": 0,
	"qsrReward": 0,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 2000
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, 3*constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 3000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 3000000000,
			"startTime": 1000000200,
			"revokeTime": 0,
			"expirationTime": 1000011000,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "0e2257f0df6548c4f14869e1f8626a27244cabe1a8e6308e4153ac8a45c73d55"
		}
	]
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, tokens[0], big.NewInt(200*g.Zexp), g.User2.Address),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	autoreceive(t, z, g.User2.Address)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, 1*constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User2.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000240,
			"revokeTime": 0,
			"expirationTime": 1000003840,
			"stakeAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"id": "6aa3ba0df80a50f22c37596507ef63e4a7b54f7751ed508e2f2fc578ee15a915"
		}
	]
}`)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 0)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 0)
	z.ExpectBalance(types.LiquidityContract, tokens[0], 20*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, tokens[1], 0*g.Zexp)
	z.InsertMomentumsTo(60*6 + 2)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 70407079646,
	"qsrAmount": 188053097345
}`)
	common.Json(liquidityAPI.GetUncollectedReward(g.User2.Address)).Equals(t, `
{
	"address": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"znnAmount": 23192920353,
	"qsrAmount": 61946902654
}`)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 0)
	z.ExpectBalance(types.LiquidityContract, tokens[0], 2000000000)
	z.ExpectBalance(types.LiquidityContract, tokens[1], 0)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 0)
}

func TestLiquidity_SimpleDonate(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=100
t=2001-09-09T01:46:50+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=100
t=2001-09-09T01:48:20+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:48:30+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:18}"
t=2001-09-09T01:48:40+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=100
t=2001-09-09T01:48:40+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=100
`)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 0)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 0)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABICommon.PackMethodPanic(definition.DonateMethodName),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        common.Big100,
	}).Error(t, nil)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABICommon.PackMethodPanic(definition.DonateMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        common.Big100,
	}).Error(t, nil)

	z.InsertMomentumsTo(10)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 100)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 100)

	sporkAPI := embedded.NewSporkApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABICommon.PackMethodPanic(definition.DonateMethodName),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        common.Big100,
	}).Error(t, nil)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABICommon.PackMethodPanic(definition.DonateMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        common.Big100,
	}).Error(t, nil)

	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 200)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 200)

}

func TestLiquidity_SimpleUpdate(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995200000 BlockReward:+9916666627 TotalReward:+21911866627 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2100000000000}" total-weight=2500000000000 self-weight=2100000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152000000 BlockReward:+9999999960 TotalReward:+11151999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152000000 BlockReward:+9999999960 TotalReward:+11151999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=0 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T02:47:10+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:370}"
t=2001-09-09T02:48:20+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=371 last-update-height=0
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12096000000 BlockReward:+9999999960 TotalReward:+22095999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2100000000000}" total-weight=2500000000000 self-weight=2100000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152000000 BlockReward:+9999999960 TotalReward:+11151999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152000000 BlockReward:+9999999960 TotalReward:+11151999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=1 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=1 znnReward=187200000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=1 qsrReward=500000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=2
`)

	z.InsertMomentumsTo(60*6 + 2)

	sporkAPI := embedded.NewSporkApi(z)
	//liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	z.InsertMomentumsTo(60*6*2 + 2)
}

// Add Znn token tuple (min: 1000, percentage: 50% znn, 50% qsr)
// Add Qsr token tuple (min: 2000, percentage: 50% znn, 50% qsr)
// Set 20*10^8 znn reward from contract
// Set 12*10^8 qsr reward from contract
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8) -> Send to contract
// Register an entry for User 1 (token: znn, amount: 10*10^8)
// Register an entry for User 1 (token: qsr, amount: 10*10^8)
// Update for epoch 1 (znn-amount: 1872*10^8 + 20*10^8, qsr-amount: 5000*10^8 + 12*10^8)
// Rewards for znn: 50% * 1892*10^8 = 946*10^8 znn, 50% * 5012*10^8 = 2506*10^8 qsr
// Rewards for qsr: 50% * 1892*10^8 = 946*10^8 znn, 50% * 5012*10^8 = 2506*10^8 qsr
// Rewards for first entry (token: znn, amount: 10*10^8) -> 100% * 946*10^8 znn, 100% * 2506*10^8 qsr
// Rewards for second entry (token: qsr, amount: 10*10^8) -> 100% * 946*10^8 znn, 100% * 2506*10^8 qsr
func TestLiquidity_CollectReward1(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}"
t=2001-09-09T01:49:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+30000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:49:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+30000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995047669 BlockReward:+9916666627 TotalReward:+21911714296 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099833333333}" total-weight=2499833333333 self-weight=2099833333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=187200000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T02:47:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T02:47:40+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12095815665 BlockReward:+9999999960 TotalReward:+22095815625 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099800000000}" total-weight=2499800000000 self-weight=2099800000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152092167 BlockReward:+9999999960 TotalReward:+11152092127 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499800000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152092167 BlockReward:+9999999960 TotalReward:+11152092127 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499800000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="distribute znn rewards from the liquidity contract" module=embedded contract=liquidity znn-amount=2000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="distribute qsr rewards from the liquidity contract" module=embedded contract=liquidity qsr-amount=1200000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=1 znn-total-amount=189200000000 qsr-total-amount=501200000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=5000 qsr-percentage=5000 znn-rewards=94600000000 qsr-rewards=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=5000 qsr-percentage=5000 znn-rewards=94600000000 qsr-rewards=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=94600000000 qsr-amount=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1a2ukxhyameh230e58m5x8t znn-amount=94600000000 qsr-amount=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=722 last-update-height=361
t=2001-09-09T03:47:00+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19685200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=2000000000
t=2001-09-09T03:47:00+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181048800000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" burned-amount=1200000000
t=2001-09-09T03:48:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=189200000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:48:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=501200000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)
	sporkAPI := embedded.NewSporkApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address
	issueMultipleTokensSetup(t, z)
	customZts := make([]string, 0)
	for _, zts := range tokens {
		customZts = append(customZts, zts.String())
	}
	percentages := []uint32{
		uint32(5000),
		uint32(5000),
	}
	minAmounts := []*big.Int{
		big.NewInt(1000),
		big.NewInt(2000),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			percentages,
			percentages,
			minAmounts,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetAdditionalRewardMethodName,
			big.NewInt(20*g.Zexp),
			big.NewInt(12*g.Zexp),
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.InsertMomentumsTo(60*6 + 4)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1872*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 5000*g.Zexp)

	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"isHalted": false,
	"znnReward": 2000000000,
	"qsrReward": 1200000000,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 2000
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000003640,
			"revokeTime": 0,
			"expirationTime": 1000007240,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "1da162c0e83fec52dbb41aeee324a8c432c25d76f6990780401ef939fa1449ee"
		}
	]
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[1],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 2000000000,
	"totalWeightedAmount": 2000000000,
	"count": 2,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000003640,
			"revokeTime": 0,
			"expirationTime": 1000007240,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "1da162c0e83fec52dbb41aeee324a8c432c25d76f6990780401ef939fa1449ee"
		},
		{
			"amount": 1000000000,
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"weightedAmount": 1000000000,
			"startTime": 1000003660,
			"revokeTime": 0,
			"expirationTime": 1000007260,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "47da3f6fbca59d5812d692d5dbd73c131c04d63cd09d4ad48dc46a64d770ae96"
		}
	]
}`)
	z.InsertMomentumsTo(60*6*2 + 8)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 189200000000,
	"qsrAmount": 501200000000
}`)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1852*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 4988*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11998*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data:      definition.ABILiquidity.PackMethodPanic(definition.CollectRewardMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 0,
	"qsrAmount": 0
}`)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1852*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 4988*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11998*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)
	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 13890*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 125012*g.Zexp)
}

// Add Znn token tuple (min: 1000, percentage: 50% znn, 50% qsr)
// Add Qsr token tuple (min: 2000, percentage: 50% znn, 50% qsr)
// Set 50*10^8 znn reward from contract
// Set 12*10^8 qsr reward from contract
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8) -> Send to contract
// Register an entry for User 1 (token: znn, amount: 10*10^8)
// Register an entry for User 2 (token: qsr, amount: 10*10^8)
// Update for epoch 1 (znn-amount: 1872*10^8 + 50*10^8, qsr-amount: 5000*10^8 + 12*10^8)
// Rewards for znn: 50% * 1992*10^8 = 961*10^8 znn, 50% * 5012*10^8 = 2506*10^8 qsr
// Rewards for qsr: 50% * 1992*10^8 = 961*10^8 znn, 50% * 5012*10^8 = 2506*10^8 qsr
// Rewards for first entry (token: znn, amount: 10*10^8, User1) -> 100% * 961*10^8 znn, 100% * 2506*10^8 qsr
// Rewards for second entry (token: qsr, amount: 10*10^8, User2) -> 100% * 961*10^8 znn, 100% * 2506*10^8 qsr
func TestLiquidity_CollectReward2(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}"
t=2001-09-09T01:49:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+30000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:49:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+30000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995047669 BlockReward:+9916666627 TotalReward:+21911714296 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099833333333}" total-weight=2499833333333 self-weight=2099833333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=187200000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T02:47:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T02:47:40+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+50000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T02:48:00+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12095815665 BlockReward:+9999999960 TotalReward:+22095815625 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099800000000}" total-weight=2499800000000 self-weight=2099800000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152092167 BlockReward:+9999999960 TotalReward:+11152092127 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499800000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152092167 BlockReward:+9999999960 TotalReward:+11152092127 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499800000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="distribute znn rewards from the liquidity contract" module=embedded contract=liquidity znn-amount=5000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="distribute qsr rewards from the liquidity contract" module=embedded contract=liquidity qsr-amount=1200000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=1 znn-total-amount=192200000000 qsr-total-amount=501200000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=5000 qsr-percentage=5000 znn-rewards=96100000000 qsr-rewards=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=5000 qsr-percentage=5000 znn-rewards=96100000000 qsr-rewards=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=96100000000 qsr-amount=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx token-standard=zts1a2ukxhyameh230e58m5x8t znn-amount=96100000000 qsr-amount=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=722 last-update-height=361
t=2001-09-09T03:47:00+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19682200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=5000000000
t=2001-09-09T03:47:00+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181048800000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" burned-amount=1200000000
t=2001-09-09T03:48:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19778300000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=96100000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:48:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181299400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=250600000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:48:40+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=96100000000 to-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
t=2001-09-09T03:48:40+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=250600000000 to-address=z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx
`)
	sporkAPI := embedded.NewSporkApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address
	issueMultipleTokensSetup(t, z)
	customZts := make([]string, 0)
	for _, zts := range tokens {
		customZts = append(customZts, zts.String())
	}
	percentages := []uint32{
		uint32(5000),
		uint32(5000),
	}
	minAmounts := []*big.Int{
		big.NewInt(1000),
		big.NewInt(2000),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			percentages,
			percentages,
			minAmounts,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetAdditionalRewardMethodName,
			big.NewInt(50*g.Zexp),
			big.NewInt(12*g.Zexp),
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.InsertMomentumsTo(60*6 + 4)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1872*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 5000*g.Zexp)

	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"isHalted": false,
	"znnReward": 5000000000,
	"qsrReward": 1200000000,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 2000
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11998*g.Zexp)
	z.ExpectBalance(g.User1.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11998*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000003640,
			"revokeTime": 0,
			"expirationTime": 1000007240,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "5b342afdae1ab23533775d59ca1a446fdd79dda936134355f6365d0defec609b"
		}
	]
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.TokenContract,
		Data:      definition.ABIToken.PackMethodPanic(definition.MintMethodName, tokens[1], big.NewInt(200*g.Zexp), g.User2.Address),
	}).Error(t, nil)
	z.InsertNewMomentum() // cemented send block
	z.InsertNewMomentum() // cemented token-receive-block
	autoreceive(t, z, g.User2.Address)
	z.ExpectBalance(g.User2.Address, tokens[1], 200*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User2.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[1],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User2.Address, tokens[1], 190*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User2.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"weightedAmount": 1000000000,
			"startTime": 1000003680,
			"revokeTime": 0,
			"expirationTime": 1000007280,
			"stakeAddress": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
			"id": "4e03d3b1ebb307ff21ebb425e437e943229af26a2eaa5f4ecc21fa1f34630f6e"
		}
	]
}`)
	z.InsertMomentumsTo(60*6*2 + 8)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 96100000000,
	"qsrAmount": 250600000000
}`)
	common.Json(liquidityAPI.GetUncollectedReward(g.User2.Address)).Equals(t, `
{
	"address": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"znnAmount": 96100000000,
	"qsrAmount": 250600000000
}`)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1822*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 4988*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, tokens[0], 10*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, tokens[1], 10*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11998*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	z.ExpectBalance(g.User1.Address, tokens[1], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data:      definition.ABILiquidity.PackMethodPanic(definition.CollectRewardMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 0,
	"qsrAmount": 0
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.LiquidityContract,
		Data:      definition.ABILiquidity.PackMethodPanic(definition.CollectRewardMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1822*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 4988*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, tokens[0], 10*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, tokens[1], 10*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11998*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	z.ExpectBalance(g.User1.Address, tokens[1], 300*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8000*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 80000*g.Zexp)
	z.ExpectBalance(g.User2.Address, tokens[0], 0*g.Zexp)
	z.ExpectBalance(g.User2.Address, tokens[1], 190*g.Zexp)
	autoreceive(t, z, g.User1.Address)
	autoreceive(t, z, g.User2.Address)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12959*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 122506*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.ZnnTokenStandard, 8961*g.Zexp)
	z.ExpectBalance(g.User2.Address, types.QsrTokenStandard, 82506*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1822*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 4988*g.Zexp)
}

// Add Znn token tuple (min: 1000, percentage: 50% znn, 50% qsr)
// Add Qsr token tuple (min: 2000, percentage: 50% znn, 50% qsr)
// Set 50*10^8 znn reward from contract
// Set 12*10^8 qsr reward from contract
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8) -> Send to contract
// Register an entry for User 1 (token: znn, amount: 10*10^8)
// Update for epoch 1 (znn-amount: 1872*10^8 + 50*10^8, qsr-amount: 5000*10^8 + 12*10^8)
// Rewards for znn: 50% * 1992*10^8 = 961*10^8 znn, 50% * 5012*10^8 = 2506*10^8 qsr
// Rewards for qsr: 50% * 1992*10^8 = 961*10^8 znn, 50% * 5012*10^8 = 2506*10^8 qsr
// Rewards for entry (token: znn, amount: 10*10^8, User1) -> 100% * 961*10^8 znn, 100% * 2506*10^8 qsr
// Rewards for liquidity contract -> 100% * 961*10^8 znn, 100% * 2506*10^8 qsr
// The balance of the liquidity contract -> 1872*10^8 - 50*10^8 + 961*10^8 + 10*10^8 = 2783*10^8 znn
//                                       -> 5000*10^8 - 12*10^8 + 2506*10^8 = 7494*10^8 qsr
func TestLiquidity_CollectReward3(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}"
t=2001-09-09T01:49:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+30000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:49:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+30000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995047669 BlockReward:+9916666627 TotalReward:+21911714296 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099833333333}" total-weight=2499833333333 self-weight=2099833333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=187200000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19687200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T02:47:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12095815665 BlockReward:+9999999960 TotalReward:+22095815625 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099800000000}" total-weight=2499800000000 self-weight=2099800000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152092167 BlockReward:+9999999960 TotalReward:+11152092127 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499800000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152092167 BlockReward:+9999999960 TotalReward:+11152092127 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499800000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="distribute znn rewards from the liquidity contract" module=embedded contract=liquidity znn-amount=5000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="distribute qsr rewards from the liquidity contract" module=embedded contract=liquidity qsr-amount=1200000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=1 znn-total-amount=192200000000 qsr-total-amount=501200000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=5000 qsr-percentage=5000 znn-rewards=96100000000 qsr-rewards=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=5000 qsr-percentage=5000 znn-rewards=96100000000 qsr-rewards=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=96100000000 qsr-amount=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=1 znnReward=96100000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=1 qsrReward=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=722 last-update-height=361
t=2001-09-09T03:47:00+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19682200000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=5000000000
t=2001-09-09T03:47:00+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181048800000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" burned-amount=1200000000
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19778300000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=96100000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181299400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=250600000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=96100000000
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=250600000000
t=2001-09-09T03:48:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=96100000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:48:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=250600000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)
	sporkAPI := embedded.NewSporkApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address
	issueMultipleTokensSetup(t, z)
	customZts := make([]string, 0)
	for _, zts := range tokens {
		customZts = append(customZts, zts.String())
	}
	percentages := []uint32{
		uint32(5000),
		uint32(5000),
	}
	minAmounts := []*big.Int{
		big.NewInt(1000),
		big.NewInt(2000),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			percentages,
			percentages,
			minAmounts,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetAdditionalRewardMethodName,
			big.NewInt(50*g.Zexp),
			big.NewInt(12*g.Zexp),
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.InsertMomentumsTo(60*6 + 4)

	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1872*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 5000*g.Zexp)

	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"isHalted": false,
	"znnReward": 5000000000,
	"qsrReward": 1200000000,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 2000
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11998*g.Zexp)
	z.ExpectBalance(g.User1.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000003640,
			"revokeTime": 0,
			"expirationTime": 1000007240,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "5b342afdae1ab23533775d59ca1a446fdd79dda936134355f6365d0defec609b"
		}
	]
}`)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1872*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 5000*g.Zexp)
	z.InsertMomentumsTo(60*6*2 + 8)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 96100000000,
	"qsrAmount": 250600000000
}`)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 2783*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 7494*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11998*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data:      definition.ABILiquidity.PackMethodPanic(definition.CollectRewardMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 0,
	"qsrAmount": 0
}`)
	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12959*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 122506*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 2783*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 7494*g.Zexp)
}

// Add Znn token tuple (min: 1000, percentage: 50% znn, 50% qsr)
// Add Qsr token tuple (min: 2000, percentage: 50% znn, 50% qsr)
// Set 50*10^8 znn reward from contract
// Set 12*10^8 qsr reward from contract
// Register an entry for User 1 (token: znn, amount: 10*10^8)
// Update for epoch 0 (znn-amount: 1872*10^8, qsr-amount: 5000*10^8) -> Send to contract
// Update for epoch 1 (znn-amount: 1872*10^8 + 50*10^8, qsr-amount: 5000*10^8 + 12*10^8)
// Rewards for znn: 50% * 1992*10^8 = 961*10^8 znn, 50% * 5012*10^8 = 2506*10^8 qsr
// Rewards for qsr: 50% * 1992*10^8 = 961*10^8 znn, 50% * 5012*10^8 = 2506*10^8 qsr
// Rewards for entry (token: znn, amount: 10*10^8, User1) -> 100% * 961*10^8 znn, 100% * 2506*10^8 qsr
// Rewards for liquidity contract -> 100% * 961*10^8 znn, 100% * 2506*10^8 qsr
// The balance of the liquidity contract -> 1872*10^8 - 50*10^8 + 961*10^8 + 10*10^8 = 2783*10^8 znn
//                                       -> 5000*10^8 - 12*10^8 + 2506*10^8 = 7494*10^8 qsr
func TestLiquidity_CollectReward4(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}"
t=2001-09-09T01:49:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+30000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:49:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+30000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:50:20+0000 lvl=dbug msg="created liquidity stake entry" module=embedded contract=stake id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX owner=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz amount=1000000000 weighted-amount=1000000000 duration-in-days=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+11995047669 BlockReward:+9916666627 TotalReward:+21911714296 ProducedBlockNum:119 ExpectedBlockNum:120 Weight:+2099833333333}" total-weight=2499833333333 self-weight=2099833333333
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=0 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152076805 BlockReward:+9999999960 TotalReward:+11152076765 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499833333333 self-weight=200000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=0 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=0 total-reward=1000000000000 cumulated-stake=0 start-time=1000000000 end-time=1000003600
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=361 last-update-height=0
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=0 znn-total-amount=187200000000 qsr-total-amount=500000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=0 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=5000 qsr-percentage=5000 znn-rewards=93600000000 qsr-rewards=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=93600000000 qsr-amount=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 znnReward=93600000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=0 qsrReward=250000000000
t=2001-09-09T02:46:40+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=1
t=2001-09-09T02:46:40+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=361 last-update-height=0
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19593600000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=93600000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+180800000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=250000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=93600000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=250000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+12095815665 BlockReward:+9999999960 TotalReward:+22095815625 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+2099800000000}" total-weight=2499800000000 self-weight=2099800000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+1152092167 BlockReward:+9999999960 TotalReward:+11152092127 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499800000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152092167 BlockReward:+9999999960 TotalReward:+11152092127 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2499800000000 self-weight=200000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=1 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=1 total-reward=1000000000000 cumulated-stake=0 start-time=1000003600 end-time=1000007200
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="distribute znn rewards from the liquidity contract" module=embedded contract=liquidity znn-amount=5000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="distribute qsr rewards from the liquidity contract" module=embedded contract=liquidity qsr-amount=1200000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity epoch=1 znn-total-amount=192200000000 qsr-total-amount=501200000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1ssepy9prq8azdy9afcwq2l znn-percentage=5000 qsr-percentage=5000 znn-rewards=96100000000 qsr-rewards=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="calculating percentages for each token" module=embedded contract=liquidity epoch=1 token-standard=zts1a2ukxhyameh230e58m5x8t znn-percentage=5000 qsr-percentage=5000 znn-rewards=96100000000 qsr-rewards=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity stake reward" module=embedded contract=liquidity id=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX stake-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz token-standard=zts1ssepy9prq8azdy9afcwq2l znn-amount=96100000000 qsr-amount=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=1 znnReward=96100000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating liquidity balance" module=embedded contract=liquidity epoch=1 qsrReward=250600000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=2
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 current-height=722 last-update-height=361
t=2001-09-09T03:47:00+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19588600000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" burned-amount=5000000000
t=2001-09-09T03:47:00+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+180798800000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" burned-amount=1200000000
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19684700000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=96100000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181049400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=250600000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=96100000000
t=2001-09-09T03:47:10+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=250600000000
t=2001-09-09T03:48:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+19874400000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=189700000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T03:48:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+181550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500600000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)
	sporkAPI := embedded.NewSporkApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address
	issueMultipleTokensSetup(t, z)
	customZts := make([]string, 0)
	for _, zts := range tokens {
		customZts = append(customZts, zts.String())
	}
	percentages := []uint32{
		uint32(5000),
		uint32(5000),
	}
	minAmounts := []*big.Int{
		big.NewInt(1000),
		big.NewInt(2000),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			percentages,
			percentages,
			minAmounts,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetAdditionalRewardMethodName,
			big.NewInt(50*g.Zexp),
			big.NewInt(12*g.Zexp),
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"isHalted": false,
	"znnReward": 5000000000,
	"qsrReward": 1200000000,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 2000
		}
	]
}`)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11998*g.Zexp)
	z.ExpectBalance(g.User1.Address, tokens[0], 300*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.LiquidityContract,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.LiquidityStakeMethodName, constants.StakeTimeMinSec),
		TokenStandard: tokens[0],
		Amount:        big.NewInt(10 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(g.User1.Address, tokens[0], 290*g.Zexp)
	common.Json(liquidityAPI.GetLiquidityStakeEntriesByAddress(g.User1.Address, 0, 5)).Equals(t, `
{
	"totalAmount": 1000000000,
	"totalWeightedAmount": 1000000000,
	"count": 1,
	"list": [
		{
			"amount": 1000000000,
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"weightedAmount": 1000000000,
			"startTime": 1000000220,
			"revokeTime": 0,
			"expirationTime": 1000003820,
			"stakeAddress": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
			"id": "73bf88e105132d2206da49ac61615daf13971f41bb86896f870cf73816f4b338"
		}
	]
}`)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 0*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, tokens[0], 10*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, tokens[1], 0*g.Zexp)
	z.InsertMomentumsTo(60*6 + 4)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 936*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 2500*g.Zexp)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 93600000000,
	"qsrAmount": 250000000000
}`)
	z.InsertMomentumsTo(60*6*2 + 8)
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 189700000000,
	"qsrAmount": 500600000000
}`)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1847*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 4994*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 11998*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 120000*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data:      definition.ABILiquidity.PackMethodPanic(definition.CollectRewardMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetUncollectedReward(g.User1.Address)).Equals(t, `
{
	"address": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"znnAmount": 0,
	"qsrAmount": 0
}`)
	autoreceive(t, z, g.User1.Address)
	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 13895*g.Zexp)
	z.ExpectBalance(g.User1.Address, types.QsrTokenStandard, 125006*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.ZnnTokenStandard, 1847*g.Zexp)
	z.ExpectBalance(types.LiquidityContract, types.QsrTokenStandard, 4994*g.Zexp)
}

func TestLiquidity_ChangeAdministrator(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=dbug msg=created module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:false EnforcementHeight:0}"
t=2001-09-09T01:47:00+0000 lvl=dbug msg=activated module=embedded contract=spork spork="&{Id:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Name:spork-bridge Description:activate spork for bridge Activated:true EnforcementHeight:9}"
t=2001-09-09T01:48:20+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+10000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}"
t=2001-09-09T01:48:40+0000 lvl=dbug msg="issued ZTS" module=embedded contract=token token="{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+20000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}"
t=2001-09-09T01:49:00+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-2 TokenSymbol:LIQ2 TokenDomain: TotalSupply:+30000000000 MaxSupply:+200000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1ssepy9prq8azdy9afcwq2l}" minted-amount=10000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
t=2001-09-09T01:49:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz TokenName:test.tok3n_liquidity-1 TokenSymbol:LIQ1 TokenDomain: TotalSupply:+30000000000 MaxSupply:+100000000000 Decimals:6 IsMintable:true IsBurnable:true IsUtility:false TokenStandard:zts1a2ukxhyameh230e58m5x8t}" minted-amount=20000000000 to-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz
`)
	sporkAPI := embedded.NewSporkApi(z)
	liquidityAPI := embedded.NewLiquidityApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-bridge",              // name
			"activate spork for bridge", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id
	constants.MinUnhaltDurationInMomentums = 30
	constants.MinAdministratorDelay = 30
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.BridgeSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(10)
	constants.InitialBridgeAdministrator = g.User1.Address
	issueMultipleTokensSetup(t, z)
	customZts := make([]string, 0)
	for _, zts := range tokens {
		customZts = append(customZts, zts.String())
	}
	percentages := []uint32{
		uint32(5000),
		uint32(5000),
	}
	minAmounts := []*big.Int{
		big.NewInt(1000),
		big.NewInt(2000),
	}
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetTokenTupleMethodName,
			customZts,
			percentages,
			percentages,
			minAmounts,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz",
	"isHalted": false,
	"znnReward": 0,
	"qsrReward": 0,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 2000
		}
	]
}`)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.ChangeLiquidityAdministratorMethodName,
			g.User2.Address,
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetAdditionalRewardMethodName,
			big.NewInt(50*g.Zexp),
			big.NewInt(12*g.Zexp),
		),
	}).Error(t, constants.ErrPermissionDenied)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User2.Address,
		ToAddress: types.LiquidityContract,
		Data: definition.ABILiquidity.PackMethodPanic(definition.SetAdditionalRewardMethodName,
			big.NewInt(50*g.Zexp),
			big.NewInt(12*g.Zexp),
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	common.Json(liquidityAPI.GetLiquidityInfo()).Equals(t, `
{
	"administrator": "z1qr4pexnnfaexqqz8nscjjcsajy5hdqfkgadvwx",
	"isHalted": false,
	"znnReward": 5000000000,
	"qsrReward": 1200000000,
	"tokenTuples": [
		{
			"tokenStandard": "zts1ssepy9prq8azdy9afcwq2l",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 1000
		},
		{
			"tokenStandard": "zts1a2ukxhyameh230e58m5x8t",
			"znnPercentage": 5000,
			"qsrPercentage": 5000,
			"minAmount": 2000
		}
	]
}`)
}
