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

// TestSEC_ContractReceiveValidation exercises the contract-receive validation.
// It is enforced without condition when a block is admitted to the account
// pool or generated locally (AccountBlockTransaction), which is not a
// consensus decision, and from definition.CommunitySporkAddressRenewalStartHeight
// on when a block is validated as part of a momentum being inserted
// (AccountBlockTransactionInMomentum, which receives that momentum's height).
// A contract receive
// carries the contract's outgoing sends as descendant blocks; the parent
// commits only to each descendant's hash, not its content, and ComputeHash
// covers neither the parent's nor a descendant's BasePlasma/TotalPlasma.
// Without the rule a peer can keep an honest hash while changing a
// descendant's amount, recipient or data, padding a descendant's credential
// fields, or raising the parent's or a descendant's unhashed plasma fields.
//
// The test takes the node's own committed refund receive as the honest block
// and drives the transaction verifier directly, the layer the regenerated
// block passes through before the account pool serializes it. The honest
// block is accepted in every case. On the admission path every tampered copy
// is rejected whatever the rule height. On the momentum path a tampered copy
// is accepted when the momentum is below the rule height (the behaviour every
// node had before the rule, kept so that nodes agree on history up to the
// height) and rejected from the rule height on, with the boundary pinned on
// both sides; the block's acknowledged momentum plays no part, so a source
// send confirmed just before the height cannot be received under the old
// rule afterwards. Local only.
func TestSEC_ContractReceiveValidation(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()

	savedExpiration := constants.FuseExpiration
	constants.FuseExpiration = 10
	defer func() { constants.FuseExpiration = savedExpiration }()
	savedRuleHeight := definition.CommunitySporkAddressRenewalStartHeight
	defer func() { definition.CommunitySporkAddressRenewalStartHeight = savedRuleHeight }()

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
	acknowledged := honest.MomentumAcknowledged.Height
	if acknowledged == 0 {
		t.Fatalf("honest receive acknowledges no momentum")
	}
	frontier, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.FailIfErr(t, err)
	// The momentum a block validated now would be inserted in.
	inclusion := frontier.Height + 1
	if acknowledged >= inclusion {
		t.Fatalf("test premise broken: acknowledged %d is not below the inclusion momentum %d", acknowledged, inclusion)
	}

	v := verifier.NewAccountBlockVerifier(z.Chain(), z.Consensus())

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

	// Each regime picks a verifier entry point and a rule height, and states
	// whether the tampered copies must be rejected. The honest block must pass
	// in every regime.
	admission := func(tx *nom.AccountBlockTransaction) error { return v.AccountBlockTransaction(tx) }
	inMomentum := func(height uint64) func(*nom.AccountBlockTransaction) error {
		return func(tx *nom.AccountBlockTransaction) error { return v.AccountBlockTransactionInMomentum(tx, height) }
	}
	regimes := []struct {
		name       string
		ruleHeight uint64
		verify     func(*nom.AccountBlockTransaction) error
		rejects    bool
	}{
		{"admission, rule height far above: always strict", inclusion + 1000, admission, true},
		{"admission, rule height zero: strict", 0, admission, true},
		{"momentum below the rule height: old behaviour", inclusion + 1, inMomentum(inclusion), false},
		{"momentum at the rule height: new rule applies", inclusion, inMomentum(inclusion), true},
		// The receive acknowledges a momentum below the rule height; the
		// momentum it is inserted in is at the rule height: the new rule
		// applies.
		{"momentum at the rule height, acknowledged momentum below it: new rule applies", acknowledged + 1, inMomentum(acknowledged + 1), true},
		{"momentum far above the rule height: new rule applies", 0, inMomentum(inclusion), true},
	}
	for _, regime := range regimes {
		t.Run(regime.name, func(t *testing.T) {
			definition.CommunitySporkAddressRenewalStartHeight = regime.ruleHeight

			if err := regime.verify(&nom.AccountBlockTransaction{Block: honest.Copy()}); err != nil {
				t.Fatalf("honest contract receive must be accepted: %v", err)
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					tampered := honest.Copy()
					if tc.onParent {
						tc.mutate(tampered)
					} else {
						tc.mutate(tampered.DescendantBlocks[0])
					}
					err := regime.verify(&nom.AccountBlockTransaction{Block: tampered})
					if !regime.rejects {
						if err != nil {
							t.Fatalf("%s: below the rule height the verifier must judge by the old rule, got %v", tc.name, err)
						}
						return
					}
					if err == nil {
						t.Fatalf("verifier accepted a tampered block (%s)", tc.name)
					}
					if !errors.Is(err, tc.wantErr) {
						t.Fatalf("%s: expected %v, got %v", tc.name, tc.wantErr, err)
					}
					if !strings.Contains(err.Error(), tc.wantSubst) {
						t.Fatalf("%s: expected error to mention %q, got %v", tc.name, tc.wantSubst, err)
					}
				})
			}
		})
	}

	// In a momentum below the rule height only the new checks are off. The
	// checks every block already received stay in force, so a tampered parent
	// hash and an invalid descendant version are still rejected there.
	t.Run("old checks still apply in a momentum below the rule height", func(t *testing.T) {
		definition.CommunitySporkAddressRenewalStartHeight = inclusion + 1
		controls := []struct {
			name      string
			onParent  bool
			mutate    func(*nom.AccountBlock)
			wantErr   error
			wantSubst string
		}{
			{"parent hash tampered", true, func(p *nom.AccountBlock) { p.Hash = nonzero }, verifier.ErrABHashInvalid, "hash is different"},
			{"descendant version invalid", false, func(d *nom.AccountBlock) { d.Version = 99 }, verifier.ErrABDescendantVerify, "version is invalid"},
		}
		for _, tc := range controls {
			t.Run(tc.name, func(t *testing.T) {
				tampered := honest.Copy()
				if tc.onParent {
					tc.mutate(tampered)
				} else {
					tc.mutate(tampered.DescendantBlocks[0])
				}
				err := v.AccountBlockTransactionInMomentum(&nom.AccountBlockTransaction{Block: tampered}, inclusion)
				if err == nil {
					t.Fatalf("%s: an old check was skipped below the rule height", tc.name)
				}
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("%s: expected %v, got %v", tc.name, tc.wantErr, err)
				}
				if !strings.Contains(err.Error(), tc.wantSubst) {
					t.Fatalf("%s: expected error to mention %q, got %v", tc.name, tc.wantSubst, err)
				}
			})
		}
	})
}
