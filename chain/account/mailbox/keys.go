package mailbox

var (
	unreceivedBlockPrefix         = []byte{4}
	pendingBlockPrefix            = []byte{5}
	blockWhichReceives            = []byte{6}
	sequencerNumInsertedKey       = []byte{7}
	sequencerHeaderByHeightPrefix = []byte{8}
)
