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
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/rlp"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/protocol/downloader"
	"github.com/zenon-network/go-zenon/protocol/fetcher"
)

func errResp(code errCode, format string, v ...interface{}) error {
	return fmt.Errorf("%v - %v", code, fmt.Sprintf(format, v...))
}

// ProtocolManager drives the eth subprotocol on top of the p2p
// server. It exposes one p2p.Protocol per supported version through
// SubProtocols and runs a handler loop for every connected peer:
// after the status handshake the peer is registered with the
// downloader and the loop dispatches its messages, serving history
// requests from the chain and feeding inbound momentums to the
// downloader or fetcher and account blocks to the bridge's pool.
//
// Two background loops complete the picture: the syncer, on new
// connections and every few seconds, runs the downloader against the
// best peer once at least minPeers are connected, and only downloads
// when that peer advertises a higher frontier; the txsync loop sends
// pending account blocks to each newly connected peer.
type ProtocolManager struct {
	minPeers       int
	protVer, netId int

	txpool   txPool
	chainman chainManager

	downloader *downloader.Downloader
	fetcher    *fetcher.Fetcher
	peers      *peerSet
	// quarantine holds peers refused after serving a side-chain the
	// bridge rejected on a consensus check; see rejectPeer.
	quarantine *peerQuarantine
	// admission serializes admitting a connection (the quarantine check
	// and both registrations, see admit) with quarantining a node and
	// removing its connection (rejectPeer, removePeer). A verdict on a
	// node therefore lands either before its reconnection is admitted,
	// and refuses it, or after, and finds it registered to remove.
	admission sync.Mutex
	// progress is what ReadyToProduce measures the stall window from.
	progress *producerProgress

	SubProtocols []p2p.Protocol

	// channels for fetcher, syncer, txsyncLoop
	newPeerCh chan *peer
	txsyncCh  chan *txsync
	quitSync  chan struct{}

	// wait group is used for graceful shutdowns during downloading
	// and processing
	wg   sync.WaitGroup
	quit bool

	stopOnce sync.Once
}

// NewProtocolManager returns a new ethereum sub protocol manager. The Ethereum sub protocol manages peers capable
// with the ethereum network.
func NewProtocolManager(minPeers int, networkId uint64, bridge ChainBridge) *ProtocolManager {
	// Create the protocol manager with the base fields
	manager := &ProtocolManager{
		minPeers:   minPeers,
		txpool:     bridge,
		chainman:   bridge,
		peers:      newPeerSet(),
		quarantine: newPeerQuarantine(peerQuarantinePeriod, peerQuarantineCap),
		progress:   newProducerProgress(),
		newPeerCh:  make(chan *peer, 1),
		txsyncCh:   make(chan *txsync),
		quitSync:   make(chan struct{}),
		netId:      int(networkId),
	}
	// Initiate a sub-protocol for every implemented version we can handle
	manager.SubProtocols = make([]p2p.Protocol, len(ProtocolVersions))
	for i := 0; i < len(manager.SubProtocols); i++ {
		version := ProtocolVersions[i]

		manager.SubProtocols[i] = p2p.Protocol{
			Name:    "eth",
			Version: version,
			Length:  ProtocolLengths[i],
			Run: func(p p2p.Peer, rw p2p.MsgReadWriter) error {
				peer := manager.newPeer(int(version), int(networkId), p, rw)
				if err := manager.announcePeer(peer); err != nil {
					return err
				}
				return manager.handle(peer)
			},
		}
	}
	// Construct the different synchronisation mechanisms
	manager.downloader = downloader.New(
		manager.chainman.HasBlock,
		manager.chainman.GetBlock,
		manager.chainman.CurrentBlock,
		manager.insertFromDownload,
		manager.removePeer,
		manager.rejectPeer)

	verifier := func(detailed *nom.DetailedMomentum) error {
		return manager.chainman.VerifyMomentum(detailed)
	}
	heighter := func() uint64 {
		momentum := manager.chainman.CurrentBlock()
		return momentum.Height
	}
	manager.fetcher = fetcher.New(
		manager.chainman.GetBlock,
		verifier,
		manager.BroadcastMomentum,
		heighter,
		manager.chainman.InsertChain,
		manager.removePeer,
		manager.rejectPeer)

	return manager
}

// rejectPeer is the downloader's and fetcher's import-failure hook. When
// the bridge reports a SideChainRejected, the peer served a branch this
// node refuses on a consensus check; it is quarantined, so it cannot
// restart the download-and-reject cycle on the next sync interval or by
// reconnecting, and removed. Any other failure is left to the caller's
// existing handling.
//
// The import that reports the verdict may outlive the connection that
// served the bytes: unregistering a peer does not join its import. The
// node may have reconnected meanwhile, so the quarantine entry and the
// removal are made under admission, together: a reconnection still in
// its handshake is refused when it reaches admit, and one already
// admitted is the connection removed here.
func (pm *ProtocolManager) rejectPeer(id string, err error) {
	var rejected *SideChainRejected
	if !errors.As(err, &rejected) {
		return
	}
	pm.admission.Lock()
	pm.quarantine.add(id)
	peer := pm.unregister(id)
	pm.admission.Unlock()
	log.Info("quarantining peer after rejected side-chain", "peer-id", id, "momentum-identifier", rejected.Momentum, "reason", rejected.Cause, "period", peerQuarantinePeriod)
	if peer != nil {
		peer.Peer.Disconnect(p2p.DiscUselessPeer)
	}
}

func (pm *ProtocolManager) removePeer(id string) {
	pm.admission.Lock()
	peer := pm.unregister(id)
	pm.admission.Unlock()
	// Short circuit if the peer was already removed
	if peer == nil {
		return
	}
	log.Debug("removing peer", "peer-id", id)
	// Hard disconnect at the networking layer
	peer.Peer.Disconnect(p2p.DiscUselessPeer)
}

// unregister removes the connection registered for id from the downloader
// and the peer set and returns it, or nil when none is registered. The
// caller holds admission, so the two registrations leave together, as
// admit makes them.
func (pm *ProtocolManager) unregister(id string) *peer {
	peer := pm.peers.Peer(id)
	if peer == nil {
		return nil
	}
	pm.downloader.UnregisterPeer(id)
	if err := pm.peers.Unregister(id); err != nil {
		log.Error("peer removal failed", "peer-id", id, "reason", err)
	}
	return peer
}

// admit registers p with the peer set and the downloader, unless p's
// node is quarantined or the downloader refuses its head. The check and
// both registrations happen under admission (see rejectPeer), so this is
// the quarantine check that counts: a node quarantined while p's
// handshake was in flight is refused here, and one quarantined after
// admit returns is removed by rejectPeer. The check before the handshake
// in handle only spares a refused node the round-trip.
func (pm *ProtocolManager) admit(p *peer) error {
	pm.admission.Lock()
	defer pm.admission.Unlock()
	if pm.quarantine.active(p.id) {
		return errPeerQuarantined
	}
	if err := pm.peers.Register(p); err != nil {
		return err
	}
	if err := pm.downloader.RegisterPeer(p.id, p.version, p.Head(), p.RequestHashes, p.RequestHashesFromNumber, p.RequestBlocks); err != nil {
		if uerr := pm.peers.Unregister(p.id); uerr != nil {
			log.Error("peer removal failed", "peer-id", p.id, "reason", uerr)
		}
		return err
	}
	return nil
}

// Start launches the background sync loops: the syncer, which fires
// downloader runs, and the transaction sync loop, which seeds new
// peers with pending account blocks.
func (pm *ProtocolManager) Start() {
	// start sync handlers
	pm.wg.Add(1)
	go func() {
		pm.syncer()
		pm.wg.Done()
	}()

	pm.wg.Add(1)
	go func() {
		pm.txsyncLoop()
		pm.wg.Done()
	}()
}

// errShuttingDown is returned to a protocol goroutine that connects after
// Stop has quit the syncer.
var errShuttingDown = errors.New("protocol manager shutting down")

// announcePeer hands a freshly connected peer to the syncer. After Stop
// nothing drains newPeerCh, and the transport's peer goroutine is joined
// on exit, so the hand-off gives up on quitSync instead of blocking.
func (pm *ProtocolManager) announcePeer(p *peer) error {
	select {
	case pm.newPeerCh <- p:
		return nil
	case <-pm.quitSync:
		return errShuttingDown
	}
}

// Stop terminates the sync loops and waits for in-flight download and
// processing work to drain; with a download in progress this can take
// several seconds. Stop is idempotent: a second call returns immediately.
func (pm *ProtocolManager) Stop() {
	pm.stopOnce.Do(func() {
		// Showing a log message. During download / process this could actually
		// take between 5 to 10 seconds and therefor feedback is required.
		log.Info("Stopping protocol handler...")

		pm.quit = true
		//pm.txSub.Unsubscribe()         // quits txBroadcastLoop
		//pm.minedBlockSub.Unsubscribe() // quits blockBroadcastLoop
		close(pm.quitSync) // quits syncer, fetcher, txsyncLoop

		// Wait for any process action
		pm.wg.Wait()

		log.Info("Protocol handler stopped")
	})
}

func (pm *ProtocolManager) newPeer(pv, nv int, p p2p.Peer, rw p2p.MsgReadWriter) *peer {
	return newPeer(pv, nv, p, rw)
}

// handle is the callback invoked to manage the life cycle of an eth peer. When
// this function terminates, the peer is disconnected.
func (pm *ProtocolManager) handle(p *peer) error {
	if pm.quarantine.active(p.id) {
		log.Info("refusing quarantined peer", "peer-id", p.id, "address", p.RemoteAddr().String())
		return errPeerQuarantined
	}
	log.Info("peer connected", "peer-id", p.id, "address", p.RemoteAddr().String(), "name", p.Name())

	// Execute the Ethereum handshake
	td, head, genesis := pm.chainman.Status()
	if err := p.Handshake(td, head, genesis); err != nil {
		log.Info("handshake failed", "peer", p, "name", p.Name())
		return err
	}
	// Register the peer locally and in the downloader. If the node was
	// quarantined during the handshake or the downloader considers it
	// banned, we disconnect.
	if err := pm.admit(p); err != nil {
		if errors.Is(err, errPeerQuarantined) {
			log.Info("refusing peer quarantined during handshake", "peer-id", p.id, "address", p.RemoteAddr().String())
		} else {
			log.Error("peer addition failed", "peer-id", p.id, "reason", err)
		}
		return err
	}
	defer pm.removePeer(p.id)
	// Propagate existing transactions. new transactions appearing
	// after this will be sent via broadcasts.
	pm.syncTransactions(p)

	// main loop. handle incoming messages.
	for {
		if err := pm.handleMsg(p); err != nil {
			log.Info("message handling failed", "peer-id", p.id, "reason", err)
			return err
		}
	}
}

// gatherBlocksForReply reads hashes from a GetBlocksMsg stream, deduplicates
// them, and collects blocks until the count or reply-size limit is reached.
// It returns the collected blocks and the total number of hashes read (before
// dedup), for logging. The getBlock function is injected for testability.
//
// The MaxBlocksRequest bound counts every decoded hash, before dedup, because
// every named hash costs the receiver a store lookup whether or not the block
// exists and whether or not the hash repeats (issue #84). The dedup skips the
// redundant lookup but not the budget. The soft reply cap stops the loop once
// the encoded reply exceeds SoftResponseLimit, which bounds the read and
// encode work a single request can cause (issue #124). It is a soft bound on
// this handler's work, not a guarantee that the transport accepts what
// follows: the reply is still one message, and both transports reject frames
// above their own cap (10 MiB in libp2p; 10 MiB plus a frame header on legacy
// RLPx).
func gatherBlocksForReply(msgStream *rlp.Stream, getBlock func(types.Hash) *nom.DetailedMomentum) (blocks []*nom.DetailedMomentum, hashCount int, err error) {
	blocks, hashCount, _, err = gatherBlocksForReplyBudgeted(msgStream, getBlock, nil)
	return blocks, hashCount, err
}

// gatherBlocksForReplyBudgeted is gatherBlocksForReply charging the peer's
// request budget (see RequestLookupRate): one lookup unit per hash before
// it is looked up, and the miss remainder after a lookup finds nothing, so
// a peer that has run dry never reaches the store. It also returns the
// encoded size of the collected blocks, which the caller charges as reply
// bytes before sending. A nil budget charges nothing.
func gatherBlocksForReplyBudgeted(msgStream *rlp.Stream, getBlock func(types.Hash) *nom.DetailedMomentum, budget *requestBudget) (blocks []*nom.DetailedMomentum, hashCount int, replySize int, err error) {
	var (
		hash types.Hash
		seen = make(map[types.Hash]struct{})
	)
	for {
		// Every requested hash costs a store lookup whether or not the
		// block exists, so bound the lookups, not only the hits: after
		// MaxBlocksRequest of them, answer with what was found and leave
		// the rest of the message unread and undecoded. Disconnecting
		// instead would cost the same lookups and only cut off peers on
		// releases that do not split requests.
		if hashCount >= MaxBlocksRequest {
			break
		}
		derr := msgStream.Decode(&hash)
		if derr == rlp.EOL {
			break
		} else if derr != nil {
			return nil, hashCount, 0, errResp(ErrDecode, "getBlocks: %v", derr)
		}
		hashCount++

		// Skip duplicates: each unique hash costs one store lookup, but
		// repeating a hash must not multiply the reply payload (issue #124).
		// The hash still counted toward MaxBlocksRequest above.
		if _, dup := seen[hash]; dup {
			continue
		}
		seen[hash] = struct{}{}

		// Retrieve the requested block, stopping if enough was found. The
		// lookup is admitted against the peer's budget first.
		if budget != nil {
			if err := budget.admitLookups(1); err != nil {
				return nil, hashCount, 0, err
			}
		}
		block := getBlock(hash)
		if block == nil {
			if budget != nil {
				if err := budget.chargeMiss(); err != nil {
					return nil, hashCount, 0, err
				}
			}
			continue
		}
		encoded, encErr := rlp.EncodeToBytes(block)
		if encErr != nil {
			continue
		}
		// Include the block, then stop if the reply has grown past
		// the soft limit. Appending before checking ensures at least
		// one block is always returned, matching go-ethereum.
		replySize += len(encoded)
		blocks = append(blocks, block)
		if replySize > SoftResponseLimit {
			break
		}
		if len(blocks) >= downloader.MaxBlockFetch {
			break
		}
	}
	return blocks, hashCount, replySize, nil
}

// handleMsg is invoked whenever an inbound message is received from a remote
// peer. The remote connection is torn down upon returning any error.
func (pm *ProtocolManager) handleMsg(p *peer) error {
	// Read the next message from the remote peer, and ensure it's fully consumed
	msg, err := p.rw.ReadMsg()
	if err != nil {
		return err
	}
	if msg.Size > ProtocolMaxMsgSize {
		return errResp(ErrMsgTooLarge, "%v > %v", msg.Size, ProtocolMaxMsgSize)
	}
	defer msg.Discard()

	// Handle the message depending on its contents
	switch msg.Code {
	case StatusMsg:
		// Status messages should never arrive after the handshake
		return errResp(ErrExtraStatusMsg, "uncontrolled status message")

	case GetBlockHashesMsg:
		// Retrieve the number of hashes to return and from which origin hash
		var request getBlockHashesData
		if err := msg.Decode(&request); err != nil {
			return errResp(ErrDecode, "%v: %v", msg, err)
		}
		if request.Amount > uint64(downloader.MaxHashFetch) {
			request.Amount = uint64(downloader.MaxHashFetch)
		}
		// One unit for the request plus one per hash the walk may visit,
		// admitted before the chain is touched.
		budget := p.requestBudget()
		if err := budget.admitLookups(1 + float64(request.Amount)); err != nil {
			return err
		}
		// Retrieve the hashes from the block chain and return them
		hashes, err := pm.chainman.GetBlockHashesFromHash(request.Hash, request.Amount)
		if err != nil {
			return err
		}
		if len(hashes) == 0 {
			log.Info("invalid block hash", "hash", request.Hash.Bytes()[:4])
		}
		// Approximate encoded size: one RLP string header per hash, the
		// list header ignored.
		if err := budget.admitReplyBytes(len(hashes) * (types.HashSize + 1)); err != nil {
			return err
		}
		return p.SendBlockHashes(hashes)

	case GetBlockHashesFromNumberMsg:
		// Retrieve and decode the number of hashes to return and from which origin number
		var request getBlockHashesFromNumberData
		if err := msg.Decode(&request); err != nil {
			return errResp(ErrDecode, "%v: %v", msg, err)
		}

		if request.Amount > uint64(downloader.MaxHashFetch) {
			request.Amount = uint64(downloader.MaxHashFetch)
		}
		// One unit for the request and one for the height lookup; the walk
		// is charged once its length is known, before it runs.
		budget := p.requestBudget()
		if err := budget.admitLookups(2); err != nil {
			return err
		}
		// Calculate the last block that should be retrieved, and short circuit if unavailable
		last, err := pm.chainman.GetBlockByNumber(request.Number + request.Amount - 1)
		if err != nil {
			return err
		}
		if last == nil {
			// The frontier read is a lookup too.
			if err := budget.admitLookups(1); err != nil {
				return err
			}
			last = pm.chainman.CurrentBlock()
			request.Amount = last.Height - request.Number + 1
		}
		if last.Height < request.Number {
			return p.SendBlockHashes(nil)
		}
		// One unit per hash the walk visits, admitted before it runs.
		if err := budget.admitLookups(float64(request.Amount)); err != nil {
			return err
		}
		// Retrieve the hashes from the last block backwards, reverse and return
		hashes, err := pm.chainman.GetBlockHashesFromHash(last.Hash, request.Amount)
		if err != nil {
			return err
		}

		for i := 0; i < len(hashes)/2; i++ {
			hashes[i], hashes[len(hashes)-1-i] = hashes[len(hashes)-1-i], hashes[i]
		}
		// Approximate encoded size: one RLP string header per hash, the
		// list header ignored.
		if err := budget.admitReplyBytes(len(hashes) * (types.HashSize + 1)); err != nil {
			return err
		}
		return p.SendBlockHashes(hashes)

	case BlockHashesMsg:
		// A batch of hashes arrived to one of our previous requests
		msgStream := rlp.NewStream(msg.Payload, uint64(msg.Size))

		var hashes []types.Hash
		if err := msgStream.Decode(&hashes); err != nil {
			break
		}

		// Deliver them all to the downloader for queuing
		err := pm.downloader.DeliverHashes(p.id, hashes, p.closed)
		if err != nil {
			log.Debug("failed to deliver hashes", "reason", err)
		}

	case GetBlocksMsg:
		// Decode the retrieval message
		msgStream := rlp.NewStream(msg.Payload, uint64(msg.Size))
		if _, err := msgStream.List(); err != nil {
			return err
		}
		// One unit for the request; the hashes are charged as they are
		// looked up, the reply by its approximate encoded size (the sum of
		// the encoded blocks, the list header ignored) before it is sent.
		budget := p.requestBudget()
		if err := budget.admitLookups(1); err != nil {
			return err
		}
		blocks, hashCount, replySize, err := gatherBlocksForReplyBudgeted(msgStream, pm.chainman.GetBlock, budget)
		if err != nil {
			return err
		}
		if len(blocks) == 0 && hashCount > 0 {
			log.Debug("no blocks found for requested hashes", "peer-id", p.id, "count", hashCount)
		}
		if err := budget.admitReplyBytes(replySize); err != nil {
			return err
		}
		return p.SendBlocks(blocks)

	case BlocksMsg:
		// Decode the arrived block message
		msgStream := rlp.NewStream(msg.Payload, uint64(msg.Size))

		var blocks []*nom.DetailedMomentum
		if err := msgStream.Decode(&blocks); err != nil {
			log.Debug("failed to decode momentum", "reason", err)
			blocks = nil
			return err
		}

		hashes := make([]types.Hash, len(blocks))
		for i, block := range blocks {
			// Decoded from the peer, so do not assume the elements are populated.
			if block == nil || block.Momentum == nil {
				return errResp(ErrDecode, "%v: incomplete momentum at index %d", msg, i)
			}
			block.Momentum.EnsureCache()
			hashes[i] = block.Momentum.Hash
		}

		// Filter out any explicitly requested blocks, deliver the rest to the downloader
		if blocks := pm.fetcher.Filter(p.id, blocks); len(blocks) > 0 {
			if err := pm.downloader.DeliverBlocks(p.id, blocks, p.closed); err != nil {
				log.Debug("failed to deliver blocks", "reason", err)
			}
		}

	case NewBlockHashesMsg:
		// Retrieve and deseralize the remote new block hashes notification
		msgStream := rlp.NewStream(msg.Payload, uint64(msg.Size))

		var hashes []types.Hash
		if err := msgStream.Decode(&hashes); err != nil {
			break
		}

		// Mark the hashes as present at the remote node
		for _, hash := range hashes {
			p.MarkBlock(hash)
			p.SetHead(hash)
		}
		// Schedule all the unknown hashes for retrieval
		unknown := make([]types.Hash, 0, len(hashes))
		for _, hash := range hashes {
			if !pm.chainman.HasBlock(hash) {
				unknown = append(unknown, hash)
			}
		}
		for _, hash := range unknown {
			pm.fetcher.Notify(p.id, hash, time.Now(), p.RequestBlocks)
		}

	case NewBlockMsg:
		// Retrieve and decode the propagated block
		var detailed *nom.DetailedMomentum
		if err := msg.Decode(&detailed); err != nil {
			return errResp(ErrDecode, "%v: %v", msg, err)
		}
		// Decoded from the peer, so do not assume the message is populated.
		if detailed == nil || detailed.Momentum == nil {
			return errResp(ErrDecode, "%v: incomplete momentum", msg)
		}

		detailed.Momentum.EnsureCache()

		// Mark the peer as owning the block and schedule it for import
		p.MarkBlock(detailed.Momentum.Hash)
		p.SetHead(detailed.Momentum.Hash)

		if pm.SyncInfo().State == SyncDone {
			pm.fetcher.Enqueue(p.id, detailed)

			// TODO: Schedule a sync to cover potential gaps (this needs proto update)
			if detailed.Momentum.Height > p.Td() {
				p.SetTd(detailed.Momentum.Height)
				go func() {
					pm.synchronise(p)
				}()
			}
		}

	case TxMsg:
		// Transactions arrived, parse all of them and deliver to the pool
		var txs []*nom.AccountBlock
		if err := msg.Decode(&txs); err != nil {
			return errResp(ErrDecode, "msg %v: %v", msg, err)
		}
		for i, tx := range txs {
			// Validate and mark the remote transaction
			if tx == nil {
				return errResp(ErrDecode, "transaction %d is nil", i)
			}
			p.MarkTransaction(tx.Hash)
		}
		pm.wg.Add(1)
		pm.txpool.AddAccountBlocks(txs)
		pm.wg.Done()
	default:
		return errResp(ErrInvalidMsgCode, "%v", msg.Code)
	}
	return nil
}

// BroadcastMomentum will  propagate a block to a subset of it's peers, or
// will only announce it's availability (depending what's requested).
func (pm *ProtocolManager) BroadcastMomentum(detailed *nom.DetailedMomentum, propagate bool) {
	hash := detailed.Momentum.Hash
	peers := pm.peers.PeersWithoutBlock(hash)

	// Peers that don't receive the full momentum below still need a hash
	// announcement if we have the block.
	announce := peers

	// If propagation is requested, send to a subset of the peer
	if propagate {
		numPeers := len(peers)
		if numPeers > 10 {
			numPeers = int(math.Sqrt(float64(numPeers-10))) + 10
		}
		// Send the block to a subset of our peers
		transfer := peers[:numPeers]
		for _, p := range transfer {
			if err := p.SendNewMomentum(detailed); err != nil {
				log.Debug("failed to propagated momentum", "peer-id", p.id, "reason", err)
			}
		}
		log.Info("propagated momentum to peers", "num-peers", len(transfer), "momentum-identifier", detailed.Momentum.Identifier())
		announce = peers[numPeers:]
	}

	// Otherwise if the block is indeed in out own chain, announce it
	if pm.chainman.HasBlock(hash) {
		for _, p := range announce {
			if err := p.SendNewBlockHashes([]types.Hash{hash}); err != nil {
				log.Debug("failed to announce momentum", "peer-id", p.id, "reason", err)
			}
		}
		log.Info("announced momentum to peers", "num-peers", len(announce), "momentum-identifier", detailed.Momentum.Identifier())
	}
}

// BroadcastAccountBlock will propagate a transaction to all peers which are not known to
// already have the given transaction.
func (pm *ProtocolManager) BroadcastAccountBlock(tx *nom.AccountBlock) {
	// Broadcast transaction to a batch of peers not knowing about it
	peers := pm.peers.PeersWithoutTx(tx.Hash)
	for _, p := range peers {
		if err := p.SendTransactions([]*nom.AccountBlock{tx}); err != nil {
			log.Debug("failed to propagated account-block", "peer-id", p.id, "reason", err)
		}
	}
	log.Info("propagated account-block to peers", "num-peers", len(peers), "account-block-header", tx.Header())
}

// SyncInfo reports the current sync state together with the local
// frontier momentum height and the height advertised by the best
// peer. See SyncState for how the state is derived.
func (pm *ProtocolManager) SyncInfo() *SyncInfo {
	return pm.syncInfo()
}
