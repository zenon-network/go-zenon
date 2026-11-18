package tests

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// TestSEC_UnknownAccountWithPreviousHash drives the account-block verifier
// with a block for an address that has no account chain yet but claims a
// height above one and a non-zero previous hash. The verifier cannot find
// the previous block through the acknowledged momentum, falls back to the
// account's global frontier, and that frontier does not exist. The block
// must be rejected as "previous block is missing", not abort the verifier.
// The block carries no valid signature; the context lookup runs before the
// credential checks, which is what makes the input reachable unauthenticated
// through both block ingress paths. Local only.
func TestSEC_UnknownAccountWithPreviousHash(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	common.FailIfErr(t, err)
	fresh := types.PubKeyToAddress(pub)
	frontier := z.Chain().GetFrontierMomentumStore().Identifier()

	v := verifier.NewAccountBlockVerifier(z.Chain(), z.Consensus())
	for _, height := range []uint64{2, 3, 1000} {
		block := &nom.AccountBlock{
			BlockType:            nom.BlockTypeUserSend,
			Address:              fresh,
			Height:               height,
			PreviousHash:         types.HexToHashPanic("0000000000000000000000000000000000000000000000000000000000000001"),
			MomentumAcknowledged: frontier,
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("height %d: verifier panicked: %v", height, r)
				}
			}()
			if err := v.AccountBlock(block); err != verifier.ErrABPreviousMissing {
				t.Fatalf("height %d: expected %v, got %v", height, verifier.ErrABPreviousMissing, err)
			}
		}()
	}
}
