package types

var (
	AcceleratorSpork        = NewImplementedSpork("6d2b1e6cb4025f2f45533f0fe22e9b7ce2014d91cc960471045fa64eee5a6ba3")
	HtlcSpork               = NewImplementedSpork("ceb7e3808ef17ea910adda2f3ab547be4cdfb54de8400ce3683258d06be1354b")
	BridgeAndLiquiditySpork = NewImplementedSpork("ddd43466769461c5b5d109c639da0f50a7eeb96ad6e7274b1928a35c431d7b1b")

	ImplementedSporksMap = map[Hash]bool{
		AcceleratorSpork.SporkId:        true,
		HtlcSpork.SporkId:               true,
		BridgeAndLiquiditySpork.SporkId: true,
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
