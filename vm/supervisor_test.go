package vm

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
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
