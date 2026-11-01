package protocol

import (
	"sync"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
)

// producerStallWindow is how long a best peer's advertised height may keep
// production blocked without a download session importing anything. A
// failing session (hash fetch, block fetch and import) completes well
// inside it, so two attempts fit before the node concludes the target is
// not reachable from its peers.
const producerStallWindow = 60 * time.Second

// producerProgress records the last time a download session imported a
// batch. Only the downloader's imports count: the fetcher's gossip imports
// extend whatever branch the node is on and say nothing about whether the
// advertised target can be reached.
type producerProgress struct {
	mu         sync.Mutex
	lastImport time.Time
}

func newProducerProgress() *producerProgress {
	return &producerProgress{lastImport: common.Clock.Now()}
}

func (p *producerProgress) markImport() {
	now := common.Clock.Now()
	p.mu.Lock()
	p.lastImport = now
	p.mu.Unlock()
}

// idleFor is the time since the last download import, or since start-up
// when there has been none.
func (p *producerProgress) idleFor() time.Duration {
	now := common.Clock.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	return now.Sub(p.lastImport)
}

// insertFromDownload is the chain insert the downloader is given. It
// records sync progress when the call left new momentums on the chain, as
// reported by the bridge under its insert lock, before handing the result
// back. A batch the node already had is not progress, so a peer that keeps
// serving a known suffix cannot hold production; a valid prefix that stayed
// on the chain before a later candidate failed is progress, since the
// frontier moved on verified momentums; an extension that was rejected is
// not. A replacement that was rolled back to the fork point and then
// rejected counts once, since the frontier did move; its retry from the
// fork point does not, so a peer serving the same rejected branch holds
// production for one window and no longer.
func (pm *ProtocolManager) insertFromDownload(momentums []*nom.DetailedMomentum) (int, error) {
	index, moved, err := pm.chainman.InsertChainMoved(momentums)
	if moved {
		pm.progress.markImport()
	}
	return index, err
}

// importWork is what the readiness predicate reads from the downloader:
// whether a session is running, what its queue and import worker hold,
// and whether the worker owns blocks. The downloader satisfies it; tests
// pass a model of its queue-to-worker handoff.
type importWork interface {
	Synchronising() bool
	Stats() (pending int, cached int, importing int, estimate time.Duration)
	Processing() bool
}

// ReadyToProduce reports whether a pillar on this node may produce for the
// current slot. It is the producer's gate; SyncInfo stays the telemetry
// and keeps its meaning.
//
// Production is blocked while fewer than minPeers peers are connected,
// while a download session is running, its import worker owns blocks, or
// its import queue is draining (the frontier is about to move, whatever
// the advertised target says), and otherwise, while the node is behind the
// best peer's advertised height, until no session has imported a batch for
// producerStallWindow.
// An advertised height is a claim, not a verified chain: once the window
// has passed without an import the claim no longer holds production,
// whatever peers connect or reconnect in the meantime, until a session
// does import a batch again.
func (pm *ProtocolManager) ReadyToProduce() bool {
	return pm.readyToProduce(pm.downloader)
}

// readyToProduce is ReadyToProduce reading the downloader through work.
func (pm *ProtocolManager) readyToProduce(work importWork) bool {
	info := pm.syncInfo()
	if info.State == NotEnoughPeers {
		return false
	}
	if work.Synchronising() {
		return false
	}
	// Stats before Processing, in that order: a delivered batch sits in the
	// queue, where Stats counts it, until the worker takes it, and the
	// worker sets its flag before it takes anything. Read the other way
	// round, a worker starting between the two reads could take the batch
	// before Stats looked and both checks would come back empty.
	if _, cached, importing, _ := work.Stats(); cached > 0 || importing > 0 {
		return false
	}
	if work.Processing() {
		return false
	}
	if info.State == SyncDone {
		return true
	}
	return pm.progress.idleFor() >= producerStallWindow
}
