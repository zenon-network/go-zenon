package momentum

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
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
		// The panic value must identify the malformed field, not just be any error.
		msg, ok := r.(error)
		if !ok {
			t.Fatalf("expected error panic, got %T: %v", r, r)
		}
		if !strings.Contains(msg.Error(), "invalid previous hash") {
			t.Fatalf("expected panic to mention 'invalid previous hash', got: %v", msg)
		}
	}()
	_ = store.Identifier()
}

// TestIdentifierCorruptFrontierEntry verifies that Identifier panics with
// a diagnostic naming the momentum store and the frontier height when the
// frontier entry exists but cannot be deserialized.
func TestIdentifierCorruptFrontierEntry(t *testing.T) {
	memdb := db.NewMemDB()
	version := types.HashHeight{Height: 1, Hash: types.Hash{1}}
	common.FailIfErr(t, db.SetFrontier(memdb, version, []byte{0xff, 0xfe, 0xfd}))
	ms := &momentumStore{DB: memdb}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on corrupt frontier entry, got none")
		}
		msg := fmt.Sprintf("%v", r)
		if !strings.Contains(msg, "momentum store: frontier momentum entry at height 1") {
			t.Fatalf("expected diagnostic naming the momentum store and entry height, got: %v", msg)
		}
	}()
	ms.Identifier()
}
