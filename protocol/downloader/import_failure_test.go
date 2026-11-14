package downloader

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

type importFailureRun struct {
	d        *Downloader
	dropped  []string
	reported []string
}

// newImportFailureRun returns a downloader whose chain insert fails every
// batch at index with err, and which records the peers it drops and the
// peers it reports to the import-failure hook.
func newImportFailureRun(index int, err error) *importFailureRun {
	r := &importFailureRun{}
	r.d = New(
		func(types.Hash) bool { return false },
		func(types.Hash) *nom.DetailedMomentum { return nil },
		func() *nom.Momentum { return &nom.Momentum{} },
		func([]*nom.DetailedMomentum) (int, error) { return index, err },
		func(id string) { r.dropped = append(r.dropped, id) },
		func(id string, _ error) { r.reported = append(r.reported, id) },
	)
	return r
}

// file puts a delivered block at the given height into the cache, as
// Deliver leaves it.
func (r *importFailureRun) file(height uint64, deliverer, sessionPeer string) {
	hash := types.NewHash([]byte{byte(height)})
	r.d.queue.blockCache[height-1] = &Block{
		RawBlock:    &nom.DetailedMomentum{Momentum: &nom.Momentum{Height: height, Hash: hash}},
		OriginPeer:  deliverer,
		SessionPeer: sessionPeer,
	}
	r.d.queue.blockPool[hash] = height
}

// An import failure is held against the peer it belongs to: the deliverer
// of the block at the reported index, unless the failure is a verdict on
// the batch, which no deliverer answers for. The peer whose hash list the
// batch was requested from answers for it when the hashes decide the
// verdict, and nobody does when it is inconclusive. Every failure ends the
// session's queue.
func TestProcessDropsThePeerAnImportFailureBelongsTo(t *testing.T) {
	cases := map[string]struct {
		err          error
		sessionPeer  string
		wantDropped  []string
		wantReported []string
	}{
		"a verdict on the block at the index": {
			err:          errors.New("momentum failed verification"),
			sessionPeer:  "origin",
			wantDropped:  []string{"second"},
			wantReported: []string{"second"},
		},
		"a verdict on the batch": {
			err:         fmt.Errorf("momentums to insert do not chain: %w", ErrBatchRejected),
			sessionPeer: "origin",
			wantDropped: []string{"origin"},
		},
		"a verdict on a batch that was not requested inside a session": {
			err: fmt.Errorf("momentums to insert do not chain: %w", ErrBatchRejected),
		},
		"an inconclusive verdict on the batch": {
			err:         fmt.Errorf("won't insert side-chain which is not longer: %w", ErrBatchInconclusive),
			sessionPeer: "origin",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newImportFailureRun(1, c.err)
			r.file(1, "first", c.sessionPeer)
			r.file(2, "second", c.sessionPeer)

			r.d.process()

			if !reflect.DeepEqual(r.dropped, c.wantDropped) {
				t.Fatalf("dropped %v, want %v", r.dropped, c.wantDropped)
			}
			if !reflect.DeepEqual(r.reported, c.wantReported) {
				t.Fatalf("reported to the import-failure hook %v, want %v", r.reported, c.wantReported)
			}
			if pending, cached := r.d.queue.Size(); pending != 0 || cached != 0 {
				t.Fatalf("a failed import must end the session's queue: pending=%d cached=%d", pending, cached)
			}
		})
	}
}

// The import worker can still hold a batch when its session is over and
// another, following a different peer, has begun. A verdict on that batch
// belongs to the peer of the session that requested it, which the blocks
// carry, not to the peer of whichever session is running.
func TestBatchVerdictFollowsTheSessionThatRequestedTheBatch(t *testing.T) {
	r := newImportFailureRun(0, nil)
	q := r.d.queue

	deliver := func(sessionPeer string, height uint64) types.Hash {
		t.Helper()
		q.Reset()
		q.SetSessionPeer(sessionPeer)
		q.Prepare(height)
		hash := types.NewHash([]byte{byte(height)})
		q.Insert([]types.Hash{hash}, true)
		supplier := newPeer("supplier", eth61, types.Hash{}, nil, nil, nil)
		if q.Reserve(supplier, 1) == nil {
			t.Fatal("nothing reserved")
		}
		if err := q.Deliver(supplier.id, []*nom.DetailedMomentum{{Momentum: &nom.Momentum{Height: height, Hash: hash}}}); err != nil {
			t.Fatalf("deliver: %v", err)
		}
		return hash
	}

	deliver("first origin", 1)
	taken := q.TakeBlocks()
	if len(taken) != 1 || taken[0].OriginPeer != "supplier" || taken[0].SessionPeer != "first origin" {
		t.Fatalf("delivered block not tagged with its deliverer and session peer: %+v", taken)
	}

	// The next session starts while the batch above is still being
	// imported, and files a block of its own.
	second := deliver("second origin", 5)
	if block := q.GetBlock(second); block == nil || block.SessionPeer != "second origin" {
		t.Fatalf("block of the second session not tagged with its session peer: %+v", block)
	}
	if taken[0].SessionPeer != "first origin" {
		t.Fatalf("a new session changed the tag of a batch already taken: %+v", taken[0])
	}

	r.d.dropForImportFailure(taken[0], fmt.Errorf("refused: %w", ErrBatchRejected))
	if !reflect.DeepEqual(r.dropped, []string{"first origin"}) {
		t.Fatalf("dropped %v, want the peer of the session that requested the batch", r.dropped)
	}

	// Resetting the queue forgets the session peer with the cache.
	q.Reset()
	if q.sessionPeer != "" {
		t.Fatalf("session peer %q survived a reset", q.sessionPeer)
	}
}
