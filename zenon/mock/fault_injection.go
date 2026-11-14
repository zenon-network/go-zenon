package mock

import "github.com/zenon-network/go-zenon/common/types"

// HoldInsertsFor makes the mock reject every account block for address
// instead of inserting it, until called again with nil; see failInsertFor.
// It is exported for tests outside this package that need a contract's
// receives held back, and is not part of MockZenon. Call it between
// momentum rounds, never while one is running.
func (zenon *mockZenon) HoldInsertsFor(address *types.Address) {
	zenon.failInsertFor = address
}
