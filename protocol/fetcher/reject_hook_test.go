package fetcher

import (
	"errors"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

// newTestMomentum returns a hashed momentum at height extending prevHash.
func newTestMomentum(height uint64, prevHash types.Hash) *nom.Momentum {
	m := &nom.Momentum{
		Version:         1,
		ChainIdentifier: 69,
		PreviousHash:    prevHash,
		Height:          height,
		TimestampUnix:   uint64(time.Now().Unix()),
	}
	m.Hash = m.ComputeHash()
	return m
}

// An import failure reaches the reject hook with the originating peer and
// the error before insert gives up, so the protocol layer can classify it.
func TestInsert_RejectHookReceivesImportError(t *testing.T) {
	parent := newTestMomentum(1, types.Hash{})
	block := newTestMomentum(2, parent.Hash)
	importErr := errors.New("rejected on consensus")

	type rejection struct {
		peer string
		err  error
	}
	rejections := make(chan rejection, 1)
	f := New(
		func(hash types.Hash) *nom.DetailedMomentum {
			if hash == parent.Hash {
				return &nom.DetailedMomentum{Momentum: parent}
			}
			return nil
		},
		func(*nom.Momentum, *nom.Momentum) error { return nil },
		func(*nom.DetailedMomentum, bool) {},
		func() uint64 { return 1 },
		func([]*nom.DetailedMomentum) (int, error) { return 0, importErr },
		func(string) {},
		func(id string, err error) { rejections <- rejection{id, err} },
	)
	// Started so the loop takes the done notice insert sends on return.
	f.Start()
	defer f.Stop()

	f.insert("peer-a", &nom.DetailedMomentum{Momentum: block})
	select {
	case got := <-rejections:
		if got.peer != "peer-a" || !errors.Is(got.err, importErr) {
			t.Fatalf("hook got peer=%q err=%v", got.peer, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reject hook not called within 5s")
	}
}
