package protocol_test

import (
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/protocol/downloader"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// downloadRun is a real downloader importing through a real bridge, with
// peers whose replies the test scripts. It records which peers the
// downloader drops and which it reports with a SideChainRejected, the
// verdict the protocol manager quarantines for.
type downloadRun struct {
	t *testing.T
	d *downloader.Downloader

	// over is closed when the test ends; a scripted peer that withholds a
	// reply waits on it.
	over chan struct{}

	mu          sync.Mutex
	dropped     []string
	quarantined []string
}

func newDownloadRun(t *testing.T, bridge protocol.ChainBridge) *downloadRun {
	t.Helper()
	r := &downloadRun{t: t, over: make(chan struct{})}
	r.d = downloader.New(
		bridge.HasBlock,
		bridge.GetBlock,
		bridge.CurrentBlock,
		bridge.InsertChain,
		func(id string) {
			r.mu.Lock()
			r.dropped = append(r.dropped, id)
			r.mu.Unlock()
		},
		func(id string, err error) {
			var rejected *protocol.SideChainRejected
			if errors.As(err, &rejected) {
				r.mu.Lock()
				r.quarantined = append(r.quarantined, id)
				r.mu.Unlock()
			}
		},
	)
	t.Cleanup(func() {
		close(r.over)
		r.d.Terminate()
	})
	return r
}

// A scripted peer answers a request for count hashes from height from with
// what its hashFn returns, and a request for blocks by calling deliver from
// its blockFn, or by not calling it. Both run on a goroutine of their own
// and may wait. deliver returns once the downloader has accepted the
// delivery; the downloader takes deliveries in the order it accepted them.
type (
	hashFn  func(from uint64, count int) []types.Hash
	blockFn func(hashes []types.Hash, deliver func([]*nom.DetailedMomentum))
)

func (r *downloadRun) register(id string, head types.Hash, hashes hashFn, blocks blockFn) {
	r.t.Helper()
	common.FailIfErr(r.t, r.d.RegisterPeer(id, int(protocol.ProtocolVersions[0]), head, nil,
		func(from uint64, count int) error {
			_ = r.d.DeliverHashes(id, hashes(from, count), nil)
			return nil
		},
		func(hashes []types.Hash) error {
			blocks(hashes, func(reply []*nom.DetailedMomentum) { _ = r.d.DeliverBlocks(id, reply, nil) })
			return nil
		}))
}

// synchronise runs one session against origin and returns the peers the
// downloader dropped and the peers it reported for quarantine, once the
// session and every import it started are over.
func (r *downloadRun) synchronise(origin string, head types.Hash, height uint64) (dropped, quarantined []string) {
	r.t.Helper()
	done := make(chan struct{})
	go func() {
		r.d.Synchronise(origin, head, height)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		r.t.Fatal("the download session did not end")
	}
	// A session can end with delivered blocks still waiting for the import
	// worker. A batch is in the cache until the worker, whose flag is
	// already raised by then, takes it, so reading the cache first and the
	// flag second sees the batch in one of the two until it is imported
	// or refused.
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, cached, _, _ := r.d.Stats()
		if cached == 0 && !r.d.Processing() {
			break
		}
		if time.Now().After(deadline) {
			r.t.Fatal("the import worker did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.dropped...), append([]string(nil), r.quarantined...)
}

// advertise answers a hash request the way the protocol handler does for a
// node whose hash at height h is chain[h-1]: clipped to its head, newest
// first, nothing beyond the head.
func advertise(chain []types.Hash) hashFn {
	head := uint64(len(chain))
	return func(from uint64, count int) []types.Hash {
		if from > head {
			return nil
		}
		last := from + uint64(count) - 1
		if last > head {
			last = head
		}
		var reply []types.Hash
		for h := last; h >= from && h >= 1; h-- {
			reply = append(reply, chain[h-1])
		}
		return reply
	}
}

// serve answers a block request with the momentums of have that were asked
// for. A peer registered with it must have every hash it can be asked for.
func serve(have ...*nom.DetailedMomentum) blockFn {
	return func(hashes []types.Hash, deliver func([]*nom.DetailedMomentum)) {
		deliver(pick(hashes, have))
	}
}

func pick(hashes []types.Hash, have []*nom.DetailedMomentum) []*nom.DetailedMomentum {
	var reply []*nom.DetailedMomentum
	for _, hash := range hashes {
		for _, detailed := range have {
			if detailed.Momentum.Hash == hash {
				reply = append(reply, detailed)
			}
		}
	}
	return reply
}

// localHashes returns the hashes of z's chain, the hash at height h at
// index h-1.
func localHashes(t *testing.T, z mock.MockZenon) []types.Hash {
	t.Helper()
	frontier, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.FailIfErr(t, err)
	hashes := make([]types.Hash, 0, frontier.Height)
	for height := uint64(1); height <= frontier.Height; height++ {
		hashes = append(hashes, momentumAt(t, z.Chain(), height).Hash)
	}
	return hashes
}

// withBranch returns the first shared hashes of chain followed by the
// hashes of branch.
func withBranch(chain []types.Hash, shared int, branch []*nom.DetailedMomentum) []types.Hash {
	remote := append([]types.Hash(nil), chain[:shared]...)
	for _, detailed := range branch {
		remote = append(remote, detailed.Momentum.Hash)
	}
	return remote
}

func tip(chain []types.Hash) (types.Hash, uint64) {
	return chain[len(chain)-1], uint64(len(chain))
}

const (
	originPeer   = "origin"
	supplierPeer = "supplier"
)

// The harness itself: two honest peers serve a genuine extension, it is
// imported and nobody is dropped.
func TestDownload_GenuineExtensionFromTwoPeersDropsNobody(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	genuine := buildFork(t, z, 6, 8, 1)
	_, bridge := newSideChainBridge(z)
	run := newDownloadRun(t, bridge)

	remote := withBranch(localHashes(t, z), 6, genuine)
	head, height := tip(remote)
	run.register(originPeer, head, advertise(remote), serve(genuine...))
	run.register(supplierPeer, head, advertise(remote), serve(genuine...))

	dropped, quarantined := run.synchronise(originPeer, head, height)
	common.Expect(t, len(dropped), 0)
	common.Expect(t, len(quarantined), 0)
	expectFrontierAt(t, z.Chain(), genuine[1].Momentum)
}

// The session origin lists a hash of its own at height 7 followed by the
// genuine hash at height 8, and makes both momentums arrive in one batch,
// the genuine one delivered by an honest peer. The batch does not chain.
// The peer that delivered the genuine momentum did nothing wrong and must
// stay; the origin must go, whether what it served at 7 is bytes that do
// not hash to the listed hash or a momentum of its own making that does.
//
// The origin needs nothing but the timing of its own replies for this. It
// lists the hash at 7 first and the hash at 8 only once it has been asked
// for the momentum at 7, so that the request for 8 goes to another peer,
// and it answers the request for 7 only after that peer has delivered. The
// honest peer does not have the origin's hash at 7; should it be asked for
// it first, it has nothing to deliver and the request moves on to the
// origin.
func TestDownload_BatchThatDoesNotChainIsNotBlamedOnTheHonestSupplier(t *testing.T) {
	cases := map[string]func(frontier *nom.Momentum) *nom.Momentum{
		"bytes that do not hash to the listed hash": func(frontier *nom.Momentum) *nom.Momentum {
			garbage := unsignedMomentum(frontier, 7)
			garbage.Hash = types.NewHash([]byte("a hash no momentum has"))
			return garbage
		},
		"a momentum of the origin's own making": func(frontier *nom.Momentum) *nom.Momentum {
			return attackerMomentum(frontier, 7)
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			z := mock.NewMockZenon(t)
			defer z.StopPanic()
			z.InsertMomentumsTo(6)
			genuine := buildFork(t, z, 6, 8, 1)
			garbage := detailedOf(build(momentumAt(t, z.Chain(), 6)))[0]
			_, bridge := newSideChainBridge(z)
			before := snapshotChain(t, z.Chain())
			run := newDownloadRun(t, bridge)

			local := localHashes(t, z)
			head, height := genuine[1].Momentum.Hash, uint64(8)
			askedForGarbage := make(chan struct{})
			genuineDelivered := make(chan struct{})
			var asked, delivered sync.Once

			run.register(originPeer, head,
				func(from uint64, count int) []types.Hash {
					switch {
					case from <= 6:
						return advertise(local)(from, count)
					case from == 7:
						return []types.Hash{garbage.Momentum.Hash}
					case from == 8:
						select {
						case <-askedForGarbage:
						case <-run.over:
						}
						return []types.Hash{genuine[1].Momentum.Hash}
					}
					return nil
				},
				func(hashes []types.Hash, deliver func([]*nom.DetailedMomentum)) {
					asked.Do(func() { close(askedForGarbage) })
					select {
					case <-genuineDelivered:
					case <-run.over:
					}
					deliver(pick(hashes, []*nom.DetailedMomentum{garbage}))
				})
			run.register(supplierPeer, head, advertise(withBranch(local, 6, genuine)),
				func(hashes []types.Hash, deliver func([]*nom.DetailedMomentum)) {
					reply := pick(hashes, genuine)
					deliver(reply)
					if len(reply) != 0 {
						delivered.Do(func() { close(genuineDelivered) })
					}
				})

			dropped, quarantined := run.synchronise(originPeer, head, height)
			for _, id := range dropped {
				if id == supplierPeer {
					t.Fatalf("the peer that delivered the genuine momentum was dropped (dropped: %v)", dropped)
				}
			}
			if len(dropped) != 1 || dropped[0] != originPeer {
				t.Fatalf("expected exactly the origin to be dropped, got %v", dropped)
			}
			common.Expect(t, len(quarantined), 0)
			expectChainEquals(t, z.Chain(), before)
		})
	}
}

// The origin is honest and lists the genuine chain; another peer answers
// the hash it is asked for with bytes that carry that hash and a different
// previous hash. Whichever height it is asked for and whichever delivery
// arrives first, the batch fails on its shape, and the failure belongs to
// the peer that served the altered bytes, not to the origin whose list was
// sound.
func TestDownload_AlteredBytesUnderAListedHashAreBlamedOnTheirDeliverer(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	genuine := buildFork(t, z, 6, 8, 1)
	_, bridge := newSideChainBridge(z)
	run := newDownloadRun(t, bridge)

	changed := make([]*nom.DetailedMomentum, len(genuine))
	for i, detailed := range genuine {
		changed[i] = altered(detailed, otherPrevious)
	}

	remote := withBranch(localHashes(t, z), 6, genuine)
	head, height := tip(remote)
	run.register(originPeer, head, advertise(remote), serve(genuine...))
	run.register(supplierPeer, head, advertise(remote), serve(changed...))

	dropped, quarantined := run.synchronise(originPeer, head, height)
	if len(dropped) != 1 || dropped[0] != supplierPeer {
		t.Fatalf("expected exactly the peer that served altered bytes to be dropped, got %v", dropped)
	}
	common.Expect(t, len(quarantined), 0)
}

// The origin is honest and on a longer branch that forks below the local
// frontier. It is slow to list the branch: the hash of the first momentum
// arrives, that momentum is downloaded and handed to the import worker,
// and the rest of the list is still outstanding. A batch of one is not
// longer than the local chain and is refused. Every peer did what it was
// asked and the hash list is sound: the batch was simply not all of it.
// Nobody may be dropped for that, neither the peer that delivered the
// first momentum nor the origin.
func TestDownload_PrefixThatIsNotYetLongerDropsNobody(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(6)
	branch := buildFork(t, z, 5, 7, 2)
	_, bridge := newSideChainBridge(z)
	before := snapshotChain(t, z.Chain())
	run := newDownloadRun(t, bridge)

	remote := withBranch(localHashes(t, z), 5, branch)
	head, height := tip(remote)
	run.register(originPeer, head,
		func(from uint64, count int) []types.Hash {
			if from == 7 {
				<-run.over
				return nil
			}
			return advertise(remote[:6])(from, count)
		},
		serve(branch...))
	run.register(supplierPeer, head, advertise(remote), serve(branch...))

	dropped, quarantined := run.synchronise(originPeer, head, height)
	common.Expect(t, len(dropped), 0)
	common.Expect(t, len(quarantined), 0)
	expectChainEquals(t, z.Chain(), before)

	// The branch itself is sound: handed over whole, it replaces the local
	// one.
	_, err := bridge.InsertChain(branch)
	common.FailIfErr(t, err)
	expectFrontierAt(t, z.Chain(), branch[1].Momentum)
}

// The origin is on a branch that forks further below the local frontier
// than the node rolls back, and serves no momentums itself: the one request
// it is sent goes unanswered. Another peer delivers the rest, the first
// momentum of the branch among them. The batch is refused as too deep,
// which is decided by where the origin's hash list attaches to a local
// chain that did not move during the session: the origin goes, the peer
// that delivered the momentums it was asked for stays.
func TestDownload_BranchTooDeepIsBlamedOnTheOrigin(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(40)
	branch := buildFork(t, z, 5, 41, 2)
	_, bridge := newSideChainBridge(z)
	before := snapshotChain(t, z.Chain())
	run := newDownloadRun(t, bridge)

	remote := withBranch(localHashes(t, z), 5, branch)
	head, height := tip(remote)
	run.register(originPeer, head, advertise(remote),
		func([]types.Hash, func([]*nom.DetailedMomentum)) { <-run.over })
	run.register(supplierPeer, head, advertise(remote), serve(branch...))

	dropped, quarantined := run.synchronise(originPeer, head, height)
	if len(dropped) != 1 || dropped[0] != originPeer {
		t.Fatalf("expected exactly the origin to be dropped, got %v", dropped)
	}
	common.Expect(t, len(quarantined), 0)
	expectChainEquals(t, z.Chain(), before)
}

// The origin's branch forks exactly as deep as the node still rolls back
// when the session begins. While the branch is being downloaded the local
// chain grows by a momentum of its own, by another import path, and the
// branch is one too deep by the time it is imported. The origin's list
// fitted the chain it was requested against, and both peers served what
// they were asked for: nobody may be dropped.
func TestDownload_BranchTooDeepOnlyBecauseTheLocalChainGrewDropsNobody(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(40)
	branch := buildFork(t, z, 10, 42, 2)
	_, bridge := newSideChainBridge(z)
	run := newDownloadRun(t, bridge)

	remote := withBranch(localHashes(t, z), 10, branch)
	head, height := tip(remote)
	// The local chain grows once the session is under way, before any
	// momentum of the branch has been delivered, so before any import.
	var grow sync.Once
	growThenServe := func(hashes []types.Hash, deliver func([]*nom.DetailedMomentum)) {
		grow.Do(func() { z.InsertMomentumsTo(41) })
		deliver(pick(hashes, branch))
	}
	run.register(originPeer, head, advertise(remote), growThenServe)
	run.register(supplierPeer, head, advertise(remote), growThenServe)

	dropped, quarantined := run.synchronise(originPeer, head, height)
	common.Expect(t, len(dropped), 0)
	common.Expect(t, len(quarantined), 0)
	if momentumAt(t, z.Chain(), 41).Hash == branch[30].Momentum.Hash {
		t.Fatal("the branch was imported; the fixture did not make it too deep")
	}
}
