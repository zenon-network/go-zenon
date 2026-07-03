package momentum

// Key prefixes of the momentum-chain database. Prefixes 0-2 are
// reserved by the common/db version helpers (frontier identifier,
// heights by hash, entries by height), which store the momentums
// themselves; accountStorePrefix and accountMailboxPrefix are
// extended with an address to form per-account subsets.
var (
	accountStorePrefix            = []byte{3}
	accountMailboxPrefix          = []byte{4}
	blockConfirmationHeightPrefix = []byte{5}
	accountZNNBalancePrefix       = []byte{8}
	accountHeaderByHashPrefix     = []byte{9}
)
