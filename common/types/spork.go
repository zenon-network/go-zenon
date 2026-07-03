package types

var (
	// AcceleratorSpork activates the Accelerator-Z embedded contract
	// together with pillar/Accelerator voting.
	AcceleratorSpork = NewImplementedSpork("6d2b1e6cb4025f2f45533f0fe22e9b7ce2014d91cc960471045fa64eee5a6ba3")
	// HtlcSpork activates the HTLC embedded contract.
	HtlcSpork = NewImplementedSpork("ceb7e3808ef17ea910adda2f3ab547be4cdfb54de8400ce3683258d06be1354b")
	// BridgeAndLiquiditySpork activates the bridge and liquidity
	// embedded contracts.
	BridgeAndLiquiditySpork = NewImplementedSpork("ddd43466769461c5b5d109c639da0f50a7eeb96ad6e7274b1928a35c431d7b1b")

	// ImplementedSporksMap is the set of spork IDs this build knows
	// how to enforce. After every momentum insertion the momentum pool
	// checks each activated spork that has reached its enforcement
	// height against this map; if any is missing, the node terminates
	// and asks for a binary upgrade (see chain/momentum_pool.go).
	ImplementedSporksMap = map[Hash]bool{
		AcceleratorSpork.SporkId:        true,
		HtlcSpork.SporkId:               true,
		BridgeAndLiquiditySpork.SporkId: true,
	}
)

// ImplementedSpork identifies a protocol upgrade supported by this
// build. SporkId is the hash that on-chain spork entries (created via
// the spork embedded contract) must match for the feature to be
// recognized.
type ImplementedSpork struct {
	SporkId Hash
}

// NewImplementedSpork builds an ImplementedSpork from the hex form of
// its spork ID, panicking on invalid input. It is meant for the
// hard-coded spork IDs declared in this package.
func NewImplementedSpork(SporkIdStr string) *ImplementedSpork {
	return &ImplementedSpork{
		SporkId: HexToHashPanic(SporkIdStr),
	}
}
