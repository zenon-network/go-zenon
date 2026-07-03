package account

// Key prefixes of the account-chain database. Prefixes 0-2 are
// reserved by the common/db version helpers (frontier identifier,
// heights by hash, entries by height), which store the account blocks
// themselves.
var (
	balanceKeyPrefix         = []byte{3}
	storageKeyPrefix         = []byte{4}
	chainPlasmaKey           = []byte{5}
	receivedBlockPrefix      = []byte{6}
	sequencerLastReceivedKey = []byte{7}
)

// Receive-status values stored under receivedBlockPrefix markers:
// MarkAsReceived writes Received for the consumed send-block hash,
// while ReceiveStatusUnknown is the implicit status of hashes with no
// marker.
const (
	ReceiveStatusUnknown uint64 = iota
	Received
)
