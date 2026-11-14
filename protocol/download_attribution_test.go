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

	mu          sync.Mutex
	dropped     []string
	quarantined []string
}

func newDownloadRun(t *testing.T, bridge protocol.ChainBridge) *downloadRun {
	t.Helper()
	r := &downloadRun{t: t}
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
	t.Cleanup(r.d.Terminate)
	return r
}

// register adds a peer that advertises the chain whose hash at height h is
// chain[h-1], answering a hash request the way the protocol handler does
// (clipped to its head, newest first), and that answers a block request
// with serve. serve runs on its own goroutine and hands its reply to
// deliver, which returns once the downloader has accepted the delivery; a
// reply of no blocks tells the downloader the peer has none of the hashes.
func (r *downloadRun) register(id string, chain []types.Hash, serve func(hashes []types.Hash, deliver func([]*nom.DetailedMomentum))) {
	r.t.Helper()
	head := uint64(len(chain))
	hashes := func(from uint64, count int) error {
		var reply []types.Hash
		if from <= head {
			last := from + uint64(count) - 1
			if last > head {
				last = head
			}
			for h := last; h >= from && h >= 1; h-- {
				reply = append(reply, chain[h-1])
			}
		}
		_ = r.d.DeliverHashes(id, reply, nil)
		return nil
	}
	blocks := func(hashes []types.Hash) error {
		serve(hashes, func(reply []*nom.DetailedMomentum) { _ = r.d.DeliverBlocks(id, reply, nil) })
		return nil
	}
	common.FailIfErr(r.t, r.d.RegisterPeer(id, int(protocol.ProtocolVersions[0]), chain[head-1], nil, hashes, blocks))
}

// synchronise runs one session against origin and returns the peers the
// downloader dropped and the peers it reported for quarantine, once the
// session and every import it started are over.
func (r *downloadRun) synchronise(origin string, chain []types.Hash) (dropped, quarantined []string) {
	r.t.Helper()
	done := make(chan struct{})
	go func() {
		r.d.Synchronise(origin, chain[len(chain)-1], uint64(len(chain)))
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

// serveFrom answers a block request with the momentums of have that were
// asked for, or with no blocks when it has none of them.
func serveFrom(have ...*nom.DetailedMomentum) func([]types.Hash, func([]*nom.DetailedMomentum)) {
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

	remote := append(localHashes(t, z), genuine[0].Momentum.Hash, genuine[1].Momentum.Hash)
	run.register(originPeer, remote, serveFrom(genuine[0]))
	run.register(supplierPeer, remote, serveFrom(genuine[1]))

	dropped, quarantined := run.synchronise(originPeer, remote)
	common.Expect(t, len(dropped), 0)
	common.Expect(t, len(quarantined), 0)
	expectFrontierAt(t, z.Chain(), genuine[1].Momentum)
}

// The session origin lists a hash of its own at height 7 followed by the
// genuine hash at height 8, serves what it listed at 7 only after an honest
// peer has delivered the genuine 8, and so makes both arrive in one batch.
// The batch does not chain. The peer that delivered the genuine momentum
// did nothing wrong and must stay; the origin must go, whether what it
// served at 7 is bytes that do not hash to the listed hash or a momentum of
// its own making that does.
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

			listed := append(localHashes(t, z), garbage.Momentum.Hash, genuine[1].Momentum.Hash)
			genuineDelivered := make(chan struct{})
			run.register(originPeer, listed, func(hashes []types.Hash, deliver func([]*nom.DetailedMomentum)) {
				reply := pick(hashes, []*nom.DetailedMomentum{garbage})
				if len(reply) != 0 {
					select {
					case <-genuineDelivered:
					case <-time.After(20 * time.Second):
					}
				}
				deliver(reply)
			})
			var once sync.Once
			run.register(supplierPeer, listed, func(hashes []types.Hash, deliver func([]*nom.DetailedMomentum)) {
				reply := pick(hashes, []*nom.DetailedMomentum{genuine[1]})
				deliver(reply)
				if len(reply) != 0 {
					once.Do(func() { close(genuineDelivered) })
				}
			})

			dropped, quarantined := run.synchronise(originPeer, listed)
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

	altered := make([]*nom.DetailedMomentum, len(genuine))
	for i, detailed := range genuine {
		momentum := *detailed.Momentum
		momentum.PreviousHash = types.NewHash([]byte("not the previous momentum"))
		altered[i] = &nom.DetailedMomentum{Momentum: &momentum, AccountBlocks: detailed.AccountBlocks}
	}

	remote := append(localHashes(t, z), genuine[0].Momentum.Hash, genuine[1].Momentum.Hash)
	run.register(originPeer, remote, serveFrom(genuine...))
	run.register(supplierPeer, remote, serveFrom(altered...))

	dropped, quarantined := run.synchronise(originPeer, remote)
	if len(dropped) != 1 || dropped[0] != supplierPeer {
		t.Fatalf("expected exactly the peer that served altered bytes to be dropped, got %v", dropped)
	}
	common.Expect(t, len(quarantined), 0)
}

// The origin is honest and on a longer branch that forks below the local
// frontier. The first momentum of that branch arrives while the second is
// still outstanding, so the import worker is handed a batch of one, which
// is not longer than the local chain and is refused. Every peer did what it
// was asked and the hash list is sound: the batch was simply not all of it.
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

	remote := append(localHashes(t, z)[:5], branch[0].Momentum.Hash, branch[1].Momentum.Hash)
	sessionOver := make(chan struct{})
	defer close(sessionOver)
	run.register(originPeer, remote, serveFrom(branch[0]))
	run.register(supplierPeer, remote, func(hashes []types.Hash, deliver func([]*nom.DetailedMomentum)) {
		reply := pick(hashes, []*nom.DetailedMomentum{branch[1]})
		if len(reply) != 0 {
			// Slow, or withholding: the second momentum never arrives
			// while the session lasts.
			<-sessionOver
			return
		}
		deliver(reply)
	})

	dropped, quarantined := run.synchronise(originPeer, remote)
	common.Expect(t, len(dropped), 0)
	common.Expect(t, len(quarantined), 0)
	expectChainEquals(t, z.Chain(), before)

	// The branch itself is sound: handed over whole, it replaces the local
	// one.
	_, err := bridge.InsertChain(branch)
	common.FailIfErr(t, err)
	expectFrontierAt(t, z.Chain(), branch[1].Momentum)
}
