package mock

import (
	"testing"
	"time"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
)

func TestStateGenesis(t *testing.T) {
	z := NewMockZenon(t)
	defer z.StopPanic()

	store := z.Chain().GetFrontierMomentumStore()
	common.ExpectBytes(t, store.Identifier().Hash.Bytes(), "0x0385d849ee33b94c8783288c148e3ae741c2ecec98b08b3f59d6bcc219168fe5")

	cacheStore := z.Chain().GetFrontierCacheStore()
	common.ExpectBytes(t, cacheStore.Identifier().Hash.Bytes(), "0x0385d849ee33b94c8783288c148e3ae741c2ecec98b08b3f59d6bcc219168fe5")

	genesis, err := store.GetMomentumByHeight(1)
	common.FailIfErr(t, err)
	common.ExpectString(t, string(genesis.Data[0:43]), "This is the genesis config used for testing")

	z.ExpectBalance(g.User1.Address, types.ZnnTokenStandard, 12000*g.Zexp)
	z.ExpectCacheFusedAmount(g.User1.Address, 10000*g.Zexp)
}

func TestStateProducer(t *testing.T) {
	time.Local = time.UTC
	z := NewMockZenon(t)
	defer z.StopPanic()

	defer z.SaveLogs(common.PillarLogger).HideHashes().Equals(t, `
t=2001-09-09T01:46:40+0000 lvl=info msg="producing momentum" module=pillar submodule=worker event="{StartTime:2001-09-09 01:46:50 +0000 UTC EndTime:2001-09-09 01:47:00 +0000 UTC Producer:z1qz8v73ea2vy2rrlq7skssngu8cm8mknjjkr2ju Name:}"
t=2001-09-09T01:46:40+0000 lvl=info msg="broadcasting own momentum" module=pillar submodule=worker identifier="{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:2}"
t=2001-09-09T01:46:50+0000 lvl=info msg="start creating autoreceive blocks" module=pillar submodule=worker
t=2001-09-09T01:46:50+0000 lvl=info msg="checking if can update contracts" module=pillar submodule=worker
t=2001-09-09T01:46:50+0000 lvl=info msg="producing momentum" module=pillar submodule=worker event="{StartTime:2001-09-09 01:47:00 +0000 UTC EndTime:2001-09-09 01:47:10 +0000 UTC Producer:z1qqc8hqalt8je538849rf78nhgek30axq8h0g69 Name:}"
t=2001-09-09T01:46:50+0000 lvl=info msg="broadcasting own momentum" module=pillar submodule=worker identifier="{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:3}"
t=2001-09-09T01:47:00+0000 lvl=info msg="start creating autoreceive blocks" module=pillar submodule=worker
t=2001-09-09T01:47:00+0000 lvl=info msg="checking if can update contracts" module=pillar submodule=worker
t=2001-09-09T01:47:00+0000 lvl=info msg="producing momentum" module=pillar submodule=worker event="{StartTime:2001-09-09 01:47:10 +0000 UTC EndTime:2001-09-09 01:47:20 +0000 UTC Producer:z1qz8v73ea2vy2rrlq7skssngu8cm8mknjjkr2ju Name:}"
t=2001-09-09T01:47:00+0000 lvl=info msg="broadcasting own momentum" module=pillar submodule=worker identifier="{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:4}"
t=2001-09-09T01:47:10+0000 lvl=info msg="start creating autoreceive blocks" module=pillar submodule=worker
t=2001-09-09T01:47:10+0000 lvl=info msg="checking if can update contracts" module=pillar submodule=worker
t=2001-09-09T01:47:10+0000 lvl=info msg="producing momentum" module=pillar submodule=worker event="{StartTime:2001-09-09 01:47:20 +0000 UTC EndTime:2001-09-09 01:47:30 +0000 UTC Producer:z1qz8v73ea2vy2rrlq7skssngu8cm8mknjjkr2ju Name:}"
t=2001-09-09T01:47:10+0000 lvl=info msg="broadcasting own momentum" module=pillar submodule=worker identifier="{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:5}"
t=2001-09-09T01:47:20+0000 lvl=info msg="start creating autoreceive blocks" module=pillar submodule=worker
t=2001-09-09T01:47:20+0000 lvl=info msg="checking if can update contracts" module=pillar submodule=worker
t=2001-09-09T01:47:20+0000 lvl=info msg="producing block to update embedded-contract" module=pillar submodule=worker contract-address=z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg
t=2001-09-09T01:47:20+0000 lvl=info msg="producing block to update embedded-contract" module=pillar submodule=worker contract-address=z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62
t=2001-09-09T01:47:20+0000 lvl=info msg="producing block to update embedded-contract" module=pillar submodule=worker contract-address=z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r
t=2001-09-09T01:47:20+0000 lvl=info msg="producing block to update embedded-contract" module=pillar submodule=worker contract-address=z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae
t=2001-09-09T01:47:20+0000 lvl=info msg="producing block to update embedded-contract" module=pillar submodule=worker contract-address=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22
t=2001-09-09T01:47:20+0000 lvl=eror msg="failed to update contracts" module=pillar submodule=worker reason="method not found in the abi"
t=2001-09-09T01:47:20+0000 lvl=info msg="producing momentum" module=pillar submodule=worker event="{StartTime:2001-09-09 01:47:30 +0000 UTC EndTime:2001-09-09 01:47:40 +0000 UTC Producer:z1qqq43dyrswfehx9w9td43exflqzcxrt7g6alah Name:}"
t=2001-09-09T01:47:20+0000 lvl=info msg="broadcasting own momentum" module=pillar submodule=worker identifier="{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:6}"
t=2001-09-09T01:47:30+0000 lvl=info msg="start creating autoreceive blocks" module=pillar submodule=worker
t=2001-09-09T01:47:30+0000 lvl=info msg="generated embedded-block" module=pillar submodule=worker send-block-header="{Address:z1qz8v73ea2vy2rrlq7skssngu8cm8mknjjkr2ju HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:2}}" identifier="{Address:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:2}}" send-block-hash=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX returned-error=nil
t=2001-09-09T01:47:30+0000 lvl=info msg="created autoreceive-block" module=pillar submodule=worker identifier="{Address:z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:2}}"
t=2001-09-09T01:47:30+0000 lvl=info msg="generated embedded-block" module=pillar submodule=worker send-block-header="{Address:z1qz8v73ea2vy2rrlq7skssngu8cm8mknjjkr2ju HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:4}}" identifier="{Address:z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:1}}" send-block-hash=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX returned-error=nil
t=2001-09-09T01:47:30+0000 lvl=info msg="created autoreceive-block" module=pillar submodule=worker identifier="{Address:z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:1}}"
t=2001-09-09T01:47:30+0000 lvl=info msg="generated embedded-block" module=pillar submodule=worker send-block-header="{Address:z1qz8v73ea2vy2rrlq7skssngu8cm8mknjjkr2ju HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:3}}" identifier="{Address:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:1}}" send-block-hash=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX returned-error=nil
t=2001-09-09T01:47:30+0000 lvl=info msg="created autoreceive-block" module=pillar submodule=worker identifier="{Address:z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62 HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:1}}"
t=2001-09-09T01:47:30+0000 lvl=info msg="generated embedded-block" module=pillar submodule=worker send-block-header="{Address:z1qz8v73ea2vy2rrlq7skssngu8cm8mknjjkr2ju HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:5}}" identifier="{Address:z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:1}}" send-block-hash=XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX returned-error=nil
t=2001-09-09T01:47:30+0000 lvl=info msg="created autoreceive-block" module=pillar submodule=worker identifier="{Address:z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae HashHeight:{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:1}}"
t=2001-09-09T01:47:30+0000 lvl=info msg="checking if can update contracts" module=pillar submodule=worker
t=2001-09-09T01:47:30+0000 lvl=info msg="producing block to update embedded-contract" module=pillar submodule=worker contract-address=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22
t=2001-09-09T01:47:30+0000 lvl=eror msg="failed to update contracts" module=pillar submodule=worker reason="method not found in the abi"
t=2001-09-09T01:47:30+0000 lvl=info msg="producing momentum" module=pillar submodule=worker event="{StartTime:2001-09-09 01:47:40 +0000 UTC EndTime:2001-09-09 01:47:50 +0000 UTC Producer:z1qz8v73ea2vy2rrlq7skssngu8cm8mknjjkr2ju Name:}"
t=2001-09-09T01:47:30+0000 lvl=info msg="broadcasting own momentum" module=pillar submodule=worker identifier="{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:7}"
t=2001-09-09T01:47:40+0000 lvl=info msg="start creating autoreceive blocks" module=pillar submodule=worker
t=2001-09-09T01:47:40+0000 lvl=info msg="checking if can update contracts" module=pillar submodule=worker
t=2001-09-09T01:47:40+0000 lvl=info msg="producing block to update embedded-contract" module=pillar submodule=worker contract-address=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22
t=2001-09-09T01:47:40+0000 lvl=eror msg="failed to update contracts" module=pillar submodule=worker reason="method not found in the abi"
t=2001-09-09T01:47:40+0000 lvl=info msg="producing momentum" module=pillar submodule=worker event="{StartTime:2001-09-09 01:47:50 +0000 UTC EndTime:2001-09-09 01:48:00 +0000 UTC Producer:z1qz8v73ea2vy2rrlq7skssngu8cm8mknjjkr2ju Name:}"
t=2001-09-09T01:47:40+0000 lvl=info msg="broadcasting own momentum" module=pillar submodule=worker identifier="{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:8}"
t=2001-09-09T01:47:50+0000 lvl=info msg="start creating autoreceive blocks" module=pillar submodule=worker
t=2001-09-09T01:47:50+0000 lvl=info msg="checking if can update contracts" module=pillar submodule=worker
t=2001-09-09T01:47:50+0000 lvl=info msg="producing block to update embedded-contract" module=pillar submodule=worker contract-address=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22
t=2001-09-09T01:47:50+0000 lvl=eror msg="failed to update contracts" module=pillar submodule=worker reason="method not found in the abi"
t=2001-09-09T01:47:50+0000 lvl=info msg="producing momentum" module=pillar submodule=worker event="{StartTime:2001-09-09 01:48:00 +0000 UTC EndTime:2001-09-09 01:48:10 +0000 UTC Producer:z1qz8v73ea2vy2rrlq7skssngu8cm8mknjjkr2ju Name:}"
t=2001-09-09T01:47:50+0000 lvl=info msg="broadcasting own momentum" module=pillar submodule=worker identifier="{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:9}"
t=2001-09-09T01:48:00+0000 lvl=info msg="start creating autoreceive blocks" module=pillar submodule=worker
t=2001-09-09T01:48:00+0000 lvl=info msg="checking if can update contracts" module=pillar submodule=worker
t=2001-09-09T01:48:00+0000 lvl=info msg="producing block to update embedded-contract" module=pillar submodule=worker contract-address=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22
t=2001-09-09T01:48:00+0000 lvl=eror msg="failed to update contracts" module=pillar submodule=worker reason="method not found in the abi"
t=2001-09-09T01:48:00+0000 lvl=info msg="producing momentum" module=pillar submodule=worker event="{StartTime:2001-09-09 01:48:10 +0000 UTC EndTime:2001-09-09 01:48:20 +0000 UTC Producer:z1qqc8hqalt8je538849rf78nhgek30axq8h0g69 Name:}"
t=2001-09-09T01:48:00+0000 lvl=info msg="broadcasting own momentum" module=pillar submodule=worker identifier="{Hash:XXXHASHXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX Height:10}"
t=2001-09-09T01:48:10+0000 lvl=info msg="start creating autoreceive blocks" module=pillar submodule=worker
t=2001-09-09T01:48:10+0000 lvl=info msg="checking if can update contracts" module=pillar submodule=worker
t=2001-09-09T01:48:10+0000 lvl=info msg="producing block to update embedded-contract" module=pillar submodule=worker contract-address=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22
t=2001-09-09T01:48:10+0000 lvl=eror msg="failed to update contracts" module=pillar submodule=worker reason="method not found in the abi"
`)

	constants.UpdateMinNumMomentums = 5

	z.InsertMomentumsTo(10)
}
