package tests

import (
	"crypto/ed25519"
	"crypto/rand"
	"math/big"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// freezeMultisigBlock runs template through the real VM/verifier pipeline (PreviousHash/Height/
// MomentumAcknowledged/FusedPlasma + block.Hash) without an actual single-sig signature, exactly
// what an external wallet would need a node's help computing before it can collect threshold
// signatures against the frozen block's hash. The structural pipeline mutates the template in
// place; the trailing ErrABMultisigAuthMissing (MultisigAuth not assembled yet) is expected and
// discarded here -- everything before that check (Hash included) has already been written onto
// template.
func freezeMultisigBlock(t *testing.T, supervisor *vm.Supervisor, template *nom.AccountBlock) *nom.AccountBlock {
	t.Helper()
	noopSign := func(data []byte) ([]byte, *types.Address, []byte, error) {
		return nil, nil, nil, nil
	}
	_, err := supervisor.GenerateFromTemplate(template, noopSign)
	if err != verifier.ErrABMultisigAuthMissing {
		t.Fatalf("expected freezing to fail only with ErrABMultisigAuthMissing, got %v", err)
	}
	if template.Hash.IsZero() {
		t.Fatalf("expected block.Hash to be computed while freezing")
	}
	return template
}

type multisigWalletSigner struct {
	pub  ed25519.PublicKey
	pair *wallet.KeyPair
}

func genMultisigWalletSigner(t *testing.T) multisigWalletSigner {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	common.FailIfErr(t, err)
	return multisigWalletSigner{pub: pub, pair: &wallet.KeyPair{Public: pub, Private: priv}}
}

// fuseQsrTo fuses qsrAmount QSR from g.User1 to beneficiary so it has enough plasma to send its
// own blocks (a freshly-created multisig account starts with none).
func fuseQsrTo(t *testing.T, z mock.MockZenon, beneficiary types.Address, qsrAmount int64) {
	t.Helper()
	z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     types.PlasmaContract,
		Data:          definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, beneficiary),
		TokenStandard: types.QsrTokenStandard,
		Amount:        big.NewInt(qsrAmount * g.Zexp),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
}

// TestMultisigWallet_CreateTemplate_PublishRawTransaction builds a CreateMultisig send with the
// wallet template helper, signs it normally (single-sig, the creator's own funded account, like
// any other embedded-contract call), and submits it through the existing PublishRawTransaction
// RPC path -- end-to-end proof that the wallet-built template is byte-compatible with what the
// chain expects.
func TestMultisigWallet_CreateTemplate_PublishRawTransaction(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activateMultisig(t, z)

	s1 := genMultisigWalletSigner(t)
	s2 := genMultisigWalletSigner(t)
	signers := []ed25519.PublicKey{g.User1.Public, s1.pub, s2.pub}

	template := wallet.NewCreateMultisigTemplate(g.User1.Address, 42, 2, signers)

	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	transaction, err := supervisor.GenerateFromTemplate(template, g.User1.Signer)
	common.FailIfErr(t, err)

	ledgerApi := api.NewLedgerApi(z)
	common.FailIfErr(t, ledgerApi.PublishRawTransaction(&api.AccountBlock{AccountBlock: *transaction.Block}))
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	derived := wallet.DeriveMultisigAddress(g.User1.Public, 42)
	if derived != types.MultisigCreationToAddress(g.User1.Public, 42) {
		t.Fatalf("DeriveMultisigAddress diverges from the canonical derivation")
	}

	rec, err := definition.GetMultisigRecord(z.Chain().GetFrontierAccountStore(types.MultisigContract).Storage(), derived)
	common.FailIfErr(t, err)
	if rec == nil {
		t.Fatal("expected a multisig record to exist after CreateMultisig")
	}
	if rec.Active.Threshold != 2 || len(rec.Active.Signers) != 3 {
		t.Fatalf("unexpected active policy: %+v", rec.Active)
	}
}

// TestMultisigWallet_FullRoundTrip is the strongest end-to-end proof of the wallet helpers: build
// a ChangePolicy template with the wallet helper for a freshly created multisig account, freeze
// it (PreviousHash/Height/MomentumAcknowledged/FusedPlasma/Hash, mirroring what a node-assisted
// wallet would do before collecting signatures), partial-sign it with >= threshold signers,
// assemble MultisigAuth, and submit it through PublishRawTransaction -- exercising the full
// wallet -> sign -> assemble -> submit -> verify round trip against the real verifier.
func TestMultisigWallet_FullRoundTrip(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activateMultisig(t, z)

	s1 := genMultisigWalletSigner(t)
	s2 := genMultisigWalletSigner(t)
	signers := []ed25519.PublicKey{g.User1.Public, s1.pub, s2.pub}
	const nonce = 7
	const threshold = 2

	createTemplate := wallet.NewCreateMultisigTemplate(g.User1.Address, nonce, threshold, signers)
	z.InsertSendBlock(createTemplate, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	multisigAddr := wallet.DeriveMultisigAddress(g.User1.Public, nonce)
	if rec, err := definition.GetMultisigRecord(z.Chain().GetFrontierAccountStore(types.MultisigContract).Storage(), multisigAddr); err != nil {
		t.Fatal(err)
	} else if rec == nil {
		t.Fatal("expected multisig record to exist before exercising its first block")
	}

	// the freshly created multisig account starts with no fused plasma of its own.
	fuseQsrTo(t, z, multisigAddr, 50)

	changeTemplate := wallet.NewChangePolicyTemplate(multisigAddr, threshold, signers, false)
	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	frozen := freezeMultisigBlock(t, supervisor, changeTemplate)

	sigs := [][]byte{
		wallet.SignMultisigBlock(frozen, g.User1),
		wallet.SignMultisigBlock(frozen, s1.pair),
	}
	wallet.AssembleMultisigAuth(frozen, sigs)

	ledgerApi := api.NewLedgerApi(z)
	common.FailIfErr(t, ledgerApi.PublishRawTransaction(&api.AccountBlock{AccountBlock: *frozen}))
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	frontier := z.Chain().GetFrontierAccountStore(multisigAddr).Identifier()
	if frontier.Height != 1 {
		t.Fatalf("expected the multisig account's first block to be confirmed, height = %v", frontier.Height)
	}

	rec, err := definition.GetMultisigRecord(z.Chain().GetFrontierAccountStore(types.MultisigContract).Storage(), multisigAddr)
	common.FailIfErr(t, err)
	if rec == nil || rec.Pending == nil {
		t.Fatalf("expected the ChangePolicy to have staged a pending change, got %+v", rec)
	}
}

// TestMultisigApi_GetPolicy exercises the read-only RPC inspection module: the active policy
// returned must match the on-chain record, and once a ChangePolicy stages a pending
// change, the RPC response must reflect both Active/Pending before maturity and the promoted
// Active (no more Pending) after maturity -- using the same definition.Promote() the verifier
// and ChangePolicy.ReceiveBlock use.
func TestMultisigApi_GetPolicy(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activateMultisig(t, z)

	s1 := genMultisigWalletSigner(t)
	s2 := genMultisigWalletSigner(t)
	signers := []ed25519.PublicKey{g.User1.Public, s1.pub, s2.pub}
	const nonce = 11
	const threshold = 2

	createTemplate := wallet.NewCreateMultisigTemplate(g.User1.Address, nonce, threshold, signers)
	z.InsertSendBlock(createTemplate, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	multisigAddr := wallet.DeriveMultisigAddress(g.User1.Public, nonce)
	multisigApi := embedded.NewMultisigApi(z)

	info, err := multisigApi.GetPolicy(multisigAddr, nil)
	common.FailIfErr(t, err)
	if info == nil || info.Active == nil {
		t.Fatalf("expected an active policy, got %+v", info)
	}
	if info.Active.Threshold != threshold || len(info.Active.Signers) != len(signers) {
		t.Fatalf("unexpected active policy from RPC: %+v", info.Active)
	}
	if info.Pending != nil {
		t.Fatalf("expected no pending change yet, got %+v", info.Pending)
	}

	fuseQsrTo(t, z, multisigAddr, 50)

	newSigners := []ed25519.PublicKey{g.User1.Public, s1.pub}
	changeTemplate := wallet.NewChangePolicyTemplate(multisigAddr, 2, newSigners, false)
	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	frozen := freezeMultisigBlock(t, supervisor, changeTemplate)
	wallet.AssembleMultisigAuth(frozen, [][]byte{
		wallet.SignMultisigBlock(frozen, g.User1),
		wallet.SignMultisigBlock(frozen, s1.pair),
	})

	ledgerApi := api.NewLedgerApi(z)
	common.FailIfErr(t, ledgerApi.PublishRawTransaction(&api.AccountBlock{AccountBlock: *frozen}))
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	pendingHeight := z.Chain().GetFrontierMomentumStore().Identifier().Height

	beforeMaturity := pendingHeight
	info, err = multisigApi.GetPolicy(multisigAddr, &beforeMaturity)
	common.FailIfErr(t, err)
	if info.Active.Threshold != threshold || len(info.Active.Signers) != len(signers) {
		t.Fatalf("expected Active to still be the old policy before maturity, got %+v", info.Active)
	}
	if info.Pending == nil || info.PendingHeight == 0 {
		t.Fatalf("expected a pending change to be reported before maturity, got %+v", info)
	}
	if len(info.Pending.Signers) != len(newSigners) {
		t.Fatalf("unexpected pending policy: %+v", info.Pending)
	}

	z.InsertMomentumsTo(pendingHeight + 60)
	afterMaturity := pendingHeight + 60
	info, err = multisigApi.GetPolicy(multisigAddr, &afterMaturity)
	common.FailIfErr(t, err)
	if info.Active.Threshold != 2 || len(info.Active.Signers) != len(newSigners) {
		t.Fatalf("expected Active to be the matured policy, got %+v", info.Active)
	}
	if info.Pending != nil {
		t.Fatalf("expected no pending change once matured, got %+v", info.Pending)
	}
}
