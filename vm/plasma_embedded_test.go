package vm

import (
	"testing"

	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm/constants"
)

// EmbeddedSendBasePlasma prices the argument bytes of an embedded send past
// the 4-byte selector and bounds the whole Data at MaxDataLength.
func TestEmbeddedSendBasePlasma(t *testing.T) {
	const tier = constants.EmbeddedSimplePlasma
	cases := []struct {
		dataLength int
		want       uint64
		err        error
	}{
		{0, tier, nil},
		{4, tier, nil},
		{5, tier + constants.ABByteDataPlasma, nil},
		{4 + 32, tier + 32*constants.ABByteDataPlasma, nil},
		{constants.MaxDataLength, tier + (constants.MaxDataLength-4)*constants.ABByteDataPlasma, nil},
		{constants.MaxDataLength + 1, 0, verifier.ErrABDataTooBig},
	}
	for _, c := range cases {
		got, err := EmbeddedSendBasePlasma(tier, c.dataLength)
		if err != c.err {
			t.Fatalf("data length %d: error %v, want %v", c.dataLength, err, c.err)
		}
		if got != c.want {
			t.Fatalf("data length %d: plasma %d, want %d", c.dataLength, got, c.want)
		}
	}
}
