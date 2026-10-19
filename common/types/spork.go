package types

var (
	// AcceleratorSpork enables the Accelerator-Z project methods and
	// pillar voting on them, and adds the liquidity contract's Fund and
	// BurnZnn methods; the accelerator's Donate and the liquidity
	// contract's Update and Donate exist from genesis.
	AcceleratorSpork = NewImplementedSpork("6d2b1e6cb4025f2f45533f0fe22e9b7ce2014d91cc960471045fa64eee5a6ba3")
	// HtlcSpork activates the HTLC embedded contract.
	HtlcSpork = NewImplementedSpork("ceb7e3808ef17ea910adda2f3ab547be4cdfb54de8400ce3683258d06be1354b")
	// PtlcSpork activates the PTLC embedded contract.
	PtlcSpork = NewImplementedSpork("e59f1e97f1096a18c2bc8421345b2633b11b839adb153e96dc65034c95f83f8d")
	// BridgeAndLiquiditySpork activates the bridge embedded contract and
	// adds the administration and staking methods to the liquidity
	// contract, which exists from genesis.
	BridgeAndLiquiditySpork = NewImplementedSpork("ddd43466769461c5b5d109c639da0f50a7eeb96ad6e7274b1928a35c431d7b1b")

	// PLACEHOLDER HASH. Sporks are identified by the hash of their
	// CreateSpork transaction, which is deterministic from the tx
	// contents and therefore not known until governance broadcasts the
	// proposal. The release flow is:
	//
	//   1. Ship this binary with the placeholder. No on-chain spork can
	//      match it, so IsSporkActive always returns false. Safe-by-default.
	//   2. Governance broadcasts the CreateSpork tx. The resulting
	//      send-block hash is the real SporkId.
	//   3. Replace the placeholder with that hash, release a new binary,
	//      coordinate the operator upgrade campaign.
	//   4. Governance broadcasts ActivateSpork. After
	//      SporkMinHeightDelay momentums, EnforcementHeight passes and
	//      every node activates the spork.
	//
	// Tests and devnet override ImplementedSporksMap at runtime with
	// the locally-generated hash (see vm/embedded/tests/spork_test.go
	// for the existing pattern).

	// Libp2pSpork gates the activation of the libp2p networking stack.
	// Until this spork's EnforcementHeight is reached on a node's local
	// chain, the legacy (devp2p/RLPX) backend is in use; at activation,
	// every node atomically switches to the libp2p backend.
	Libp2pSpork = NewImplementedSpork("0000000000000000000000000000000000000000000000000000000000000001")

	// DynamicPlasmaSpork gates the activation of the dynamic plasma pricing
	// model. Until this spork's EnforcementHeight is reached, the legacy
	// fixed-price plasma model is in use; at activation, momentums switch
	// to version 2 with adaptive fusion/work prices.
	DynamicPlasmaSpork = NewImplementedSpork("0000000000000000000000000000000000000000000000000000000000000002")

	// ImplementedSporksMap is the set of spork IDs this build knows
	// how to enforce. After every momentum insertion the momentum pool
	// checks each activated spork that has reached its enforcement
	// height against this map; if any is missing, the node terminates
	// and asks for a binary upgrade (see chain/momentum_pool.go).
	ImplementedSporksMap = map[Hash]bool{
		AcceleratorSpork.SporkId:        true,
		HtlcSpork.SporkId:               true,
		PtlcSpork.SporkId:               true,
		BridgeAndLiquiditySpork.SporkId: true,
		Libp2pSpork.SporkId:             true,
		DynamicPlasmaSpork.SporkId:      true,
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
