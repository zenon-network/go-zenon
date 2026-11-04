package protocol

import (
	"errors"
	"sync"
	"time"

	"github.com/zenon-network/go-zenon/common"
)

const (
	// peerQuarantinePeriod is how long a peer that served a side-chain
	// rejected on a consensus check is refused after the rejection. It
	// bounds the download-and-reject cycle such a peer can cause to one
	// attempt per period instead of one per sync interval.
	peerQuarantinePeriod = time.Hour
	// peerQuarantineCap bounds the quarantine; the entry expiring
	// soonest is evicted when it is full.
	peerQuarantineCap = 1024
)

var errPeerQuarantined = errors.New("peer is quarantined after serving a rejected side-chain")

// peerQuarantine remembers peer ids to refuse, each until an expiry read
// from common.Clock.
type peerQuarantine struct {
	mu    sync.Mutex
	ttl   time.Duration
	cap   int
	until map[string]time.Time
}

func newPeerQuarantine(ttl time.Duration, cap int) *peerQuarantine {
	return &peerQuarantine{ttl: ttl, cap: cap, until: make(map[string]time.Time)}
}

// add quarantines id for ttl from now, refreshing an existing entry.
func (q *peerQuarantine) add(id string) {
	now := common.Clock.Now()
	q.mu.Lock()
	defer q.mu.Unlock()
	for peer, until := range q.until {
		if !now.Before(until) {
			delete(q.until, peer)
		}
	}
	if _, known := q.until[id]; !known && len(q.until) >= q.cap {
		var soonest string
		var soonestUntil time.Time
		for peer, until := range q.until {
			if soonestUntil.IsZero() || until.Before(soonestUntil) {
				soonest, soonestUntil = peer, until
			}
		}
		delete(q.until, soonest)
	}
	q.until[id] = now.Add(q.ttl)
}

// active reports whether id is quarantined and the quarantine has not
// expired.
func (q *peerQuarantine) active(id string) bool {
	now := common.Clock.Now()
	q.mu.Lock()
	defer q.mu.Unlock()
	until, ok := q.until[id]
	if !ok {
		return false
	}
	if !now.Before(until) {
		delete(q.until, id)
		return false
	}
	return true
}

func (q *peerQuarantine) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.until)
}
