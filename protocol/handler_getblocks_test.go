package protocol

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/rlp"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/protocol/downloader"
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
	return serveGetBlocksPayload(t, chain, hashes)
}

// serveGetBlocksPayload is serveGetBlocks for an arbitrary RLP-encodable
// payload, so a test can put something on the wire that is not a well-formed
// list of hashes.
func serveGetBlocksPayload(t *testing.T, chain *lookupCountingChain, payload interface{}) (getBlocksResult, error) {
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
		sendErr <- p2p.Send(app, GetBlocksMsg, payload)
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

// Stopping at the lookup bound leaves the rest of the oversized message
// unread; handleMsg discards it on return, so the next message on the same
// connection is decoded from its own start and served normally.
func TestHandleGetBlocks_NextMessageAfterOversizedRequestIsServed(t *testing.T) {
	known, knownHashes := knownBlocks(1)
	chain := &lookupCountingChain{known: known}

	app, net := p2p.MsgPipe()
	defer func() { _ = app.Close() }()
	pm := &ProtocolManager{chainman: chain}
	p := &peer{rw: net, id: "test-peer"}

	sendErr := make(chan error, 1)
	go func() {
		if err := p2p.Send(app, GetBlocksMsg, unknownHashes(MaxBlocksRequest+8)); err != nil {
			sendErr <- err
			return
		}
		sendErr <- p2p.Send(app, GetBlocksMsg, knownHashes)
	}()

	replies := make(chan []*nom.DetailedMomentum, 2)
	go func() {
		for i := 0; i < 2; i++ {
			msg, err := app.ReadMsg()
			if err != nil {
				t.Errorf("reply %d: %v", i, err)
				replies <- nil
				continue
			}
			if msg.Code != BlocksMsg {
				t.Errorf("reply %d code %d, want BlocksMsg (%d)", i, msg.Code, BlocksMsg)
			}
			var blocks []*nom.DetailedMomentum
			if err := rlp.NewStream(msg.Payload, uint64(msg.Size)).Decode(&blocks); err != nil {
				t.Errorf("decode reply %d: %v", i, err)
			}
			replies <- blocks
		}
	}()

	for i := 0; i < 2; i++ {
		if err := pm.handleMsg(p); err != nil {
			t.Fatalf("handleMsg %d: %v", i, err)
		}
	}
	if err := <-sendErr; err != nil {
		t.Fatalf("send requests: %v", err)
	}
	first, second := <-replies, <-replies
	if len(first) != 0 {
		t.Fatalf("oversized request answered with %d blocks, want an empty reply", len(first))
	}
	if len(second) != 1 || second[0].Momentum.Hash != knownHashes[0] {
		t.Fatalf("second request answered with %d blocks, want the one known block", len(second))
	}
	if chain.lookups != MaxBlocksRequest+1 {
		t.Fatalf("%d lookups across both requests, want %d", chain.lookups, MaxBlocksRequest+1)
	}
}

// The sender splits at the reply cap, so every request a node running this
// code sends stays within the receiver's lookup bound with room for misses.
func TestMaxBlocksRequest_CoversEveryHonestRequester(t *testing.T) {
	if MaxBlocksRequest != 2*downloader.MaxBlockFetch {
		t.Fatalf("MaxBlocksRequest %d is not twice the request size of %d", MaxBlocksRequest, downloader.MaxBlockFetch)
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

// A request naming more hashes than the bound is answered after exactly
// MaxBlocksRequest lookups; the hashes past the bound are never looked up
// and the peer is kept.
func TestHandleGetBlocks_OversizedRequestIsAnsweredAfterBoundedLookups(t *testing.T) {
	for _, count := range []int{MaxBlocksRequest + 1, 8 * MaxBlocksRequest} {
		chain := &lookupCountingChain{}

		result, err := serveGetBlocks(t, chain, unknownHashes(count))
		if err != nil {
			t.Fatalf("%d hashes: handleMsg: %v", count, err)
		}
		if !result.answered || len(result.blocks) != 0 {
			t.Fatalf("%d hashes: answered=%v with %d blocks, want an empty reply", count, result.answered, len(result.blocks))
		}
		if chain.lookups != MaxBlocksRequest {
			t.Fatalf("%d hashes: %d lookups, want exactly %d", count, chain.lookups, MaxBlocksRequest)
		}
	}
}

// Known and unknown hashes count the same: the bound is on lookups, not on
// hits. The blocks found within the bound are returned.
func TestHandleGetBlocks_MixedRequestPastLimitIsAnsweredWithTheHitsSeen(t *testing.T) {
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
	if err != nil {
		t.Fatalf("handleMsg: %v", err)
	}
	if !result.answered || len(result.blocks) != len(knownHashes) {
		t.Fatalf("answered=%v with %d blocks, want the %d known blocks", result.answered, len(result.blocks), len(knownHashes))
	}
	if chain.lookups != MaxBlocksRequest {
		t.Fatalf("%d lookups, want exactly %d", chain.lookups, MaxBlocksRequest)
	}
}

// Repeating one hash does not shrink the request: the bound counts entries as
// decoded, not distinct hashes, because every entry costs a store lookup
// budget slot whether or not the hash repeats. With dedup (issue #124), the
// repeat lookups are skipped but the budget is still consumed: a request that
// names the same unknown hash MaxBlocksRequest+1 times is answered after
// exactly MaxBlocksRequest decodes like any other oversized request, and the
// single distinct hash is looked up once.
func TestHandleGetBlocks_DuplicateHashesCountTowardLimit(t *testing.T) {
	chain := &lookupCountingChain{}

	hashes := make([]types.Hash, MaxBlocksRequest+1)
	for i := range hashes {
		hashes[i] = unknownHashes(1)[0]
	}

	result, err := serveGetBlocks(t, chain, hashes)
	if err != nil {
		t.Fatalf("handleMsg: %v", err)
	}
	if !result.answered || len(result.blocks) != 0 {
		t.Fatalf("answered=%v with %d blocks, want an empty reply", result.answered, len(result.blocks))
	}
	// Dedup skips the redundant lookups, but only one distinct hash exists.
	if chain.lookups != 1 {
		t.Fatalf("%d lookups, want 1 (dedup skips repeats of the same hash)", chain.lookups)
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

// A peer on an earlier release does not split its requests. Its oversized
// request is answered either way: with a full reply when the reply cap of
// MaxBlockFetch found blocks is reached within the first MaxBlocksRequest
// hashes, and otherwise with whatever was found in those hashes.
func TestHandleGetBlocks_UnsplitLegacyRequest(t *testing.T) {
	const legacyBatch = MaxBlocksRequest + 44

	t.Run("enough hits before the bound is answered", func(t *testing.T) {
		known, knownHashes := knownBlocks(downloader.MaxBlockFetch)
		chain := &lookupCountingChain{known: known}
		hashes := append(knownHashes, unknownHashes(legacyBatch-len(knownHashes))...)

		result, err := serveGetBlocks(t, chain, hashes)
		if err != nil {
			t.Fatalf("handleMsg: %v", err)
		}
		if !result.answered || len(result.blocks) != downloader.MaxBlockFetch {
			t.Fatalf("answered=%v with %d blocks, want %d blocks", result.answered, len(result.blocks), downloader.MaxBlockFetch)
		}
		if chain.lookups != downloader.MaxBlockFetch {
			t.Fatalf("%d lookups, want %d", chain.lookups, downloader.MaxBlockFetch)
		}
	})

	t.Run("last allowed hit completes the reply", func(t *testing.T) {
		// The MaxBlockFetch-th hit is the MaxBlocksRequest-th hash: the reply
		// cap ends the loop on the same iteration the request bound would
		// trip on the next one.
		known, knownHashes := knownBlocks(downloader.MaxBlockFetch)
		chain := &lookupCountingChain{known: known}
		hashes := append(unknownHashes(MaxBlocksRequest-downloader.MaxBlockFetch), knownHashes...)
		hashes = append(hashes, unknownHashes(legacyBatch-len(hashes))...)

		result, err := serveGetBlocks(t, chain, hashes)
		if err != nil {
			t.Fatalf("handleMsg: %v", err)
		}
		if !result.answered || len(result.blocks) != downloader.MaxBlockFetch {
			t.Fatalf("answered=%v with %d blocks, want %d blocks", result.answered, len(result.blocks), downloader.MaxBlockFetch)
		}
		if chain.lookups != MaxBlocksRequest {
			t.Fatalf("%d lookups, want exactly %d", chain.lookups, MaxBlocksRequest)
		}
	})

	t.Run("too few hits before the bound gets the hits seen", func(t *testing.T) {
		known, knownHashes := knownBlocks(downloader.MaxBlockFetch - 1)
		chain := &lookupCountingChain{known: known}
		hashes := append(knownHashes, unknownHashes(legacyBatch-len(knownHashes))...)

		result, err := serveGetBlocks(t, chain, hashes)
		if err != nil {
			t.Fatalf("handleMsg: %v", err)
		}
		if !result.answered || len(result.blocks) != downloader.MaxBlockFetch-1 {
			t.Fatalf("answered=%v with %d blocks, want %d", result.answered, len(result.blocks), downloader.MaxBlockFetch-1)
		}
		if chain.lookups != MaxBlocksRequest {
			t.Fatalf("%d lookups, want exactly %d", chain.lookups, MaxBlocksRequest)
		}
	})
}

// A payload that is not a list of 32-byte hashes is a decode error: the
// handler stops at the malformed element, looks nothing further up, and does
// not answer.
func TestHandleGetBlocks_MalformedPayloadIsRejected(t *testing.T) {
	valid := unknownHashes(1)[0]
	cases := []struct {
		name          string
		payload       interface{}
		lookupsBefore int  // hashes that decode before the malformed element
		decodeErr     bool // reported as ErrDecode (a bad outer list is a raw rlp error)
	}{
		{"not a list", "not-a-list", 0, false},
		{"short hash", [][]byte{valid[:], valid[:31]}, 1, true},
		{"long hash", [][]byte{valid[:], append(valid[:], 0)}, 1, true},
		{"nested list", []interface{}{valid[:], []interface{}{valid[:]}}, 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chain := &lookupCountingChain{}

			result, err := serveGetBlocksPayload(t, chain, tc.payload)
			if err == nil {
				t.Fatal("handleMsg accepted the payload")
			}
			if tc.decodeErr && !strings.Contains(err.Error(), errCode(ErrDecode).String()) {
				t.Fatalf("error %q does not report %q", err, errCode(ErrDecode).String())
			}
			if result.answered {
				t.Fatal("a rejected request was answered")
			}
			if chain.lookups != tc.lookupsBefore {
				t.Fatalf("%d lookups, want %d", chain.lookups, tc.lookupsBefore)
			}
		})
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
// its callers hand it, no single request on the wire names more than
// downloader.MaxBlockFetch hashes, the most the remote answers per message,
// so every chunk can be answered in full and stays within the lookup bound.
func TestRequestBlocks_SplitsBatchesLargerThanMaxBlockFetch(t *testing.T) {
	for _, count := range []int{0, 1, downloader.MaxBlockFetch, downloader.MaxBlockFetch + 1, 2 * downloader.MaxBlockFetch, 2*downloader.MaxBlockFetch + 5} {
		app, net := p2p.MsgPipe()
		p := &peer{rw: net, id: "test-peer"}
		hashes := unknownHashes(count)
		requests := readGetBlocksRequests(t, app)

		if err := p.RequestBlocks(hashes); err != nil {
			t.Fatalf("%d hashes: RequestBlocks: %v", count, err)
		}
		_ = app.Close()

		frames := <-requests
		// Every frame is full except the last, so the frame count is fixed by
		// the batch size. An empty batch is one empty frame, as before.
		wantFrames := (count + downloader.MaxBlockFetch - 1) / downloader.MaxBlockFetch
		if count == 0 {
			wantFrames = 1
		}
		if len(frames) != wantFrames {
			t.Fatalf("%d hashes: %d frames on the wire, want %d", count, len(frames), wantFrames)
		}
		var sent []types.Hash
		for i, request := range frames {
			if len(request) > downloader.MaxBlockFetch {
				t.Fatalf("%d hashes: request %d names %d hashes, want at most %d", count, i, len(request), downloader.MaxBlockFetch)
			}
			if i < len(frames)-1 && len(request) != downloader.MaxBlockFetch {
				t.Fatalf("%d hashes: request %d names %d hashes, want a full %d", count, i, len(request), downloader.MaxBlockFetch)
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

// failAfterWriter is a MsgReadWriter whose WriteMsg succeeds a fixed number of
// times and then fails. It counts every attempt, successful or not.
type failAfterWriter struct {
	succeed  int
	attempts int
	err      error
}

func (w *failAfterWriter) ReadMsg() (p2p.Msg, error) { panic("not used by RequestBlocks") }

func (w *failAfterWriter) WriteMsg(msg p2p.Msg) error {
	w.attempts++
	if w.attempts > w.succeed {
		return w.err
	}
	return nil
}

// A write failure on a later chunk is returned to the caller and no further
// chunks are attempted, so the caller sees the same error it would have seen
// from an unsplit send.
func TestRequestBlocks_StopsAtFirstFailedChunk(t *testing.T) {
	wantErr := errors.New("connection reset")
	rw := &failAfterWriter{succeed: 1, err: wantErr}
	p := &peer{rw: rw, id: "test-peer"}

	err := p.RequestBlocks(unknownHashes(3*downloader.MaxBlockFetch + 1))
	if !errors.Is(err, wantErr) {
		t.Fatalf("RequestBlocks returned %v, want %v", err, wantErr)
	}
	// One successful chunk, one failed chunk, and nothing after the failure.
	if rw.attempts != 2 {
		t.Fatalf("%d write attempts, want 2 (one success, one failure)", rw.attempts)
	}
}

// --- issue #124: hash dedup and reply-size cap ---

// makeTestMomentum returns a minimal DetailedMomentum with the given hash.
func makeTestMomentum(hash types.Hash) *nom.DetailedMomentum {
	return &nom.DetailedMomentum{
		Momentum: &nom.Momentum{
			Hash:   hash,
			Height: 100,
		},
		AccountBlocks: []*nom.AccountBlock{},
	}
}

// makeLargeMomentum returns a DetailedMomentum with a large Data field.
// The Data field is the simplest way to inflate the RLP-encoded size.
func makeLargeMomentum(hash types.Hash, dataSize int) *nom.DetailedMomentum {
	return &nom.DetailedMomentum{
		Momentum: &nom.Momentum{
			Hash:   hash,
			Height: 100,
			Data:   make([]byte, dataSize),
		},
		AccountBlocks: []*nom.AccountBlock{},
	}
}

// streamFromHashes creates an RLP stream from a list of hashes, as a
// GetBlocksMsg payload would carry.
func streamFromHashes(t *testing.T, hashes []types.Hash) *rlp.Stream {
	t.Helper()
	encoded, err := rlp.EncodeToBytes(hashes)
	if err != nil {
		t.Fatalf("encode hashes: %v", err)
	}
	stream := rlp.NewStream(bytes.NewReader(encoded), uint64(len(encoded)))
	if _, err := stream.List(); err != nil {
		t.Fatalf("open list: %v", err)
	}
	return stream
}

func TestGatherBlocks_DeduplicatesHashes(t *testing.T) {
	hash1 := types.HexToHashPanic("0100000000000000000000000000000000000000000000000000000000000000")
	hash2 := types.HexToHashPanic("0200000000000000000000000000000000000000000000000000000000000000")

	// Request: [hash1, hash1, hash1, hash2] — hash1 repeated 3 times.
	hashes := []types.Hash{hash1, hash1, hash1, hash2}
	stream := streamFromHashes(t, hashes)

	lookupCount := 0
	blocks, hashCount, err := gatherBlocksForReply(stream, func(h types.Hash) *nom.DetailedMomentum {
		lookupCount++
		return makeTestMomentum(h)
	})
	if err != nil {
		t.Fatalf("gatherBlocksForReply: %v", err)
	}

	// All 4 hashes counted (MaxBlocksRequest counts decoded hashes, not
	// distinct ones), but only 2 unique lookups.
	if hashCount != 4 {
		t.Errorf("hashCount = %d, want 4", hashCount)
	}
	if lookupCount != 2 {
		t.Errorf("lookupCount = %d, want 2 (dedup should skip repeats)", lookupCount)
	}
	if len(blocks) != 2 {
		t.Errorf("len(blocks) = %d, want 2", len(blocks))
	}
}

func TestGatherBlocks_ReplySizeCap(t *testing.T) {
	largeHash := types.HexToHashPanic("ff00000000000000000000000000000000000000000000000000000000000000")
	largeBlock := makeLargeMomentum(largeHash, 3*1024*1024) // 3 MB Data field

	// Verify this block exceeds the soft limit when encoded.
	encoded, err := rlp.EncodeToBytes(largeBlock)
	if err != nil {
		t.Fatalf("encode large block: %v", err)
	}
	t.Logf("large block encodes to %d bytes (soft limit %d)", len(encoded), softResponseLimit)

	// A fixture that no longer crosses the limit means this test stopped
	// testing anything. Skip would report that as a pass, so fail instead:
	// whoever changed the fixture or the limit has to look.
	if len(encoded) <= softResponseLimit {
		t.Fatalf("test block (%d bytes) does not exceed soft limit (%d), adjust test data",
			len(encoded), softResponseLimit)
	}

	// Trailing hash: if the loop did not stop after the large block, this
	// hash would be looked up and its block included. Its absence from the
	// reply pins the stop.
	trailingHash := types.HexToHashPanic("ee00000000000000000000000000000000000000000000000000000000000000")
	hashes := []types.Hash{largeHash, trailingHash}
	stream := streamFromHashes(t, hashes)

	lookupCount := 0
	blocks, hashCount, err := gatherBlocksForReply(stream, func(h types.Hash) *nom.DetailedMomentum {
		lookupCount++
		return largeBlock
	})
	if err != nil {
		t.Fatalf("gatherBlocksForReply: %v", err)
	}

	// The large block is included (always return at least one), but the
	// loop stops after it because the reply exceeds the soft limit. The
	// trailing hash is never looked up.
	if len(blocks) != 1 {
		t.Errorf("len(blocks) = %d, want 1 (block included but stops after)", len(blocks))
	}
	if hashCount != 1 {
		t.Errorf("hashCount = %d, want 1 (trailing hash never decoded)", hashCount)
	}
	if lookupCount != 1 {
		t.Errorf("lookupCount = %d, want 1 (trailing hash never looked up)", lookupCount)
	}
}

func TestGatherBlocks_CapAccumulates(t *testing.T) {
	smallHash := types.HexToHashPanic("0100000000000000000000000000000000000000000000000000000000000000")
	smallBlock := makeTestMomentum(smallHash)

	smallEncoded, err := rlp.EncodeToBytes(smallBlock)
	if err != nil {
		t.Fatalf("encode small block: %v", err)
	}

	mediumHash := types.HexToHashPanic("0200000000000000000000000000000000000000000000000000000000000000")
	mediumBlock := makeLargeMomentum(mediumHash, 2*1024*1024) // 2 MB Data field
	mediumEncoded, err := rlp.EncodeToBytes(mediumBlock)
	if err != nil {
		t.Fatalf("encode medium block: %v", err)
	}
	t.Logf("small=%d bytes, medium=%d bytes, soft limit=%d", len(smallEncoded), len(mediumEncoded), softResponseLimit)

	if len(smallEncoded)+len(mediumEncoded) <= softResponseLimit {
		t.Fatalf("combined size (%d bytes) does not exceed soft limit (%d), adjust test data",
			len(smallEncoded)+len(mediumEncoded), softResponseLimit)
	}

	// Trailing hash: if the loop did not stop after the medium block, this
	// hash would be looked up and its block included. Its absence pins the
	// stop.
	trailingHash := types.HexToHashPanic("0300000000000000000000000000000000000000000000000000000000000000")
	hashes := []types.Hash{smallHash, mediumHash, trailingHash}
	stream := streamFromHashes(t, hashes)

	lookupCount := 0
	blocks, hashCount, err := gatherBlocksForReply(stream, func(h types.Hash) *nom.DetailedMomentum {
		lookupCount++
		if h == smallHash {
			return smallBlock
		}
		return mediumBlock
	})
	if err != nil {
		t.Fatalf("gatherBlocksForReply: %v", err)
	}

	// Both blocks are included: small fits, medium is appended (pushing
	// the total over the limit), then the loop stops. The trailing hash is
	// never looked up.
	if len(blocks) != 2 {
		t.Errorf("len(blocks) = %d, want 2 (small + medium, stops after medium)", len(blocks))
	}
	if hashCount != 2 {
		t.Errorf("hashCount = %d, want 2 (trailing hash never decoded)", hashCount)
	}
	if lookupCount != 2 {
		t.Errorf("lookupCount = %d, want 2 (trailing hash never looked up)", lookupCount)
	}
}

func TestGatherBlocks_MaxBlockFetchLimit(t *testing.T) {
	// Request more unique hashes than MaxBlockFetch (128).
	var hashes []types.Hash
	for i := 0; i < 200; i++ {
		var h types.Hash
		h[0] = byte(i)
		h[1] = byte(i >> 8)
		hashes = append(hashes, h)
	}
	stream := streamFromHashes(t, hashes)

	blocks, hashCount, err := gatherBlocksForReply(stream, func(h types.Hash) *nom.DetailedMomentum {
		return makeTestMomentum(h)
	})
	if err != nil {
		t.Fatalf("gatherBlocksForReply: %v", err)
	}

	if len(blocks) != 128 {
		t.Errorf("len(blocks) = %d, want 128 (MaxBlockFetch)", len(blocks))
	}
	if hashCount != 128 {
		t.Errorf("hashCount = %d, want 128", hashCount)
	}
}

func TestGatherBlocks_MaxBlocksRequestBound(t *testing.T) {
	// Request more hashes than MaxBlocksRequest (256) to verify the
	// lookup bound from issue #84 is preserved after the rebase. Use
	// unknown hashes so the lookup bound (not the reply cap) is what
	// stops the loop.
	hashes := unknownHashes(MaxBlocksRequest + 10)
	stream := streamFromHashes(t, hashes)

	lookupCount := 0
	blocks, hashCount, err := gatherBlocksForReply(stream, func(h types.Hash) *nom.DetailedMomentum {
		lookupCount++
		return nil // all unknown
	})
	if err != nil {
		t.Fatalf("gatherBlocksForReply: %v", err)
	}

	if hashCount != MaxBlocksRequest {
		t.Errorf("hashCount = %d, want %d (MaxBlocksRequest)", hashCount, MaxBlocksRequest)
	}
	if lookupCount != MaxBlocksRequest {
		t.Errorf("lookupCount = %d, want %d (MaxBlocksRequest)", lookupCount, MaxBlocksRequest)
	}
	if len(blocks) != 0 {
		t.Errorf("len(blocks) = %d, want 0 (all unknown)", len(blocks))
	}
}

func TestGatherBlocks_EmptyRequest(t *testing.T) {
	stream := streamFromHashes(t, nil)

	blocks, hashCount, err := gatherBlocksForReply(stream, func(h types.Hash) *nom.DetailedMomentum {
		t.Error("GetBlock called for empty request")
		return nil
	})
	if err != nil {
		t.Fatalf("gatherBlocksForReply: %v", err)
	}
	if len(blocks) != 0 {
		t.Errorf("len(blocks) = %d, want 0", len(blocks))
	}
	if hashCount != 0 {
		t.Errorf("hashCount = %d, want 0", hashCount)
	}
}

func TestGatherBlocks_MissingBlocksSkipped(t *testing.T) {
	hash1 := types.HexToHashPanic("0100000000000000000000000000000000000000000000000000000000000000")
	hash2 := types.HexToHashPanic("0200000000000000000000000000000000000000000000000000000000000000")
	hash3 := types.HexToHashPanic("0300000000000000000000000000000000000000000000000000000000000000")

	hashes := []types.Hash{hash1, hash2, hash3}
	stream := streamFromHashes(t, hashes)

	blocks, hashCount, err := gatherBlocksForReply(stream, func(h types.Hash) *nom.DetailedMomentum {
		if h == hash2 {
			return nil // not found
		}
		return makeTestMomentum(h)
	})
	if err != nil {
		t.Fatalf("gatherBlocksForReply: %v", err)
	}

	if len(blocks) != 2 {
		t.Errorf("len(blocks) = %d, want 2 (hash2 not found)", len(blocks))
	}
	if hashCount != 3 {
		t.Errorf("hashCount = %d, want 3", hashCount)
	}
}
