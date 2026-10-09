package embedded

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/common/types"
)

// The slice was sized from the receiver's OLD length but indexed over the
// decoded aux entries - decoding a non-empty list into a fresh receiver
// panicked with index out of range.
func TestFusionEntryListUnmarshalFreshReceiver(t *testing.T) {
	original := &FusionEntryList{
		QsrAmount: big.NewInt(5000),
		Count:     1,
		Fusions: []*FusionEntry{{
			QsrAmount:        big.NewInt(5000),
			Beneficiary:      types.PlasmaContract,
			ExpirationHeight: 42,
			Id:               types.NewHash([]byte{1}),
			IsRevocable:      true,
		}},
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}

	decoded := new(FusionEntryList)
	if err := json.Unmarshal(data, decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Fusions) != 1 {
		t.Fatalf("decoded %d fusions, want 1", len(decoded.Fusions))
	}
	reencoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(reencoded) != string(data) {
		t.Fatalf("round-trip mismatch:\n got  %s\n want %s", reencoded, data)
	}
}
