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

// Package downloader contains the manual full chain synchronisation.
package downloader

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang-collections/collections/set"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

const (
	eth61 = 61 // Constant to check for new protocol support
)

var (
	log = common.DownloaderLogger

	MinHashFetch  = 512 // Minimum amount of hashes to not consider a peer stalling
	MaxHashFetch  = 512 // Amount of hashes to be fetched per retrieval request
	MaxBlockFetch = 128 // Amount of blocks to be fetched per retrieval request

	hashTTL         = 5 * time.Second  // Time it takes for a hash request to time out
	blockSoftTTL    = 3 * time.Second  // Request completion threshold for increasing or decreasing a peer's bandwidth
	blockHardTTL    = 3 * blockSoftTTL // Maximum time allowance before a block request is considered expired
	crossCheckCycle = time.Second      // Period after which to check for expired cross checks

	maxQueuedHashes = 256 * 1024 // Maximum number of hashes to queue for import (DOS protection)
	maxBannedHashes = 4096       // Number of bannable hashes before phasing old ones out
	maxBlockProcess = 256        // Number of blocks to import at once into the chain
)

var (
	errBusy             = errors.New("busy")
	errUnknownPeer      = errors.New("peer is unknown or unhealthy")
	errBadPeer          = errors.New("action from bad peer ignored")
	errStallingPeer     = errors.New("peer is stalling")
	errBannedHead       = errors.New("peer head hash already banned")
	errNoPeers          = errors.New("no peers to keep download active")
	errPendingQueue     = errors.New("pending items in queue")
	errTimeout          = errors.New("timeout")
	errEmptyHashSet     = errors.New("empty hash set by peer")
	errPeersUnavailable = errors.New("no peers available or all peers tried for block download process")
	errAlreadyInPool    = errors.New("hash already in pool")
	errInvalidChain     = errors.New("retrieved hash chain is invalid")
	errCrossCheckFailed = errors.New("block cross-check failed")
	errCancelHashFetch  = errors.New("hash fetching canceled (requested)")
	errCancelBlockFetch = errors.New("block downloading canceled (requested)")
	errNoSyncActive     = errors.New("no sync active")
)

// hashCheckFn is a callback type for verifying a hash's presence in the local chain.
type hashCheckFn func(types.Hash) bool

// blockRetrievalFn is a callback type for retrieving a block from the local chain.
type blockRetrievalFn func(types.Hash) *nom.DetailedMomentum

// headRetrievalFn is a callback type for retrieving the head block from the local chain.
type headRetrievalFn func() *nom.Momentum

// chainInsertFn is a callback type to insert a batch of blocks into the local chain.
type chainInsertFn func([]*nom.DetailedMomentum) (int, error)

// peerDropFn is a callback type for dropping a peer detected as malicious.
type peerDropFn func(id string)

type blockPack struct {
	peerId string
	blocks []*nom.DetailedMomentum
}

type hashPack struct {
	peerId string
	hashes []types.Hash
}

type crossCheck struct {
	expire time.Time
	parent types.Hash
}

type Downloader struct {
	queue  *queue                     // Scheduler for selecting the hashes to download
	peers  *peerSet                   // Set of active peers from which download can proceed
	checks map[types.Hash]*crossCheck // Pending cross checks to verify a hash chain
	banned *set.Set                   // Set of hashes we've received and banned

	interrupt int32 // Atomic boolean to signal termination

	// Statistics
	importStart time.Time // Instance when the last blocks were taken from the cache
	importQueue []*Block  // Previously taken blocks to check import progress
	importDone  int       // Number of taken blocks already imported from the last batch
	importLock  sync.Mutex

	// Callbacks
	hasBlock    hashCheckFn      // Checks if a block is present in the chain
	getBlock    blockRetrievalFn // Retrieves a block from the chain
	headBlock   headRetrievalFn  // Retrieves the head block from the chain
	insertChain chainInsertFn    // Injects a batch of blocks into the chain
	dropPeer    peerDropFn       // Drops a peer for misbehaving

	// Status
	synchroniseMock func(id string, hash types.Hash) error // Replacement for synchronise during testing
	synchronising   int32
	processing      int32
	notified        int32

	// Channels
	newPeerCh chan *peer
	hashCh    chan hashPack  // Channel receiving inbound hashes
	blockCh   chan blockPack // Channel receiving inbound blocks
	processCh chan bool      // Channel to signal the block fetcher of new or finished work

	cancelCh   chan struct{} // Channel to cancel mid-flight syncs
	cancelLock sync.RWMutex  // Lock to protect the cancel channel in delivers
}

// Block is an origin-tagged blockchain block.
type Block struct {
	RawBlock   *nom.DetailedMomentum
	OriginPeer string
}

// New creates a new downloader to fetch hashes and blocks from remote peers.
func New(hasBlock hashCheckFn, getBlock blockRetrievalFn, headBlock headRetrievalFn, insertChain chainInsertFn, dropPeer peerDropFn) *Downloader {
	// Create the base downloader
	downloader := &Downloader{
		queue:       newQueue(),
		peers:       newPeerSet(),
		hasBlock:    hasBlock,
		getBlock:    getBlock,
		headBlock:   headBlock,
		insertChain: insertChain,
		dropPeer:    dropPeer,
		newPeerCh:   make(chan *peer, 1),
		hashCh:      make(chan hashPack, 1),
		blockCh:     make(chan blockPack, 1),
		processCh:   make(chan bool, 1),
	}
	// Inject all the known bad hashes
	downloader.banned = set.New()

	return downloader
}

// Stats retrieves the current status of the downloader.
func (d *Downloader) Stats() (pending int, cached int, importing int, estimate time.Duration) {
	// Fetch the download status
	pending, cached = d.queue.Size()

	// Figure out the import progress
	d.importLock.Lock()
	defer d.importLock.Unlock()

	for len(d.importQueue) > 0 && d.hasBlock(d.importQueue[0].RawBlock.Momentum.Hash) {
		d.importQueue = d.importQueue[1:]
		d.importDone++
	}
	importing = len(d.importQueue)

	// Make an estimate on the total sync
	estimate = 0
	if d.importDone > 0 {
		estimate = time.Since(d.importStart) / time.Duration(d.importDone) * time.Duration(pending+cached+importing)
	}
	return
}

// Synchronising returns whether the downloader is currently retrieving blocks.
func (d *Downloader) Synchronising() bool {
	return atomic.LoadInt32(&d.synchronising) > 0
}

// RegisterPeer injects a new download peer into the set of block source to be
// used for fetching hashes and blocks from.
func (d *Downloader) RegisterPeer(id string, version int, head types.Hash, getRelHashes relativeHashFetcherFn, getAbsHashes absoluteHashFetcherFn, getBlocks blockFetcherFn) error {
	// If the peer wants to send a banned hash, reject
	if d.banned.Has(head) {
		log.Debug("Register rejected, head hash banned:", id)
		return errBannedHead
	}
	// Otherwise try to construct and register the peer
	log.Debug("Registering peer", id)
	if err := d.peers.Register(newPeer(id, version, head, getRelHashes, getAbsHashes, getBlocks)); err != nil {
		log.Error("Register failed", "reason", err)
		return err
	}
	return nil
}

// UnregisterPeer remove a peer from the known list, preventing any action from
// the specified peer.
func (d *Downloader) UnregisterPeer(id string) error {
	log.Debug("Unregistering peer", id)
	if err := d.peers.Unregister(id); err != nil {
		log.Error("Unregister failed", "reason", err)
		return err
	}
	return nil
}

// Synchronise tries to sync up our local block chain with a remote peer, both
// adding various sanity checks as well as wrapping it with various log entries.
func (d *Downloader) Synchronise(id string, head types.Hash, td uint64) {
	log.Debug("Attempting synchronisation", "peer-id", id, "head", head, "TD", td)

	switch err := d.synchronise(id, head, td); err {
	case nil:
		log.Debug("Synchronisation completed")

	case errBusy:
		log.Debug("Synchronisation already in progress")

	case errTimeout, errBadPeer, errStallingPeer, errBannedHead, errEmptyHashSet, errPeersUnavailable, errInvalidChain, errCrossCheckFailed:
		log.Info("Removing peer", "peer-id", id, "reason", err)
		d.dropPeer(id)

	case errPendingQueue:
		log.Debug("Synchronisation aborted", "reason", err)

	default:
		log.Warn("Synchronisation failed", "reason", err)
	}
}

// synchronise will select the peer and use it for synchronising. If an empty string is given
// it will use the best peer possible and synchronize if it's TD is higher than our own. If any of the
// checks fail an error will be returned. This method is synchronous
func (d *Downloader) synchronise(id string, hash types.Hash, td uint64) error {
	// Mock out the synchonisation if testing
	if d.synchroniseMock != nil {
		return d.synchroniseMock(id, hash)
	}

	// Make sure only one goroutine is ever allowed past this point at once
	if !atomic.CompareAndSwapInt32(&d.synchronising, 0, 1) {
		return errBusy
	}
	defer atomic.StoreInt32(&d.synchronising, 0)

	// If the head hash is banned, terminate immediately
	if d.banned.Has(hash) {
		return errBannedHead
	}
	// Post a user notification of the sync (only once per session)
	if atomic.CompareAndSwapInt32(&d.notified, 0, 1) {
		log.Info("Block synchronisation started", "peer-id", id, "hash", hash, "td", td)
	}

	// Abort if the queue still contains some leftover data
	if _, cached := d.queue.Size(); cached > 0 && d.queue.GetHeadBlock() != nil {
		return errPendingQueue
	}
	// Reset the queue and peer set to clean any internal leftover state
	d.queue.Reset()
	d.peers.Reset()
	d.checks = make(map[types.Hash]*crossCheck)

	// Create cancel channel for aborting mid-flight
	d.cancelLock.Lock()
	d.cancelCh = make(chan struct{})
	d.cancelLock.Unlock()

	// Retrieve the origin peer and initiate the downloading process
	p := d.peers.Peer(id)
	if p == nil {
		return errUnknownPeer
	}
	return d.syncWithPeer(p, hash, td)
}

// Has checks if the downloader knows about a particular hash, meaning that its
// either already downloaded of pending retrieval.
func (d *Downloader) Has(hash types.Hash) bool {
	return d.queue.Has(hash)
}

// syncWithPeer starts a block synchronization based on the hash chain from the
// specified peer and head hash.
func (d *Downloader) syncWithPeer(p *peer, hash types.Hash, td uint64) (err error) {
	defer func() {
		if err != nil {
			d.cancel()
		}
	}()

	log.Info("Synchronizing with the zenon network", "peer-id", p.id, "version", p.version)
	switch p.version {
	case eth61:
		// New eth/61, use forward, concurrent hash and block retrieval algorithm
		number, err := d.findAncestor(p)
		if err != nil {
			return err
		}
		errc := make(chan error, 2)
		go func() { errc <- d.fetchHashes(p, td, number+1) }()
		go func() { errc <- d.fetchBlocks(number + 1) }()

		// If any fetcher fails, cancel the other
		if err := <-errc; err != nil {
			d.cancel()
			<-errc
			return err
		}
		log.Info("Synchronization completed")
		return <-errc

	default:
		// Something very wrong, stop right here
		log.Error("Unsupported zenon protocol", "version", p.version)
		return errBadPeer
	}
}

// cancel cancels all of the operations and resets the queue. It returns true
// if the cancel operation was completed.
func (d *Downloader) cancel() {
	// Close the current cancel channel
	d.cancelLock.Lock()
	if d.cancelCh != nil {
		select {
		case <-d.cancelCh:
			// Channel was already closed
		default:
			close(d.cancelCh)
		}
	}
	d.cancelLock.Unlock()

	// Reset the queue
	d.queue.Reset()
}

// Terminate interrupts the downloader, canceling all pending operations.
func (d *Downloader) Terminate() {
	atomic.StoreInt32(&d.interrupt, 1)
	d.cancel()
}

// findAncestor tries to locate the common ancestor block of the local chain and
// a remote peers blockchain. In the general case when our node was in sync and
// on the correct chain, checking the top N blocks should already get us a match.
// In the rare scenario when we ended up on a long soft fork (i.e. none of the
// head blocks match), we do a binary search to find the common ancestor.
func (d *Downloader) findAncestor(p *peer) (uint64, error) {
	log.Info("looking for common ancestor", "peer", p)

	// Request out head blocks to short circuit ancestor location
	head := d.headBlock().Height
	from := int64(head) - int64(MaxHashFetch)
	if from < 0 {
		from = 0
	}
	go p.getAbsHashes(uint64(from), MaxHashFetch)

	// Wait for the remote response to the head fetch
	number, hash := uint64(0), types.Hash{}
	timeout := time.After(hashTTL)

	for finished := false; !finished; {
		select {
		case <-d.cancelCh:
			return 0, errCancelHashFetch

		case hashPack := <-d.hashCh:
			// Discard anything not from the origin peer
			if hashPack.peerId != p.id {
				log.Info("Received hashes from incorrect peer", "peer ID", hashPack.peerId)
				break
			}
			// Make sure the peer actually gave something valid
			hashes := hashPack.hashes
			if len(hashes) == 0 {
				log.Info("%v: empty head hash set", "peer", p)
				return 0, errEmptyHashSet
			}
			// Check if a common ancestor was found
			finished = true
			for i := len(hashes) - 1; i >= 0; i-- {
				if d.hasBlock(hashes[i]) {
					number, hash = uint64(from)+uint64(i), hashes[i]
					break
				}
			}

		case <-d.blockCh:
			// Out of bounds blocks received, ignore them

		case <-timeout:
			log.Info("head hash timeout", "peer", p)
			return 0, errTimeout
		}
	}
	// If the head fetch already found an ancestor, return
	if hash.IsZero() {
		log.Info("common ancestor", "peer", p, "number", number, "hash", hash[:4])
		return number, nil
	}
	// Ancestor not found, we need to binary search over our chain
	start, end := uint64(0), head
	for start+1 < end {
		// Split our chain interval in two, and request the hash to cross check
		check := (start + end) / 2

		timeout := time.After(hashTTL)
		go p.getAbsHashes(uint64(check), 1)

		// Wait until a reply arrives to this request
		for arrived := false; !arrived; {
			select {
			case <-d.cancelCh:
				return 0, errCancelHashFetch

			case hashPack := <-d.hashCh:
				// Discard anything not from the origin peer
				if hashPack.peerId != p.id {
					log.Info("Received hashes from incorrect peer", "peer ID", hashPack.peerId)
					break
				}
				// Make sure the peer actually gave something valid
				hashes := hashPack.hashes
				if len(hashes) != 1 {
					log.Info("invalid search hash set", "peer", p, "num-hashes", len(hashes))
					return 0, errBadPeer
				}
				arrived = true

				// Modify the search interval based on the response
				detailed := d.getBlock(hashes[0])
				if detailed == nil {
					end = check
					break
				}
				block := detailed.Momentum
				if block.Height != check {
					log.Info("non requested hash", "peer", p, "momentum-height", block.Height, "momentum-hash", block.Hash.Bytes()[:4], "wanted-height", check)
					return 0, errBadPeer
				}
				start = check

			case <-d.blockCh:
				// Out of bounds blocks received, ignore them

			case <-timeout:
				log.Info("search hash timeout", "peer", p)
				return 0, errTimeout
			}
		}
	}
	return start, nil
}

// fetchHashes keeps retrieving hashes from the requested number, until no more
// are returned, potentially throttling on the way.
func (d *Downloader) fetchHashes(p *peer, td uint64, from uint64) error {
	log.Info("%downloading hashes from", "peer", p, "from-height", from)

	// Create a timeout timer, and the associated hash fetcher
	timeout := time.NewTimer(0) // timer to dump a non-responsive active peer
	<-timeout.C                 // timeout channel should be initially empty
	defer timeout.Stop()

	getHashes := func(from uint64) {
		log.Debug("fetching hashes", "peer", p, MaxHashFetch, from)

		go p.getAbsHashes(from, MaxHashFetch)
		timeout.Reset(hashTTL)
	}
	// Start pulling hashes, until all are exhausted
	getHashes(from)
	//gotHashes := false

	for {
		select {
		case <-d.cancelCh:
			return errCancelHashFetch

		case hashPack := <-d.hashCh:
			// Make sure the active peer is giving us the hashes
			if hashPack.peerId != p.id {
				log.Info("Received hashes from incorrect peer", "peer ID", hashPack.peerId)
				break
			}
			timeout.Stop()

			// If no more hashes are inbound, notify the block fetcher and return
			if len(hashPack.hashes) == 0 {
				log.Info("no available hashes", "peer", p)

				select {
				case d.processCh <- false:
				case <-d.cancelCh:
				}
				// If no hashes were retrieved at all, the peer violated it's TD promise that it had a
				// better chain compared to ours. The only exception is if it's promised blocks were
				// already imported by other means (e.g. fecher):
				//
				// R <remote peer>, L <local node>: Both at block 10
				// R: Mine block 11, and propagate it to L
				// L: Queue block 11 for import
				// L: Notice that R's head and TD increased compared to ours, start sync
				// L: Import of block 11 finishes
				// L: Sync begins, and finds common ancestor at 11
				// L: Request new hashes up from 11 (R's TD was higher, it must have something)
				// R: Nothing to give

				// TODO: fix this
				//if !gotHashes && td.Cmp(d.headBlock().Td) > 0 {
				//	return errStallingPeer
				//}
				return nil
			}
			//gotHashes = true

			// Otherwise insert all the new hashes, aborting in case of junk
			log.Debug("inserting momentums", "peer", p, "num-momentums", len(hashPack.hashes), "from-height", from)

			inserts := d.queue.Insert(hashPack.hashes, true)
			if len(inserts) != len(hashPack.hashes) {
				log.Info("stale hashes", "peer", p)
				return errBadPeer
			}
			// Notify the block fetcher of new hashes, but stop if queue is full
			cont := d.queue.Pending() < maxQueuedHashes
			select {
			case d.processCh <- cont:
			default:
			}
			if !cont {
				return nil
			}
			// Queue not yet full, fetch the next batch
			from += uint64(len(hashPack.hashes))
			getHashes(from)

		case <-timeout.C:
			log.Info("hash request timed out", "peer", p)
			return errTimeout
		}
	}
}

// fetchBlocks iteratively downloads the scheduled hashes, taking any available
// peers, reserving a chunk of blocks for each, waiting for delivery and also
// periodically checking for timeouts.
func (d *Downloader) fetchBlocks(from uint64) error {
	log.Info("Downloading momentums", "from-height", from)
	defer log.Info("Block download terminated", "")

	// Create a timeout timer for scheduling expiration tasks
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	update := make(chan struct{}, 1)

	// Prepare the queue and fetch blocks until the hash fetcher's done
	d.queue.Prepare(from)
	finished := false

	for {
		select {
		case <-d.cancelCh:
			return errCancelBlockFetch

		case blockPack := <-d.blockCh:
			// If the peer was previously banned and failed to deliver it's pack
			// in a reasonable time frame, ignore it's message.
			if peer := d.peers.Peer(blockPack.peerId); peer != nil {
				// Deliver the received chunk of blocks, and demote in case of errors
				err := d.queue.Deliver(blockPack.peerId, blockPack.blocks)
				switch err {
				case nil:
					// If no blocks were delivered, demote the peer (need the delivery above)
					if len(blockPack.blocks) == 0 {
						peer.Demote()
						peer.SetIdle()
						log.Debug("no blocks delivered", "peer", peer)
						break
					}
					// All was successful, promote the peer and potentially start processing
					peer.Promote()
					peer.SetIdle()
					log.Debug("delivered blocks", "peer", peer, "num-blocks", len(blockPack.blocks))
					go d.process()

				case errInvalidChain:
					// The hash chain is invalid (blocks are not ordered properly), abort
					return err

				case errNoFetchesPending:
					// Peer probably timed out with its delivery but came through
					// in the end, demote, but allow to to pull from this peer.
					peer.Demote()
					peer.SetIdle()
					log.Debug("out of bound delivery", "peer", peer)

				case errStaleDelivery:
					// Delivered something completely else than requested, usually
					// caused by a timeout and delivery during a new sync cycle.
					// Don't set it to idle as the original request should still be
					// in flight.
					peer.Demote()
					log.Debug("stale delivery", "peer", "peer", peer)

				default:
					// Peer did something semi-useful, demote but keep it around
					peer.Demote()
					peer.SetIdle()
					log.Debug("delivery partially failed", "peer", peer, "reason", err)
					go d.process()
				}
			}
			// Blocks arrived, try to update the progress
			select {
			case update <- struct{}{}:
			default:
			}

		case cont := <-d.processCh:
			// The hash fetcher sent a continuation flag, check if it's done
			if !cont {
				finished = true
			}
			// Hashes arrive, try to update the progress
			select {
			case update <- struct{}{}:
			default:
			}

		case <-ticker.C:
			// Sanity check update the progress
			select {
			case update <- struct{}{}:
			default:
			}

		case <-update:
			// Short circuit if we lost all our peers
			if d.peers.Len() == 0 {
				return errNoPeers
			}
			// Check for block request timeouts and demote the responsible peers
			for _, pid := range d.queue.Expire(blockHardTTL) {
				if peer := d.peers.Peer(pid); peer != nil {
					peer.Demote()
					log.Debug("Block delivery timeout", "peer", peer)
				}
			}
			// If there's noting more to fetch, wait or terminate
			if d.queue.Pending() == 0 {
				if d.queue.InFlight() == 0 && finished {
					log.Info("Block fetching completed", "")
					return nil
				}
				break
			}
			// Send a download request to all idle peers, until throttled
			for _, peer := range d.peers.IdlePeers() {
				// Short circuit if throttling activated
				if d.queue.Throttle() {
					break
				}
				// Reserve a chunk of hashes for a peer. A nil can mean either that
				// no more hashes are available, or that the peer is known not to
				// have them.
				request := d.queue.Reserve(peer, peer.Capacity())
				if request == nil {
					continue
				}
				log.Debug("requesting blocks", "peer", peer, "num-blocks", len(request.Hashes))
				// Fetch the chunk and make sure any errors return the hashes to the queue
				if err := peer.Fetch(request); err != nil {
					log.Error("fetch failed, rescheduling", "peer", peer)
					d.queue.Cancel(request)
				}
			}
			// Make sure that we have peers available for fetching. If all peers have been tried
			// and all failed throw an error
			if !d.queue.Throttle() && d.queue.InFlight() == 0 {
				return errPeersUnavailable
			}
		}
	}
}

// process takes blocks from the queue and tries to import them into the chain.
//
// The algorithmic flow is as follows:
//  - The `processing` flag is swapped to 1 to ensure singleton access
//  - The current `cancel` channel is retrieved to detect sync abortions
//  - Blocks are iteratively taken from the cache and inserted into the chain
//  - When the cache becomes empty, insertion stops
//  - The `processing` flag is swapped back to 0
//  - A post-exit check is made whether new blocks became available
//     - This step is important: it handles a potential race condition between
//       checking for no more work, and releasing the processing "mutex". In
//       between these state changes, a block may have arrived, but a processing
//       attempt denied, so we need to re-enter to ensure the block isn't left
//       to idle in the cache.
func (d *Downloader) process() {
	// Make sure only one goroutine is ever allowed to process blocks at once
	if !atomic.CompareAndSwapInt32(&d.processing, 0, 1) {
		return
	}
	// If the processor just exited, but there are freshly pending items, try to
	// reenter. This is needed because the goroutine spinned up for processing
	// the fresh blocks might have been rejected entry to to this present thread
	// not yet releasing the `processing` state.
	defer func() {
		if atomic.LoadInt32(&d.interrupt) == 0 && d.queue.GetHeadBlock() != nil {
			d.process()
		}
	}()
	// Release the lock upon exit (note, before checking for reentry!), and set
	// the import statistics to zero.
	defer func() {
		d.importLock.Lock()
		d.importQueue = nil
		d.importDone = 0
		d.importLock.Unlock()

		atomic.StoreInt32(&d.processing, 0)
	}()
	// Repeat the processing as long as there are blocks to import
	for {
		// Fetch the next batch of blocks
		blocks := d.queue.TakeBlocks()
		if len(blocks) == 0 {
			return
		}
		// Reset the import statistics
		d.importLock.Lock()
		d.importStart = time.Now()
		d.importQueue = blocks
		d.importDone = 0
		d.importLock.Unlock()

		// Actually import the blocks
		log.Info("Inserting side-chain", "num-account-blocks", len(blocks), "start-height", blocks[0].RawBlock.Momentum.Height, "end-height", blocks[len(blocks)-1].RawBlock.Momentum.Height)
		for len(blocks) != 0 {
			// Check for any termination requests
			if atomic.LoadInt32(&d.interrupt) == 1 {
				return
			}
			// Retrieve the first batch of blocks to insert
			max := int(math.Min(float64(len(blocks)), float64(maxBlockProcess)))
			raw := make([]*nom.DetailedMomentum, 0, max)
			for _, block := range blocks[:max] {
				raw = append(raw, block.RawBlock)
			}
			// Try to inset the blocks, drop the originating peer if there's an error
			index, err := d.insertChain(raw)
			if err != nil {
				log.Info("Block import failed", "momentum-height", raw[index].Momentum.Height, "reason", err)
				d.dropPeer(blocks[index].OriginPeer)
				d.cancel()
				return
			}
			blocks = blocks[max:]
		}
	}
}

// DeliverBlocks injects a new batch of blocks received from a remote node.
// This is usually invoked through the BlocksMsg by the protocol handler.
func (d *Downloader) DeliverBlocks(id string, blocks []*nom.DetailedMomentum) error {
	// Make sure the downloader is active
	if atomic.LoadInt32(&d.synchronising) == 0 {
		return errNoSyncActive
	}
	// Deliver or abort if the sync is canceled while queuing
	d.cancelLock.RLock()
	cancel := d.cancelCh
	d.cancelLock.RUnlock()

	select {
	case d.blockCh <- blockPack{id, blocks}:
		return nil

	case <-cancel:
		return errNoSyncActive
	}
}

// DeliverHashes injects a new batch of hashes received from a remote node into
// the download schedule. This is usually invoked through the BlockHashesMsg by
// the protocol handler.
func (d *Downloader) DeliverHashes(id string, hashes []types.Hash) error {
	// Make sure the downloader is active
	if atomic.LoadInt32(&d.synchronising) == 0 {
		return errNoSyncActive
	}
	// Deliver or abort if the sync is canceled while queuing
	d.cancelLock.RLock()
	cancel := d.cancelCh
	d.cancelLock.RUnlock()

	select {
	case d.hashCh <- hashPack{id, hashes}:
		return nil

	case <-cancel:
		return errNoSyncActive
	}
}
