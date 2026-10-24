package zenon

import (
	"path"

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/wallet"
)

// Config holds the parameters NewZenon needs to build a node: the
// peer-count thresholds passed to the protocol manager, the data
// directory under which each subsystem opens its database, the
// optional pillar producing key pair (the coinbase), the genesis
// configuration the chain is initialized against, and the
// subscription limit handed to the RPC subscription server.
type Config struct {
	MinPeers          int
	MinConnectedPeers int
	DataDir           string
	ProducingKeyPair  *wallet.KeyPair
	GenesisConfig     store.Genesis
	// MaxSubscriptions bounds live ledger subscriptions across all RPC
	// connections; zero selects the subscribe server's default.
	MaxSubscriptions int
}

// NewDBManager opens a versioned db.Manager rooted at the named
// subdirectory of the config data directory (for example "nom" for
// the chain ledger). It returns an error if the database cannot be
// opened.
func (c *Config) NewDBManager(inside string) (db.Manager, error) {
	return db.NewLevelDBManager(path.Join(c.DataDir, inside))
}

// NewLevelDB opens a plain leveldb at the named subdirectory of the
// config data directory (for example "consensus"), returning both the
// db.DB wrapper and the underlying *leveldb.DB so the caller can close
// it on shutdown. It returns an error if the database cannot be
// opened.
func (c *Config) NewLevelDB(inside string) (db.DB, *leveldb.DB, error) {
	return db.NewLevelDB(path.Join(c.DataDir, inside))
}
