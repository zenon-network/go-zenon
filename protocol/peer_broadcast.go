package protocol

import (
	"bytes"
	"sync"
	"sync/atomic"

	"github.com/zenon-network/go-zenon/p2p"
)

// maxQueuedBroadcasts bounds the broadcasts waiting for one peer's
// writer. Healthy peers drain the queue as fast as the producer fills it;
// a peer that stops reading is dropped by the transport once a write
// outlasts its 20 s deadline, so the queue only has to absorb a burst or
// a slow reader, not hold a dead peer's backlog.
const maxQueuedBroadcasts = 1024

// maxQueuedBroadcastBytes bounds the encoded bytes of the broadcasts
// queued for one peer, counting the one being written, so a slow peer
// cannot pin maxQueuedBroadcasts large messages (account blocks with
// large data, or momentums carrying them). A message is admitted past
// the budget only when nothing else is queued, so one larger than the
// budget still goes out; the transports cap a message at 10 MiB. Peers
// share a broadcast's encoding, so the heap the queues pin is bounded by
// the broadcasts the slowest peer holds, not by the number of peers.
const maxQueuedBroadcastBytes = 16 << 20

// broadcastMsg is one encoded broadcast waiting in a peer's queue. The
// payload is encoded once by the broadcaster and shared, read-only, by
// every peer it is queued for.
type broadcastMsg struct {
	code    uint64
	payload []byte
}

// newBroadcastMsg returns a broadcast of the given message code whose
// payload is the already RLP-encoded message body.
func newBroadcastMsg(code uint64, payload []byte) broadcastMsg {
	return broadcastMsg{code: code, payload: payload}
}

// msg returns a p2p.Msg for one write of m. Each call gets its own
// reader over the shared payload, so the payload is never consumed.
func (m broadcastMsg) msg() p2p.Msg {
	return p2p.Msg{Code: m.code, Size: uint32(len(m.payload)), Payload: bytes.NewReader(m.payload)}
}

// enqueueResult is what happened to a broadcast handed to a peer.
type enqueueResult int

const (
	// enqueued: the broadcast is in the queue and the writer sends it
	// after everything queued before it.
	enqueued enqueueResult = iota
	// queueFull: the queue was full, by count or by bytes, and the
	// broadcast was not queued.
	queueFull
	// queueClosed: the peer's writer has stopped (or never started) and
	// the broadcast was not queued.
	queueClosed
)

// broadcastQueue holds the broadcasts waiting to be written to one peer
// and the state of the goroutine that writes them. Broadcasts are queued
// without blocking and written by that goroutine in queue order, so the
// broadcaster (the pillar worker, while it holds its working lock, or the
// fetcher) never waits on a peer's transport; see issue #262.
//
// Only broadcasts go through the queue. Request replies, requests and the
// initial transaction sync are still written directly by the goroutines
// that make them, as before: those were never ordered with respect to
// broadcasts, which came from another goroutine.
type broadcastQueue struct {
	queue chan broadcastMsg
	// bytes is the encoded size of what is queued or being written;
	// maxBytes bounds it (see maxQueuedBroadcastBytes).
	bytes    atomic.Int64
	maxBytes int64
	// term is closed by stop; done is closed when the writer has returned.
	term chan struct{}
	done chan struct{}

	// mu guards stopped, started and the start of the disconnect
	// goroutine, which wg joins.
	mu      sync.Mutex
	stopped bool
	started bool
	wg      sync.WaitGroup
	// disconnecting is set once the peer is being disconnected for an
	// overflow; the writer then discards what is queued.
	disconnecting atomic.Bool
}

// newBroadcastQueue returns an empty queue, writer not started, that holds at most size
// broadcasts and maxBytes of their encoded payloads; see
// maxQueuedBroadcasts and maxQueuedBroadcastBytes.
func newBroadcastQueue(size int, maxBytes int64) *broadcastQueue {
	return &broadcastQueue{
		queue:    make(chan broadcastMsg, size),
		maxBytes: maxBytes,
		term:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// enqueue queues m for the writer without blocking.
func (q *broadcastQueue) enqueue(m broadcastMsg) enqueueResult {
	if q == nil {
		return queueClosed
	}
	select {
	case <-q.term:
		return queueClosed
	default:
	}
	// Reserve the bytes first, so concurrent broadcasters cannot both
	// pass the budget check; give them back if the queue is full by count.
	n := int64(len(m.payload))
	for {
		cur := q.bytes.Load()
		if cur > 0 && cur+n > q.maxBytes {
			return queueFull
		}
		if q.bytes.CompareAndSwap(cur, cur+n) {
			break
		}
	}
	select {
	case q.queue <- m:
		return enqueued
	default:
		q.bytes.Add(-n)
		return queueFull
	}
}

// requestDisconnect disconnects p, once, after a broadcast that must not
// be lost (an account block) found its queue full. The disconnect runs on
// its own goroutine, joined by stopBroadcast, so the broadcaster does not
// wait on the transport, and it does not wait on the writer either, which
// may be stalled in a write the disconnect is what cuts short.
func (p *peer) requestDisconnect() {
	q := p.broadcasts
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopped || q.disconnecting.Load() {
		return
	}
	q.disconnecting.Store(true)
	log.Info("disconnecting peer whose broadcast queue overflowed", "peer-id", p.id, "queued", len(q.queue))
	q.wg.Add(1)
	go func() {
		defer q.wg.Done()
		p.Peer.Disconnect(p2p.DiscUselessPeer)
	}()
}

// startBroadcast starts the goroutine that writes p's queued broadcasts.
// The caller must call stopBroadcast, which joins it.
func (p *peer) startBroadcast() {
	q := p.broadcasts
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.started || q.stopped {
		return
	}
	q.started = true
	go p.broadcastLoop(q)
}

// stopBroadcast stops p's broadcast writer and waits for it, and for an
// overflow disconnect in progress, to return. Broadcasts still queued are
// dropped, and later ones are refused. A write in progress is not
// interrupted here: the caller disconnects the peer first, and the
// transport cuts a stalled write short when it closes the stream.
// stopBroadcast is idempotent.
func (p *peer) stopBroadcast() {
	q := p.broadcasts
	if q == nil {
		return
	}
	q.mu.Lock()
	started := q.started
	if !q.stopped {
		q.stopped = true
		close(q.term)
	}
	q.mu.Unlock()
	if started {
		<-q.done
	}
	q.wg.Wait()
}

// broadcastLoop writes p's queued broadcasts in order until stopBroadcast.
// Once the peer is being disconnected for an overflow it discards what is
// queued instead of writing it: the connection is going away, and the
// writes could only delay its teardown.
func (p *peer) broadcastLoop(q *broadcastQueue) {
	defer close(q.done)
	for {
		// Prefer stopping over writing what is queued.
		select {
		case <-q.term:
			return
		default:
		}
		select {
		case <-q.term:
			return
		case m := <-q.queue:
			if !q.disconnecting.Load() {
				if err := p.rw.WriteMsg(m.msg()); err != nil {
					log.Debug("failed to write broadcast", "peer-id", p.id, "code", m.code, "reason", err)
				}
			}
			// Released once written, so the budget covers the write too.
			q.bytes.Add(-int64(len(m.payload)))
		}
	}
}
