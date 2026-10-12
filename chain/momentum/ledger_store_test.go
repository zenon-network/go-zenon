package momentum

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain/nom"
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

// TestIdentifier_MalformedFrontierMomentum verifies that Identifier() does
// not silently return a zero HashHeight when the frontier momentum record in
// the store is corrupt.  With valid frontier metadata pointing at a height
// whose serialized momentum is malformed (wrong-length hash), the old code
// took the nil-frontier branch and returned zero; the fixed code must
// surface the deserialization error via DealWithErr (panic).
func TestIdentifier_MalformedFrontierMomentum(t *testing.T) {
	mem := db.NewMemDB()

	// Build a momentum proto that will fail DeProtoHash on PreviousHash.
	valid := &nom.Momentum{
		Version:         1,
		ChainIdentifier: 1,
		Hash:            types.Hash{1},
		PreviousHash:    types.Hash{2},
		Height:          1,
		TimestampUnix:   1000000,
		Content:         nom.MomentumContent{},
		ChangesHash:     types.Hash{3},
	}
	pb := valid.Proto()
	pb.PreviousHash = &types.HashProto{Hash: []byte{1, 2, 3, 4, 5}} // wrong length
	badData, err := proto.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}

	version := types.HashHeight{Hash: types.Hash{1}, Height: 1}
	if err := db.SetFrontier(mem, version, badData); err != nil {
		t.Fatal(err)
	}

	store := NewStore(nil, mem)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic from DealWithErr on malformed frontier momentum, got nil")
		}
	}()
	_ = store.Identifier()
}
