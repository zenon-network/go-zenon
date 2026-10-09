// Package switcher implements the spork-gated p2p server wrapper that
// owns both the legacy (devp2p/RLPX) and libp2p networking backends,
// switching between them when the libp2p activation spork's
// EnforcementHeight is reached on the local chain.
//
// The switcher is the unit of integration: callers (node.go) construct
// one and treat it as a p2p.Server. The switcher decides at Start() and
// on every spork-watcher tick which backend should be live, and handles
// the atomic transition when activation fires.
//
// This package deliberately lives outside p2p/ root to avoid an import
// cycle: it needs to import both p2p/legacy and p2p/libp2p (which both
// import p2p root for shared interfaces/types). Placing the wrapper in
// a sibling subpackage keeps p2p/ root cycle-free.
package switcher

// SporkOracle is the minimal chain dependency the switcher carries.
// node.go constructs the concrete adapter that satisfies this by
// querying chain.GetFrontierMomentumStore().IsSporkActive(...) for the
// types.Libp2pSpork entry.
//
// Kept as a narrow interface so the switcher does not need to import
// the chain package, and so unit tests can supply a fake oracle.
type SporkOracle interface {
	// IsLibp2pActive reports whether the libp2p activation spork is
	// active on the caller's local chain — i.e. its EnforcementHeight
	// is ≤ the current frontier momentum height. Implementations should
	// be cheap (single map lookup against cached frontier state) since
	// the switcher polls this on a 1s ticker.
	IsLibp2pActive() bool
}
