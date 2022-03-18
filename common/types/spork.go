package types

var (
	AcceleratorSpork     = NewImplementedSpork("97f0a9636a5cc633dfa3814e431f19c1974536eef3d1ebb713db50464dc5e750")
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
