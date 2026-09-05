package protocol

import (
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/rlp"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/protocol/downloader"
	"github.com/zenon-network/go-zenon/protocol/fetcher"
)

// GetBlocksMsg names blocks by hash, and every named hash costs the receiver a
// store lookup whether or not the block exists. The tests below drive the real
// handleMsg entry point over a message pipe and count those lookups, so the
// per-request work bound is asserted on the receiver's actual behaviour rather
// than on a helper.

// lookupCountingChain is the minimal chainManager the GetBlocksMsg path needs.
// It serves the blocks it was given and counts every GetBlock call.
type lookupCountingChain struct {
	known   map[types.Hash]*nom.DetailedMomentum
	lookups int
}

func (c *lookupCountingChain) GetBlock(hash types.Hash) *nom.DetailedMomentum {
	c.lookups++
	return c.known[hash]
}

func (c *lookupCountingChain) HasBlock(types.Hash) bool { panic("not used by GetBlocksMsg") }
func (c *lookupCountingChain) GetBlockHashesFromHash(types.Hash, uint64) ([]types.Hash, error) {
	panic("not used by GetBlocksMsg")
}
func (c *lookupCountingChain) GetBlockByNumber(uint64) (*nom.Momentum, error) {
	panic("not used by GetBlocksMsg")
}
func (c *lookupCountingChain) CurrentBlock() *nom.Momentum { panic("not used by GetBlocksMsg") }
func (c *lookupCountingChain) Status() (uint64, types.Hash, types.Hash) {
	panic("not used by GetBlocksMsg")
}
func (c *lookupCountingChain) InsertChain([]*nom.DetailedMomentum) (int, error) {
	panic("not used by GetBlocksMsg")
}
func (c *lookupCountingChain) VerifyMomentum(*nom.DetailedMomentum) error {
	panic("not used by GetBlocksMsg")
}

// knownBlocks builds n distinct momentums (heights 2..n+1, so SendBlocks'
// genesis special case stays out of the way) and returns them with their
// hashes in request order.
func knownBlocks(n int) (map[types.Hash]*nom.DetailedMomentum, []types.Hash) {
	known := make(map[types.Hash]*nom.DetailedMomentum, n)
	hashes := make([]types.Hash, 0, n)
	for i := 0; i < n; i++ {
		momentum := &nom.Momentum{Height: uint64(i + 2)}
		momentum.Hash = momentum.ComputeHash()
		known[momentum.Hash] = &nom.DetailedMomentum{Momentum: momentum}
		hashes = append(hashes, momentum.Hash)
	}
	return known, hashes
}

// unknownHashes returns n distinct hashes that no test chain stores.
func unknownHashes(n int) []types.Hash {
	hashes := make([]types.Hash, n)
	for i := range hashes {
		hashes[i] = types.NewHash([]byte{'m', 'i', 's', 's', byte(i >> 24), byte(i >> 16), byte(i >> 8), byte(i)})
	}
	return hashes
}

type getBlocksResult struct {
	blocks   []*nom.DetailedMomentum
	answered bool // whether the handler wrote a BlocksMsg before the pipe closed
}

// serveGetBlocks sends one GetBlocksMsg naming hashes to a handler backed by
// chain, returns the handler's error, and reports whether a BlocksMsg reply
// was written and what it carried.
func serveGetBlocks(t *testing.T, chain *lookupCountingChain, hashes []types.Hash) (getBlocksResult, error) {
	t.Helper()

	app, net := p2p.MsgPipe()
	defer func() { _ = app.Close() }()

	pm := &ProtocolManager{chainman: chain}
	p := &peer{rw: net, id: "test-peer"}

	// The pipe blocks the writer until the reader has consumed the whole
	// payload, and the handler only discards what it did not decode after it
	// has replied. The request writer and the reply reader therefore run on
	// separate goroutines so neither can wait on the other.
	sendErr := make(chan error, 1)
	go func() {
		sendErr <- p2p.Send(app, GetBlocksMsg, hashes)
	}()

	done := make(chan getBlocksResult, 1)
	go func() {
		msg, err := app.ReadMsg()
		if err != nil {
			// The pipe was closed without a reply.
			done <- getBlocksResult{}
			return
		}
		result := getBlocksResult{answered: true}
		if msg.Code != BlocksMsg {
			t.Errorf("reply code %d, want BlocksMsg (%d)", msg.Code, BlocksMsg)
		}
		stream := rlp.NewStream(msg.Payload, uint64(msg.Size))
		if err := stream.Decode(&result.blocks); err != nil {
			t.Errorf("decode reply: %v", err)
		}
		done <- result
	}()

	err := pm.handleMsg(p)
	if err != nil {
		// No reply is coming; release the reader.
		_ = app.Close()
	}
	if serr := <-sendErr; serr != nil {
		t.Fatalf("send request: %v", serr)
	}
	return <-done, err
}

func TestMaxBlocksRequest_CoversEveryHonestRequester(t *testing.T) {
	if MaxBlocksRequest < downloader.MaxBlockFetch {
		t.Fatalf("MaxBlocksRequest %d is below the downloader batch of %d", MaxBlocksRequest, downloader.MaxBlockFetch)
	}
	if MaxBlocksRequest < fetcher.HashLimit {
		t.Fatalf("MaxBlocksRequest %d is below the fetcher announce limit of %d", MaxBlocksRequest, fetcher.HashLimit)
	}
}

func TestHandleGetBlocks_ServesKnownBlocks(t *testing.T) {
	known, hashes := knownBlocks(3)
	chain := &lookupCountingChain{known: known}

	result, err := serveGetBlocks(t, chain, hashes)
	if err != nil {
		t.Fatalf("handleMsg: %v", err)
	}
	if !result.answered {
		t.Fatal("no BlocksMsg reply")
	}
	if len(result.blocks) != 3 {
		t.Fatalf("reply carries %d blocks, want 3", len(result.blocks))
	}
	for i, block := range result.blocks {
		if block.Momentum.Hash != hashes[i] {
			t.Fatalf("block %d has hash %v, want %v", i, block.Momentum.Hash, hashes[i])
		}
	}
	if chain.lookups != 3 {
		t.Fatalf("%d lookups, want 3", chain.lookups)
	}
}

// A full-size request whose hashes are all unknown is legitimate (a peer may
// simply be ahead of us) and must be answered with an empty reply after
// exactly one lookup per hash.
func TestHandleGetBlocks_FullRequestOfUnknownHashesIsAnswered(t *testing.T) {
	chain := &lookupCountingChain{}

	result, err := serveGetBlocks(t, chain, unknownHashes(MaxBlocksRequest))
	if err != nil {
		t.Fatalf("handleMsg: %v", err)
	}
	if !result.answered {
		t.Fatal("no BlocksMsg reply")
	}
	if len(result.blocks) != 0 {
		t.Fatalf("reply carries %d blocks, want 0", len(result.blocks))
	}
	if chain.lookups != MaxBlocksRequest {
		t.Fatalf("%d lookups, want %d", chain.lookups, MaxBlocksRequest)
	}
}

// One hash past the limit is a protocol violation: the handler must return an
// error before looking that hash up, and must not answer.
func TestHandleGetBlocks_RejectsOversizedRequestBeforeExtraLookup(t *testing.T) {
	for _, count := range []int{MaxBlocksRequest + 1, 8 * MaxBlocksRequest} {
		chain := &lookupCountingChain{}

		result, err := serveGetBlocks(t, chain, unknownHashes(count))
		if err == nil {
			t.Fatalf("%d hashes: handleMsg accepted the request", count)
		}
		if !strings.Contains(err.Error(), errCode(ErrMsgTooLarge).String()) {
			t.Fatalf("%d hashes: error %q does not report %q", count, err, errCode(ErrMsgTooLarge).String())
		}
		if result.answered {
			t.Fatalf("%d hashes: a rejected request was answered", count)
		}
		if chain.lookups != MaxBlocksRequest {
			t.Fatalf("%d hashes: %d lookups, want exactly %d", count, chain.lookups, MaxBlocksRequest)
		}
	}
}

// Known and unknown hashes count the same: the limit is on the request, not
// on the hits.
func TestHandleGetBlocks_MixedRequestPastLimitIsRejected(t *testing.T) {
	known, knownHashes := knownBlocks(16)
	chain := &lookupCountingChain{known: known}

	hashes := make([]types.Hash, 0, MaxBlocksRequest+1)
	unknown := unknownHashes(MaxBlocksRequest + 1)
	for i := 0; i < MaxBlocksRequest+1; i++ {
		if i%16 == 0 && i/16 < len(knownHashes) {
			hashes = append(hashes, knownHashes[i/16])
		} else {
			hashes = append(hashes, unknown[i])
		}
	}

	result, err := serveGetBlocks(t, chain, hashes)
	if err == nil {
		t.Fatal("handleMsg accepted the request")
	}
	if result.answered {
		t.Fatal("a rejected request was answered")
	}
	if chain.lookups != MaxBlocksRequest {
		t.Fatalf("%d lookups, want exactly %d", chain.lookups, MaxBlocksRequest)
	}
}

// The reply-volume cap is unchanged: a request naming more known blocks than
// MaxBlockFetch is answered with MaxBlockFetch blocks and the remaining hashes
// are never looked up.
func TestHandleGetBlocks_ReplyStillCappedAtMaxBlockFetch(t *testing.T) {
	known, hashes := knownBlocks(MaxBlocksRequest + 4)
	chain := &lookupCountingChain{known: known}

	result, err := serveGetBlocks(t, chain, hashes)
	if err != nil {
		t.Fatalf("handleMsg: %v", err)
	}
	if !result.answered {
		t.Fatal("no BlocksMsg reply")
	}
	if len(result.blocks) != downloader.MaxBlockFetch {
		t.Fatalf("reply carries %d blocks, want %d", len(result.blocks), downloader.MaxBlockFetch)
	}
	if chain.lookups != downloader.MaxBlockFetch {
		t.Fatalf("%d lookups, want %d", chain.lookups, downloader.MaxBlockFetch)
	}
}

func TestHandleGetBlocks_EmptyRequestIsAnswered(t *testing.T) {
	chain := &lookupCountingChain{}

	result, err := serveGetBlocks(t, chain, []types.Hash{})
	if err != nil {
		t.Fatalf("handleMsg: %v", err)
	}
	if !result.answered {
		t.Fatal("no BlocksMsg reply")
	}
	if len(result.blocks) != 0 || chain.lookups != 0 {
		t.Fatalf("reply carries %d blocks after %d lookups, want 0 and 0", len(result.blocks), chain.lookups)
	}
}

// readGetBlocksRequests reads GetBlocksMsg frames from rw until the pipe
// closes and returns the hash list carried by each.
func readGetBlocksRequests(t *testing.T, rw p2p.MsgReadWriter) <-chan [][]types.Hash {
	t.Helper()
	out := make(chan [][]types.Hash, 1)
	go func() {
		var requests [][]types.Hash
		for {
			msg, err := rw.ReadMsg()
			if err != nil {
				out <- requests
				return
			}
			if msg.Code != GetBlocksMsg {
				t.Errorf("request code %d, want GetBlocksMsg (%d)", msg.Code, GetBlocksMsg)
			}
			var hashes []types.Hash
			if err := rlp.NewStream(msg.Payload, uint64(msg.Size)).Decode(&hashes); err != nil {
				t.Errorf("decode request: %v", err)
			}
			requests = append(requests, hashes)
		}
	}()
	return out
}

// RequestBlocks is the only place this node encodes a GetBlocksMsg. Whatever
// its callers hand it, no single request on the wire may name more than
// MaxBlocksRequest hashes, or the remote side drops us.
func TestRequestBlocks_SplitsBatchesLargerThanMaxBlocksRequest(t *testing.T) {
	for _, count := range []int{1, MaxBlocksRequest, MaxBlocksRequest + 1, 2*MaxBlocksRequest + 5} {
		app, net := p2p.MsgPipe()
		p := &peer{rw: net, id: "test-peer"}
		hashes := unknownHashes(count)
		requests := readGetBlocksRequests(t, app)

		if err := p.RequestBlocks(hashes); err != nil {
			t.Fatalf("%d hashes: RequestBlocks: %v", count, err)
		}
		_ = app.Close()

		var sent []types.Hash
		for i, request := range <-requests {
			if len(request) == 0 || len(request) > MaxBlocksRequest {
				t.Fatalf("%d hashes: request %d names %d hashes, want 1..%d", count, i, len(request), MaxBlocksRequest)
			}
			sent = append(sent, request...)
		}
		if len(sent) != len(hashes) {
			t.Fatalf("%d hashes: %d hashes reached the wire", count, len(sent))
		}
		for i := range hashes {
			if sent[i] != hashes[i] {
				t.Fatalf("%d hashes: hash %d differs or is out of order", count, i)
			}
		}
	}
}
