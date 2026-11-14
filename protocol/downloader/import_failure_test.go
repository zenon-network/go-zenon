package downloader

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

// importFailureRun is a downloader over a scripted chain: insert decides
// what each batch handed to the chain insert returns, head is what the
// local head reads as, and the peers the downloader drops and reports to
// the import-failure hook are recorded.
type importFailureRun struct {
	d        *Downloader
	insert   func(batch []*nom.DetailedMomentum) (int, error)
	head     types.Hash
	dropped  []string
	reported []string
}

func newImportFailureRun() *importFailureRun {
	r := &importFailureRun{}
	r.d = New(
		func(types.Hash) bool { return false },
		func(types.Hash) *nom.DetailedMomentum { return nil },
		func() *nom.Momentum { return &nom.Momentum{Hash: r.head} },
		func(batch []*nom.DetailedMomentum) (int, error) { return r.insert(batch) },
		func(id string) { r.dropped = append(r.dropped, id) },
		func(id string, _ error) { r.reported = append(r.reported, id) },
	)
	return r
}

// failAt makes every insert fail at index with err.
func (r *importFailureRun) failAt(index int, err error) {
	r.insert = func([]*nom.DetailedMomentum) (int, error) { return index, err }
}

func heightHash(height uint64) types.Hash { return types.NewHash([]byte{byte(height)}) }

// file puts a delivered block at the given height into the cache, as
// Deliver leaves it. The chain the cache continues ends at height 0.
func (r *importFailureRun) file(height uint64, deliverer string, s *session) {
	hash := heightHash(height)
	r.d.queue.blockCache[height-1-r.d.queue.blockOffset] = &Block{
		RawBlock:   &nom.DetailedMomentum{Momentum: &nom.Momentum{Height: height, Hash: hash}},
		OriginPeer: deliverer,
		session:    s,
	}
	r.d.queue.blockPool[hash] = height
}

func (r *importFailureRun) expect(t *testing.T, dropped, reported []string) {
	t.Helper()
	if !reflect.DeepEqual(r.dropped, dropped) {
		t.Fatalf("dropped %v, want %v", r.dropped, dropped)
	}
	if !reflect.DeepEqual(r.reported, reported) {
		t.Fatalf("reported to the import-failure hook %v, want %v", r.reported, reported)
	}
}

var (
	errRejected     = fmt.Errorf("momentums to insert do not chain: %w", ErrBatchRejected)
	errMisplaced    = fmt.Errorf("can't rollback. Too far: %w", ErrBatchMisplaced)
	errInconclusive = fmt.Errorf("won't insert side-chain which is not longer: %w", ErrBatchInconclusive)
)

// An import failure is held against the peer it belongs to: the deliverer
// of the block at the reported index, unless the failure is a verdict on
// the batch, which no deliverer answers for. The peer whose hash list the
// batch was requested from answers for it when the hashes alone decide the
// verdict, and when the hashes and a local chain that has not moved since
// the session last knew it do. Nobody answers for it when the local chain
// has moved, or when the verdict is inconclusive. Every failure ends the
// session's queue.
func TestProcessDropsThePeerAnImportFailureBelongsTo(t *testing.T) {
	sessionHead := types.NewHash([]byte("local head when the session began"))
	cases := map[string]struct {
		err          error
		noSession    bool
		headMoved    bool
		wantDropped  []string
		wantReported []string
	}{
		"a verdict on the block at the index": {
			err:          errors.New("momentum failed verification"),
			wantDropped:  []string{"second"},
			wantReported: []string{"second"},
		},
		"a verdict the hashes decide": {
			err:         errRejected,
			wantDropped: []string{"origin"},
		},
		"a verdict the hashes decide, after the local chain moved": {
			err:         errRejected,
			headMoved:   true,
			wantDropped: []string{"origin"},
		},
		"a verdict the hashes decide, on a batch requested outside a session": {
			err:       errRejected,
			noSession: true,
		},
		"a verdict against a local chain that has not moved": {
			err:         errMisplaced,
			wantDropped: []string{"origin"},
		},
		"a verdict against a local chain that has moved": {
			err:       errMisplaced,
			headMoved: true,
		},
		"a verdict against the local chain, on a batch requested outside a session": {
			err:       errMisplaced,
			noSession: true,
		},
		"an inconclusive verdict": {
			err: errInconclusive,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newImportFailureRun()
			r.failAt(1, c.err)
			r.head = sessionHead
			var s *session
			if !c.noSession {
				s = newSession("origin", sessionHead)
			}
			r.file(1, "first", s)
			r.file(2, "second", s)
			if c.headMoved {
				r.head = types.NewHash([]byte("a momentum another import path added"))
			}

			r.d.process()

			r.expect(t, c.wantDropped, c.wantReported)
			if pending, cached := r.d.queue.Size(); pending != 0 || cached != 0 {
				t.Fatalf("a failed import must end the session's queue: pending=%d cached=%d", pending, cached)
			}
		})
	}
}

// A session moves the local chain itself, by importing its batches. That
// is not the chain moving under it: a later batch of the same session that
// does not fit is still the hash list's doing. The chain moving after that
// import, by another path, is.
func TestOwnImportsDoNotCountAsTheLocalChainMoving(t *testing.T) {
	for name, movedByAnother := range map[string]bool{"only the session's own import": false, "then another import path": true} {
		t.Run(name, func(t *testing.T) {
			r := newImportFailureRun()
			r.head = types.NewHash([]byte("local head when the session began"))
			s := newSession("origin", r.head)

			// The first batch is imported: the chain's head is now its last
			// momentum.
			r.insert = func(batch []*nom.DetailedMomentum) (int, error) {
				r.head = batch[len(batch)-1].Momentum.Hash
				return 0, nil
			}
			r.file(1, "first", s)
			r.file(2, "second", s)
			r.d.process()
			r.expect(t, nil, nil)
			if !s.knowsHead(heightHash(2)) {
				t.Fatal("the session does not know the head its own import left")
			}

			if movedByAnother {
				r.head = types.NewHash([]byte("a momentum another import path added"))
			}
			r.failAt(0, errMisplaced)
			r.file(3, "third", s)
			r.d.process()

			if movedByAnother {
				r.expect(t, nil, nil)
			} else {
				r.expect(t, []string{"origin"}, nil)
			}
		})
	}
}

// The import worker can still hold a batch when its session is over and
// another, following a different peer, has begun. A verdict on that batch
// belongs to the session that requested it, which the blocks carry, not to
// whichever session is running.
func TestBatchVerdictFollowsTheSessionThatRequestedTheBatch(t *testing.T) {
	r := newImportFailureRun()
	q := r.d.queue

	deliver := func(s *session, height uint64) types.Hash {
		t.Helper()
		q.Reset()
		q.SetSession(s)
		q.Prepare(height)
		hash := heightHash(height)
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

	first := newSession("first origin", r.head)
	deliver(first, 1)
	taken := q.TakeBlocks()
	if len(taken) != 1 || taken[0].OriginPeer != "supplier" || taken[0].session != first {
		t.Fatalf("delivered block not tagged with its deliverer and session: %+v", taken)
	}

	// The next session starts while the batch above is still being
	// imported, and files a block of its own.
	second := newSession("second origin", r.head)
	hash := deliver(second, 5)
	if block := q.GetBlock(hash); block == nil || block.session != second {
		t.Fatalf("block of the second session not tagged with its session: %+v", block)
	}
	if taken[0].session != first {
		t.Fatalf("a new session changed the tag of a batch already taken: %+v", taken[0])
	}

	r.d.dropForImportFailure(taken[0], errRejected)
	r.expect(t, []string{"first origin"}, nil)

	// Resetting the queue forgets the session with the cache.
	q.Reset()
	if q.session != nil {
		t.Fatalf("session of peer %q survived a reset", q.session.peer)
	}
}
