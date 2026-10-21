package account

import (
	"fmt"
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/chain/account/mailbox"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

var controlAddress = types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")

// closedLevelDB returns a db.DB wrapper around a LevelDB handle that has
// already been closed, so every Get returns leveldb.ErrClosed.
func closedLevelDB(t *testing.T) db.DB {
	wrapper, raw := db.NewLevelDB(t.TempDir())
	common.FailIfErr(t, raw.Close())
	return wrapper
}

func TestSequencerFrontIndexReadError(t *testing.T) {
	as := NewAccountStore(controlAddress, closedLevelDB(t)).(*accountStore)
	_, err := as.sequencerFrontIndex()
	if err == nil {
		t.Fatal("expected error for closed DB, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read sequencer front index") {
		t.Fatalf("expected read diagnostic, got: %v", err)
	}
}

func TestSequencerFrontPropagatesReadError(t *testing.T) {
	as := NewAccountStore(controlAddress, closedLevelDB(t))
	mb := mailbox.NewAccountMailbox(controlAddress, db.NewMemDB())
	header, err := as.SequencerFront(mb)
	if err == nil {
		t.Fatalf("expected error, got header %v", header)
	}
	if !strings.Contains(err.Error(), "failed to read sequencer front index") {
		t.Fatalf("expected counter read diagnostic, got: %v", err)
	}
}

func TestIdentifierCorruptFrontierEntry(t *testing.T) {
	memdb := db.NewMemDB()
	version := types.HashHeight{Height: 1, Hash: types.Hash{1}}
	common.FailIfErr(t, db.SetFrontier(memdb, version, []byte{0xff, 0xfe, 0xfd}))
	as := NewAccountStore(controlAddress, memdb)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on corrupt frontier entry, got none")
		}
		msg := fmt.Sprintf("%v", r)
		if !strings.Contains(msg, "account store "+controlAddress.String()) || !strings.Contains(msg, "failed to read frontier entry") {
			t.Fatalf("expected diagnostic naming the account store, got: %v", msg)
		}
	}()
	as.Identifier()
}
