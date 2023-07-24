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

type ProtocolManager struct {
	minPeers       int
	protVer, netId int

	txpool   txPool
	chainman chainManager

	downloader *downloader.Downloader
	fetcher    *fetcher.Fetcher
	peers      *peerSet

	SubProtocols []p2p.Protocol

	// channels for fetcher, syncer, txsyncLoop
	newPeerCh chan *peer
	txsyncCh  chan *txsync
	quitSync  chan struct{}

	// wait group is used for graceful shutdowns during downloading
	// and processing
	wg   sync.WaitGroup
	quit bool
}

// NewProtocolManager returns a new ethereum sub protocol manager. The Ethereum sub protocol manages peers capable
// with the ethereum network.
func NewProtocolManager(minPeers int, networkId uint64, bridge ChainBridge) *ProtocolManager {
	// Create the protocol manager with the base fields
	manager := &ProtocolManager{
		minPeers:  minPeers,
		txpool:    bridge,
		chainman:  bridge,
		peers:     newPeerSet(),
		newPeerCh: make(chan *peer, 1),
		txsyncCh:  make(chan *txsync),
		quitSync:  make(chan struct{}),
		netId:     int(networkId),
	}
	// Initiate a sub-protocol for every implemented version we can handle
	manager.SubProtocols = make([]p2p.Protocol, len(ProtocolVersions))
	for i := 0; i < len(manager.SubProtocols); i++ {
		version := ProtocolVersions[i]

		manager.SubProtocols[i] = p2p.Protocol{
			Name:    "eth",
			Version: version,
			Length:  ProtocolLengths[i],
			Run: func(p *p2p.Peer, rw p2p.MsgReadWriter) error {
				peer := manager.newPeer(int(version), int(networkId), p, rw)
				manager.newPeerCh <- peer
				return manager.handle(peer)
			},
		}
	}
	// Construct the different synchronisation mechanisms
	manager.downloader = downloader.New(
		manager.chainman.HasBlock,
		manager.chainman.GetBlock,
		manager.chainman.CurrentBlock,
		manager.chainman.InsertChain,
		manager.removePeer)

	validator := func(block *nom.Momentum, parent *nom.Momentum) error {
		//return core.ValidateHeader(pow, block.Headerr(), parent, true)
		return nil
	}
	heighter := func() uint64 {
		momentum := manager.chainman.CurrentBlock()
		return momentum.Height
	}
	manager.fetcher = fetcher.New(
		manager.chainman.GetBlock,
		validator,
		manager.BroadcastMomentum,
		heighter,
		manager.chainman.InsertChain,
		manager.removePeer)

	return manager
}

func (pm *ProtocolManager) removePeer(id string) {
	// Short circuit if the peer was already removed
	peer := pm.peers.Peer(id)
	if peer == nil {
		return
	}
	log.Debug("removing peer", "peer-id", id)

	// Unregister the peer from the downloader and Ethereum peer set
	pm.downloader.UnregisterPeer(id)
	if err := pm.peers.Unregister(id); err != nil {
		log.Error("peer removal failed", "peer-id", id, "reason", err)
	}
	// Hard disconnect at the networking layer
	if peer != nil {
		peer.Peer.Disconnect(p2p.DiscUselessPeer)
	}
}

func (pm *ProtocolManager) Start() {
	// start sync handlers
	pm.wg.Add(1)
	go func() {
		pm.syncer()
		pm.wg.Done()
	}()

	go func() {
		pm.txsyncLoop()
	}()
}

func (pm *ProtocolManager) Stop() {
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
}

func (pm *ProtocolManager) newPeer(pv, nv int, p *p2p.Peer, rw p2p.MsgReadWriter) *peer {
	return newPeer(pv, nv, p, rw)
}

// handle is the callback invoked to manage the life cycle of an eth peer. When
// this function terminates, the peer is disconnected.
func (pm *ProtocolManager) handle(p *peer) error {
	log.Info("peer connected", "peer-id", p.id, "address", p.RemoteAddr().String(), "name", p.Name())

	// Execute the Ethereum handshake
	td, head, genesis := pm.chainman.Status()
	if err := p.Handshake(td, head, genesis); err != nil {
		log.Info("handshake failed", "peer", p, "name", p.Name())
		return err
	}
	// Register the peer locally
	if err := pm.peers.Register(p); err != nil {
		log.Error("peer addition failed", "peer-id", p.id, "reason", err)
		return err
	}
	defer pm.removePeer(p.id)

	// Register the peer in the downloader. If the downloader considers it banned, we disconnect
	if err := pm.downloader.RegisterPeer(p.id, p.version, p.Head(), p.RequestHashes, p.RequestHashesFromNumber, p.RequestBlocks); err != nil {
		return err
	}
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
		// Retrieve the hashes from the block chain and return them
		hashes, err := pm.chainman.GetBlockHashesFromHash(request.Hash, request.Amount)
		if err != nil {
			return err
		}
		if len(hashes) == 0 {
			log.Info("invalid block hash", "hash", request.Hash.Bytes()[:4])
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
		// Calculate the last block that should be retrieved, and short circuit if unavailable
		last, err := pm.chainman.GetBlockByNumber(request.Number + request.Amount - 1)
		if err != nil {
			return err
		}
		if last == nil {
			last = pm.chainman.CurrentBlock()
			request.Amount = last.Height - request.Number + 1
		}
		if last.Height < request.Number {
			return p.SendBlockHashes(nil)
		}
		// Retrieve the hashes from the last block backwards, reverse and return
		hashes, err := pm.chainman.GetBlockHashesFromHash(last.Hash, request.Amount)
		if err != nil {
			return err
		}

		for i := 0; i < len(hashes)/2; i++ {
			hashes[i], hashes[len(hashes)-1-i] = hashes[len(hashes)-1-i], hashes[i]
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
		err := pm.downloader.DeliverHashes(p.id, hashes)
		if err != nil {
			log.Debug("failed to deliver hashes", "reason", err)
		}

	case GetBlocksMsg:
		// Decode the retrieval message
		msgStream := rlp.NewStream(msg.Payload, uint64(msg.Size))
		if _, err := msgStream.List(); err != nil {
			return err
		}
		// Gather blocks until the fetch or network limits is reached
		var (
			hash   types.Hash
			hashes []types.Hash
			blocks []*nom.DetailedMomentum
		)
		for {
			err := msgStream.Decode(&hash)
			if err == rlp.EOL {
				break
			} else if err != nil {
				return errResp(ErrDecode, "msg %v: %v", msg, err)
			}
			hashes = append(hashes, hash)

			// Retrieve the requested block, stopping if enough was found
			if block := pm.chainman.GetBlock(hash); block != nil {
				blocks = append(blocks, block)
				if len(blocks) >= downloader.MaxBlockFetch {
					break
				}
			}
		}

		if len(blocks) == 0 && len(hashes) > 0 {
			list := "["
			for _, hash := range hashes {
				list += fmt.Sprintf("%x, ", hash[:4])
			}
			list = list[:len(list)-2] + "]"

			log.Debug("no blocks found for requested hashes", "peer-id", p.id, "hashes", list)
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
			block.Momentum.EnsureCache()
			hashes[i] = block.Momentum.Hash
		}

		// Filter out any explicitly requested blocks, deliver the rest to the downloader
		if blocks := pm.fetcher.Filter(blocks); len(blocks) > 0 {
			if err := pm.downloader.DeliverBlocks(p.id, blocks); err != nil {
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
	}

	// Otherwise if the block is indeed in out own chain, announce it
	if pm.chainman.HasBlock(hash) {
		for _, p := range peers {
			if err := p.SendNewBlockHashes([]types.Hash{hash}); err != nil {
				log.Debug("failed to announce momentum", "peer-id", p.id, "reason", err)
			}
		}
		log.Info("announced momentum to peers", "num-peers", len(peers), "momentum-identifier", detailed.Momentum.Identifier())
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

func (pm *ProtocolManager) SyncInfo() *SyncInfo {
	return pm.syncInfo()
}
