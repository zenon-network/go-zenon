package types

type BasePlasma struct {
	Fusion uint64
	Pow    uint64
}

func NewBasePlasma(fusion uint64, pow uint64) BasePlasma {
	return BasePlasma{Fusion: fusion, Pow: pow}
}

func (p *BasePlasma) Add(plasma BasePlasma) {
	p.Fusion += plasma.Fusion
	p.Pow += plasma.Pow
}

func (p *BasePlasma) Total() uint64 {
	return p.Fusion + p.Pow
}
