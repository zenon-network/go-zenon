package db

// skipDeletedDb filters deletion tombstones out of iteration. The
// LevelDB-backed Manager wraps its reconstructed historical views in
// it so that keys deleted by rollback patches (stored as one-byte
// tombstones by ApplyWithoutOverride) do not surface when iterating.
type skipDeletedDb struct {
	db
}

func (db *skipDeletedDb) NewIterator(prefix []byte) StorageIterator {
	return newSkipDeletedIterator(db.db.NewIterator(prefix))
}

// skipDeletedIterator advances past entries whose encoded value is a
// tombstone — anything shorter than two bytes, i.e. the empty record
// written by enableDeleteDB.Delete or the one-byte marker written by
// ApplyWithoutOverride — yielding only live entries.
type skipDeletedIterator struct {
	StorageIterator
}

func (i *skipDeletedIterator) Next() bool {
	for {
		if !i.StorageIterator.Next() {
			return false
		}
		val := i.StorageIterator.Value()
		if len(val) > 1 {
			return true
		}
	}
}

func newSkipDeletedIterator(iterator StorageIterator) StorageIterator {
	return &skipDeletedIterator{
		StorageIterator: iterator,
	}
}
func newSkipDelete(db db) db {
	return &skipDeletedDb{
		db: db,
	}
}
