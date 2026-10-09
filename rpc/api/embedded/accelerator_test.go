package embedded

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// Same receiver-vs-aux sizing mistake as FusionEntryList: decoding a project
// with phases into a fresh receiver panicked with index out of range.
func TestProjectUnmarshalFreshReceiver(t *testing.T) {
	original := &Project{
		Id:             types.NewHash([]byte{1}),
		Owner:          types.AcceleratorContract,
		Name:           "project",
		ZnnFundsNeeded: big.NewInt(100),
		QsrFundsNeeded: big.NewInt(200),
		Status:         definition.VotingStatus,
		PhaseIds:       []types.Hash{types.NewHash([]byte{2})},
		Votes:          &definition.VoteBreakdown{Id: types.NewHash([]byte{1})},
		Phases: []*Phase{{
			Phase: &definition.Phase{
				Id:             types.NewHash([]byte{2}),
				ProjectId:      types.NewHash([]byte{1}),
				Name:           "phase",
				ZnnFundsNeeded: big.NewInt(10),
				QsrFundsNeeded: big.NewInt(20),
			},
			Votes: &definition.VoteBreakdown{Id: types.NewHash([]byte{2})},
		}},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	decoded := new(Project)
	if err := json.Unmarshal(data, decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Phases) != 1 {
		t.Fatalf("decoded %d phases, want 1", len(decoded.Phases))
	}
	reencoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(reencoded) != string(data) {
		t.Fatalf("round-trip mismatch:\n got  %s\n want %s", reencoded, data)
	}
}
