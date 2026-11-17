package downloader

import (
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

// Processing covers the handoff inside process: after a batch is taken out
// of the queue and before it is published as importQueue, Stats reports no
// cached or importing blocks and the session may already be over, yet the
// worker owns blocks that are about to change the frontier. Before the
// worker starts the same batch is visible to Stats as cached, and the
// worker raises its flag before it takes anything, so a caller that reads
// Stats and then Processing sees the batch in one of the two at every
// moment.
func TestProcessingCoversTheImportHandoff(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	imported := make(chan struct{}, 1)
	d := New(
		func(types.Hash) bool { return false },
		func(types.Hash) *nom.DetailedMomentum { return nil },
		func() *nom.Momentum { return &nom.Momentum{} },
		func([]*nom.DetailedMomentum) (int, error) { imported <- struct{}{}; return 0, nil },
		func(string) {},
		nil,
	)
	d.processHook = func([]*Block) {
		close(entered)
		<-release
	}
	// One delivered block at the head of the cache, as a finished session
	// leaves it; no session is running.
	block := &nom.Momentum{Height: 1, Hash: types.NewHash([]byte{1})}
	d.queue.blockCache[0] = &Block{RawBlock: &nom.DetailedMomentum{Momentum: block}, OriginPeer: "p"}
	d.queue.blockPool[block.Hash] = block.Height

	if d.Processing() {
		t.Fatal("processing before the worker started")
	}
	if _, cached, _, _ := d.Stats(); cached != 1 {
		t.Fatalf("Stats must see the delivered batch before the worker takes it, cached=%d", cached)
	}
	done := make(chan struct{})
	go func() { d.process(); close(done) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not take the batch")
	}
	if d.Synchronising() {
		t.Fatal("no session is running in this test")
	}
	if _, cached, importing, _ := d.Stats(); cached != 0 || importing != 0 {
		t.Fatalf("Stats sees the batch during the handoff: cached=%d importing=%d", cached, importing)
	}
	if !d.Processing() {
		t.Fatal("Processing must report the worker owning the batch during the handoff")
	}
	close(release)
	select {
	case <-imported:
	case <-time.After(5 * time.Second):
		t.Fatal("batch was not imported")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not exit")
	}
	if d.Processing() {
		t.Fatal("Processing still set after the worker exited")
	}
}
