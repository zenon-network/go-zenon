package fetcher

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

func newTestMomentum(height uint64, prevHash types.Hash) *nom.Momentum {
	m := &nom.Momentum{
		Version:         1,
		ChainIdentifier: 69,
		PreviousHash:    prevHash,
		Height:          height,
		TimestampUnix:   uint64(time.Now().Unix()),
	}
	m.Hash = m.ComputeHash()
	return m
}

type testHarness struct {
	f            *Fetcher
	droppedPeers chan string
	broadcasts   chan *nom.DetailedMomentum
	quit         chan struct{}
}

func newTestHarness(getBlock blockRetrievalFn, verifyBlock blockVerifierFn, insertChain chainInsertFn) *testHarness {
	h := &testHarness{
		droppedPeers: make(chan string, 10),
		broadcasts:   make(chan *nom.DetailedMomentum, 10),
		quit:         make(chan struct{}),
	}

	h.f = New(
		getBlock,
		verifyBlock,
		func(block *nom.DetailedMomentum, propagate bool) {
			if propagate {
				h.broadcasts <- block
			}
		},
		func() uint64 { return 1 },
		insertChain,
		func(id string) { h.droppedPeers <- id },
	)

	go func() {
		h.f.loop()
		close(h.quit)
	}()

	return h
}

func (h *testHarness) stop() {
	h.f.Stop()
	<-h.quit
}

func TestInsert_VerifyBlockCalled(t *testing.T) {
	parent := newTestMomentum(1, types.Hash{})
	block := newTestMomentum(2, parent.Hash)

	var verifyCalled, insertCalled bool
	var mu sync.Mutex

	h := newTestHarness(
		func(hash types.Hash) *nom.DetailedMomentum {
			if hash == parent.Hash {
				return &nom.DetailedMomentum{Momentum: parent}
			}
			return nil
		},
		func(detailed *nom.DetailedMomentum) error {
			mu.Lock()
			verifyCalled = true
			mu.Unlock()
			return nil
		},
		func(momentums []*nom.DetailedMomentum) (int, error) {
			mu.Lock()
			insertCalled = true
			mu.Unlock()
			return 1, nil
		},
	)
	defer h.stop()

	h.f.insert("test-peer", &nom.DetailedMomentum{Momentum: block})

	// Wait for broadcast (indicates successful full flow)
	select {
	case <-h.broadcasts:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for broadcast")
	}

	mu.Lock()
	defer mu.Unlock()
	if !verifyCalled {
		t.Error("verifyBlock was not called")
	}
	if !insertCalled {
		t.Error("insertChain was not called")
	}
}

func TestInsert_VerifyBlockFails_DropsPeer(t *testing.T) {
	parent := newTestMomentum(1, types.Hash{})
	block := newTestMomentum(2, parent.Hash)

	verifyErr := errors.New("invalid momentum: bad chain identifier")
	var insertCalled bool

	h := newTestHarness(
		func(hash types.Hash) *nom.DetailedMomentum {
			if hash == parent.Hash {
				return &nom.DetailedMomentum{Momentum: parent}
			}
			return nil
		},
		func(detailed *nom.DetailedMomentum) error {
			return verifyErr
		},
		func(momentums []*nom.DetailedMomentum) (int, error) {
			insertCalled = true
			return 0, nil
		},
	)
	defer h.stop()

	h.f.insert("bad-peer", &nom.DetailedMomentum{Momentum: block})

	// Should drop the peer
	select {
	case peer := <-h.droppedPeers:
		if peer != "bad-peer" {
			t.Errorf("expected peer 'bad-peer' to be dropped, got %q", peer)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for peer drop")
	}

	// Should NOT broadcast
	select {
	case <-h.broadcasts:
		t.Error("broadcastBlock should not be called when verification fails")
	case <-time.After(500 * time.Millisecond):
		// expected
	}

	if insertCalled {
		t.Error("insertChain should not be called when verification fails")
	}
}

func TestInsert_InsertChainFails_NoBroadcast(t *testing.T) {
	parent := newTestMomentum(1, types.Hash{})
	block := newTestMomentum(2, parent.Hash)

	insertErr := errors.New("VM execution failed")
	var verifyCalled bool

	h := newTestHarness(
		func(hash types.Hash) *nom.DetailedMomentum {
			if hash == parent.Hash {
				return &nom.DetailedMomentum{Momentum: parent}
			}
			return nil
		},
		func(detailed *nom.DetailedMomentum) error {
			verifyCalled = true
			return nil
		},
		func(momentums []*nom.DetailedMomentum) (int, error) {
			return 0, insertErr
		},
	)
	defer h.stop()

	h.f.insert("test-peer", &nom.DetailedMomentum{Momentum: block})

	// Wait for the done signal (insert completes even on failure)
	select {
	case <-h.droppedPeers:
		t.Error("peer should not be dropped on insertion failure")
	case <-time.After(1 * time.Second):
		// expected — insert() sends to done channel, not droppedPeers
	}

	// Should NOT broadcast
	select {
	case <-h.broadcasts:
		t.Error("broadcastBlock should not be called when insertion fails")
	case <-time.After(500 * time.Millisecond):
		// expected
	}

	if !verifyCalled {
		t.Error("verifyBlock should be called even when insertion fails")
	}
}

func TestInsert_Success_BroadcastsAfterInsertion(t *testing.T) {
	parent := newTestMomentum(1, types.Hash{})
	block := newTestMomentum(2, parent.Hash)

	var verifyTime, insertTime time.Time
	var mu sync.Mutex

	h := newTestHarness(
		func(hash types.Hash) *nom.DetailedMomentum {
			if hash == parent.Hash {
				return &nom.DetailedMomentum{Momentum: parent}
			}
			return nil
		},
		func(detailed *nom.DetailedMomentum) error {
			mu.Lock()
			verifyTime = time.Now()
			mu.Unlock()
			return nil
		},
		func(momentums []*nom.DetailedMomentum) (int, error) {
			mu.Lock()
			insertTime = time.Now()
			mu.Unlock()
			return 1, nil
		},
	)
	defer h.stop()

	h.f.insert("test-peer", &nom.DetailedMomentum{Momentum: block})

	select {
	case received := <-h.broadcasts:
		if received.Momentum.Hash != block.Hash {
			t.Errorf("broadcast received wrong block: got %v, want %v", received.Momentum.Hash, block.Hash)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for broadcast")
	}

	mu.Lock()
	defer mu.Unlock()
	if verifyTime.IsZero() {
		t.Error("verifyBlock was not called")
	}
	if insertTime.IsZero() {
		t.Error("insertChain was not called")
	}
	// Verify must happen before insert
	if !verifyTime.Before(insertTime) && !verifyTime.Equal(insertTime) {
		t.Error("verifyBlock should be called before insertChain")
	}
}

func TestInsert_ParentUnknown_Aborts(t *testing.T) {
	block := newTestMomentum(2, types.Hash{0xff})

	var verifyCalled, insertCalled bool

	h := newTestHarness(
		func(hash types.Hash) *nom.DetailedMomentum {
			return nil // parent not found
		},
		func(detailed *nom.DetailedMomentum) error {
			verifyCalled = true
			return nil
		},
		func(momentums []*nom.DetailedMomentum) (int, error) {
			insertCalled = true
			return 1, nil
		},
	)
	defer h.stop()

	h.f.insert("test-peer", &nom.DetailedMomentum{Momentum: block})

	// Should NOT broadcast
	select {
	case <-h.broadcasts:
		t.Error("broadcastBlock should not be called when parent is unknown")
	case <-time.After(500 * time.Millisecond):
		// expected
	}

	// Should NOT drop peer (parent unknown is not a misbehavior)
	select {
	case <-h.droppedPeers:
		t.Error("peer should not be dropped when parent is unknown")
	case <-time.After(500 * time.Millisecond):
		// expected
	}

	if verifyCalled {
		t.Error("verifyBlock should not be called when parent is unknown")
	}
	if insertCalled {
		t.Error("insertChain should not be called when parent is unknown")
	}
}
