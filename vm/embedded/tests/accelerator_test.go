package tests

import (
	"testing"
	"time"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

func TestAccelerator(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	defer z.SaveLogs(common.EmbeddedLogger).Equals(t, `
t=2001-09-09T01:46:50+0000 lvl=info msg="received donation" module=embedded contract=common embedded=z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22 from-address=z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz zts=zts1znnxxxxxxxxxxxxx9z4ulx amount=100
`)

	defer z.CallContract(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.AcceleratorContract,
		Data:          definition.ABICommon.PackMethodPanic(definition.DonateMethodName),
		TokenStandard: types.ZnnTokenStandard,
		Amount:        common.Big100,
	}).Error(t, nil)

	z.InsertMomentumsTo(10)
}
