package momentum

import (
	"fmt"

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// SetFrontier stores momentum as the new frontier entry of this
// version (see db.SetFrontier). In normal operation the db.Manager
// writes frontier entries itself when committing transactions, so
// this helper is currently unused.
func (ms *momentumStore) SetFrontier(momentum *nom.Momentum) error {
	data, err := momentum.Serialize()
	if err != nil {
		return err
	}

	return db.SetFrontier(ms.DB, momentum.Identifier(), data)
}

// parseMomentum deserializes a stored momentum entry, translating
// leveldb.ErrNotFound into a nil momentum with a nil error — the
// not-found convention all getters of this store share.
func parseMomentum(data []byte, err error) (*nom.Momentum, error) {
	if err == leveldb.ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return nom.DeserializeMomentum(data)
}

func (ms *momentumStore) GetFrontierMomentum() (*nom.Momentum, error) {
	return parseMomentum(db.GetEntryByHeight(ms.DB, db.GetFrontierIdentifier(ms.DB).Height))
}
func (ms *momentumStore) GetMomentumByHash(hash types.Hash) (*nom.Momentum, error) {
	return parseMomentum(db.GetEntryByHash(ms.DB, hash))
}
func (ms *momentumStore) GetMomentumsByHash(blockHash types.Hash, higher bool, count uint64) ([]*nom.Momentum, error) {
	momentum, err := ms.GetMomentumByHash(blockHash)
	if err != nil {
		return nil, err
	}
	return ms.GetMomentumsByHeight(momentum.Height, higher, count)
}
func (ms *momentumStore) GetMomentumByHeight(height uint64) (*nom.Momentum, error) {
	return parseMomentum(db.GetEntryByHeight(ms.DB, height))
}

// GetMomentumsByHeight returns up to count momentums in ascending
// height order: [height, height+count) when higher is true, the count
// momentums ending at height (inclusive) otherwise. Heights past the
// frontier yield nil entries in the result.
func (ms *momentumStore) GetMomentumsByHeight(height uint64, higher bool, count uint64) ([]*nom.Momentum, error) {
	var to, from uint64
	if higher {
		from = height
		to = height + count
	} else {
		if height+1 <= count {
			from = 1
		} else {
			from = height + 1 - count
		}
		to = height + 1
	}
	return ms.getMomentumsByRange(from, to)
}

// PrefetchMomentum expands the headers of momentum.Content into the
// full confirmed account blocks, producing the nom.DetailedMomentum
// that is broadcast to momentum event listeners.
func (ms *momentumStore) PrefetchMomentum(momentum *nom.Momentum) (*nom.DetailedMomentum, error) {
	accountBlocks := make([]*nom.AccountBlock, len(momentum.Content))
	for index := range momentum.Content {
		var err error
		accountBlocks[index], err = ms.GetAccountBlock(*momentum.Content[index])
		if err != nil {
			return nil, fmt.Errorf("error while prefetching account-blocks for insert-momentum event. %w", err)
		}
	}

	return &nom.DetailedMomentum{
		Momentum:      momentum,
		AccountBlocks: accountBlocks,
	}, nil
}

func (ms *momentumStore) getMomentumsByRange(from, to uint64) ([]*nom.Momentum, error) {
	list := make([]*nom.Momentum, 0, to-from)
	for i := from; i < to; i += 1 {
		momentum, err := ms.GetMomentumByHeight(i)
		if err != nil {
			return nil, err
		}
		list = append(list, momentum)
	}
	return list, nil
}
