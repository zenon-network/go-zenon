package types

import "testing"

func TestImplementedSporksMap_RegistersPillarJailSpork(t *testing.T) {
	if !ImplementedSporksMap[PillarJailSpork.SporkId] {
		t.Fatalf("PillarJailSpork %v is not in ImplementedSporksMap, so a node on this build would terminate when the spork is enforced", PillarJailSpork.SporkId)
	}
}
