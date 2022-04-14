package types

var (
	AcceleratorSpork     = NewImplementedSpork("6d2b1e6cb4025f2f45533f0fe22e9b7ce2014d91cc960471045fa64eee5a6ba3")
	ImplementedSporksMap = map[Hash]bool{
		AcceleratorSpork.SporkId: true,
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
