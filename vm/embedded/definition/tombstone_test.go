package definition

import (
	"math/big"
	"testing"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// Tombstone deletion markers (empty values) left by a rollback must be
// skipped instead of panicking the parse helpers, matching the
// by-id getters' treatment of empty values as absent.
//
// The tombstone encoding is produced by enableDeleteDB.Delete(false),
// which writes Put(key, []byte{}) — the same encoding that
// AppendPatchToLevelDBBatch(..., false) writes during a Pop. Using
// Delete() through the wrapper pins the encoding rather than assuming
// it.

func TestGetAllSentinelInfoSkipsTombstones(t *testing.T) {
	context := db.NewMemDB()

	owner1, err := types.BytesToAddress(make([]byte, 20))
	if err != nil {
		t.Fatal(err)
	}
	owner2, err := types.BytesToAddress(append(make([]byte, 19), 2))
	if err != nil {
		t.Fatal(err)
	}

	real := &SentinelInfo{
		SentinelInfoKey: SentinelInfoKey{Owner: owner1},
		ZnnAmount:       big.NewInt(100),
		QsrAmount:       big.NewInt(200),
	}
	if err := context.Put(real.Key(), real.Data()); err != nil {
		t.Fatal(err)
	}

	// Insert a second sentinel, then delete it through the enableDelete
	// wrapper (fullDelete=false), which writes an empty-value tombstone.
	tombstone := &SentinelInfo{
		SentinelInfoKey: SentinelInfoKey{Owner: owner2},
		ZnnAmount:       big.NewInt(300),
		QsrAmount:       big.NewInt(400),
	}
	if err := context.Put(tombstone.Key(), tombstone.Data()); err != nil {
		t.Fatal(err)
	}
	if err := context.Delete(tombstone.Key()); err != nil {
		t.Fatal(err)
	}

	// Verify the tombstone encoding: the key exists but reads as
	// not-found through the enableDelete wrapper (empty value → nil).
	rawVal, err := context.Get(tombstone.Key())
	if err != leveldb.ErrNotFound {
		t.Fatalf("expected tombstone key to read as not-found, got val=%v err=%v", rawVal, err)
	}

	sentinels := GetAllSentinelInfo(context)
	if len(sentinels) != 1 {
		t.Fatalf("expected 1 sentinel, got %d", len(sentinels))
	}
	if sentinels[0].Owner != real.Owner {
		t.Fatalf("expected sentinel owner %v, got %v", real.Owner, sentinels[0].Owner)
	}
}

func TestIterateSentinelEntriesSkipsTombstones(t *testing.T) {
	context := db.NewMemDB()

	owner1, err := types.BytesToAddress(make([]byte, 20))
	if err != nil {
		t.Fatal(err)
	}
	owner2, err := types.BytesToAddress(append(make([]byte, 19), 2))
	if err != nil {
		t.Fatal(err)
	}

	real := &SentinelInfo{
		SentinelInfoKey: SentinelInfoKey{Owner: owner1},
		ZnnAmount:       big.NewInt(100),
		QsrAmount:       big.NewInt(200),
	}
	if err := context.Put(real.Key(), real.Data()); err != nil {
		t.Fatal(err)
	}

	tombstone := &SentinelInfo{
		SentinelInfoKey: SentinelInfoKey{Owner: owner2},
		ZnnAmount:       big.NewInt(300),
		QsrAmount:       big.NewInt(400),
	}
	if err := context.Put(tombstone.Key(), tombstone.Data()); err != nil {
		t.Fatal(err)
	}
	if err := context.Delete(tombstone.Key()); err != nil {
		t.Fatal(err)
	}

	count := 0
	err = IterateSentinelEntries(context, func(s *SentinelInfo) error {
		count++
		if s.Owner != real.Owner {
			t.Fatalf("expected sentinel owner %v, got %v", real.Owner, s.Owner)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 sentinel, got %d", count)
	}
}

func TestGetProjectListSkipsTombstones(t *testing.T) {
	context := db.NewMemDB()

	owner1, err := types.BytesToAddress(make([]byte, 20))
	if err != nil {
		t.Fatal(err)
	}
	owner2, err := types.BytesToAddress(append(make([]byte, 19), 2))
	if err != nil {
		t.Fatal(err)
	}

	real := &Project{
		Id:             types.NewHash([]byte{1}),
		Owner:          owner1,
		Name:           "test-project",
		ZnnFundsNeeded: big.NewInt(1000),
		QsrFundsNeeded: big.NewInt(2000),
	}
	if err := context.Put(real.Key(), real.Data()); err != nil {
		t.Fatal(err)
	}

	tombstone := &Project{
		Id:             types.NewHash([]byte{2}),
		Owner:          owner2,
		Name:           "rolled-back-project",
		ZnnFundsNeeded: big.NewInt(3000),
		QsrFundsNeeded: big.NewInt(4000),
	}
	if err := context.Put(tombstone.Key(), tombstone.Data()); err != nil {
		t.Fatal(err)
	}
	if err := context.Delete(tombstone.Key()); err != nil {
		t.Fatal(err)
	}

	// Verify the tombstone encoding.
	rawVal, err := context.Get(tombstone.Key())
	if err != leveldb.ErrNotFound {
		t.Fatalf("expected tombstone key to read as not-found, got val=%v err=%v", rawVal, err)
	}

	projects, err := GetProjectList(context)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Fatalf("expected 1 project, got %d", len(projects))
	}
	if projects[0].Id != real.Id {
		t.Fatalf("expected project id %v, got %v", real.Id, projects[0].Id)
	}
	if projects[0].Name != "test-project" {
		t.Fatalf("expected project name %q, got %q", "test-project", projects[0].Name)
	}
}
