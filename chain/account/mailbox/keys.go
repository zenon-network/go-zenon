package mailbox

// Key prefixes of an account's mailbox subset (see chain/momentum for
// how the subset is carved out of the momentum-chain database).
// unreceivedBlockPrefix is a permanent index of every send ever
// addressed to the account, while pendingBlockPrefix holds only the
// sends not yet received.
var (
	unreceivedBlockPrefix         = []byte{4}
	pendingBlockPrefix            = []byte{5}
	blockWhichReceives            = []byte{6}
	sequencerNumInsertedKey       = []byte{7}
	sequencerHeaderByHeightPrefix = []byte{8}
)
