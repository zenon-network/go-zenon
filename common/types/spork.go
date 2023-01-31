package types

var (
	AcceleratorSpork = NewImplementedSpork("6d2b1e6cb4025f2f45533f0fe22e9b7ce2014d91cc960471045fa64eee5a6ba3")
	// BridgeSpork TODO: change hash for bridge spork
	BridgeSpork          = NewImplementedSpork("7c0a642a7a05e8d32eac1a0b6d5d7ede56a7c51ca99b15d70659e478031fbe86")
	ImplementedSporksMap = map[Hash]bool{
		AcceleratorSpork.SporkId: true,
		BridgeSpork.SporkId:      true,
	}
)

type ImplementedSpork struct {
	SporkId Hash
}

func NewImplementedSpork(SporkIdStr string) *ImplementedSpork {
	return &ImplementedSpork{
		SporkId: HexToHashPanic(SporkIdStr),
	}
}
