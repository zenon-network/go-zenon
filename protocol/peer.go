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
	"sync"

	lru "github.com/hashicorp/golang-lru"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/protocol/downloader"
)

var (
	errAlreadyRegistered = errors.New("peer is already registered")
	errNotRegistered     = errors.New("peer is not registered")
)

const (
	maxKnownTxs    = 32768 // Maximum transactions hashes to keep in the known list (prevent DOS)
	maxKnownBlocks = 1024  // Maximum block hashes to keep in the known list (prevent DOS)
)

type peer struct {
	*p2p.Peer

	rw p2p.MsgReadWriter

	version int // Protocol version negotiated
	network int // Network ID being on

	id string

	head types.Hash
	td   uint64
	lock sync.RWMutex

	knownTxs    *lru.Cache // Set of transaction hashes known to be known by this peer
	knownBlocks *lru.Cache // Set of block hashes known to be known by this peer
}

func newPeer(version, network int, p *p2p.Peer, rw p2p.MsgReadWriter) *peer {
	id := p.ID()

	knownTxs, err := lru.New(maxKnownTxs)
	common.DealWithErr(err)
	knownBlocks, err := lru.New(maxKnownBlocks)
	common.DealWithErr(err)

	return &peer{
		Peer:        p,
		rw:          rw,
		version:     version,
		network:     network,
		id:          fmt.Sprintf("%x", id[:8]),
		knownTxs:    knownTxs,
		knownBlocks: knownBlocks,
	}
}

// Head retrieves a copy of the current head (most recent) hash of the peer.
func (p *peer) Head() (hash types.Hash) {
	p.lock.RLock()
	defer p.lock.RUnlock()

	copy(hash[:], p.head[:])
	return hash
}

// SetHead updates the head (most recent) hash of the peer.
func (p *peer) SetHead(hash types.Hash) {
	p.lock.Lock()
	defer p.lock.Unlock()

	copy(p.head[:], hash[:])
}

// Td retrieves the current total difficulty of a peer.
func (p *peer) Td() uint64 {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.td
}

// SetTd updates the current total difficulty of a peer.
func (p *peer) SetTd(td uint64) {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.td = td
}

// MarkBlock marks a block as known for the peer, ensuring that the block will
// never be propagated to this particular peer.
func (p *peer) MarkBlock(hash types.Hash) {
	p.knownBlocks.Add(hash, nil)
}

// MarkTransaction marks a transaction as known for the peer, ensuring that it
// will never be propagated to this particular peer.
func (p *peer) MarkTransaction(hash types.Hash) {
	p.knownTxs.Add(hash, nil)
}

// SendTransactions sends transactions to the peer and includes the hashes
// in its transaction hash set for future reference.
func (p *peer) SendTransactions(txs []*nom.AccountBlock) error {
	for _, tx := range txs {
		p.knownTxs.Add(tx.Hash, nil)
	}
	return p2p.Send(p.rw, TxMsg, txs)
}

// SendBlockHashes sends a batch of known hashes to the remote peer.
func (p *peer) SendBlockHashes(hashes []types.Hash) error {
	return p2p.Send(p.rw, BlockHashesMsg, hashes)
}

// SendBlocks sends a batch of blocks to the remote peer.
func (p *peer) SendBlocks(blocks []*nom.DetailedMomentum) error {
	// make sure timestamp-unix is present
	for _, block := range blocks {
		block.Momentum.EnsureCache()
	}
	var content nom.MomentumContent
	var accountBlocks []*nom.AccountBlock
	for _, block := range blocks {
		if block.Momentum.Height == 1 {
			content = block.Momentum.Content
			block.Momentum.Content = nil
			accountBlocks = block.AccountBlocks
			block.AccountBlocks = nil
		}
	}
	err := p2p.Send(p.rw, BlocksMsg, blocks)
	for _, block := range blocks {
		if block.Momentum.Height == 1 {
			block.Momentum.Content = content
			block.AccountBlocks = accountBlocks
		}
	}

	return err
}

// SendNewBlockHashes announces the availability of a number of blocks through
// a hash notification.
func (p *peer) SendNewBlockHashes(hashes []types.Hash) error {
	for _, hash := range hashes {
		p.knownBlocks.Add(hash, nil)
	}
	return p2p.Send(p.rw, NewBlockHashesMsg, hashes)
}

// SendNewMomentum propagates an entire block to a remote peer.
func (p *peer) SendNewMomentum(detailed *nom.DetailedMomentum) error {
	detailed.Momentum.EnsureCache()
	p.knownBlocks.Add(detailed.Momentum.Hash, nil)
	return p2p.Send(p.rw, NewBlockMsg, detailed)
}

// RequestHashes fetches a batch of hashes from a peer, starting at from, going
// towards the genesis block.
func (p *peer) RequestHashes(from types.Hash) error {
	log.Info("fetching hashes", "peer-id", p.id, "max-fetch", downloader.MaxHashFetch, "from", from[:4])
	return p2p.Send(p.rw, GetBlockHashesMsg, getBlockHashesData{from, uint64(downloader.MaxHashFetch)})
}

// RequestHashesFromNumber fetches a batch of hashes from a peer, starting at the
// requested block number, going upwards towards the genesis block.
func (p *peer) RequestHashesFromNumber(from uint64, count int) error {
	log.Info("fetching hashes", "peer-id", p.id, "num-fetch", count, "from", from)
	return p2p.Send(p.rw, GetBlockHashesFromNumberMsg, getBlockHashesFromNumberData{from, uint64(count)})
}

// RequestBlocks fetches a batch of blocks corresponding to the specified hashes.
func (p *peer) RequestBlocks(hashes []types.Hash) error {
	log.Info("fetching", "peer-id", p.id, "num-blocks", len(hashes))
	return p2p.Send(p.rw, GetBlocksMsg, hashes)
}

// Handshake executes the eth protocol handshake, negotiating version number,
// network IDs, difficulties, head and genesis blocks.
func (p *peer) Handshake(td uint64, head types.Hash, genesis types.Hash) error {
	// Send out own handshake in a new thread
	errc := make(chan error, 1)
	go func() {
		errc <- p2p.Send(p.rw, StatusMsg, &statusData{
			ProtocolVersion: uint32(p.version),
			NetworkId:       uint32(p.network),
			TD:              td,
			CurrentBlock:    head,
			GenesisBlock:    genesis,
		})
	}()
	// In the mean time retrieve the remote status message
	msg, err := p.rw.ReadMsg()
	if err != nil {
		return err
	}
	if msg.Code != StatusMsg {
		return errResp(ErrNoStatusMsg, "first msg has code %x (!= %x)", msg.Code, StatusMsg)
	}
	if msg.Size > ProtocolMaxMsgSize {
		return errResp(ErrMsgTooLarge, "%v > %v", msg.Size, ProtocolMaxMsgSize)
	}
	// Decode the handshake and make sure everything matches
	var status statusData
	if err := msg.Decode(&status); err != nil {
		return errResp(ErrDecode, "msg %v: %v", msg, err)
	}
	if status.GenesisBlock != genesis {
		return errResp(ErrGenesisBlockMismatch, "%x (!= %x)", status.GenesisBlock, genesis)
	}
	if int(status.NetworkId) != p.network {
		return errResp(ErrNetworkIdMismatch, "%d (!= %d)", status.NetworkId, p.network)
	}
	if int(status.ProtocolVersion) != p.version {
		return errResp(ErrProtocolVersionMismatch, "%d (!= %d)", status.ProtocolVersion, p.version)
	}
	// Configure the remote peer, and sanity check out handshake too
	p.td, p.head = status.TD, status.CurrentBlock
	return <-errc
}

// String implements fmt.Stringer.
func (p *peer) String() string {
	return fmt.Sprintf("Peer %s [%s]", p.id,
		fmt.Sprintf("eth/%2d", p.version),
	)
}

// peerSet represents the collection of active peers currently participating in
// the Ethereum sub-protocol.
type peerSet struct {
	peers map[string]*peer
	lock  sync.RWMutex
}

// newPeerSet creates a new peer set to track the active participants.
func newPeerSet() *peerSet {
	return &peerSet{
		peers: make(map[string]*peer),
	}
}

// Register injects a new peer into the working set, or returns an error if the
// peer is already known.
func (ps *peerSet) Register(p *peer) error {
	ps.lock.Lock()
	defer ps.lock.Unlock()

	if _, ok := ps.peers[p.id]; ok {
		return errAlreadyRegistered
	}
	ps.peers[p.id] = p
	return nil
}

// Unregister removes a remote peer from the active set, disabling any further
// actions to/from that particular entity.
func (ps *peerSet) Unregister(id string) error {
	ps.lock.Lock()
	defer ps.lock.Unlock()

	if _, ok := ps.peers[id]; !ok {
		return errNotRegistered
	}
	delete(ps.peers, id)
	return nil
}

// Peer retrieves the registered peer with the given id.
func (ps *peerSet) Peer(id string) *peer {
	ps.lock.RLock()
	defer ps.lock.RUnlock()

	return ps.peers[id]
}

// Len returns if the current number of peers in the set.
func (ps *peerSet) Len() int {
	ps.lock.RLock()
	defer ps.lock.RUnlock()

	return len(ps.peers)
}

// PeersWithoutBlock retrieves a list of peers that do not have a given block in
// their set of known hashes.
func (ps *peerSet) PeersWithoutBlock(hash types.Hash) []*peer {
	ps.lock.RLock()
	defer ps.lock.RUnlock()

	list := make([]*peer, 0, len(ps.peers))
	for _, p := range ps.peers {
		if !p.knownBlocks.Contains(hash) {
			list = append(list, p)
		}
	}
	return list
}

// PeersWithoutTx retrieves a list of peers that do not have a given transaction
// in their set of known hashes.
func (ps *peerSet) PeersWithoutTx(hash types.Hash) []*peer {
	ps.lock.RLock()
	defer ps.lock.RUnlock()

	list := make([]*peer, 0, len(ps.peers))
	for _, p := range ps.peers {
		if !p.knownTxs.Contains(hash) {
			list = append(list, p)
		}
	}
	return list
}

// BestPeer retrieves the known peer with the currently highest total difficulty.
func (ps *peerSet) BestPeer() *peer {
	ps.lock.RLock()
	defer ps.lock.RUnlock()

	var (
		bestPeer *peer
		bestTd   uint64
	)
	for _, p := range ps.peers {
		if td := p.Td(); bestPeer == nil || bestTd < td {
			bestPeer, bestTd = p, td
		}
	}
	return bestPeer
}
