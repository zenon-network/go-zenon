package vm

import (
	"crypto/ed25519"
	"math/big"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/momentum"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// stubMomentumStore returns a fixed frontier momentum and nil for every
// other lookup, which is enough for setBlockMomentum to succeed when
// the template already carries a non-zero MomentumAcknowledged.
type stubMomentumStore struct {
	store.Momentum
	frontier *nom.Momentum
}

func (s *stubMomentumStore) GetFrontierMomentum() (*nom.Momentum, error) {
	return s.frontier, nil
}

// stubChainForGenerate embeds chain.Chain and overrides the lookups
// that GenerateFromTemplate makes, so a test can force the nil-store
// panic path in newBlockContext without a real chain.
type stubChainForGenerate struct {
	chain.Chain
	momentumStore store.Momentum
	accountStore  store.Account
	cacheStore    store.Cache
}

func (c *stubChainForGenerate) ChainIdentifier() uint64 { return 1 }

func (c *stubChainForGenerate) GetFrontierMomentumStore() store.Momentum {
	return c.momentumStore
}

func (c *stubChainForGenerate) GetAccountStore(_ types.Address, _ types.HashHeight) store.Account {
	return c.accountStore
}

func (c *stubChainForGenerate) GetCacheStore(_ types.HashHeight) store.Cache {
	return c.cacheStore
}

// TestGenerateFromTemplate_NilAccountStoreRecover drives
// GenerateFromTemplate past setAll (frontier momentum readable,
// template fields set) and into newBlockContext, where the stub chain's
// GetAccountStore returns nil. newUserBlockContext panics on that nil;
// without the deferred recover the panic escapes to the producer
// goroutine's RecoverStack handler, which re-panics and ends the
// process. The recover must convert it to constants.ErrVmRunPanic and
// a nil transaction.
func TestGenerateFromTemplate_NilAccountStoreRecover(t *testing.T) {
	frontier := &nom.Momentum{
		Hash:   types.HexToHashPanic("01aab631b44daf47f68810a416bea4ed3695e89c0ab9668a0ab8acb834dfb900"),
		Height: 1,
	}
	chain := &stubChainForGenerate{
		momentumStore: &stubMomentumStore{frontier: frontier},
		accountStore:  nil,
		cacheStore:    nil,
	}
	s := NewSupervisor(chain, nil)

	template := &nom.AccountBlock{
		BlockType:            nom.BlockTypeUserSend,
		Address:              types.ParseAddressPanic("z1qzuej4d8292sstg38qpnca9tfpfmglqjryffpv"),
		ToAddress:            types.PillarContract,
		MomentumAcknowledged: frontier.Identifier(),
		PreviousHash:         types.HexToHashPanic("02aab631b44daf47f68810a416bea4ed3695e89c0ab9668a0ab8acb834dfb901"),
		Height:               2,
	}

	transaction, err := s.GenerateFromTemplate(template, nil)
	common.ExpectError(t, err, constants.ErrVmRunPanic)
	common.Expect(t, transaction, nil)
}

// TestGenerateFromTemplate_NilCacheStoreRecover is the cache-side twin:
// the account store resolves (a real in-memory store) but GetCacheStore
// returns nil, which newUserBlockContext also panics on. Same recover,
// same outcome.
func TestGenerateFromTemplate_NilCacheStoreRecover(t *testing.T) {
	userAddr := types.ParseAddressPanic("z1qzuej4d8292sstg38qpnca9tfpfmglqjryffpv")
	frontier := &nom.Momentum{
		Hash:   types.HexToHashPanic("01aab631b44daf47f68810a416bea4ed3695e89c0ab9668a0ab8acb834dfb900"),
		Height: 1,
	}
	chain := &stubChainForGenerate{
		momentumStore: &stubMomentumStore{frontier: frontier},
		accountStore:  account.NewAccountStore(userAddr, db.NewMemDB()),
		cacheStore:    nil,
	}
	s := NewSupervisor(chain, nil)

	template := &nom.AccountBlock{
		BlockType:            nom.BlockTypeUserSend,
		Address:              userAddr,
		ToAddress:            types.PillarContract,
		MomentumAcknowledged: frontier.Identifier(),
		PreviousHash:         types.HexToHashPanic("02aab631b44daf47f68810a416bea4ed3695e89c0ab9668a0ab8acb834dfb901"),
		Height:               2,
	}

	transaction, err := s.GenerateFromTemplate(template, nil)
	common.ExpectError(t, err, constants.ErrVmRunPanic)
	common.Expect(t, transaction, nil)
}

type embeddedContextCountingChain struct {
	stubChainForGenerate
	genesis               *nom.Momentum
	getMomentumStoreCalls int
}

func (c *embeddedContextCountingChain) GetGenesisMomentum() *nom.Momentum {
	return c.genesis
}

func (c *embeddedContextCountingChain) GetMomentumStore(_ types.HashHeight) store.Momentum {
	c.getMomentumStoreCalls++
	return c.momentumStore
}

// TestNewEmbeddedBlockContext_ReusesMomentumStoreForPillarReader pins that
// constructing an embedded VM context opens the historical momentum store once
// and passes a snapshot of it to consensus.FixedPillarReaderForStore instead
// of invoking chain.GetMomentumStore a second time.
func TestNewEmbeddedBlockContext_ReusesMomentumStoreForPillarReader(t *testing.T) {
	ts := time.Unix(1637755200, 0)
	genesis := &nom.Momentum{
		Hash:          types.HexToHashPanic("01aab631b44daf47f68810a416bea4ed3695e89c0ab9668a0ab8acb834dfb900"),
		Height:        1,
		TimestampUnix: uint64(ts.Unix()),
		Timestamp:     &ts,
	}
	memStore := momentum.NewStore(nil, db.NewMemDB())
	counting := &embeddedContextCountingChain{
		stubChainForGenerate: stubChainForGenerate{
			momentumStore: memStore,
			accountStore:  account.NewAccountStore(types.PillarContract, db.NewMemDB()),
			cacheStore:    noSporkCache{},
		},
		genesis: genesis,
	}
	cs := consensus.NewConsensus(db.NewMemDB(), counting, true)
	s := NewSupervisor(counting, cs)
	counting.getMomentumStoreCalls = 0

	block := &nom.AccountBlock{
		BlockType:            nom.BlockTypeContractReceive,
		Address:              types.PillarContract,
		MomentumAcknowledged: genesis.Identifier(),
		Height:               1,
	}
	ctx := s.newEmbeddedBlockContext(block)
	common.Expect(t, counting.getMomentumStoreCalls, 1)
	if ctx.EpochTicker() == nil {
		t.Fatal("expected embedded context to expose initialized pillar reader")
	}
	common.Expect(t, counting.getMomentumStoreCalls, 1)
}

type countingTxVerifier struct {
	verifier.Verifier
	blockCalls           int
	blockInMomentumCalls int
	txCalls              int
	txInMomentumCalls    int
	lastTx               *nom.AccountBlockTransaction
	lastTotalPlasma      uint64
}

func (v *countingTxVerifier) AccountBlock(*nom.AccountBlock) error {
	v.blockCalls++
	return nil
}

func (v *countingTxVerifier) AccountBlockInMomentum(*nom.AccountBlock) error {
	v.blockInMomentumCalls++
	return nil
}

func (v *countingTxVerifier) AccountBlockTransaction(tx *nom.AccountBlockTransaction) error {
	v.txCalls++
	v.lastTx = tx
	v.lastTotalPlasma = tx.Block.TotalPlasma
	return nil
}

func (v *countingTxVerifier) AccountBlockTransactionInMomentum(tx *nom.AccountBlockTransaction, _ uint64) error {
	v.txInMomentumCalls++
	v.lastTx = tx
	v.lastTotalPlasma = tx.Block.TotalPlasma
	return nil
}

// TestApplyBlock_VerifiesTransactionOnce pins that externally supplied blocks
// (ApplyBlock and ApplyBlockInMomentum) run transaction verification exactly
// once before VM execution rather than repeating it inside packBlockWith,
// while locally signed blocks (GenerateFromTemplate) still verify once after
// signing.
func TestApplyBlock_VerifiesTransactionOnce(t *testing.T) {
	userAddr := types.ParseAddressPanic("z1qzuej4d8292sstg38qpnca9tfpfmglqjryffpv")
	frontier := &nom.Momentum{
		Hash:   types.HexToHashPanic("01aab631b44daf47f68810a416bea4ed3695e89c0ab9668a0ab8acb834dfb900"),
		Height: 1,
	}
	chainStub := &stubChainForGenerate{
		momentumStore: &stubMomentumStore{frontier: frontier},
		accountStore:  account.NewAccountStore(userAddr, db.NewMemDB()),
		cacheStore:    noSporkCache{},
	}
	counting := &countingTxVerifier{}
	s := NewSupervisor(chainStub, nil)
	s.verifier = counting

	newUserSend := func() *nom.AccountBlock {
		return &nom.AccountBlock{
			Version:              1,
			ChainIdentifier:      1,
			BlockType:            nom.BlockTypeUserSend,
			Address:              userAddr,
			ToAddress:            userAddr,
			Amount:               big.NewInt(0),
			MomentumAcknowledged: frontier.Identifier(),
			Height:               1,
			Difficulty:           constants.AccountBlockBasePlasma * constants.PoWDifficultyPerPlasma,
		}
	}

	// ApplyBlock (signFunc == nil): verifies once before VM, not again in packBlockWith.
	tx, err := s.ApplyBlock(newUserSend())
	common.FailIfErr(t, err)
	common.Expect(t, counting.blockCalls, 1)
	common.Expect(t, counting.blockInMomentumCalls, 0)
	common.Expect(t, counting.txCalls, 1)
	common.Expect(t, counting.lastTotalPlasma, uint64(0))
	if tx.Block.TotalPlasma == 0 || tx.Changes == nil {
		t.Fatalf("expected VM execution and packing after pre-VM verify, got totalPlasma=%d changes=%v", tx.Block.TotalPlasma, tx.Changes)
	}

	// ApplyBlockInMomentum (signFunc == nil): verifies once before VM via AccountBlockInMomentum,
	// not again in packBlockWith.
	tx, err = s.ApplyBlockInMomentum(newUserSend(), 10)
	common.FailIfErr(t, err)
	common.Expect(t, counting.blockCalls, 1)
	common.Expect(t, counting.blockInMomentumCalls, 1)
	common.Expect(t, counting.txInMomentumCalls, 1)
	common.Expect(t, counting.lastTotalPlasma, uint64(0))
	if tx.Block.TotalPlasma == 0 || tx.Changes == nil {
		t.Fatalf("expected VM execution and packing after pre-VM verify, got totalPlasma=%d changes=%v", tx.Block.TotalPlasma, tx.Changes)
	}

	// VerifyBlockInMomentum routes through AccountBlockInMomentum and AccountBlockTransactionInMomentum.
	err = s.VerifyBlockInMomentum(newUserSend(), 10)
	common.FailIfErr(t, err)
	common.Expect(t, counting.blockCalls, 1)
	common.Expect(t, counting.blockInMomentumCalls, 2)
	common.Expect(t, counting.txInMomentumCalls, 2)

	// GenerateFromTemplate (signFunc != nil): skips pre-VM verify and verifies once after signing.
	counting.txCalls = 0
	counting.lastTx = nil
	tx, err = s.GenerateFromTemplate(newUserSend(), func(data []byte) ([]byte, *types.Address, []byte, error) {
		return []byte("sig"), &userAddr, []byte("pub"), nil
	})
	common.FailIfErr(t, err)
	common.Expect(t, counting.txCalls, 1)
	if counting.lastTx == nil || counting.lastTx.Changes == nil || len(tx.Block.Signature) == 0 {
		t.Fatalf("expected GenerateFromTemplate to verify after signing and packing")
	}

	// ApplyBlock on an externally supplied embedded send with canonical Data:
	// ValidateSendBlock re-packs Data to the same bytes, so the hash is
	// unchanged and transaction verification still runs once pre-VM.
	counting.txCalls = 0
	acctStore := account.NewAccountStore(userAddr, db.NewMemDB())
	common.FailIfErr(t, acctStore.SetBalance(types.QsrTokenStandard, new(big.Int).Mul(constants.FuseMinAmount, big.NewInt(10))))
	chainStub.accountStore = acctStore
	fuseData := definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, userAddr)
	canonicalFuse := &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      1,
		BlockType:            nom.BlockTypeUserSend,
		Address:              userAddr,
		ToAddress:            types.PlasmaContract,
		Amount:               new(big.Int).Set(constants.FuseMinAmount),
		TokenStandard:        types.QsrTokenStandard,
		MomentumAcknowledged: frontier.Identifier(),
		Height:               1,
		Data:                 append([]byte{}, fuseData...),
		Difficulty:           constants.EmbeddedSimplePlasma * constants.PoWDifficultyPerPlasma,
	}
	_, err = s.ApplyBlock(canonicalFuse)
	common.FailIfErr(t, err)
	common.Expect(t, counting.txCalls, 1)

	// ApplyBlock on an externally supplied embedded send whose Data is re-packed
	// to different bytes by ValidateSendBlock must re-run transaction
	// verification in packBlockWith so a real verifier catches the resulting
	// hash mismatch.
	counting.txCalls = 0
	trailingFuse := &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      1,
		BlockType:            nom.BlockTypeUserSend,
		Address:              userAddr,
		ToAddress:            types.PlasmaContract,
		Amount:               new(big.Int).Set(constants.FuseMinAmount),
		TokenStandard:        types.QsrTokenStandard,
		MomentumAcknowledged: frontier.Identifier(),
		Height:               1,
		Data:                 append(append([]byte{}, fuseData...), 0x00),
		Difficulty:           constants.EmbeddedSimplePlasma * constants.PoWDifficultyPerPlasma,
	}
	_, err = s.ApplyBlock(trailingFuse)
	common.FailIfErr(t, err)
	common.Expect(t, counting.txCalls, 2)
}

type reverifyMomentumStore struct {
	store.Momentum
	frontier *nom.Momentum
}

func (s *reverifyMomentumStore) ChainIdentifier() uint64 { return 1 }

func (s *reverifyMomentumStore) Identifier() types.HashHeight {
	return s.frontier.Identifier()
}

func (s *reverifyMomentumStore) GetFrontierMomentum() (*nom.Momentum, error) {
	return s.frontier, nil
}

func (s *reverifyMomentumStore) GetMomentumByHeight(height uint64) (*nom.Momentum, error) {
	if height == s.frontier.Height {
		return s.frontier, nil
	}
	return nil, nil
}

type reverifyCacheStore struct {
	store.Cache
	dynamicPlasma bool
	fusedQsr      *big.Int
}

func (c *reverifyCacheStore) IsSporkActive(spork *types.ImplementedSpork) (bool, error) {
	return spork == types.DynamicPlasmaSpork && c.dynamicPlasma, nil
}

func (c *reverifyCacheStore) GetChainPlasma(types.Address) (*big.Int, error) {
	return big.NewInt(0), nil
}

func (c *reverifyCacheStore) GetStakeBeneficialAmount(types.Address) (*big.Int, error) {
	return new(big.Int).Set(c.fusedQsr), nil
}

// TestApplyBlock_RejectsRePackedEmbeddedData pins that externally supplied
// embedded-contract send blocks whose Data is mutated in place by
// ValidateSendBlock (trailing bytes before DynamicPlasmaSpork, or non-canonical
// ABI padding even after DynamicPlasmaSpork) are rejected with
// verifier.ErrABHashInvalid rather than accepted with a corrupted Data/Hash
// pair.
func TestApplyBlock_RejectsRePackedEmbeddedData(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 42
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	userAddr := types.PubKeyToAddress(pub)

	frontier := &nom.Momentum{
		Hash:   types.HexToHashPanic("01aab631b44daf47f68810a416bea4ed3695e89c0ab9668a0ab8acb834dfb900"),
		Height: 1,
	}

	newSupervisor := func(dynamicPlasma bool) *Supervisor {
		tenFuse := new(big.Int).Mul(constants.FuseMinAmount, big.NewInt(10))
		acctStore := account.NewAccountStore(userAddr, db.NewMemDB())
		common.FailIfErr(t, acctStore.SetBalance(types.QsrTokenStandard, tenFuse))
		chainStub := &stubChainForGenerate{
			momentumStore: &reverifyMomentumStore{frontier: frontier},
			accountStore:  acctStore,
			cacheStore: &reverifyCacheStore{
				dynamicPlasma: dynamicPlasma,
				fusedQsr:      tenFuse,
			},
		}
		return NewSupervisor(chainStub, nil)
	}

	canonicalFuse := definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, userAddr)
	trailingFuse := append(append([]byte{}, canonicalFuse...), 0x00)
	nonCanonicalPadFuse := append([]byte{}, canonicalFuse...)
	// Flip a byte in the 12-byte left-padding of the 32-byte ABI address word
	// (bytes [4..15]): same exact length as canonical (passes CheckNoTrailingBytes),
	// unpacked identically by ABIPlasma.UnpackMethod, then zeroed by PackMethod.
	nonCanonicalPadFuse[4] = 0x01

	makeSignedFuse := func(data []byte) *nom.AccountBlock {
		b := &nom.AccountBlock{
			Version:              1,
			ChainIdentifier:      1,
			BlockType:            nom.BlockTypeUserSend,
			Address:              userAddr,
			ToAddress:            types.PlasmaContract,
			Amount:               new(big.Int).Set(constants.FuseMinAmount),
			TokenStandard:        types.QsrTokenStandard,
			MomentumAcknowledged: frontier.Identifier(),
			Height:               1,
			Data:                 append([]byte{}, data...),
			FusedPlasma:          constants.EmbeddedSimplePlasma + uint64(len(data))*constants.ABByteDataPlasma,
		}
		b.Hash = b.ComputeHash()
		b.PublicKey = append([]byte{}, pub...)
		b.Signature = ed25519.Sign(priv, b.Hash.Bytes())
		return b
	}

	t.Run("pre_dynamic_plasma_trailing_bytes_ApplyBlock", func(t *testing.T) {
		s := newSupervisor(false)
		tx, err := s.ApplyBlock(makeSignedFuse(trailingFuse))
		common.ExpectError(t, err, verifier.ErrABHashInvalid)
		common.Expect(t, tx, nil)
	})

	t.Run("pre_dynamic_plasma_trailing_bytes_ApplyBlockInMomentum", func(t *testing.T) {
		s := newSupervisor(false)
		tx, err := s.ApplyBlockInMomentum(makeSignedFuse(trailingFuse), 10)
		common.ExpectError(t, err, verifier.ErrABHashInvalid)
		common.Expect(t, tx, nil)
	})

	t.Run("pre_dynamic_plasma_canonical_accepted", func(t *testing.T) {
		s := newSupervisor(false)
		tx, err := s.ApplyBlock(makeSignedFuse(canonicalFuse))
		common.FailIfErr(t, err)
		if tx == nil || tx.Block.ComputeHash() != tx.Block.Hash {
			t.Fatalf("expected valid transaction with matching hash, got %+v", tx)
		}
	})

	t.Run("post_dynamic_plasma_trailing_bytes_rejected_by_spork_rule", func(t *testing.T) {
		s := newSupervisor(true)
		tx, err := s.ApplyBlock(makeSignedFuse(trailingFuse))
		common.ExpectError(t, err, constants.ErrABDataTrailingBytes)
		common.Expect(t, tx, nil)
	})

	t.Run("post_dynamic_plasma_noncanonical_padding_rejected", func(t *testing.T) {
		s := newSupervisor(true)
		tx, err := s.ApplyBlock(makeSignedFuse(nonCanonicalPadFuse))
		common.ExpectError(t, err, verifier.ErrABHashInvalid)
		common.Expect(t, tx, nil)
	})

	t.Run("post_dynamic_plasma_canonical_accepted", func(t *testing.T) {
		s := newSupervisor(true)
		tx, err := s.ApplyBlock(makeSignedFuse(canonicalFuse))
		common.FailIfErr(t, err)
		if tx == nil || tx.Block.ComputeHash() != tx.Block.Hash {
			t.Fatalf("expected valid transaction with matching hash, got %+v", tx)
		}
	})
}
