package types

import "testing"

func TestLibp2pSporkStaysImplemented(t *testing.T) {
	if !ImplementedSporksMap[Libp2pSpork.SporkId] {
		t.Fatal("Libp2pSpork missing from ImplementedSporksMap: nodes halt on chains that activate it")
	}
}
