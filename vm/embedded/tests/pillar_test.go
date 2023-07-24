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
	"github.com/zenon-network/go-zenon/vm/embedded/implementation"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

const (
	momentumsInHour = 360
)

// ** Depends on the way on which the consensus computes delegation & pillars. **
// Delegate in such a way that it doesn't influence the first epoch and it fully influences the second epoch.
// The rewards for each epoch for User1 should stay the same.
func TestConsensus_1(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T02:36:40+0000 lvl=info msg="delegating to pillar" module=embedded contract=pillar address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz pillar-name=TEST-pillar-cool height=301
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
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+5184000000 BlockReward:+9999999960 TotalReward:+15183999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+900000000000}" total-weight=2500000000000 self-weight=900000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+8064000000 BlockReward:+9999999960 TotalReward:+18063999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+1400000000000}" total-weight=2500000000000 self-weight=1400000000000
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
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+5184000000 BlockReward:+9999999960 TotalReward:+15183999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+900000000000}" total-weight=2500000000000 self-weight=900000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+8064000000 BlockReward:+9999999960 TotalReward:+18063999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+1400000000000}" total-weight=2500000000000 self-weight=1400000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152000000 BlockReward:+9999999960 TotalReward:+11151999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=2 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000007200 end-time=1000010800
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=2 total-reward=1000000000000 cumulated-stake=0 start-time=1000007200 end-time=1000010800
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=2 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=3
t=2001-09-09T04:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+20061600000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T04:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+182050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T04:47:20+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T04:47:20+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
`)

	z.InsertMomentumsTo(10 * 30)
	// Delegate User1 -> Pillar2Name
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.DelegateMethodName, g.Pillar2Name),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        common.Big0,
	}).Error(t, nil)

	z.InsertMomentumsTo(momentumsInHour * 4)

	z.InsertNewMomentum()
}

// ** Depends on the way on which the consensus computes delegation & pillars. **
// Delegate in the middle of the second epoch
// The rewards for each epoch for User1 should stay the same.
func TestConsensus_2(t *testing.T) {
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
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T03:16:40+0000 lvl=info msg="delegating to pillar" module=embedded contract=pillar address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz pillar-name=TEST-pillar-cool height=541
t=2001-09-09T03:46:50+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=722 last-update-height=361
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+9792000000 BlockReward:+9999999960 TotalReward:+19791999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+1700000000000}" total-weight=2500000000000 self-weight=1700000000000
t=2001-09-09T03:46:50+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=1 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+3456000000 BlockReward:+9999999960 TotalReward:+13455999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+600000000000}" total-weight=2500000000000 self-weight=600000000000
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
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+5184000000 BlockReward:+9999999960 TotalReward:+15183999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+900000000000}" total-weight=2500000000000 self-weight=900000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+8064000000 BlockReward:+9999999960 TotalReward:+18063999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+1400000000000}" total-weight=2500000000000 self-weight=1400000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=2 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152000000 BlockReward:+9999999960 TotalReward:+11151999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=2 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000007200 end-time=1000010800
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=2 total-reward=1000000000000 cumulated-stake=0 start-time=1000007200 end-time=1000010800
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=3
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=1083 last-update-height=722
t=2001-09-09T04:47:00+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=2 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T04:47:00+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=3
t=2001-09-09T04:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+20061600000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T04:47:10+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+182050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T04:47:20+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T04:47:20+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg current-height=1444 last-update-height=1083
t=2001-09-09T05:47:10+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=3 pillar-name=TEST-pillar-1 reward="&{DelegationReward:+5184000000 BlockReward:+9999999960 TotalReward:+15183999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+900000000000}" total-weight=2500000000000 self-weight=900000000000
t=2001-09-09T05:47:10+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=3 pillar-name=TEST-pillar-cool reward="&{DelegationReward:+8064000000 BlockReward:+9999999960 TotalReward:+18063999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+1400000000000}" total-weight=2500000000000 self-weight=1400000000000
t=2001-09-09T05:47:10+0000 lvl=dbug msg="computer pillar-reward" module=embedded contract=pillar epoch=3 pillar-name=TEST-pillar-znn reward="&{DelegationReward:+1152000000 BlockReward:+9999999960 TotalReward:+11151999960 ProducedBlockNum:120 ExpectedBlockNum:120 Weight:+200000000000}" total-weight=2500000000000 self-weight=200000000000
t=2001-09-09T05:47:10+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=pillar epoch=4
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r current-height=1444 last-update-height=1083
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating sentinel reward" module=embedded contract=sentinel epoch=3 total-znn-reward=187200000000 total-qsr-reward=500000000000 cumulated-sentinel=0 start-time=1000010800 end-time=1000014400
t=2001-09-09T05:47:10+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=sentinel epoch=4
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 current-height=1444 last-update-height=1083
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating stake reward" module=embedded contract=stake epoch=3 total-reward=1000000000000 cumulated-stake=0 start-time=1000010800 end-time=1000014400
t=2001-09-09T05:47:10+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=stake epoch=4
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating contract state" module=embedded contract=common contract=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae current-height=1444 last-update-height=1083
t=2001-09-09T05:47:10+0000 lvl=dbug msg="updating liquidity reward" module=embedded contract=liquidity epoch=3 znn-amount=187200000000 qsr-amount=500000000000
t=2001-09-09T05:47:10+0000 lvl=dbug msg="invalid update - rewards not due yet" module=embedded contract=liquidity epoch=4
t=2001-09-09T05:47:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg TokenName:Zenon Coin TokenSymbol:ZNN TokenDomain:zenon.network TotalSupply:+20248800000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1znnxxxxxxxxxxxxx9z4ulx}" minted-amount=187200000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T05:47:20+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+182550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T05:47:30+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T05:47:30+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
`)

	z.InsertMomentumsTo(momentumsInHour * 1.5)
	// Delegate User1 -> Pillar2Name
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.DelegateMethodName, g.Pillar2Name),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        common.Big0,
	}).Error(t, nil)
	z.InsertMomentumsTo(momentumsInHour * 5)
}

// Try to register without QSR deposited before
//   - gets the 15K ZNN back, since the transaction is rollback
func TestPillar_RegisterWithoutQsr(t *testing.T) {
	z := mock.NewMockZenon(t)
	pillarApi := embedded.NewPillarApi(z, true)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, ``)

	common.Json(pillarApi.GetQsrRegistrationCost()).Equals(t, `"15000000000000"`)
	z.ExpectBalance(g.Pillar4.Address, types.ZnnTokenStandard, 16000*g.Zexp)
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar4.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.RegisterMethodName, g.Pillar4Name, g.Pillar4.Address, g.Pillar4.Address, uint8(0), uint8(100)),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.PillarStakeAmount,
	}).Error(t, constants.ErrNotEnoughDepositedQsr)
	z.InsertNewMomentum()
	z.ExpectBalance(g.Pillar4.Address, types.ZnnTokenStandard, 1000*g.Zexp)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	autoreceive(t, z, g.Pillar4.Address)
	z.ExpectBalance(g.Pillar4.Address, types.ZnnTokenStandard, 1600000000000)

	z.InsertNewMomentum()
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar4.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.RegisterMethodName, g.Pillar4Name, g.Pillar4.Address, g.Pillar4.Address, uint8(0), uint8(100)),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.PillarStakeAmount,
	}).Error(t, constants.ErrNotEnoughDepositedQsr)
	z.InsertNewMomentum()
}

// Deposit - Withdraw - Withdraw - Deposit - Withdraw
// - test RPC GetDepositedQsr method
func TestPillar_DepositQsr(t *testing.T) {
	z := mock.NewMockZenon(t)
	pillarApi := embedded.NewPillarApi(z, true)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, ``)

	z.ExpectBalance(g.Pillar4.Address, types.QsrTokenStandard, 200000*g.Zexp)

	z.InsertMomentumsTo(10)
	// Deposit & Withdraw
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar4.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        constants.PillarQsrStakeBaseAmount,
	}).Error(t, nil)
	// Add send-blocks
	z.InsertNewMomentum()
	common.Json(pillarApi.GetDepositedQsr(g.Pillar4.Address)).Equals(t, `"15000000000000"`)
	z.ExpectBalance(g.Pillar4.Address, types.QsrTokenStandard, 200000*g.Zexp-15000000000000)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar4.Address,
		ToAddress: types.PillarContract,
		Data:      definition.ABIPillars.PackMethodPanic(definition.WithdrawQsrMethodName),
	}).Error(t, nil)
	// Add send & receive blocks
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	autoreceive(t, z, g.Pillar4.Address)
	common.Json(pillarApi.GetDepositedQsr(g.Pillar4.Address)).Equals(t, `"0"`)
	z.ExpectBalance(g.Pillar4.Address, types.QsrTokenStandard, 200000*g.Zexp)

	// withdraw again, should receive error
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar4.Address,
		ToAddress: types.PillarContract,
		Data:      definition.ABIPillars.PackMethodPanic(definition.WithdrawQsrMethodName),
	}).Error(t, constants.ErrNothingToWithdraw)

	z.InsertMomentumsTo(20)
	z.ExpectBalance(g.Pillar4.Address, types.QsrTokenStandard, 200000*g.Zexp)
	// Deposit & Withdraw
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar4.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        constants.PillarQsrStakeBaseAmount,
	}).Error(t, nil)
	// Add send-blocks
	z.InsertNewMomentum()
	common.Json(pillarApi.GetDepositedQsr(g.Pillar4.Address)).Equals(t, `"15000000000000"`)

	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar4.Address,
		ToAddress: types.PillarContract,
		Data:      definition.ABIPillars.PackMethodPanic(definition.WithdrawQsrMethodName),
	}).Error(t, nil)
	z.InsertMomentumsTo(30)
	common.Json(pillarApi.GetDepositedQsr(g.Pillar4.Address)).Equals(t, `"0"`)
}

// Register a pillar depositing weird amounts of QSR
//   - support custom deposit values
//   - be able to withdraw the deposited QSR in case of too much deposited
func TestPillar_RegisterWithWeirdQsrDeposits(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:47:00+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+165550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" burned-amount=15000000000000
`)

	// deposit
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar4.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(12345612345678),
	}).Error(t, nil)

	// deposit
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar4.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(2345612345678),
	}).Error(t, nil)

	// deposit
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar4.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(4345612345678),
	}).Error(t, nil)

	// register
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar4.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.RegisterMethodName, g.Pillar4Name, g.Pillar4.Address, g.Pillar4.Address, uint8(0), uint8(100)),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.PillarStakeAmount,
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	// withdraw
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar4.Address,
		ToAddress: types.PillarContract,
		Data:      definition.ABIPillars.PackMethodPanic(definition.WithdrawQsrMethodName),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	autoreceive(t, z, g.Pillar4.Address)

	z.ExpectBalance(g.Pillar4.Address, types.QsrTokenStandard, 5000000000000)
}

// Register a legacy pillar after 2 normal pillar are already registered.
// The first pillar should take 150K qsr, the second one 160K, the legacy one 150K
// Register another normal pillar requiring 170K
func TestPillar_RegisterLegacyPillar(t *testing.T) {
	z := mock.NewMockZenon(t)
	pillarApi := embedded.NewPillarApi(z, true)
	swapApi := embedded.NewSwapApi(z)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:47:10+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+165550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" burned-amount=15000000000000
t=2001-09-09T01:47:30+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+149550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" burned-amount=16000000000000
t=2001-09-09T01:47:30+0000 lvl=dbug msg=swap-utils-log module=embedded contract=swap-utils-log expected-message=0xXXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
t=2001-09-09T01:47:40+0000 lvl=dbug msg=swap-utils-log module=embedded contract=swap-utils-log expected-message=0xXXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
t=2001-09-09T01:47:50+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+134550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" burned-amount=15000000000000
`)

	common.Json(pillarApi.GetQsrRegistrationCost()).Equals(t, `"15000000000000"`)
	// deposit QSR for first normal pillar
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar4.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(150000 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	// register the first normal pillar
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar4.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.RegisterMethodName, g.Pillar4Name, g.Pillar4.Address, g.Pillar4.Address, uint8(0), uint8(100)),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.PillarStakeAmount,
	}).Error(t, nil)
	z.InsertNewMomentum()

	common.Json(pillarApi.GetQsrRegistrationCost()).Equals(t, `"16000000000000"`)
	// deposit QSR for second normal pillar
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar5.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(160000 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()

	// register the second normal pillar
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar5.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.RegisterMethodName, g.Pillar5Name, g.Pillar5.Address, g.Pillar5.Address, uint8(0), uint8(100)),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.PillarStakeAmount,
	}).Error(t, nil)
	z.InsertNewMomentum()

	// deposit QSR for the legacy pillar
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar6.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(150000 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	//register the legacy pillar
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar6.Address,
		ToAddress: types.PillarContract,
		Data: definition.ABIPillars.PackMethodPanic(definition.LegacyRegisterMethodName, g.Pillar6Name, g.Pillar6.Address, g.Pillar6.Address, uint8(0), uint8(100),
			g.Secp1PubKeyB64,
			stringErrDealWith(implementation.SignLegacyPillarMessage(g.Pillar6.Address, g.Secp1PrvKey, g.Secp1PubKeyB64))),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.PillarStakeAmount,
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertMomentumsTo(65)
	common.Json(pillarApi.GetAll(0, 10)).SubJson(ListOfName()).Equals(t, `
{
	"count": 6,
	"list": [
		{
			"name": "TEST-pillar-1"
		},
		{
			"name": "TEST-pillar-cool"
		},
		{
			"name": "TEST-pillar-znn"
		},
		{
			"name": "TEST-pillar-6-quasar"
		},
		{
			"name": "TEST-pillar-wewe"
		},
		{
			"name": "TEST-pillar-zumba"
		}
	]
}`)
	common.Json(pillarApi.GetQsrRegistrationCost()).Equals(t, `"17000000000000"`)
	common.Json(swapApi.GetLegacyPillars()).Equals(t, `
[
	{
		"keyIdHash": "c955c2b650452d670179068995a51132463e2d13f7519d64ff283af99dd14b43",
		"numPillars": 2
	}
]`)
}

// Change pillar 1 producing address to pillar 4
// Change pillar 1 producing address to pillar 1 (valid since it was used in the past by pillar 1)
// Change pillar 2 producing address to pillar 4 (should fail since pillar 1 already used this in the past)
func TestPillar_ChangeProducingAddress(t *testing.T) {
	z := mock.NewMockZenon(t)
	pillarApi := embedded.NewPillarApi(z, true)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=info msg="Updating pillar producer address" module=embedded contract=pillar pillar-name=TEST-pillar-1 old-address=z1qqq43dyrswfehx9w9td43exflqzcxrt7g6alah new-address=z1qplpsv3wcm64js30jlumxlatgxxkqr6hgv30fg
t=2001-09-09T01:46:50+0000 lvl=info msg="Updating pillar reward address" module=embedded contract=pillar pillar-name=TEST-pillar-1 old-address=z1qqq43dyrswfehx9w9td43exflqzcxrt7g6alah new-address=z1qzv6ch3znujldgkq3krlzq38hu5n2pqg3xsjgv
t=2001-09-09T01:46:50+0000 lvl=info msg="Updating pillar give-block-reward-percentage" module=embedded contract=pillar pillar-name=TEST-pillar-1 old=0 new=20
t=2001-09-09T01:46:50+0000 lvl=info msg="Updating pillar give-delegate-reward-percentage" module=embedded contract=pillar pillar-name=TEST-pillar-1 old=100 new=50
t=2001-09-09T02:20:00+0000 lvl=info msg="Updating pillar producer address" module=embedded contract=pillar pillar-name=TEST-pillar-1 old-address=z1qplpsv3wcm64js30jlumxlatgxxkqr6hgv30fg new-address=z1qqq43dyrswfehx9w9td43exflqzcxrt7g6alah
`)

	interest := &struct {
		A types.Address `json:"producerAddress"`
		B int           `json:"giveMomentumRewardPercentage"`
		C int           `json:"giveDelegateRewardPercentage"`
	}{}

	common.Json(pillarApi.GetByName(g.Pillar1Name)).SubJson(interest).Equals(t, `
{
	"producerAddress": "z1qqq43dyrswfehx9w9td43exflqzcxrt7g6alah",
	"giveMomentumRewardPercentage": 0,
	"giveDelegateRewardPercentage": 100
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.PillarContract,
		Data:      definition.ABIPillars.PackMethodPanic(definition.UpdatePillarMethodName, g.Pillar1Name, g.Pillar4.Address, g.Pillar5.Address, uint8(20), uint8(50)),
	}).Error(t, nil)
	z.InsertMomentumsTo(200)
	common.Json(pillarApi.GetByName(g.Pillar1Name)).SubJson(interest).Equals(t, `
{
	"producerAddress": "z1qplpsv3wcm64js30jlumxlatgxxkqr6hgv30fg",
	"giveMomentumRewardPercentage": 20,
	"giveDelegateRewardPercentage": 50
}`)
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar1.Address,
		ToAddress: types.PillarContract,
		Data:      definition.ABIPillars.PackMethodPanic(definition.UpdatePillarMethodName, g.Pillar1Name, g.Pillar1.Address, g.Pillar5.Address, uint8(20), uint8(50)),
	}).Error(t, nil)
	z.InsertMomentumsTo(300)
	common.Json(pillarApi.GetByName(g.Pillar1Name)).SubJson(interest).Equals(t, `
{
	"producerAddress": "z1qqq43dyrswfehx9w9td43exflqzcxrt7g6alah",
	"giveMomentumRewardPercentage": 20,
	"giveDelegateRewardPercentage": 50
}`)
}

// Register pillar 4
// Revoke pillar 4
// Register a new pillar with the name of pillar 4 (should fail since an inactive pillar owns the name)
func TestPillar_RegisterRevokeRegisterPillar(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	pillarApi := embedded.NewPillarApi(z, true)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:47:10+0000 lvl=dbug msg="burned ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+165550000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" burned-amount=15000000000000
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
t=2001-09-09T02:46:50+0000 lvl=dbug msg="minted ZTS" module=embedded contract=token token="&{Owner:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 TokenName:QuasarCoin TokenSymbol:QSR TokenDomain:zenon.network TotalSupply:+166050000000000 MaxSupply:+4611686018427387903 Decimals:8 IsMintable:true IsBurnable:true IsUtility:true TokenStandard:zts1qsrxxxxxxxxxxxxxmrhjll}" minted-amount=500000000000 to-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=187200000000
t=2001-09-09T02:47:00+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae from-address=z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0 zts=zts1qsrxxxxxxxxxxxxxmrhjll amount=500000000000
`)
	defer func() {
		constants.PillarEpochRevokeTime = 28800
		constants.PillarEpochLockTime = 144000
	}()
	constants.PillarEpochRevokeTime = 60
	constants.PillarEpochLockTime = 60

	common.Json(pillarApi.GetQsrRegistrationCost()).Equals(t, `"15000000000000"`)
	// deposit QSR for Pillar 4
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar4.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(150000 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	// register Pillar 4
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar4.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.RegisterMethodName, g.Pillar4Name, g.Pillar4.Address, g.Pillar4.Address, uint8(0), uint8(100)),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.PillarStakeAmount,
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertMomentumsTo(10)

	common.Json(pillarApi.GetAll(0, 100)).SubJson(ListOfName()).Equals(t, `
{
	"count": 4,
	"list": [
		{
			"name": "TEST-pillar-1"
		},
		{
			"name": "TEST-pillar-cool"
		},
		{
			"name": "TEST-pillar-znn"
		},
		{
			"name": "TEST-pillar-wewe"
		}
	]
}`)

	// revoke Pillar 4
	defer z.CallContract(&nom.AccountBlock{
		Address:   g.Pillar4.Address,
		ToAddress: types.PillarContract,
		Data:      definition.ABIPillars.PackMethodPanic(definition.RevokeMethodName, g.Pillar4Name),
	}).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	autoreceive(t, z, g.Pillar4.Address)

	common.Json(pillarApi.GetAll(0, 100)).SubJson(ListOfName()).Equals(t, `
{
	"count": 3,
	"list": [
		{
			"name": "TEST-pillar-1"
		},
		{
			"name": "TEST-pillar-cool"
		},
		{
			"name": "TEST-pillar-znn"
		}
	]
}`)

	common.Json(pillarApi.GetQsrRegistrationCost()).Equals(t, `"15000000000000"`)
	common.Json(pillarApi.CheckNameAvailability(g.Pillar4Name)).Equals(t, `false`)
	// deposit QSR for Pillar 4
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar5.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(160000 * g.Zexp),
	}).Error(t, nil)
	z.InsertNewMomentum()
	// try to register Pillar 4 again
	defer z.CallContract(&nom.AccountBlock{
		Address:       g.Pillar5.Address,
		ToAddress:     types.PillarContract,
		Data:          definition.ABIPillars.PackMethodPanic(definition.RegisterMethodName, g.Pillar4Name, g.Pillar4.Address, g.Pillar4.Address, uint8(0), uint8(100)),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.PillarStakeAmount,
	}).Error(t, constants.ErrNotUnique)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	common.Json(pillarApi.GetAll(0, 100)).Equals(t, `
{
	"count": 3,
	"list": [
		{
			"name": "TEST-pillar-1",
			"rank": 0,
			"type": 1,
			"ownerAddress": "z1qqq43dyrswfehx9w9td43exflqzcxrt7g6alah",
			"producerAddress": "z1qqq43dyrswfehx9w9td43exflqzcxrt7g6alah",
			"withdrawAddress": "z1qqq43dyrswfehx9w9td43exflqzcxrt7g6alah",
			"isRevocable": false,
			"revokeCooldown": 20,
			"revokeTimestamp": 0,
			"giveMomentumRewardPercentage": 0,
			"giveDelegateRewardPercentage": 100,
			"currentStats": {
				"producedMomentums": 4,
				"expectedMomentums": 10
			},
			"weight": "2100000000000"
		},
		{
			"name": "TEST-pillar-cool",
			"rank": 1,
			"type": 1,
			"ownerAddress": "z1qz8v73ea2vy2rrlq7skssngu8cm8mknjjkr2ju",
			"producerAddress": "z1qz8v73ea2vy2rrlq7skssngu8cm8mknjjkr2ju",
			"withdrawAddress": "z1qz8v73ea2vy2rrlq7skssngu8cm8mknjjkr2ju",
			"isRevocable": false,
			"revokeCooldown": 20,
			"revokeTimestamp": 0,
			"giveMomentumRewardPercentage": 0,
			"giveDelegateRewardPercentage": 100,
			"currentStats": {
				"producedMomentums": 6,
				"expectedMomentums": 10
			},
			"weight": "200000000000"
		},
		{
			"name": "TEST-pillar-znn",
			"rank": 2,
			"type": 1,
			"ownerAddress": "z1qqc8hqalt8je538849rf78nhgek30axq8h0g69",
			"producerAddress": "z1qqc8hqalt8je538849rf78nhgek30axq8h0g69",
			"withdrawAddress": "z1qqc8hqalt8je538849rf78nhgek30axq8h0g69",
			"isRevocable": false,
			"revokeCooldown": 20,
			"revokeTimestamp": 0,
			"giveMomentumRewardPercentage": 0,
			"giveDelegateRewardPercentage": 100,
			"currentStats": {
				"producedMomentums": 6,
				"expectedMomentums": 10
			},
			"weight": "200000000000"
		}
	]
}`)

	z.InsertMomentumsTo(500)

	common.Json(pillarApi.GetUncollectedReward(g.Pillar1.Address)).Equals(t, `
{
	"address": "z1qqq43dyrswfehx9w9td43exflqzcxrt7g6alah",
	"znnAmount": "10487866627",
	"qsrAmount": "0"
}`)
	common.Json(pillarApi.GetFrontierRewardByPage(g.Pillar1.Address, 0, 2)).Equals(t, `
{
	"count": 1,
	"list": [
		{
			"epoch": 0,
			"znnAmount": "10487866627",
			"qsrAmount": "0"
		}
	]
}`)
}
