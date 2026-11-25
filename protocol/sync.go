// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package protocol

import (
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
)

const (
	forceSyncCycle = 4 * time.Second // Time interval to force syncs, even if few peers are available

	// unproductivePeerBackoff is how long a peer whose download session
	// ended without importing anything, while the node stayed below the
	// height the peer advertised, is passed over for download sessions.
	// An advertised height is a claim; without the backoff the peer that
	// claims the most would get every session, every forceSyncCycle, and
	// the other peers none.
	unproductivePeerBackoff = time.Minute

	// importDrainPoll is how often the sync worker checks whether the
	// blocks a finished session fetched have been imported.
	importDrainPoll = 10 * time.Millisecond
)

type txsync struct {
	p   *peer
	txs []*nom.AccountBlock
}

// syncTransactions starts sending all currently pending transactions to the given peer.
func (pm *ProtocolManager) syncTransactions(p *peer) {
	txs := pm.txpool.GetTransactions()
	if len(txs) == 0 {
		return
	}
	select {
	case pm.txsyncCh <- &txsync{p, txs}:
	case <-pm.quitSync:
	}
}

// txsyncLoop takes care of the initial transaction sync for each new
// connection. When a new peer appears, we relay all currently pending
// transactions. In order to minimise egress bandwidth usage, we send
// the transactions in small packs to one peer at a time.
func (pm *ProtocolManager) txsyncLoop() {
	var (
		pending = make(map[string]*txsync)
		sending = false               // whether a send is active
		pack    = new(txsync)         // the pack that is being sent
		done    = make(chan error, 1) // result of the send
	)

	// send starts a sending a pack of transactions from the sync.
	send := func(s *txsync) {
		// Fill pack with transactions up to the target size.
		pack.p = s.p
		pack.txs = pack.txs[:0]
		for i := 0; i < len(s.txs); i++ {
			pack.txs = append(pack.txs, s.txs[i])
		}
		// Remove the transactions that will be sent.
		s.txs = s.txs[:copy(s.txs, s.txs[len(pack.txs):])]
		if len(s.txs) == 0 {
			delete(pending, s.p.id)
		}
		// Send the pack in the background.
		log.Debug("sending transactions", "peer-id", s.p.Peer.ID(), "num-blocks", len(pack.txs))
		sending = true
		go func() {
			done <- pack.p.SendTransactions(pack.txs)
		}()
	}

	// pick chooses the next pending sync.
	pick := func() *txsync {
		if len(pending) == 0 {
			return nil
		}
		n := rand.Intn(len(pending)) + 1
		for _, s := range pending {
			if n--; n == 0 {
				return s
			}
		}
		return nil
	}

	for {
		select {
		case s := <-pm.txsyncCh:
			pending[s.p.id] = s
			if !sending {
				send(s)
			}
		case err := <-done:
			sending = false
			// Stop tracking peers that cause send failures.
			if err != nil {
				log.Info("tx send failed", "peer-id", pack.p.Peer.ID(), "reason", err)
				delete(pending, pack.p.id)
			}
			// Schedule the next send.
			if s := pick(); s != nil {
				send(s)
			}
		case <-pm.quitSync:
			return
		}
	}
}

// syncer is responsible for periodically synchronising with the network, both
// downloading hashes and blocks as well as handling the announcement handler.
//
// It asks the sync worker for a session on every new peer and every
// forceSyncCycle; the handler asks on a gap (requestSync). The worker runs
// the sessions one at a time, and Stop joins it.
func (pm *ProtocolManager) syncer() {
	// Start and ensure cleanup of sync mechanisms
	pm.fetcher.Start()
	defer pm.fetcher.Stop()
	defer pm.downloader.Terminate()

	pm.wg.Add(1)
	go func() {
		defer pm.wg.Done()
		pm.syncWorker()
	}()

	// Wait for different events to fire synchronisation operations
	forceSync := time.NewTicker(forceSyncCycle)
	defer forceSync.Stop()
	for {
		select {
		case <-pm.newPeerCh:
			pm.requestSync()
		case <-forceSync.C:
			// Force a sync even if not enough peers are present
			pm.requestSync()
		case <-pm.quitSync:
			return
		}
	}
}

// requestSync asks the sync worker for a session, without waiting. A
// request made while one is already pending is merged into it.
func (pm *ProtocolManager) requestSync() {
	select {
	case pm.syncReqCh <- struct{}{}:
	default:
	}
}

// syncWorker runs every download session, one at a time, from choosing the
// peer to judging the outcome (see synchronise). Nothing else starts one,
// so what the chain receives from the downloader while a session is being
// run is that session's.
func (pm *ProtocolManager) syncWorker() {
	for {
		select {
		case <-pm.syncReqCh:
		case <-pm.quitSync:
			return
		}
		// Make sure we have peers to select from, then sync
		if pm.peers.Len() < pm.minPeers {
			continue
		}
		pm.synchronise(pm.syncPeer())
	}
}

func (pm *ProtocolManager) syncInfo() *SyncInfo {
	// find height details
	currentHeight := pm.chainman.CurrentBlock().Height
	targetHeight := uint64(0)
	if best := pm.peers.BestPeer(); best != nil {
		targetHeight = best.Td()
	}

	// find state
	var state SyncState
	if currentHeight >= targetHeight {
		state = SyncDone
	} else {
		state = Syncing
	}

	// if there are not enough peers, return this instead
	if pm.peers.Len() < pm.minPeers {
		state = NotEnoughPeers
	}

	return &SyncInfo{
		State:         state,
		CurrentHeight: currentHeight,
		TargetHeight:  targetHeight,
	}
}

// syncPeer is the peer the syncer synchronises with. Peers passed over
// after an unproductive session (see judgeSession) are not considered, and
// it is nil when every peer is passed over; the backoff then runs out
// before the peers get another session.
//
// Ordinarily it is the peer advertising the highest height. After a
// session that imported nothing, and until one imports again, it is the
// peer ahead of the node that has waited longest for a session since its
// last one or since it connected, when there is one. Connections made
// later, such as fresh identities claiming ever higher heights, then
// queue behind the peers already waiting, so each of those gets a session
// after at most as many others as were waiting before it.
func (pm *ProtocolManager) syncPeer() *peer {
	eligible := func(p *peer) bool { return !pm.unproductive.active(p.id) }
	if pm.preferWaiting.Load() {
		local := pm.chainman.CurrentBlock().Height
		ahead := func(p *peer) bool { return p.Td() > local && eligible(p) }
		if p := pm.peers.leastRecentlyServedWhere(ahead); p != nil {
			return p
		}
	}
	return pm.peers.bestPeerWhere(eligible)
}

// synchronise tries to sync up our local block chain with a remote peer.
// The sync worker calls it; it returns once the session has been judged.
func (pm *ProtocolManager) synchronise(peer *peer) {
	// Short circuit if no peers are available
	if peer == nil {
		return
	}
	// The peer minimum holds for every session.
	if pm.peers.Len() < pm.minPeers {
		return
	}
	// A peer passed over after an unproductive session gets no session
	// until its backoff runs out.
	if pm.unproductive.active(peer.id) {
		return
	}
	// Make sure the peer's TD is higher than our own. If not drop.
	td := peer.Td()
	if td <= pm.chainman.CurrentBlock().Height {
		return
	}

	log.Debug("syncing", "peer-id", peer.Peer.ID(), "peer-height", td, "our-height", pm.chainman.CurrentBlock().Height)
	// Otherwise, try to sync with the downloader
	imports, failures := pm.progress.importCount(), pm.progress.failureCount()
	pm.session.begin(peer.id)
	defer pm.session.end()
	attempted, err := pm.downloader.Synchronise(peer.id, peer.Head(), td)
	if !attempted {
		return
	}
	pm.peers.markTurn(peer.id)
	// Retrieval has ended; wait until the blocks the session fetched have
	// been imported or discarded before judging it.
	for pm.downloader.ImportPending() {
		select {
		case <-pm.quitSync:
			return
		case <-time.After(importDrainPoll):
		}
	}
	if err == nil && pm.progress.failureCount() != failures {
		// Retrieval completed, and the import of what it fetched failed
		// afterwards; the downloader dealt with whoever was at fault.
		err = errSessionImportFailed
	}
	pm.judgeSession(peer.id, td, imports, err)
}

// errSessionImportFailed is the outcome of a session whose retrieval
// completed and whose blocks then failed to import.
var errSessionImportFailed = errors.New("session blocks failed to import")

// judgeSession judges a session against peer id whose fetched blocks have
// been imported or discarded. td is the height the peer advertised when
// the session began, imports the download import count then, and ended
// the error retrieval ended with, if any.
//
// A session that imported a batch is productive, and peers are again
// chosen by advertised height. One that ends with the node at most one
// below td is not judged: the momentum a relay has just passed on may not
// be imported on the relay yet, and another path, such as the fetcher, may
// have reached the target first.
//
// Any other session is unproductive: until a session imports, peers are
// chosen by how long they have waited (see syncPeer), so the peer goes
// behind the others. The peer is also passed over for
// unproductivePeerBackoff when the session's end is its own doing: its
// hash list ran out short of the height it advertised, with nothing
// failing on the way, or the downloader removed it. It is not when the
// downloader removed another peer, which was then at fault, or when the
// session ended without blaming anyone: a stall with blocks requested
// from other peers, or an import the downloader held against nobody, such
// as a batch that no longer fit a chain another path had moved. Its
// connection is left alone either way.
func (pm *ProtocolManager) judgeSession(id string, td uint64, imports uint64, ended error) {
	if pm.progress.importCount() != imports {
		pm.preferWaiting.Store(false)
		return
	}
	if pm.chainman.CurrentBlock().Height+1 >= td {
		return
	}
	pm.preferWaiting.Store(true)
	origin, other := pm.session.drops()
	if other || (ended != nil && !origin) {
		log.Info("download session imported nothing, peer not at fault", "peer-id", id, "peer-height", td, "reason", ended, "other-peer-removed", other)
		return
	}
	pm.unproductive.add(id)
	log.Info("passing over peer after a download session that imported nothing", "peer-id", id, "peer-height", td, "period", unproductivePeerBackoff)
}

// syncSession records, while the sync worker runs a session, which peer it
// is against and which peers the downloader removed meanwhile.
type syncSession struct {
	mu     sync.Mutex
	origin string
	// originDropped and otherDropped record whether the downloader
	// removed the session's peer, and any other peer.
	originDropped, otherDropped bool
}

func (s *syncSession) begin(origin string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.origin, s.originDropped, s.otherDropped = origin, false, false
}

func (s *syncSession) end() {
	s.begin("")
}

// dropped records that the downloader removed peer id.
func (s *syncSession) dropped(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.origin == "":
	case id == s.origin:
		s.originDropped = true
	default:
		s.otherDropped = true
	}
}

// drops reports whether the downloader removed the session's peer, and
// whether it removed any other peer, since the session began.
func (s *syncSession) drops() (origin, other bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.originDropped, s.otherDropped
}

// childAdmission remembers which nodes have had a child of the local head
// handed to the fetcher while the node was not in sync. It is keyed by
// node id, which outlives a connection, and forgets everything when the
// head moves.
type childAdmission struct {
	mu     sync.Mutex
	parent types.Hash
	nodes  map[string]struct{}
}

// admit reports whether node id may hand over a child of parent, and
// records it if so.
func (c *childAdmission) admit(id string, parent types.Hash) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.nodes == nil || c.parent != parent {
		c.parent, c.nodes = parent, make(map[string]struct{})
	}
	if _, ok := c.nodes[id]; ok {
		return false
	}
	c.nodes[id] = struct{}{}
	return true
}
