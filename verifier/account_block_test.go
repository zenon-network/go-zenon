package verifier

import (
	"encoding/binary"
	"errors"
	"math"
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/pow"
)

// stubSporkMomentumStore overrides IsSporkActive so tests can toggle the
// Dynamic Plasma spork without a full chain.
type stubSporkMomentumStore struct {
	store.Momentum
	dynamicPlasmaActive bool
}

func (s *stubSporkMomentumStore) IsSporkActive(spork *types.ImplementedSpork) (bool, error) {
	if spork == types.DynamicPlasmaSpork {
		return s.dynamicPlasmaActive, nil
	}
	return false, nil
}

// A user receive block with non-empty Data must be rejected once the Dynamic
// Plasma spork is enforced at the block's acknowledged momentum.
func TestAccountBlockVerifier_ReceiveData_DynamicPlasmaEnforced_RejectsData(t *testing.T) {
	address := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")

	block := &nom.AccountBlock{
		Version:       1,
		BlockType:     nom.BlockTypeUserReceive,
		Address:       address,
		Height:        2,
		Data:          []byte("trailing"),
		FromBlockHash: types.NewHash([]byte("send")),
	}

	abv := &accountBlockVerifier{
		block:         block,
		accountStore:  nil,
		momentumStore: &stubSporkMomentumStore{dynamicPlasmaActive: true},
		frontierStore: &stubSporkMomentumStore{dynamicPlasmaActive: true},
	}

	err := abv.receiveData()
	if err != ErrABDataMustBeEmpty {
		t.Fatalf("expected ErrABDataMustBeEmpty, got %v", err)
	}
}

// A user receive block with non-empty Data is accepted before the Dynamic
// Plasma spork is enforced.
func TestAccountBlockVerifier_ReceiveData_DynamicPlasmaNotEnforced_AllowsData(t *testing.T) {
	address := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")

	block := &nom.AccountBlock{
		Version:       1,
		BlockType:     nom.BlockTypeUserReceive,
		Address:       address,
		Height:        2,
		Data:          []byte("some data"),
		FromBlockHash: types.NewHash([]byte("send")),
	}

	abv := &accountBlockVerifier{
		block:         block,
		accountStore:  nil,
		momentumStore: &stubSporkMomentumStore{dynamicPlasmaActive: false},
		frontierStore: &stubSporkMomentumStore{dynamicPlasmaActive: false},
	}

	if err := abv.receiveData(); err != nil {
		t.Fatalf("expected nil error before spork, got %v", err)
	}
}

// A user receive block with empty Data is accepted regardless of spork state.
func TestAccountBlockVerifier_ReceiveData_EmptyData_AllwaysAllowed(t *testing.T) {
	address := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")

	block := &nom.AccountBlock{
		Version:       1,
		BlockType:     nom.BlockTypeUserReceive,
		Address:       address,
		Height:        2,
		Data:          nil,
		FromBlockHash: types.NewHash([]byte("send")),
	}

	// With spork enforced
	abv := &accountBlockVerifier{
		block:         block,
		accountStore:  nil,
		momentumStore: &stubSporkMomentumStore{dynamicPlasmaActive: true},
		frontierStore: &stubSporkMomentumStore{dynamicPlasmaActive: true},
	}
	if err := abv.receiveData(); err != nil {
		t.Fatalf("expected nil error for empty data with spork enforced, got %v", err)
	}

	// Without spork
	abv.momentumStore = &stubSporkMomentumStore{dynamicPlasmaActive: false}
	abv.frontierStore = &stubSporkMomentumStore{dynamicPlasmaActive: false}
	if err := abv.receiveData(); err != nil {
		t.Fatalf("expected nil error for empty data without spork, got %v", err)
	}
}

// An embedded (contract) receive block with Data is not affected by this
// rule — embedded blocks have their own data semantics.
func TestAccountBlockVerifier_ReceiveData_EmbeddedAddress_SkipsCheck(t *testing.T) {
	block := &nom.AccountBlock{
		Version:       1,
		BlockType:     nom.BlockTypeContractReceive,
		Address:       types.PlasmaContract,
		Height:        2,
		Data:          []byte("contract data"),
		FromBlockHash: types.NewHash([]byte("send")),
	}

	abv := &accountBlockVerifier{
		block:         block,
		accountStore:  nil,
		momentumStore: &stubSporkMomentumStore{dynamicPlasmaActive: true},
		frontierStore: &stubSporkMomentumStore{dynamicPlasmaActive: true},
	}

	if err := abv.receiveData(); err != nil {
		t.Fatalf("expected nil error for embedded receive block, got %v", err)
	}
}

// A send block is not affected by the receive-data rule.
func TestAccountBlockVerifier_ReceiveData_SendBlock_SkipsCheck(t *testing.T) {
	address := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")

	block := &nom.AccountBlock{
		Version:   1,
		BlockType: nom.BlockTypeUserSend,
		Address:   address,
		Height:    2,
		Data:      []byte("send data"),
	}

	abv := &accountBlockVerifier{
		block:         block,
		accountStore:  nil,
		momentumStore: &stubSporkMomentumStore{dynamicPlasmaActive: true},
		frontierStore: &stubSporkMomentumStore{dynamicPlasmaActive: true},
	}

	if err := abv.receiveData(); err != nil {
		t.Fatalf("expected nil error for send block, got %v", err)
	}
}

// When momentumStore is nil (user blocks), receiveData falls back to
// chain.GetMomentumStore(block.MomentumAcknowledged). With a nil chain the
// check must not panic; it should return ErrABMAMissing.
func TestAccountBlockVerifier_ReceiveData_NilChain_ReturnsMAMissing(t *testing.T) {
	address := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")

	block := &nom.AccountBlock{
		Version:       1,
		BlockType:     nom.BlockTypeUserReceive,
		Address:       address,
		Height:        2,
		Data:          []byte("data"),
		FromBlockHash: types.NewHash([]byte("send")),
	}

	abv := &accountBlockVerifier{
		block:         block,
		accountStore:  nil,
		momentumStore: nil, // simulates user block
		frontierStore: &stubSporkMomentumStore{dynamicPlasmaActive: true},
		chain:         nil, // no chain available
	}

	err := abv.receiveData()
	if err != ErrABMAMissing {
		t.Fatalf("expected ErrABMAMissing with nil chain, got %v", err)
	}
}

type stubSequencerAccountStore struct {
	store.Account
	next *types.AccountHeader
}

func (s *stubSequencerAccountStore) SequencerFront(store.AccountMailbox) (*types.AccountHeader, error) {
	return s.next, nil
}

type stubSequencerFrontierStore struct {
	store.Momentum
	confirmHeights map[types.Hash]uint64
	blocks         map[types.Hash]*nom.AccountBlock
}

func (s *stubSequencerFrontierStore) GetAccountMailbox(types.Address) store.AccountMailbox {
	return nil
}

func (s *stubSequencerFrontierStore) GetBlockConfirmationHeight(hash types.Hash) (uint64, error) {
	return s.confirmHeights[hash], nil
}

func (s *stubSequencerFrontierStore) GetAccountBlockByHash(hash types.Hash) (*nom.AccountBlock, error) {
	return s.blocks[hash], nil
}

// When frontierStore's sequencer front was confirmed strictly after the
// block's MomentumAcknowledged height, the mailbox at MomentumAcknowledged had
// no pending entry yet, so sequencer() must return ErrABSequencerNothing.
func TestAccountBlockVerifier_SequencerRejectsSendConfirmedAfterMA(t *testing.T) {
	send := &nom.AccountBlock{
		Address: types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt"),
		Hash:    types.NewHash([]byte("later-send")),
		Height:  2,
	}
	header := send.Header()

	abv := &accountBlockVerifier{
		block: &nom.AccountBlock{
			BlockType:            nom.BlockTypeContractReceive,
			Address:              types.StakeContract,
			FromBlockHash:        send.Hash,
			MomentumAcknowledged: types.HashHeight{Hash: types.NewHash([]byte("ma")), Height: 10},
		},
		accountStore: &stubSequencerAccountStore{next: &header},
		frontierStore: &stubSequencerFrontierStore{
			confirmHeights: map[types.Hash]uint64{send.Hash: 11},
			blocks:         map[types.Hash]*nom.AccountBlock{send.Hash: send},
		},
	}

	if err := abv.sequencer(); err != ErrABSequencerNothing {
		t.Fatalf("expected ErrABSequencerNothing for send confirmed after MA, got %v", err)
	}

	// Control: once MA reaches the confirmation height, the same sequencer check passes.
	abv.block.MomentumAcknowledged.Height = 11
	if err := abv.sequencer(); err != nil {
		t.Fatalf("expected nil once MA reaches confirmation height, got %v", err)
	}
}

type stubPoWCacheStore struct {
	store.Cache
	dynamicPlasmaActive bool
	err                 error
}

func (c *stubPoWCacheStore) IsSporkActive(spork *types.ImplementedSpork) (bool, error) {
	if c.err != nil {
		return false, c.err
	}
	if spork == types.DynamicPlasmaSpork {
		return c.dynamicPlasmaActive, nil
	}
	return false, nil
}

type stubPoWMomentumStore struct {
	store.Momentum
	chainID             uint64
	frontier            *nom.Momentum
	dynamicPlasmaActive bool
	sporkErr            error
}

func (m *stubPoWMomentumStore) ChainIdentifier() uint64 { return m.chainID }

func (m *stubPoWMomentumStore) Identifier() types.HashHeight {
	return m.frontier.Identifier()
}

func (m *stubPoWMomentumStore) GetMomentumByHeight(height uint64) (*nom.Momentum, error) {
	if m.frontier != nil && height == m.frontier.Height {
		return m.frontier, nil
	}
	return nil, nil
}

func (m *stubPoWMomentumStore) IsSporkActive(spork *types.ImplementedSpork) (bool, error) {
	if m.sporkErr != nil {
		return false, m.sporkErr
	}
	if spork == types.DynamicPlasmaSpork {
		return m.dynamicPlasmaActive, nil
	}
	return false, nil
}

type stubPoWAccountStore struct {
	store.Account
	frontier *nom.AccountBlock
}

func (a *stubPoWAccountStore) Frontier() (*nom.AccountBlock, error) {
	return a.frontier, nil
}

type stubPoWChain struct {
	chain.Chain
	frontierStore store.Momentum
	accountStore  store.Account
	cacheStore    store.Cache
}

func (c *stubPoWChain) GetFrontierMomentumStore() store.Momentum {
	return c.frontierStore
}

func (c *stubPoWChain) GetAccountStore(types.Address, types.HashHeight) store.Account {
	return c.accountStore
}

func (c *stubPoWChain) GetCacheStore(types.HashHeight) store.Cache {
	return c.cacheStore
}

func findNonceFor(t *testing.T, block *nom.AccountBlock, predicate func(*nom.AccountBlock) bool) {
	t.Helper()
	for i := uint64(0); i < 100000; i++ {
		binary.LittleEndian.PutUint64(block.Nonce.Data[:], i)
		if predicate(block) {
			return
		}
	}
	t.Fatalf("failed to find matching nonce for difficulty %d", block.Difficulty)
}

func newPoWTestBlock(addr types.Address, ma types.HashHeight, difficulty uint64) *nom.AccountBlock {
	return &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      1,
		BlockType:            nom.BlockTypeUserSend,
		Address:              addr,
		ToAddress:            addr,
		Amount:               big.NewInt(0),
		Height:               1,
		MomentumAcknowledged: ma,
		Difficulty:           difficulty,
	}
}

// Strict AccountBlock admission must reject any difficulty >= 2^63 (even with a
// legacy-valid nonce) and any difficulty > dp.MaxDifficultyForAccountBlock,
// even when DynamicPlasmaSpork is not yet enforced.
func TestAccountBlockVerifier_StrictPoW_RejectsHighDifficultyPreDP(t *testing.T) {
	addr := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	ma := &nom.Momentum{
		Hash:   types.NewHash([]byte("m1")),
		Height: 1,
	}
	chainStub := &stubPoWChain{
		frontierStore: &stubPoWMomentumStore{chainID: 1, frontier: ma},
		accountStore:  &stubPoWAccountStore{},
		cacheStore:    &stubPoWCacheStore{dynamicPlasmaActive: false},
	}
	v := NewAccountBlockVerifier(chainStub, nil)

	for _, difficulty := range []uint64{1 << 63, math.MaxUint64 - 1, math.MaxUint64} {
		block := newPoWTestBlock(addr, ma.Identifier(), difficulty)
		findNonceFor(t, block, pow.CheckPoWNonceLegacy)

		if err := v.AccountBlock(block); !errors.Is(err, ErrABPoWInvalid) {
			t.Fatalf("AccountBlock(difficulty=%d) pre-DP: got %v, want ErrABPoWInvalid", difficulty, err)
		}
	}

	// Difficulty = dp.MaxDifficultyForAccountBlock + 1 is also rejected by
	// strict powSporkRules even pre-DP, while == dp.MaxDifficultyForAccountBlock passes.
	abvOver := &accountBlockVerifier{
		block:         newPoWTestBlock(addr, ma.Identifier(), dp.MaxDifficultyForAccountBlock+1),
		frontierStore: chainStub.frontierStore,
		chain:         chainStub,
		strictPoW:     true,
	}
	if err := abvOver.powSporkRules(); !errors.Is(err, ErrABPoWInvalid) {
		t.Fatalf("strict powSporkRules(MaxDifficulty+1) pre-DP: got %v, want ErrABPoWInvalid", err)
	}

	abvAtCap := &accountBlockVerifier{
		block:         newPoWTestBlock(addr, ma.Identifier(), dp.MaxDifficultyForAccountBlock),
		frontierStore: chainStub.frontierStore,
		chain:         chainStub,
		strictPoW:     true,
	}
	if err := abvAtCap.powSporkRules(); err != nil {
		t.Fatalf("strict powSporkRules(MaxDifficulty) pre-DP: got %v, want nil", err)
	}
}

// AccountBlockInMomentum pre-DP must accept difficulties >= 2^63 with a
// legacy-valid nonce, reject them when the nonce fails legacy verification
// (e.g., 2^64-2 with digest < 2^63), and allow (MaxDifficulty, 2^63) once pow()
// has verified the nonce.
func TestAccountBlockVerifier_InMomentum_PreDP_LegacyCompatibility(t *testing.T) {
	addr := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	ma := &nom.Momentum{
		Hash:   types.NewHash([]byte("m1")),
		Height: 1,
	}
	chainStub := &stubPoWChain{
		frontierStore: &stubPoWMomentumStore{chainID: 1, frontier: ma},
		accountStore:  &stubPoWAccountStore{},
		cacheStore:    &stubPoWCacheStore{dynamicPlasmaActive: false},
	}
	v := NewAccountBlockVerifier(chainStub, nil)

	for _, difficulty := range []uint64{1 << 63, math.MaxUint64 - 1, math.MaxUint64} {
		block := newPoWTestBlock(addr, ma.Identifier(), difficulty)
		findNonceFor(t, block, pow.CheckPoWNonceLegacy)

		if err := v.AccountBlockInMomentum(block); err != nil {
			t.Fatalf("AccountBlockInMomentum(difficulty=%d, legacy-valid) pre-DP: got %v, want nil", difficulty, err)
		}
	}

	// At 2^64 - 2, legacy target is 2^63; a nonce whose digest < 2^63 must be rejected.
	invalidLegacy := newPoWTestBlock(addr, ma.Identifier(), math.MaxUint64-1)
	findNonceFor(t, invalidLegacy, func(b *nom.AccountBlock) bool {
		return !pow.CheckPoWNonceLegacy(b)
	})
	if err := v.AccountBlockInMomentum(invalidLegacy); !errors.Is(err, ErrABPoWInvalid) {
		t.Fatalf("AccountBlockInMomentum(2^64-2, legacy-invalid) pre-DP: got %v, want ErrABPoWInvalid", err)
	}

	// For dp.MaxDifficultyForAccountBlock < Difficulty < 2^63, pow() verifies
	// unsigned/legacy PoW (which agree below 2^63), and powSporkRules() allows
	// the block pre-DP in momentum context.
	abvBetween := &accountBlockVerifier{
		block:         newPoWTestBlock(addr, ma.Identifier(), dp.MaxDifficultyForAccountBlock+1),
		frontierStore: chainStub.frontierStore,
		chain:         chainStub,
		strictPoW:     false,
	}
	if err := abvBetween.powSporkRules(); err != nil {
		t.Fatalf("in-momentum powSporkRules(MaxDifficulty+1) pre-DP: got %v, want nil", err)
	}
}

// AccountBlockInMomentum post-DP must reject difficulties >= 2^63 and
// dp.MaxDifficultyForAccountBlock + 1, while accepting
// dp.MaxDifficultyForAccountBlock and valid in-range PoW.
func TestAccountBlockVerifier_InMomentum_PostDP_EnforcesCap(t *testing.T) {
	addr := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	ma := &nom.Momentum{
		Hash:   types.NewHash([]byte("m1")),
		Height: 1,
	}
	chainStub := &stubPoWChain{
		frontierStore: &stubPoWMomentumStore{chainID: 1, frontier: ma},
		accountStore:  &stubPoWAccountStore{},
		cacheStore:    &stubPoWCacheStore{dynamicPlasmaActive: true},
	}
	v := NewAccountBlockVerifier(chainStub, nil)

	for _, difficulty := range []uint64{1 << 63, math.MaxUint64 - 1, math.MaxUint64} {
		block := newPoWTestBlock(addr, ma.Identifier(), difficulty)
		findNonceFor(t, block, pow.CheckPoWNonceLegacy)

		if err := v.AccountBlockInMomentum(block); !errors.Is(err, ErrABPoWInvalid) {
			t.Fatalf("AccountBlockInMomentum(difficulty=%d) post-DP: got %v, want ErrABPoWInvalid", difficulty, err)
		}
	}

	// dp.MaxDifficultyForAccountBlock + 1 is rejected post-DP.
	abvOver := &accountBlockVerifier{
		block:         newPoWTestBlock(addr, ma.Identifier(), dp.MaxDifficultyForAccountBlock+1),
		frontierStore: chainStub.frontierStore,
		chain:         chainStub,
		strictPoW:     false,
	}
	if err := abvOver.powSporkRules(); !errors.Is(err, ErrABPoWInvalid) {
		t.Fatalf("in-momentum powSporkRules(MaxDifficulty+1) post-DP: got %v, want ErrABPoWInvalid", err)
	}

	// dp.MaxDifficultyForAccountBlock is accepted by powSporkRules post-DP.
	abvAtCap := &accountBlockVerifier{
		block:         newPoWTestBlock(addr, ma.Identifier(), dp.MaxDifficultyForAccountBlock),
		frontierStore: chainStub.frontierStore,
		chain:         chainStub,
		strictPoW:     false,
	}
	if err := abvAtCap.powSporkRules(); err != nil {
		t.Fatalf("in-momentum powSporkRules(MaxDifficulty) post-DP: got %v, want nil", err)
	}

	// Full AccountBlockInMomentum accepts a valid in-range mined PoW block post-DP.
	validBlock := newPoWTestBlock(addr, ma.Identifier(), 1500)
	findNonceFor(t, validBlock, pow.CheckPoWNonce)
	if err := v.AccountBlockInMomentum(validBlock); err != nil {
		t.Fatalf("AccountBlockInMomentum(valid PoW) post-DP: got %v, want nil", err)
	}
}

// An unreadable or missing cache store during powSporkRules must return an
// InternalError (wrapping ErrVerifierInternal), never silently accept and
// never misclassify the storage failure as ErrABPoWInvalid.
func TestAccountBlockVerifier_PoWSporkRules_UnreadableCacheReturnsInternalError(t *testing.T) {
	addr := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	ma := &nom.Momentum{
		Hash:   types.NewHash([]byte("m1")),
		Height: 1,
	}
	storeErr := errors.New("leveldb: corrupted")

	cases := []struct {
		name  string
		cache store.Cache
	}{
		{
			name:  "cache_store_read_error",
			cache: &stubPoWCacheStore{err: storeErr},
		},
		{
			name:  "nil_cache_store",
			cache: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chainStub := &stubPoWChain{
				frontierStore: &stubPoWMomentumStore{chainID: 1, frontier: ma},
				accountStore:  &stubPoWAccountStore{},
				cacheStore:    tc.cache,
			}
			v := NewAccountBlockVerifier(chainStub, nil)
			block := newPoWTestBlock(addr, ma.Identifier(), math.MaxUint64)
			findNonceFor(t, block, pow.CheckPoWNonceLegacy)

			err := v.AccountBlockInMomentum(block)
			if err == nil {
				t.Fatal("expected internal error, got nil")
			}
			if !errors.Is(err, ErrVerifierInternal) {
				t.Fatalf("expected ErrVerifierInternal, got %v", err)
			}
			if errors.Is(err, ErrABPoWInvalid) {
				t.Fatalf("storage error must not unwrap to ErrABPoWInvalid: %v", err)
			}
			if IsValidationError(err) {
				t.Fatalf("storage error must not classify as a validation error: %v", err)
			}
		})
	}

	// Momentum-store spork read error is likewise wrapped as ErrVerifierInternal.
	abv := &accountBlockVerifier{
		block:         newPoWTestBlock(addr, ma.Identifier(), math.MaxUint64),
		momentumStore: &stubPoWMomentumStore{sporkErr: storeErr},
		strictPoW:     false,
	}
	err := abv.powSporkRules()
	if !errors.Is(err, ErrVerifierInternal) || errors.Is(err, ErrABPoWInvalid) || IsValidationError(err) {
		t.Fatalf("momentumStore spork read error: got %v, want ErrVerifierInternal only", err)
	}
}

// Keep abv.pow() before previous() and momentumAcknowledged() so that below
// 2^63: (1) chain == nil tests keep working without touching stores, and
// (2) an invalid PoW nonce yields ErrABPoWInvalid before ErrABPreviousMissing
// or ErrABMAMissing (preserving protocol/sidechain_rejection.go classification).
func TestAccountBlockVerifier_PoW_NilChainAndVerdictOrderingPreserved(t *testing.T) {
	addr := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	ma := types.HashHeight{Hash: types.NewHash([]byte("missing-ma")), Height: 99}

	// Invalid PoW at difficulty < 2^63 with both missing previous block (height 2,
	// frontier height 0) and missing MA, plus chain == nil.
	block := &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      1,
		BlockType:            nom.BlockTypeUserSend,
		Address:              addr,
		ToAddress:            addr,
		Amount:               big.NewInt(0),
		Height:               2,
		PreviousHash:         types.NewHash([]byte("missing-prev")),
		MomentumAcknowledged: ma,
		Difficulty:           1500,
	}
	findNonceFor(t, block, func(b *nom.AccountBlock) bool {
		return !pow.CheckPoWNonce(b)
	})

	abv := &accountBlockVerifier{
		block:         block,
		accountStore:  &stubPoWAccountStore{frontier: &nom.AccountBlock{Height: 1, Hash: types.NewHash([]byte("other"))}},
		frontierStore: &stubPoWMomentumStore{chainID: 1, frontier: nil},
		chain:         nil,
		strictPoW:     false,
	}
	if err := abv.all(); !errors.Is(err, ErrABPoWInvalid) {
		t.Fatalf("invalid PoW (< 2^63) with missing prev/MA: got %v, want ErrABPoWInvalid", err)
	}

	// Embedded address with any non-zero difficulty (including >= 2^63) fails in pow() immediately.
	embeddedBlock := &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      1,
		BlockType:            nom.BlockTypeContractReceive,
		Address:              types.PlasmaContract,
		Height:               2,
		PreviousHash:         types.NewHash([]byte("missing-prev")),
		FromBlockHash:        types.NewHash([]byte("send")),
		MomentumAcknowledged: ma,
		Difficulty:           math.MaxUint64,
	}
	abvEmb := &accountBlockVerifier{
		block:         embeddedBlock,
		frontierStore: &stubPoWMomentumStore{chainID: 1, frontier: nil},
		chain:         nil,
	}
	if err := abvEmb.all(); !errors.Is(err, ErrABPoWInvalid) {
		t.Fatalf("embedded block with non-zero difficulty: got %v, want ErrABPoWInvalid", err)
	}

	// powSporkRules with chain == nil and Difficulty <= MaxDifficultyForAccountBlock
	// takes the fast path without touching chain.
	abvFast := &accountBlockVerifier{
		block:     newPoWTestBlock(addr, ma, dp.MaxDifficultyForAccountBlock),
		chain:     nil,
		strictPoW: false,
	}
	if err := abvFast.powSporkRules(); err != nil {
		t.Fatalf("powSporkRules fast path with nil chain: got %v, want nil", err)
	}

	// powSporkRules with chain == nil, strictPoW == false, and Difficulty >= 2^63
	// returns ErrABMAMissing without panicking.
	abvNilChain := &accountBlockVerifier{
		block:     newPoWTestBlock(addr, ma, math.MaxUint64),
		chain:     nil,
		strictPoW: false,
	}
	if err := abvNilChain.powSporkRules(); !errors.Is(err, ErrABMAMissing) {
		t.Fatalf("powSporkRules high difficulty with nil chain: got %v, want ErrABMAMissing", err)
	}

	// Strict admission rejects Difficulty > dp.MaxDifficultyForAccountBlock
	// (including >= 2^63) fail-fast inside pow(), before previous()/MA lookups.
	for _, difficulty := range []uint64{dp.MaxDifficultyForAccountBlock + 1, 1 << 63, math.MaxUint64} {
		strictBlock := &nom.AccountBlock{
			Version:              1,
			ChainIdentifier:      1,
			BlockType:            nom.BlockTypeUserSend,
			Address:              addr,
			ToAddress:            addr,
			Amount:               big.NewInt(0),
			Height:               2,
			PreviousHash:         types.NewHash([]byte("missing-prev")),
			MomentumAcknowledged: ma,
			Difficulty:           difficulty,
		}
		abvStrict := &accountBlockVerifier{
			block:         strictBlock,
			accountStore:  &stubPoWAccountStore{frontier: &nom.AccountBlock{Height: 1, Hash: types.NewHash([]byte("other"))}},
			frontierStore: &stubPoWMomentumStore{chainID: 1, frontier: nil},
			chain:         nil,
			strictPoW:     true,
		}
		if err := abvStrict.all(); !errors.Is(err, ErrABPoWInvalid) {
			t.Fatalf("strict all(difficulty=%d) with missing prev/MA: got %v, want ErrABPoWInvalid", difficulty, err)
		}
	}

	// In-momentum verification defers Difficulty >= 2^63 past previous()/MA so
	// missing context yields the non-remembered context verdict rather than ErrABPoWInvalid.
	inMomBlock := &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      1,
		BlockType:            nom.BlockTypeUserSend,
		Address:              addr,
		ToAddress:            addr,
		Amount:               big.NewInt(0),
		Height:               2,
		PreviousHash:         types.NewHash([]byte("missing-prev")),
		MomentumAcknowledged: ma,
		Difficulty:           math.MaxUint64,
	}
	abvInMom := &accountBlockVerifier{
		block:         inMomBlock,
		accountStore:  &stubPoWAccountStore{frontier: &nom.AccountBlock{Height: 1, Hash: types.NewHash([]byte("other"))}},
		frontierStore: &stubPoWMomentumStore{chainID: 1, frontier: nil},
		chain:         nil,
		strictPoW:     false,
	}
	if err := abvInMom.all(); !errors.Is(err, ErrABPreviousMissing) {
		t.Fatalf("in-momentum all(difficulty=MaxUint64) with missing prev: got %v, want ErrABPreviousMissing", err)
	}
}
