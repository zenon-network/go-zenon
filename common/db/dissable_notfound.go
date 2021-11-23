package db

import (
	"github.com/syndtr/goleveldb/leveldb"
)

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

func DisableNotFound(db DB) DB {
	return &disableNotFoundDB{DB: db}
}
