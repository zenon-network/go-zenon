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
// records a successful batch import as sync progress before handing the
// result back.
func (pm *ProtocolManager) insertFromDownload(momentums []*nom.DetailedMomentum) (int, error) {
	index, err := pm.chainman.InsertChain(momentums)
	if err == nil {
		pm.progress.markImport()
	}
	return index, err
}

// ReadyToProduce reports whether a pillar on this node may produce for the
// current slot. It is the producer's gate; SyncInfo stays the telemetry
// and keeps its meaning.
//
// Production is blocked while fewer than minPeers peers are connected,
// while a download session is running or its import queue is draining
// (the frontier is about to move, whatever the advertised target says),
// and otherwise, while the node is behind the best peer's advertised
// height, until no session has imported a batch for producerStallWindow.
// An advertised height is a claim, not a verified chain: once the window
// has passed without an import the claim no longer holds production,
// whatever peers connect or reconnect in the meantime, until a session
// does import a batch again.
func (pm *ProtocolManager) ReadyToProduce() bool {
	info := pm.syncInfo()
	if info.State == NotEnoughPeers {
		return false
	}
	if pm.downloader.Synchronising() {
		return false
	}
	if _, cached, importing, _ := pm.downloader.Stats(); cached > 0 || importing > 0 {
		return false
	}
	if info.State == SyncDone {
		return true
	}
	return pm.progress.idleFor() >= producerStallWindow
}
