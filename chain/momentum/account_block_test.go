package momentum

import (
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// storeAccountBlock writes block into the account chain of its address the
// way the account store reads it back by height.
func storeAccountBlock(t *testing.T, ms *momentumStore, block *nom.AccountBlock) {
	t.Helper()
	data, err := block.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetFrontier(ms.DB.Subset(getAccountStorePrefix(block.Address)), block.Identifier(), data); err != nil {
		t.Fatal(err)
	}
}

// GetAccountBlock is asked for a header, which names a hash as well as an
// address and height. A block stored at that height under another hash is
// not the block the header names, and must not be returned as if it were.
func TestGetAccountBlockRejectsAHeaderWithAnotherHash(t *testing.T) {
	ms := &momentumStore{DB: db.NewMemDB()}
	block := &nom.AccountBlock{Address: types.PillarContract, BlockType: nom.BlockTypeUserSend, Height: 1}
	block.Hash = block.ComputeHash()
	storeAccountBlock(t, ms, block)

	got, err := ms.GetAccountBlock(block.Header())
	if err != nil {
		t.Fatalf("GetAccountBlock(stored header): %v", err)
	}
	if got == nil || got.Hash != block.Hash {
		t.Fatalf("GetAccountBlock(stored header) = %v, want block %v", got, block.Hash)
	}

	other := block.Header()
	other.Hash = types.NewHash([]byte("another block at the same height"))
	got, err = ms.GetAccountBlock(other)
	if err == nil {
		t.Fatalf("GetAccountBlock(header with another hash) returned block %v and no error", got)
	}
	if got != nil {
		t.Fatalf("GetAccountBlock(header with another hash) returned block %v beside error %v", got.Hash, err)
	}
	if !strings.Contains(err.Error(), block.Hash.String()) || !strings.Contains(err.Error(), other.Hash.String()) {
		t.Fatalf("error %q does not name both hashes", err)
	}

	missing := block.Header()
	missing.Height = 2
	if got, err := ms.GetAccountBlock(missing); got != nil || err != nil {
		t.Fatalf("GetAccountBlock(header above the frontier) = (%v, %v), want (nil, nil)", got, err)
	}
}
