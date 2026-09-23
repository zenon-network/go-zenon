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
// getAbsHashes from its own chain, newest first like the protocol handler.
// Momentums start at height 1, so a reply never contains a height 0 hash.
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
	for h := uint64(1); h <= head; h++ {
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
		for i := count - 1; i >= 0; i-- {
			if from+uint64(i) == 0 {
				continue
			}
			hashes = append(hashes, peerHash(from+uint64(i)))
		}
		go func() { d.hashCh <- hashPack{peerId: p.id, hashes: hashes} }()
		return nil
	}
	return d, p
}

// A fork deeper than the head-scan window must be located by the binary
// search rather than falling back to 0 (a resync from genesis).
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

// A node at the first momentum must resolve the ancestor to its own height
// even when the peer's reply spans a full hash window.
func TestFindAncestorFreshNode(t *testing.T) {
	const head, forkAt = uint64(1), uint64(1)
	d, p := mockAncestorChains(head, forkAt)

	number, err := d.findAncestor(p)
	if err != nil {
		t.Fatal(err)
	}
	if number != forkAt {
		t.Fatalf("findAncestor = %d, want %d", number, forkAt)
	}
}

// A short local chain sits inside a head-scan window that starts at height 0,
// which the reply does not include; the ancestor must still be the local head.
func TestFindAncestorShortChain(t *testing.T) {
	const head, forkAt = uint64(100), uint64(100)
	d, p := mockAncestorChains(head, forkAt)

	number, err := d.findAncestor(p)
	if err != nil {
		t.Fatal(err)
	}
	if number != forkAt {
		t.Fatalf("findAncestor = %d, want %d", number, forkAt)
	}
}

// A head-scan reply larger than requested is rejected rather than trusted.
func TestFindAncestorOversizedReply(t *testing.T) {
	d, p := mockAncestorChains(1, 1)
	p.getAbsHashes = func(from uint64, count int) error {
		hashes := make([]types.Hash, MaxHashFetch+1)
		hashes[0] = testHash('s', 1)
		go func() { d.hashCh <- hashPack{peerId: p.id, hashes: hashes} }()
		return nil
	}

	if _, err := d.findAncestor(p); err != errBadPeer {
		t.Fatalf("findAncestor error = %v, want %v", err, errBadPeer)
	}
}
