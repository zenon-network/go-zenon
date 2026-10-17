package types

// BasePlasma splits a block's base plasma into the part paid by fused
// plasma and the part paid by proof of work, the two resources Dynamic
// Plasma prices separately (see package dp).
type BasePlasma struct {
	Fusion uint64
	Pow    uint64
}

// NewBasePlasma returns a BasePlasma with the given fusion and PoW parts.
func NewBasePlasma(fusion uint64, pow uint64) BasePlasma {
	return BasePlasma{Fusion: fusion, Pow: pow}
}

// Add adds plasma's fusion and PoW parts to p, in place.
func (p *BasePlasma) Add(plasma BasePlasma) {
	p.Fusion += plasma.Fusion
	p.Pow += plasma.Pow
}

// Total returns the fusion and PoW parts summed.
func (p *BasePlasma) Total() uint64 {
	return p.Fusion + p.Pow
}
