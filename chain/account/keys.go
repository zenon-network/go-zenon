package account

var (
	balanceKeyPrefix         = []byte{3}
	storageKeyPrefix         = []byte{4}
	chainPlasmaKey           = []byte{5}
	receivedBlockPrefix      = []byte{6}
	sequencerLastReceivedKey = []byte{7}

	StorageKeyPrefix = storageKeyPrefix
	ChainPlasmaKey   = chainPlasmaKey
)

const (
	ReceiveStatusUnknown uint64 = iota
	Received
)
