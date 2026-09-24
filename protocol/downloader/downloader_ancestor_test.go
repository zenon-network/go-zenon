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

// peerReply answers a getAbsHashes request the way the protocol handler does:
// the count is capped at MaxHashFetch, a request above the remote head gets
// an empty reply, the requested range is clipped to the remote head,
// momentums start at height 1 so height 0 is never included, and hashes are
// sent newest first.
func peerReply(remoteHead uint64, peerHash func(uint64) types.Hash, from uint64, count int) []types.Hash {
	if count > MaxHashFetch {
		count = MaxHashFetch
	}
	if from > remoteHead {
		return nil
	}
	last := from + uint64(count) - 1
	if last > remoteHead {
		last = remoteHead
	}
	hashes := make([]types.Hash, 0, count)
	for h := last; h >= from && h >= 1; h-- {
		hashes = append(hashes, peerHash(h))
	}
	return hashes
}

// mockAncestorChains builds a downloader over a local chain of `head` blocks
// sharing history with a peer only up to `forkAt`, and a peer whose own chain
// reaches `remoteHead` and answers getAbsHashes like the protocol handler.
func mockAncestorChains(head, remoteHead, forkAt uint64) (*Downloader, *peer) {
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

	p := newPeer("test-peer", 0, peerHash(remoteHead), nil, nil, nil)
	p.getAbsHashes = func(from uint64, count int) error {
		hashes := peerReply(remoteHead, peerHash, from, count)
		go func() { d.hashCh <- hashPack{peerId: p.id, hashes: hashes} }()
		return nil
	}
	return d, p
}

// A fork deeper than the head-scan window must be located by the binary
// search rather than falling back to 0 (a resync from genesis).
func TestFindAncestorLongFork(t *testing.T) {
	const head, forkAt = uint64(1100), uint64(10)
	d, p := mockAncestorChains(head, head, forkAt)

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
	d, p := mockAncestorChains(head, head, forkAt)

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
	d, p := mockAncestorChains(head, 2000, forkAt)

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
	d, p := mockAncestorChains(head, 2000, forkAt)

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
	d, p := mockAncestorChains(1, 1, 1)
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

// A valid reply is clipped at the remote head: a peer at height 300 answers a
// request for [0, 512) with [h300, ..., h1], and the ancestor is the local head.
func TestFindAncestorClippedReply(t *testing.T) {
	const head, remoteHead = uint64(100), uint64(300)
	d, p := mockAncestorChains(head, remoteHead, head)

	reply := peerReply(remoteHead, func(h uint64) types.Hash { return testHash('s', h) }, 0, MaxHashFetch)
	if len(reply) != int(remoteHead) {
		t.Fatalf("reply length = %d, want %d", len(reply), remoteHead)
	}
	if reply[0] != testHash('s', remoteHead) || reply[len(reply)-1] != testHash('s', 1) {
		t.Fatalf("reply must run from h%d down to h1", remoteHead)
	}

	number, err := d.findAncestor(p)
	if err != nil {
		t.Fatal(err)
	}
	if number != head {
		t.Fatalf("findAncestor = %d, want %d", number, head)
	}
}

// The head scan asks for the window [from, from+MaxHashFetch-1] with
// from = max(0, head-MaxHashFetch). Around MaxHashFetch the origin moves off 0
// and the local head leaves the window, so the ancestor becomes head-1; below
// that the window still reaches the head.
func TestFindAncestorWindowOrigin(t *testing.T) {
	window := uint64(MaxHashFetch)
	cases := []struct {
		head, want uint64
	}{
		{window - 1, window - 1},
		{window, window - 1},
		{window + 1, window},
	}
	for _, c := range cases {
		d, p := mockAncestorChains(c.head, c.head, c.head)
		number, err := d.findAncestor(p)
		if err != nil {
			t.Fatalf("head %d: %v", c.head, err)
		}
		if number != c.want {
			t.Fatalf("head %d: findAncestor = %d, want %d", c.head, number, c.want)
		}
	}
}

// A peer whose head lies below the requested window answers with no hashes,
// which is reported as an empty set rather than treated as a match.
func TestFindAncestorPeerBelowWindow(t *testing.T) {
	const head, remoteHead = uint64(1100), uint64(500)
	d, p := mockAncestorChains(head, remoteHead, remoteHead)

	reply := peerReply(remoteHead, func(h uint64) types.Hash { return testHash('s', h) }, head-uint64(MaxHashFetch), MaxHashFetch)
	if len(reply) != 0 {
		t.Fatalf("reply length = %d, want 0", len(reply))
	}

	if _, err := d.findAncestor(p); err != errEmptyHashSet {
		t.Fatalf("findAncestor error = %v, want %v", err, errEmptyHashSet)
	}
}

// A reply is rejected when the range it implies does not fit the requested
// window. Each case places a hash we hold so that a different bound fails,
// with every other hash in the reply unknown locally.
func TestFindAncestorReplyOutsideWindow(t *testing.T) {
	cases := []struct {
		name        string
		head        uint64
		numHashes   int
		matchHeight uint64
		matchIndex  int
	}{
		// window [588, 1099]
		{"single hash below window", 1100, 1, 1, 0},
		{"full reply with below-window hash first", 1100, MaxHashFetch, 1, 0},
		{"implied bottom 89 below window", 1100, MaxHashFetch, 600, 0},
		{"implied top 1100 above window", 1100, MaxHashFetch, 1099, 1},
		// window [1, 511]: the reply is longer than the matched height allows
		{"reply longer than match height", 1, MaxHashFetch - 1, 1, 0},
	}
	for _, c := range cases {
		d, p := mockAncestorChains(c.head, c.head, c.head)
		p.getAbsHashes = func(from uint64, count int) error {
			hashes := make([]types.Hash, c.numHashes)
			for i := range hashes {
				hashes[i] = testHash('x', uint64(i))
			}
			hashes[c.matchIndex] = testHash('s', c.matchHeight)
			go func() { d.hashCh <- hashPack{peerId: p.id, hashes: hashes} }()
			return nil
		}

		if _, err := d.findAncestor(p); err != errBadPeer {
			t.Fatalf("%s: findAncestor error = %v, want %v", c.name, err, errBadPeer)
		}
	}
}
