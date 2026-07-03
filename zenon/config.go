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
// optional pillar producing key pair (the coinbase), and the genesis
// configuration the chain is initialized against.
type Config struct {
	MinPeers          int
	MinConnectedPeers int
	DataDir           string
	ProducingKeyPair  *wallet.KeyPair
	GenesisConfig     store.Genesis
}

// NewDBManager opens a versioned db.Manager rooted at the named
// subdirectory of the config data directory (for example "nom" for
// the chain ledger).
func (c *Config) NewDBManager(inside string) db.Manager {
	return db.NewLevelDBManager(path.Join(c.DataDir, inside))
}

// NewLevelDB opens a plain leveldb at the named subdirectory of the
// config data directory (for example "consensus"), returning both the
// db.DB wrapper and the underlying *leveldb.DB so the caller can close
// it on shutdown.
func (c *Config) NewLevelDB(inside string) (db.DB, *leveldb.DB) {
	return db.NewLevelDB(path.Join(c.DataDir, inside))
}
