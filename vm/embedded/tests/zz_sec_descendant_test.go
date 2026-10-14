package tests

import (
	"crypto/ed25519"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// TestSEC_ContractReceiveValidation_RejectsTamper exercises the contract-receive
// validation, enforced unconditionally in this build. A contract receive carries
// the contract's outgoing sends as descendant blocks; the parent commits only to
// each descendant's hash, not its content, and ComputeHash covers neither the
// parent's nor a descendant's BasePlasma/TotalPlasma. Before this rule a peer
// could keep an honest hash while changing a descendant's amount, recipient or
// data, padding a descendant's credential fields, or raising the parent's or a
// descendant's unhashed plasma fields.
//
// The test takes the node's own committed refund receive as the honest block and
// drives the transaction verifier directly, the layer the regenerated block
// passes through before the account pool serializes it. The honest block is
// accepted; every tampered copy is rejected. Local only.
func TestSEC_ContractReceiveValidation_RejectsTamper(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()

	savedExpiration := constants.FuseExpiration
	constants.FuseExpiration = 10
	defer func() { constants.FuseExpiration = savedExpiration }()

	plasmaApi := embedded.NewPlasmaApi(z)
	fuseAmount := big.NewInt(100 * g.Zexp)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, g.User1.Address),
		TokenStandard: types.QsrTokenStandard,
		Amount:        fuseAmount,
	}, nil, mock.SkipVmChanges)
	z.InsertMomentumsTo(26)
	autoreceive(t, z, g.User1.Address)
	z.InsertMomentumsTo(45)

	entries, err := plasmaApi.GetEntriesByAddress(g.User1.Address, 0, 10)
	common.FailIfErr(t, err)
	var fuseId types.Hash
	for _, e := range entries.Fusions {
		if e.QsrAmount.Cmp(fuseAmount) == 0 {
			fuseId = e.Id
		}
	}
	if fuseId.IsZero() {
		t.Fatalf("fusion entry not found")
	}

	// Cancel the fuse so the plasma contract owes User1 a QSR refund, then let the
	// node receive it. The committed contract receive carries the refund as its
	// single descendant and is the honest block a peer's forgery would imitate.
	cancelSend := z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.User1.Address,
		ToAddress: types.PlasmaContract,
		Data:      definition.ABIPlasma.PackMethodPanic(definition.CancelFuseMethodName, fuseId),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	honest, err := z.Chain().GetFrontierAccountStore(types.PlasmaContract).Frontier()
	common.FailIfErr(t, err)
	if honest == nil || honest.FromBlockHash != cancelSend.Hash {
		t.Fatalf("plasma frontier is not the refund receive for the cancel-fuse send")
	}
	if len(honest.DescendantBlocks) != 1 {
		t.Fatalf("expected one refund descendant, got %d", len(honest.DescendantBlocks))
	}

	v := verifier.NewAccountBlockVerifier(z.Chain(), z.Consensus())

	// The honest receive is accepted.
	if err := v.AccountBlockTransaction(&nom.AccountBlockTransaction{Block: honest.Copy()}); err != nil {
		t.Fatalf("honest contract receive must be accepted: %v", err)
	}

	nonzero := types.HexToHashPanic("00000000000000000000000000000000000000000000000000000000000000ab")
	cases := []struct {
		name      string
		onParent  bool
		mutate    func(*nom.AccountBlock)
		wantErr   error
		wantSubst string
	}{
		// Descendant content is bound to its hash.
		{"descendant amount doubled", false, func(d *nom.AccountBlock) { d.Amount = new(big.Int).Mul(d.Amount, big.NewInt(2)) }, verifier.ErrABDescendantVerify, "hash is different"},
		{"descendant recipient changed", false, func(d *nom.AccountBlock) { d.ToAddress = g.User2.Address }, verifier.ErrABDescendantVerify, "hash is different"},
		{"descendant data appended", false, func(d *nom.AccountBlock) { d.Data = append(d.Data, 0xff) }, verifier.ErrABDescendantVerify, "hash is different"},
		// Descendant credentials must be empty.
		{"descendant oversized signature", false, func(d *nom.AccountBlock) { d.Signature = make([]byte, ed25519.SignatureSize+1) }, verifier.ErrABDescendantVerify, "signature must be zero"},
		{"descendant oversized public key", false, func(d *nom.AccountBlock) { d.PublicKey = make([]byte, ed25519.PublicKeySize+1) }, verifier.ErrABDescendantVerify, "publicKey must be zero"},
		{"descendant canonical-size signature", false, func(d *nom.AccountBlock) { d.Signature = make([]byte, ed25519.SignatureSize) }, verifier.ErrABDescendantVerify, "signature must be zero"},
		{"descendant canonical-size public key", false, func(d *nom.AccountBlock) { d.PublicKey = make([]byte, ed25519.PublicKeySize) }, verifier.ErrABDescendantVerify, "publicKey must be zero"},
		// Descendant unhashed VM-only fields must be zero.
		{"descendant non-zero changes-hash", false, func(d *nom.AccountBlock) { d.ChangesHash = nonzero }, verifier.ErrABDescendantVerify, "non-canonical"},
		{"descendant base plasma set", false, func(d *nom.AccountBlock) { d.BasePlasma = 1 }, verifier.ErrABDescendantVerify, "non-canonical"},
		{"descendant total plasma set", false, func(d *nom.AccountBlock) { d.TotalPlasma = 1 }, verifier.ErrABDescendantVerify, "non-canonical"},
		// Parent unhashed plasma fields must be zero.
		{"parent base plasma set", true, func(p *nom.AccountBlock) { p.BasePlasma = 7 }, verifier.ErrABPlasmaMustBeZero, "plasma must be zero"},
		{"parent total plasma set", true, func(p *nom.AccountBlock) { p.TotalPlasma = 1 << 40 }, verifier.ErrABPlasmaMustBeZero, "plasma must be zero"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tampered := honest.Copy()
			if tc.onParent {
				tc.mutate(tampered)
			} else {
				tc.mutate(tampered.DescendantBlocks[0])
			}
			err := v.AccountBlockTransaction(&nom.AccountBlockTransaction{Block: tampered})
			if err == nil {
				t.Fatalf("verifier accepted a tampered block (%s)", tc.name)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("%s: expected %v, got %v", tc.name, tc.wantErr, err)
			}
			if !strings.Contains(err.Error(), tc.wantSubst) {
				t.Fatalf("%s: expected error to mention %q, got %v", tc.name, tc.wantSubst, err)
			}
			t.Logf("rejected (%s): %v", tc.name, err)
		})
	}
}
