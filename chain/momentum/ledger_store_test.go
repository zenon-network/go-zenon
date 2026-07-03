package momentum

import (
	"testing"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

var errForcedApply = errors.New("forced apply failure")

// failingApplyDB fails every Apply call, on itself and on all subsets.
type failingApplyDB struct {
	db.DB
}

func (f *failingApplyDB) Subset(prefix []byte) db.DB {
	return &failingApplyDB{DB: f.DB.Subset(prefix)}
}
func (f *failingApplyDB) Apply(db.Patch) error {
	return errForcedApply
}

// A failed account-store Apply during momentum insertion must surface the
// error instead of silently skipping the block's ledger bookkeeping.
func TestAddAccountBlockTransactionPropagatesApplyError(t *testing.T) {
	store := NewStore(nil, &failingApplyDB{DB: db.NewMemDB()})

	patch := db.NewPatch()
	patch.Put([]byte("key"), []byte("value"))

	header := types.AccountHeader{
		Address: types.PillarContract,
		HashHeight: types.HashHeight{
			Hash:   types.ZeroHash,
			Height: 1,
		},
	}
	if err := store.AddAccountBlockTransaction(header, patch); !errors.Is(err, errForcedApply) {
		t.Fatalf("expected forced apply error to propagate, got %v", err)
	}
}
