// Package mock provides the self-contained Zenon node that the embedded
// contract test suite runs against; its chain and cache databases live
// in temporary LevelDB directories under the test's temp dir.
//
// NewMockZenon builds a chain over the mock embedded genesis, a
// consensus over an in-memory db and one pillar.Manager per
// genesis pillar key, silences the loggers and swaps common.Clock for
// a clock derived from the frontier momentum so test time advances
// only as momentums are produced. Tests then drive it through the
// MockZenon interface: InsertSendBlock and InsertReceiveBlock generate
// and cement account blocks (asserting the expected error and VM
// changes), CallContract sends to an embedded contract and defers the
// producer's result, InsertNewMomentum and InsertMomentumsTo have the
// scheduled pillar produce momentums, and ExpectBalance and SaveLogs
// assert on the resulting state and logs.
package mock

import (
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/zenon"
)

// MockZenon is the test harness a NewMockZenon node exposes. It embeds
// the production Zenon interface and adds helpers for driving momentum
// production, generating and cementing account blocks (including
// InsertSendBlockRejected, which requires the pool to refuse a block),
// calling embedded contracts and asserting on balances, cached fused
// amounts and logs. Its accessors for the non-mocked subsystems
// (Verifier, Protocol, Producer, Config) return nil.
type MockZenon interface {
	zenon.Zenon
	StopPanic()

	InsertNewMomentum()
	InsertMomentumsTo(targetHeight uint64)

	CallContract(template *nom.AccountBlock) *common.Expecter
	InsertSendBlock(template *nom.AccountBlock, expectedError error, expectedVmChanges string) *nom.AccountBlock
	InsertSendBlockRejected(template *nom.AccountBlock, expectedError error)
	InsertReceiveBlock(fromHeader types.AccountHeader, template *nom.AccountBlock, expectedError error, expectedVmChanges string) *nom.AccountBlock

	SaveLogs(logger common.Logger) *common.Expecter
	ExpectBalance(address types.Address, standard types.ZenonTokenStandard, expected int64)
	ExpectCacheFusedAmount(address types.Address, expected int64)
}
