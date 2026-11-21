package protocol

import (
	"sync"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
)

// producerStallWindow is how long anything short of an import may keep
// production blocked: a best peer's advertised height, a download session
// against it, and blocks a session has fetched that the import worker has
// not taken. It is production policy, not a bound on a session: a first
// import is expected well inside it, but every request of a session has
// its own timeout and a session can run for longer without importing,
// which is why the window is measured from the last import and from
// nothing a session does.
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
// frontier moved on verified momentums; a replacement that failed and was
// restored is not.
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
// Production is blocked while fewer than minPeers peers are connected and
// while the import worker owns blocks (the frontier is about to move,
// whatever the advertised target says). Otherwise it is blocked while the
// node is behind the best peer's advertised height, while a download
// session is running, or while fetched blocks wait in the queue, until no
// session has imported a batch for producerStallWindow.
//
// An advertised height is a claim, not a verified chain, and so is
// everything a peer can do with it short of serving momentums that import:
// a session lasts as long as its origin answers, and the queue files a
// delivered block before anything has checked it. Once the window has
// passed without an import none of these holds production, whatever peers
// connect or reconnect and however many sessions start in the meantime,
// until a session does import a batch again. A session that is importing
// renews the window with every batch, so it holds from its first import
// until the window after its last.
func (pm *ProtocolManager) ReadyToProduce() bool {
	return pm.readyToProduce(pm.downloader)
}

// readyToProduce is ReadyToProduce reading the downloader through work.
func (pm *ProtocolManager) readyToProduce(work importWork) bool {
	info := pm.syncInfo()
	if info.State == NotEnoughPeers {
		return false
	}
	session := work.Synchronising()
	// Stats before Processing, in that order: a delivered batch sits in the
	// queue, where Stats counts it, until the worker takes it, and the
	// worker sets its flag before it takes anything. Read the other way
	// round, a worker starting between the two reads could take the batch
	// before Stats looked and both checks would come back empty. Past the
	// stall window the cached count holds nothing (see below), and a batch
	// is first seen when the worker raises its flag.
	_, cached, importing, _ := work.Stats()
	// Blocks the worker has taken hold production on their own: the node
	// is inserting them and the frontier may move under a producer at any
	// moment. That lasts as long as the node's own insert, not as long as
	// a peer likes.
	if importing > 0 {
		return false
	}
	if work.Processing() {
		return false
	}
	// The window is read after the worker's flag: the worker records an
	// import before it lowers the flag, so an import that finished while
	// the reads above were made is seen here.
	if pm.progress.idleFor() >= producerStallWindow {
		// Nothing has been imported for the window. A running session and
		// blocks left in the queue are then no more than the claim that
		// led to them: a peer can start sessions back to back, keep each
		// one going and park blocks behind a head it never serves, all
		// without an import.
		return true
	}
	return info.State == SyncDone && !session && cached == 0
}
