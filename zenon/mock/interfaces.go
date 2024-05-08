package mock

import (
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/zenon"
)

type MockZenon interface {
	zenon.Zenon
	StopPanic()

	InsertNewMomentum()
	InsertMomentumsTo(targetHeight uint64)

	CallContract(template *nom.AccountBlock) *common.Expecter
	InsertSendBlock(template *nom.AccountBlock, expectedError error, expectedVmChanges string) *nom.AccountBlock
	InsertReceiveBlock(fromHeader types.AccountHeader, template *nom.AccountBlock, expectedError error, expectedVmChanges string) *nom.AccountBlock

	SaveLogs(logger common.Logger) *common.Expecter
	ExpectBalance(address types.Address, standard types.ZenonTokenStandard, expected int64)
	ExpectCacheFusedAmount(address types.Address, expected int64)
}
