package downloader

import (
	"encoding/binary"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

func testHash(prefix byte, height uint64) types.Hash {
	var b [9]byte
	b[0] = prefix
	binary.BigEndian.PutUint64(b[1:], height)
	return types.NewHash(b[:])
}

// mockAncestorChains builds a downloader over a local chain of `head` blocks
// sharing history with a peer only up to `forkAt`, and a peer answering
// getAbsHashes from its own chain.
func mockAncestorChains(head, forkAt uint64) (*Downloader, *peer) {
	localHash := func(h uint64) types.Hash {
		if h <= forkAt {
			return testHash('s', h) // shared history
		}
		return testHash('l', h)
	}
	peerHash := func(h uint64) types.Hash {
		if h <= forkAt {
			return testHash('s', h)
		}
		return testHash('p', h)
	}
	localByHash := make(map[types.Hash]uint64, head+1)
	for h := uint64(0); h <= head; h++ {
		localByHash[localHash(h)] = h
	}

	hasBlock := func(hash types.Hash) bool {
		_, ok := localByHash[hash]
		return ok
	}
	getBlock := func(hash types.Hash) *nom.DetailedMomentum {
		h, ok := localByHash[hash]
		if !ok {
			return nil
		}
		return &nom.DetailedMomentum{Momentum: &nom.Momentum{Height: h}}
	}
	headBlock := func() *nom.Momentum { return &nom.Momentum{Height: head} }

	d := New(hasBlock, getBlock, headBlock, nil, nil)

	p := newPeer("test-peer", 0, peerHash(head), nil, nil, nil)
	p.getAbsHashes = func(from uint64, count int) error {
		hashes := make([]types.Hash, 0, count)
		for i := 0; i < count; i++ {
			hashes = append(hashes, peerHash(from+uint64(i)))
		}
		go func() { d.hashCh <- hashPack{peerId: p.id, hashes: hashes} }()
		return nil
	}
	return d, p
}

// A fork deeper than the head-scan window must be located by the binary
// search; the inverted guard used to return 0 (resync from genesis) instead.
func TestFindAncestorLongFork(t *testing.T) {
	const head, forkAt = uint64(1100), uint64(10)
	d, p := mockAncestorChains(head, forkAt)

	number, err := d.findAncestor(p)
	if err != nil {
		t.Fatal(err)
	}
	if number != forkAt {
		t.Fatalf("findAncestor = %d, want %d (fork point)", number, forkAt)
	}
}

// A match inside the head-scan window must return that ancestor directly.
func TestFindAncestorRecentFork(t *testing.T) {
	const head, forkAt = uint64(1100), uint64(1090)
	d, p := mockAncestorChains(head, forkAt)

	number, err := d.findAncestor(p)
	if err != nil {
		t.Fatal(err)
	}
	if number != forkAt {
		t.Fatalf("findAncestor = %d, want %d (fork point)", number, forkAt)
	}
}
