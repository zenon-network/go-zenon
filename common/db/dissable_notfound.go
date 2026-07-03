package db

import (
	"github.com/syndtr/goleveldb/leveldb"
)

// disableNotFoundDB overrides Get to mask leveldb.ErrNotFound; all
// other DB methods pass through unchanged.
type disableNotFoundDB struct {
	DB
}

func (d *disableNotFoundDB) Get(key []byte) ([]byte, error) {
	data, err := d.DB.Get(key)
	if err == leveldb.ErrNotFound {
		return []byte{}, nil
	}
	return data, err
}

// DisableNotFound wraps db so that Get returns an empty value and a
// nil error for missing keys instead of leveldb.ErrNotFound. The
// account stores wrap embedded-contract storage this way, giving
// contract code read-missing-as-empty semantics.
func DisableNotFound(db DB) DB {
	return &disableNotFoundDB{DB: db}
}
