package tests

import (
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
