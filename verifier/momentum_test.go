package verifier

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// stubMomentumStore satisfies store.Momentum by embedding a nil interface and
// overriding only the methods content() exercises.
type stubMomentumStore struct {
	store.Momentum
}

func (*stubMomentumStore) GetFrontierAccountBlock(types.Address) (*nom.AccountBlock, error) {
	return nil, nil
}

func (*stubMomentumStore) GetMomentumByHash(types.Hash) (*nom.Momentum, error) {
	return &nom.Momentum{Version: 1}, nil
}

func (*stubMomentumStore) GetPlasmaVariables() (*definition.PlasmaVariables, error) {
	return &definition.PlasmaVariables{
		MaxBasePlasmaInMomentum: 1_000_000_000,
		FusedPlasmaTarget:       21000,
		PowPlasmaTarget:         21000,
		MaxPriceChangePercent:   10,
		PriceChangeDenominator:  20,
	}, nil
}

// A momentum content header with no matching entry in the prefetched
// account-blocks must be rejected with an error, not dereference a nil block.
func TestRawMomentumVerifier_Content_MissingAccountBlock_ReturnsError(t *testing.T) {
	address := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	headerHash := types.NewHash([]byte("header"))
	blockHash := types.NewHash([]byte("different-block"))

	momentum := &nom.Momentum{
		Content: nom.MomentumContent{
			{
				Address: address,
				HashHeight: types.HashHeight{
					Hash:   headerHash,
					Height: 1,
				},
			},
		},
	}
	accountBlocks := []*nom.AccountBlock{
		{
			Address: address,
			Hash:    blockHash,
			Height:  1,
		},
	}

	rmv := &rawMomentumVerifier{
		momentum:      momentum,
		accountBlocks: accountBlocks,
		momentumStore: &stubMomentumStore{},
	}

	err := rmv.content(false)
	if err == nil {
		t.Fatal("expected an error for a content header missing its account block, got nil")
	}
}

// A momentum content header must not be satisfied by more than one prefetched
// account-block, or dynamic-plasma price accounting could count it more than once.
func TestRawMomentumVerifier_Content_DuplicateAccountBlock_ReturnsError(t *testing.T) {
	address := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	blockHash := types.NewHash([]byte("block"))

	momentum := &nom.Momentum{
		Content: nom.MomentumContent{
			{
				Address: address,
				HashHeight: types.HashHeight{
					Hash:   blockHash,
					Height: 1,
				},
			},
		},
	}
	block := &nom.AccountBlock{
		Address: address,
		Hash:    blockHash,
		Height:  1,
	}
	accountBlocks := []*nom.AccountBlock{block, block}

	rmv := &rawMomentumVerifier{
		momentum:      momentum,
		accountBlocks: accountBlocks,
		momentumStore: &stubMomentumStore{},
	}

	err := rmv.content(true)
	if err == nil {
		t.Fatal("expected an error for a duplicated prefetched account-block, got nil")
	}
}

// Two content headers pointing at the same identifier must not both resolve to the
// same prefetched account-block, even when a distinct filler block keeps the
// lookup and content sizes equal.
func TestRawMomentumVerifier_Content_DuplicateContentHeader_ReturnsError(t *testing.T) {
	address := types.PlasmaContract
	blockAHash := types.NewHash([]byte("block-a"))
	blockBHash := types.NewHash([]byte("block-b"))

	header := types.AccountHeader{
		Address: address,
		HashHeight: types.HashHeight{
			Hash:   blockAHash,
			Height: 1,
		},
	}
	momentum := &nom.Momentum{
		Content: nom.MomentumContent{&header, &header},
	}
	// Batched contract sends skip chain-order validation, isolating the lookup-consumption check.
	accountBlocks := []*nom.AccountBlock{
		{BlockType: nom.BlockTypeContractSend, Address: address, Hash: blockAHash, Height: 1},
		{Address: address, Hash: blockBHash, Height: 2},
	}

	rmv := &rawMomentumVerifier{
		momentum:      momentum,
		accountBlocks: accountBlocks,
		momentumStore: &stubMomentumStore{},
	}

	err := rmv.content(false)
	if err == nil {
		t.Fatal("expected an error for duplicate content headers resolving to the same account block, got nil")
	}
}

// A content header must be rejected if its address doesn't match the address of the
// prefetched account-block sharing its hash/height identifier.
func TestRawMomentumVerifier_Content_AddressMismatch_ReturnsError(t *testing.T) {
	headerAddress := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	blockAddress := types.ParseAddressPanic("z1qqmqp40duzvtxvg7dwxph7724mq63t3mru297p")
	blockHash := types.NewHash([]byte("block"))

	momentum := &nom.Momentum{
		Content: nom.MomentumContent{
			{
				Address: headerAddress,
				HashHeight: types.HashHeight{
					Hash:   blockHash,
					Height: 1,
				},
			},
		},
	}
	accountBlocks := []*nom.AccountBlock{
		{Address: blockAddress, Hash: blockHash, Height: 1},
	}

	rmv := &rawMomentumVerifier{
		momentum:      momentum,
		accountBlocks: accountBlocks,
		momentumStore: &stubMomentumStore{},
	}

	err := rmv.content(false)
	if err == nil {
		t.Fatal("expected an error for a content header address mismatching its account block, got nil")
	}
}

// content() must recompute a block's price from the injected canonical base
// plasma rather than trusting the wire BasePlasma, so a momentum whose prices
// were derived by a byzantine producer from a forged wire value is rejected.
func TestRawMomentumVerifier_Content_ForgedBasePlasma_ReturnsError(t *testing.T) {
	const (
		canonical = uint64(21000)
		forged    = uint64(100000)
	)

	address := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	blockHash := types.NewHash([]byte("block"))

	block := &nom.AccountBlock{
		Address:     address,
		Hash:        blockHash,
		Height:      1,
		Difficulty:  0,
		BasePlasma:  forged,
		FusedPlasma: canonical,
	}

	momentum := &nom.Momentum{
		Version: 2,
		Content: nom.MomentumContent{
			{
				Address: address,
				HashHeight: types.HashHeight{
					Hash:   blockHash,
					Height: 1,
				},
			},
		},
	}

	config := &definition.PlasmaVariables{
		MaxBasePlasmaInMomentum: 1_000_000_000,
		FusedPlasmaTarget:       canonical,
		PowPlasmaTarget:         canonical,
		MaxPriceChangePercent:   10,
		PriceChangeDenominator:  20,
	}

	// The prices a byzantine producer would have derived from the forged wire
	// BasePlasma.
	forgedBlock := &nom.AccountBlock{
		Address:     address,
		Hash:        blockHash,
		Height:      1,
		Difficulty:  0,
		BasePlasma:  forged,
		FusedPlasma: canonical,
	}
	byzantinePlasma := dp.NewDynamicPlasma(&nom.Momentum{Version: 1}, config)
	byzantineBasePlasma := types.BasePlasma{Fusion: 0, Pow: 0}
	byzantineBasePlasma.Add(byzantinePlasma.ComputeBasePlasma(forgedBlock))
	momentum.NextFusionPrice = byzantinePlasma.NextFusionPrice(byzantineBasePlasma.Fusion)
	momentum.NextWorkPrice = byzantinePlasma.NextWorkPrice(byzantineBasePlasma.Pow)

	accountBlocks := []*nom.AccountBlock{block}

	rmv := &rawMomentumVerifier{
		momentum:      momentum,
		accountBlocks: accountBlocks,
		momentumStore: &stubMomentumStore{},
		chain:         nil,
		canonicalBasePlasma: func(chain.Chain, *nom.AccountBlock) (uint64, error) {
			return canonical, nil
		},
	}

	err := rmv.content(true)
	if err == nil {
		t.Fatal("expected an error for a momentum whose prices were derived from a forged wire base plasma, got nil")
	}
}

// content()'s dynamic-plasma loop must not recompute canonical base plasma for
// embedded-address blocks: ComputeBasePlasma already special-cases embedded
// addresses to zero, so the injected canonicalBasePlasma func must never be
// invoked for them.
func TestRawMomentumVerifier_Content_EmbeddedAddress_SkipsCanonicalRecompute(t *testing.T) {
	address := types.PlasmaContract
	blockHash := types.NewHash([]byte("block"))

	block := &nom.AccountBlock{
		BlockType: nom.BlockTypeContractReceive,
		Address:   address,
		Hash:      blockHash,
		Height:    1,
	}

	momentum := &nom.Momentum{
		Version:         2,
		NextFusionPrice: 1000,
		NextWorkPrice:   1000,
		Content: nom.MomentumContent{
			{
				Address: address,
				HashHeight: types.HashHeight{
					Hash:   blockHash,
					Height: 1,
				},
			},
		},
	}

	accountBlocks := []*nom.AccountBlock{block}

	rmv := &rawMomentumVerifier{
		momentum:      momentum,
		accountBlocks: accountBlocks,
		momentumStore: &stubMomentumStore{},
		chain:         nil,
		canonicalBasePlasma: func(chain.Chain, *nom.AccountBlock) (uint64, error) {
			t.Fatal("canonicalBasePlasma must not be called for an embedded-address block")
			return 0, nil
		},
	}

	if err := rmv.content(true); err != nil {
		t.Fatalf("expected no error for an untouched embedded-address block, got %v", err)
	}
}

// fakeContentMomentumStore implements GetFrontierAccountBlock (for a fresh, never-before-seen
// account) and GetAccountStore (serving the multisig registry live-auth now reads); every other
// method is unused and would panic if reached, per the same pattern as
// verifier/account_block_multisig_test.go's fakes.
type fakeContentMomentumStore struct {
	store.Momentum
	registryStorage db.DB
}

func (f *fakeContentMomentumStore) GetFrontierAccountBlock(types.Address) (*nom.AccountBlock, error) {
	return nil, nil
}

func (f *fakeContentMomentumStore) GetAccountStore(types.Address) store.Account {
	return &fakeAccountStore{storage: f.registryStorage}
}

func multisigTestBlock(t *testing.T, maHeight uint64) *nom.AccountBlock {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	addr := types.MultisigCreationToAddress(pub, 0)
	b := &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      1,
		BlockType:            nom.BlockTypeUserSend,
		Height:               1,
		MomentumAcknowledged: types.HashHeight{Height: maHeight},
		Address:              addr,
		ToAddress:            types.ZeroAddress,
	}
	b.Hash = b.ComputeHash()
	return b
}

// TestMomentumContent_LiveAuth_ActivePolicy_Accepted: a multisig block signed under the policy
// active at the including momentum's height is accepted by content(), the authoritative live
// authorization gate.
func TestMomentumContent_LiveAuth_ActivePolicy_Accepted(t *testing.T) {
	signers := generateSigners(t, 3)
	policy := policyFromSigners(t, 2, signers)

	block := multisigTestBlock(t, 1)
	storage := setupRegistry(t, block.Address, &definition.MultisigRecord{Active: policy})
	block.MultisigAuth = &nom.MultisigAuth{Signatures: signWithPolicy(t, policy, signers, block.Hash)}

	momentumHeight := constants.MultisigMaxMaLag + 100
	momentum := &nom.Momentum{Content: nom.NewMomentumContent([]*nom.AccountBlock{block}), Height: momentumHeight}

	rmv := &rawMomentumVerifier{
		momentum:      momentum,
		accountBlocks: []*nom.AccountBlock{block},
		momentumStore: &fakeContentMomentumStore{registryStorage: storage},
	}
	if err := rmv.content(false); err != nil {
		t.Fatalf("expected content() to accept a block signed under the active policy, got %v", err)
	}
}

// TestMomentumContent_LiveAuth_MaturedOldPolicy_Rejected: a multisig block signed under a policy
// that has since matured out of Active is rejected by content(), even though its
// MomentumAcknowledged is not stale.
func TestMomentumContent_LiveAuth_MaturedOldPolicy_Rejected(t *testing.T) {
	oldSigners := generateSigners(t, 3)
	oldPolicy := policyFromSigners(t, 2, oldSigners)
	newSigners := generateSigners(t, 3)
	newPolicy := policyFromSigners(t, 2, newSigners)

	const pendingHeight = uint64(10)
	block := multisigTestBlock(t, 1)
	storage := setupRegistry(t, block.Address, &definition.MultisigRecord{
		Active:        oldPolicy,
		Pending:       &newPolicy,
		PendingHeight: pendingHeight,
	})
	block.MultisigAuth = &nom.MultisigAuth{Signatures: signWithPolicy(t, oldPolicy, oldSigners, block.Hash)}

	// momentum height is at/after maturity, so the new policy is the active one.
	momentumHeight := pendingHeight + constants.MultisigPolicyMaturityDelay
	momentum := &nom.Momentum{Content: nom.NewMomentumContent([]*nom.AccountBlock{block}), Height: momentumHeight}

	rmv := &rawMomentumVerifier{
		momentum:      momentum,
		accountBlocks: []*nom.AccountBlock{block},
		momentumStore: &fakeContentMomentumStore{registryStorage: storage},
	}
	if err := rmv.content(false); err == nil {
		t.Fatal("expected content() to reject a block signed under a since-matured old policy")
	}
}

// TestMomentumContent_LiveAuth_StaleMA_StillAccepted: content() does not enforce an MA-lag floor:
// a validly-signed block whose MomentumAcknowledged lags the including momentum by more than
// MultisigMaxMaLag still lands, as long as it satisfies the currently active policy.
func TestMomentumContent_LiveAuth_StaleMA_StillAccepted(t *testing.T) {
	signers := generateSigners(t, 3)
	policy := policyFromSigners(t, 2, signers)

	block := multisigTestBlock(t, 1)
	storage := setupRegistry(t, block.Address, &definition.MultisigRecord{Active: policy})
	block.MultisigAuth = &nom.MultisigAuth{Signatures: signWithPolicy(t, policy, signers, block.Hash)}

	momentumHeight := constants.MultisigMaxMaLag + 1000
	momentum := &nom.Momentum{Content: nom.NewMomentumContent([]*nom.AccountBlock{block}), Height: momentumHeight}

	rmv := &rawMomentumVerifier{
		momentum:      momentum,
		accountBlocks: []*nom.AccountBlock{block},
		momentumStore: &fakeContentMomentumStore{registryStorage: storage},
	}
	if err := rmv.content(false); err != nil {
		t.Fatalf("expected content() to accept a validly-signed block despite MA lagging past MultisigMaxMaLag, got %v", err)
	}
}

// secondMultisigBlock builds a second, higher-height block for the same multisig address as base,
// for exercising the per-address policy cache against multiple blocks from one address.
func secondMultisigBlock(base *nom.AccountBlock) *nom.AccountBlock {
	b := &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      1,
		BlockType:            nom.BlockTypeUserSend,
		Height:               2,
		PreviousHash:         base.Hash,
		MomentumAcknowledged: base.MomentumAcknowledged,
		Address:              base.Address,
		ToAddress:            types.ZeroAddress,
	}
	b.Hash = b.ComputeHash()
	return b
}

// TestMomentumContent_LiveAuth_MultipleBlocksSameAddress_AllAccepted: a momentum containing
// multiple blocks from the same multisig address, all validly signed under the one active policy,
// are all authorised -- proving the per-address policy cache resolves the policy once and applies
// it identically to every block from that address.
func TestMomentumContent_LiveAuth_MultipleBlocksSameAddress_AllAccepted(t *testing.T) {
	signers := generateSigners(t, 3)
	policy := policyFromSigners(t, 2, signers)

	block1 := multisigTestBlock(t, 1)
	block2 := secondMultisigBlock(block1)
	storage := setupRegistry(t, block1.Address, &definition.MultisigRecord{Active: policy})
	block1.MultisigAuth = &nom.MultisigAuth{Signatures: signWithPolicy(t, policy, signers, block1.Hash)}
	block2.MultisigAuth = &nom.MultisigAuth{Signatures: signWithPolicy(t, policy, signers, block2.Hash)}

	momentumHeight := constants.MultisigMaxMaLag + 100
	blocks := []*nom.AccountBlock{block1, block2}
	momentum := &nom.Momentum{Content: nom.NewMomentumContent(blocks), Height: momentumHeight}

	rmv := &rawMomentumVerifier{
		momentum:      momentum,
		accountBlocks: blocks,
		momentumStore: &fakeContentMomentumStore{registryStorage: storage},
	}
	if err := rmv.content(false); err != nil {
		t.Fatalf("expected content() to accept multiple validly-signed blocks from the same address, got %v", err)
	}
}

// TestMomentumContent_LiveAuth_MultipleBlocksSameAddress_AllRejected: a momentum containing
// multiple blocks from the same multisig address, none satisfying the active policy, are all
// rejected -- the cached policy applies the same negative outcome to every block from that
// address.
func TestMomentumContent_LiveAuth_MultipleBlocksSameAddress_AllRejected(t *testing.T) {
	signers := generateSigners(t, 3)
	policy := policyFromSigners(t, 2, signers)
	unrelatedSigners := generateSigners(t, 2)

	block1 := multisigTestBlock(t, 1)
	block2 := secondMultisigBlock(block1)
	storage := setupRegistry(t, block1.Address, &definition.MultisigRecord{Active: policy})
	block1.MultisigAuth = &nom.MultisigAuth{Signatures: [][]byte{
		ed25519.Sign(unrelatedSigners[0].priv, block1.Hash.Bytes()),
		ed25519.Sign(unrelatedSigners[1].priv, block1.Hash.Bytes()),
	}}
	block2.MultisigAuth = &nom.MultisigAuth{Signatures: [][]byte{
		ed25519.Sign(unrelatedSigners[0].priv, block2.Hash.Bytes()),
		ed25519.Sign(unrelatedSigners[1].priv, block2.Hash.Bytes()),
	}}

	momentumHeight := constants.MultisigMaxMaLag + 100
	blocks := []*nom.AccountBlock{block1, block2}
	momentum := &nom.Momentum{Content: nom.NewMomentumContent(blocks), Height: momentumHeight}

	rmv := &rawMomentumVerifier{
		momentum:      momentum,
		accountBlocks: blocks,
		momentumStore: &fakeContentMomentumStore{registryStorage: storage},
	}
	if err := rmv.content(false); err == nil {
		t.Fatal("expected content() to reject a momentum where no block from the address satisfies the active policy")
	}
}
