package tests

import (
	"errors"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/chain"
	cache "github.com/zenon-network/go-zenon/chain/cache/storage"
	"github.com/zenon-network/go-zenon/chain/genesis"
	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// startChain opens the chain and cache databases in the given
// directories and runs chain.Init with genesisConfig, stopping the
// chain again before returning Init's error.
func startChain(t *testing.T, chainDir, cacheDir string, genesisConfig *genesis.GenesisConfig) error {
	t.Helper()
	chainManager, err := db.NewLevelDBManager(chainDir)
	common.FailIfErr(t, err)
	cacheManager, err := cache.NewCacheDBManager(cacheDir)
	common.FailIfErr(t, err)
	c := chain.NewChain(chainManager, cacheManager, genesis.NewGenesis(genesisConfig))
	initErr := c.Init()
	common.FailIfErr(t, c.Stop())
	return initErr
}

// withSporkAddress returns a shallow copy of the mock genesis config
// that differs only in its spork address.
func withSporkAddress(address types.Address) *genesis.GenesisConfig {
	config := *g.EmbeddedGenesis
	config.SporkAddress = &address
	return &config
}

// A chain database is bound to the spork address it was first started
// with: reopening it under a genesis config that differs only in
// SporkAddress — which the genesis momentum hash does not catch — is
// refused, and the original address still opens it.
func TestChainInitBindsSporkAddress(t *testing.T) {
	saved := types.SporkAddress
	defer func() { types.SporkAddress = saved }()

	chainDir, cacheDir := t.TempDir(), t.TempDir()
	original := g.EmbeddedGenesis
	other := withSporkAddress(g.User1.Address)
	if genesis.NewGenesis(original).GetGenesisMomentum().Hash != genesis.NewGenesis(other).GetGenesisMomentum().Hash {
		t.Fatal("the genesis momentum hash is expected not to commit to the spork address")
	}

	common.FailIfErr(t, startChain(t, chainDir, cacheDir, original))
	if *types.SporkAddress != g.Spork.Address {
		t.Fatalf("expected spork address %v, got %v", g.Spork.Address, types.SporkAddress)
	}

	// A refused start must not have installed the other spork
	// authority.
	types.SporkAddress = nil
	err := startChain(t, chainDir, cacheDir, other)
	if !errors.Is(err, chain.ErrSporkAddressMismatch) {
		t.Fatalf("expected ErrSporkAddressMismatch, got %v", err)
	}
	if types.SporkAddress != nil {
		t.Fatalf("expected types.SporkAddress to stay unset after a refused start, got %v", types.SporkAddress)
	}

	common.FailIfErr(t, startChain(t, chainDir, cacheDir, original))
}

// A malformed record is reported instead of being overwritten or
// treated as a match.
func TestChainInitRejectsCorruptSporkAddressRecord(t *testing.T) {
	saved := types.SporkAddress
	defer func() { types.SporkAddress = saved }()

	chainDir, cacheDir := t.TempDir(), t.TempDir()
	common.FailIfErr(t, startChain(t, chainDir, cacheDir, g.EmbeddedGenesis))

	raw, err := leveldb.OpenFile(chainDir, nil)
	common.FailIfErr(t, err)
	common.FailIfErr(t, raw.Put([]byte("mgenesis-spork-address"), []byte{1, 2, 3}, nil))
	common.FailIfErr(t, raw.Close())

	types.SporkAddress = nil
	if err := startChain(t, chainDir, cacheDir, g.EmbeddedGenesis); err == nil {
		t.Fatal("expected an error for a corrupt spork-address record")
	}
	if types.SporkAddress != nil {
		t.Fatalf("expected types.SporkAddress to stay unset, got %v", types.SporkAddress)
	}
}

// A database created before the binding existed has no record yet: its
// first start on a binary with the check records the configured spork
// address, and later starts are held to it.
func TestChainInitBindsExistingDatabaseOnFirstStart(t *testing.T) {
	saved := types.SporkAddress
	defer func() { types.SporkAddress = saved }()

	chainDir, cacheDir := t.TempDir(), t.TempDir()
	common.FailIfErr(t, startChain(t, chainDir, cacheDir, g.EmbeddedGenesis))

	// Drop the record to model a database written by an older binary.
	// The key is the metadata prefix byte 'm' followed by the chain's
	// record name.
	raw, err := leveldb.OpenFile(chainDir, nil)
	common.FailIfErr(t, err)
	common.FailIfErr(t, raw.Delete([]byte("mgenesis-spork-address"), nil))
	common.FailIfErr(t, raw.Close())

	other := withSporkAddress(g.User1.Address)
	common.FailIfErr(t, startChain(t, chainDir, cacheDir, other))
	if *types.SporkAddress != g.User1.Address {
		t.Fatalf("expected spork address %v, got %v", g.User1.Address, types.SporkAddress)
	}

	err = startChain(t, chainDir, cacheDir, g.EmbeddedGenesis)
	if !errors.Is(err, chain.ErrSporkAddressMismatch) {
		t.Fatalf("expected ErrSporkAddressMismatch, got %v", err)
	}
}

// failingMetadataManager is a chain database whose metadata writes
// fail, as a full disk or a failed fsync would.
type failingMetadataManager struct {
	db.Manager
	err error
}

func (m failingMetadataManager) GetMetadata(key []byte) ([]byte, error) {
	return m.Manager.(db.MetadataStore).GetMetadata(key)
}

func (m failingMetadataManager) PutMetadata([]byte, []byte) error { return m.err }

// If the binding cannot be persisted, Init fails before it installs the
// spork authority, so a node never runs under an address it has not
// durably recorded.
func TestChainInitFailsWhenSporkAddressBindingIsNotPersisted(t *testing.T) {
	saved := types.SporkAddress
	defer func() { types.SporkAddress = saved }()
	types.SporkAddress = nil

	chainManager, err := db.NewLevelDBManager(t.TempDir())
	common.FailIfErr(t, err)
	cacheManager, err := cache.NewCacheDBManager(t.TempDir())
	common.FailIfErr(t, err)
	putErr := errors.New("injected metadata write failure")
	c := chain.NewChain(failingMetadataManager{Manager: chainManager, err: putErr}, cacheManager, genesis.NewGenesis(g.EmbeddedGenesis))
	initErr := c.Init()
	common.FailIfErr(t, c.Stop())

	if !errors.Is(initErr, putErr) {
		t.Fatalf("expected the metadata write error, got %v", initErr)
	}
	if types.SporkAddress != nil {
		t.Fatalf("expected types.SporkAddress to stay unset when the binding is not persisted, got %v", types.SporkAddress)
	}
}
