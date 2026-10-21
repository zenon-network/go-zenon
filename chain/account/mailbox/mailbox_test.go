package mailbox

import (
	"strings"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
	"google.golang.org/protobuf/proto"

	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// atMost == 0 must return no hashes; the counter is checked before the first
// append so it cannot underflow into an unlimited scan.
func TestGetUnreceivedAccountBlockHashesAtMost(t *testing.T) {
	m := NewAccountMailbox(types.PillarContract, db.NewMemDB())
	for i := byte(1); i <= 3; i++ {
		if err := m.MarkAsUnreceived(types.NewHash([]byte{i})); err != nil {
			t.Fatal(err)
		}
	}

	for _, tc := range []struct {
		atMost uint64
		want   int
	}{{0, 0}, {1, 1}, {2, 2}, {10, 3}} {
		hashes, err := m.GetUnreceivedAccountBlockHashes(tc.atMost)
		if err != nil {
			t.Fatal(err)
		}
		if len(hashes) != tc.want {
			t.Fatalf("atMost=%d: got %d hashes, want %d", tc.atMost, len(hashes), tc.want)
		}
	}
}

// TestParseAccountHeaderCorrupted verifies that parseAccountHeader returns an
// error instead of panicking when the stored data is corrupted.
func TestParseAccountHeaderCorrupted(t *testing.T) {
	// Build a valid account header proto, then corrupt the HashHeight.Hash field.
	valid := &types.AccountHeader{
		Address: types.Address{1, 2, 3},
		HashHeight: types.HashHeight{
			Hash:   types.Hash{4, 5, 6},
			Height: 42,
		},
	}
	pb := valid.Proto()
	pb.HashHeight.Hash = &types.HashProto{Hash: []byte{1, 2, 3, 4, 5}} // wrong length
	data, err := proto.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}

	header, err := parseAccountHeader(data, nil)
	if err == nil {
		t.Fatal("expected error for corrupted account-header data, got nil")
	}
	if header != nil {
		t.Fatalf("expected nil header on error, got %v", header)
	}
	if !strings.Contains(err.Error(), "failed to deserialize account-header") {
		t.Fatalf("expected deserialize error message, got: %v", err)
	}
}

// TestParseAccountHeaderNotFound verifies that parseAccountHeader returns
// nil, nil for leveldb.ErrNotFound.
func TestParseAccountHeaderNotFound(t *testing.T) {
	header, err := parseAccountHeader(nil, leveldb.ErrNotFound)
	if err != nil {
		t.Fatalf("expected nil error for ErrNotFound, got: %v", err)
	}
	if header != nil {
		t.Fatalf("expected nil header for ErrNotFound, got %v", header)
	}
}

// TestGetBlockWhichReceivesCorrupted verifies that GetBlockWhichReceives
// returns an error instead of panicking when the stored data is corrupted.
func TestGetBlockWhichReceivesCorrupted(t *testing.T) {
	mem := db.NewMemDB()
	m := NewAccountMailbox(types.PillarContract, mem)

	// Write corrupted data directly to the store.
	fromHash := types.NewHash([]byte{1})
	valid := &types.AccountHeader{
		Address: types.Address{1, 2, 3},
		HashHeight: types.HashHeight{
			Hash:   types.Hash{4, 5, 6},
			Height: 42,
		},
	}
	pb := valid.Proto()
	pb.HashHeight.Hash = &types.HashProto{Hash: []byte{1, 2, 3, 4, 5}} // wrong length
	data, err := proto.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	if err := mem.Put(getBlockWhichReceivesKey(fromHash), data); err != nil {
		t.Fatal(err)
	}

	header, err := m.GetBlockWhichReceives(fromHash)
	if err == nil {
		t.Fatal("expected error for corrupted stored data, got nil")
	}
	if header != nil {
		t.Fatalf("expected nil header on error, got %v", header)
	}
}

// TestSequencerByHeightCorrupted verifies that SequencerByHeight returns an
// error instead of panicking when the stored data is corrupted.
func TestSequencerByHeightCorrupted(t *testing.T) {
	mem := db.NewMemDB()
	m := NewAccountMailbox(types.PillarContract, mem)

	valid := &types.AccountHeader{
		Address: types.Address{1, 2, 3},
		HashHeight: types.HashHeight{
			Hash:   types.Hash{4, 5, 6},
			Height: 42,
		},
	}
	pb := valid.Proto()
	pb.HashHeight.Hash = &types.HashProto{Hash: []byte{1, 2, 3, 4, 5}} // wrong length
	data, err := proto.Marshal(pb)
	if err != nil {
		t.Fatal(err)
	}
	if err := mem.Put(getSequencerHeaderByHeightKey(1), data); err != nil {
		t.Fatal(err)
	}

	header, err := m.SequencerByHeight(1)
	if err == nil {
		t.Fatal("expected error for corrupted stored data, got nil")
	}
	if header != nil {
		t.Fatalf("expected nil header on error, got %v", header)
	}
}
