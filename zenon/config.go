package zenon

import (
	"path"

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/wallet"
)

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

func (c *Config) NewDBManager(inside string) db.Manager {
	return db.NewLevelDBManager(path.Join(c.DataDir, inside))
}
func (c *Config) NewLevelDB(inside string) (db.DB, *leveldb.DB) {
	return db.NewLevelDB(path.Join(c.DataDir, inside))
}
