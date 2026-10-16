package tests

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// activateMultisig mirrors activateHtlc/activateDp: creates and activates a fresh spork, then
// retargets types.MultisigSpork.SporkId at the created spork's id and registers it as enforced
// (saveSporkState in the calling test restores the global afterwards).
func activateMultisig(t *testing.T, z mock.MockZenon) {
	saveSporkState(t)
	sporkAPI := embedded.NewSporkApi(z)
	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"spork-multisig",              // name
			"activate spork for multisig", // description
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()

	sporkList, _ := sporkAPI.GetAll(0, 10)
	id := sporkList.List[0].Id

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName,
			id, // id
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.MultisigSpork.SporkId = id
	types.ImplementedSporksMap[id] = true
	z.InsertMomentumsTo(20)
}

func genMultisigSigner(t *testing.T) ed25519.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	common.FailIfErr(t, err)
	return pub
}

// TestMultisig_PreSporkInert: before the multisig spork is enforced, MultisigContract is not in
// the GetEmbeddedMethod map, so a send to it fails the same way every other not-yet-enforced
// embedded contract does (verified against the live applySend codepath, vm/vm.go:139-161):
// ErrContractDoesntExist is returned synchronously by the send itself (the block is never
// inserted), and no policy is ever created. This is the existing, pre-multisig codebase behaviour
// for all spork-gated contracts (htlc/bridge/accelerator/dynamic-plasma alike), not something
// this phase introduces.
func TestMultisig_PreSporkInert(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	creator := g.User1.Address
	creatorKey := g.User1.Public
	otherSigner := genMultisigSigner(t)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   creator,
		ToAddress: types.MultisigContract,
		Data: definition.ABIMultisig.PackMethodPanic(definition.CreateMultisigMethodName,
			uint64(1), uint8(2), [][]byte{creatorKey, otherSigner}),
	}, constants.ErrContractDoesntExist, mock.SkipVmChanges)
	z.InsertNewMomentum()

	derived := types.MultisigCreationToAddress(creatorKey, 1)
	rec, err := definition.GetMultisigRecord(z.Chain().GetFrontierAccountStore(types.MultisigContract).Storage(), derived)
	common.FailIfErr(t, err)
	if rec != nil {
		t.Fatalf("expected no multisig record pre-spork, got %+v", rec)
	}
}

// TestMultisig_ActivatedCreateResolves: once the multisig spork is enforced, a CreateMultisig
// send to MultisigContract resolves and its embedded receive materialises the registry record —
// end-to-end proof that the phase-6 wiring (GetEmbeddedMethod -> applyMultisigDiffs -> phase-4
// methods) is reachable through the real VM/sequencer path, not just unit-level.
func TestMultisig_ActivatedCreateResolves(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activateMultisig(t, z)

	creator := g.User1.Address
	creatorKey := g.User1.Public
	otherSigner := genMultisigSigner(t)

	z.InsertSendBlock(&nom.AccountBlock{
		Address:       creator,
		ToAddress:     types.MultisigContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        constants.MultisigCreationBurnAmount,
		Data: definition.ABIMultisig.PackMethodPanic(definition.CreateMultisigMethodName,
			uint64(1), uint8(2), [][]byte{creatorKey, otherSigner}),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	z.InsertNewMomentum()

	derived := types.MultisigCreationToAddress(creatorKey, 1)
	rec, err := definition.GetMultisigRecord(z.Chain().GetFrontierAccountStore(types.MultisigContract).Storage(), derived)
	common.FailIfErr(t, err)
	if rec == nil {
		t.Fatal("expected multisig record to exist once the spork is enforced")
	}
	if rec.Active.Threshold != 2 || len(rec.Active.Signers) != 2 {
		t.Fatalf("unexpected active policy: %+v", rec.Active)
	}
}
