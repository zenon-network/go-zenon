package db

type skipDeletedDb struct {
	db
}

func (db *skipDeletedDb) NewIterator(prefix []byte) StorageIterator {
	return newSkipDeletedIterator(db.db.NewIterator(prefix))
}

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
