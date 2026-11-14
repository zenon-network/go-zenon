package db

import (
	"github.com/pkg/errors"
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/common"
)

// metadataByte prefixes node-local values that live in the Manager's
// LevelDB next to, but outside of, the versioned ledger state: they
// are not part of any version, patch or rollback, so Add and Pop never
// touch them and they never reach a state hash.
var metadataByte = []byte{109}

// MetadataStore is implemented by managers that can keep small
// node-local values outside the versioned ledger state. The values
// describe how this database was initialised (for example the spork
// address of the genesis it was created with); they are not consensus
// data and are never synced to or from peers. GetMetadata returns
// (nil, nil) for a key that was never written.
type MetadataStore interface {
	GetMetadata(key []byte) ([]byte, error)
	PutMetadata(key, value []byte) error
}

// GetMetadata returns the node-local value stored under key, or
// (nil, nil) if there is none.
func (m *ldbManager) GetMetadata(key []byte) ([]byte, error) {
	m.changes.Lock()
	defer m.changes.Unlock()
	if m.stopped {
		return nil, errors.Errorf("can't read metadata from stopped db")
	}
	value, err := m.ldb.Get(common.JoinBytes(metadataByte, key), nil)
	if err == leveldb.ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrapf(err, "read metadata %q", key)
	}
	return value, nil
}

// PutMetadata stores value under key, outside the versioned state.
// The write is synced: when PutMetadata returns nil the value is on
// stable storage, so a crash right after it cannot lose a record that
// the caller has already acted on.
func (m *ldbManager) PutMetadata(key, value []byte) error {
	m.changes.Lock()
	defer m.changes.Unlock()
	if m.stopped {
		return errors.Errorf("can't write metadata to stopped db")
	}
	batch := new(leveldb.Batch)
	batch.Put(common.JoinBytes(metadataByte, key), value)
	if err := m.writeSync(batch); err != nil {
		return errors.Wrapf(err, "write metadata %q", key)
	}
	return nil
}
