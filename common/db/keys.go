package db

// Bookkeeping keys embedded in every versioned store (see store.go):
// the identifier of the latest version lives at frontierIdentifierKey,
// a hash → height index under heightByHashPrefix, and the serialized
// commit entries (account blocks or momentums) by big-endian height
// under entryByHeightPrefix.
var (
	frontierIdentifierKey = []byte{0}
	heightByHashPrefix    = []byte{1}
	entryByHeightPrefix   = []byte{2}
)
