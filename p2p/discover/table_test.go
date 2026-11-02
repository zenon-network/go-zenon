package discover

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// heldPingTransport answers every ping only once the test releases it, so a
// sweep can run while the ping is outstanding.
type heldPingTransport struct {
	started chan NodeID
	release chan error
	onClose func() // optional, run by close
}

func (tr *heldPingTransport) ping(id NodeID, _ *net.UDPAddr) error {
	tr.started <- id
	return <-tr.release
}

func (tr *heldPingTransport) waitping(NodeID) error { return nil }

func (tr *heldPingTransport) findnode(NodeID, *net.UDPAddr, NodeID) ([]*Node, error) {
	return nil, nil
}

func (tr *heldPingTransport) close() {
	if tr.onClose != nil {
		tr.onClose()
	}
}

// fullBucket returns a bucket at capacity whose oldest entry is the last one.
func fullBucket(t *testing.T) *bucket {
	t.Helper()
	b := new(bucket)
	for i := 0; i < bucketSize; i++ {
		b.entries = append(b.entries, newNode(newTestNodeID(t), net.IPv4(10, 0, 0, byte(i+1)), 30303, 30303))
	}
	return b
}

// A bucket refresh pings the oldest entry. If its stored pong is already past
// the expiration window and a sweep runs while the ping is outstanding, the
// sweep deletes the record. A successful refresh must then write the record
// again, next to the fresh pong, or the node is kept in memory but is no
// longer offered as a seed after a restart. A failed refresh must not bring
// the deleted record back. Fails if pingreplace stops rewriting the record of
// a node that answered the refresh ping.
func TestPingReplaceRestoresRecordDeletedDuringRefresh(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pingErr error
	}{
		{"answered refresh", nil},
		{"failed refresh", errors.New("timeout")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := &heldPingTransport{started: make(chan NodeID, 1), release: make(chan error, 1)}
			tab := newTable(tr, newTestNodeID(t), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 30303}, "")
			defer tab.Close()
			tab.db.markBonded()

			b := fullBucket(t)
			oldest := b.entries[bucketSize-1]
			stale := time.Now().Add(-2 * nodeDBNodeExpiration)
			if err := tab.db.updateNode(oldest); err != nil {
				t.Fatal(err)
			}
			if err := tab.db.updateLastPong(oldest.ID, stale); err != nil {
				t.Fatal(err)
			}
			newcomer := newNode(newTestNodeID(t), net.IPv4(10, 0, 1, 1), 30303, 30303)

			done := make(chan struct{})
			go func() {
				tab.pingreplace(newcomer, b)
				close(done)
			}()
			select {
			case id := <-tr.started:
				if id != oldest.ID {
					t.Fatalf("refresh pinged %x, want the oldest entry %x", id[:4], oldest.ID[:4])
				}
			case <-time.After(5 * time.Second):
				t.Fatal("refresh never pinged the oldest entry")
			}

			// The sweep runs while the ping is outstanding and reclaims the
			// stale record.
			if err := tab.db.expireNodes(); err != nil {
				t.Fatal(err)
			}
			if tab.db.hasEntries(oldest.ID) {
				t.Fatal("precondition: the sweep did not delete the stale record while the ping was outstanding")
			}

			tr.release <- tc.pingErr
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("pingreplace did not return after the ping was answered")
			}

			if tc.pingErr != nil {
				if tab.db.hasEntries(oldest.ID) {
					t.Errorf("a failed refresh brought the deleted record back")
				}
				if b.entries[0].ID != newcomer.ID {
					t.Errorf("the unresponsive oldest entry was not replaced by the newcomer")
				}
				return
			}
			if b.entries[bucketSize-1] != oldest {
				t.Errorf("the responsive oldest entry was replaced")
			}
			if tab.db.node(oldest.ID) == nil {
				t.Errorf("node record of the entry that answered the refresh ping is gone")
			}
			if !tab.db.lastPong(oldest.ID).After(stale) {
				t.Errorf("last pong of the entry that answered the refresh ping was not refreshed")
			}
		})
	}
}

// lockWithin takes mu, giving up after d. It reports whether the lock was
// taken; the caller must unlock it if so.
func lockWithin(mu *sync.Mutex, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for !mu.TryLock() {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Millisecond)
	}
	return true
}

// bucketIDs returns the IDs in b, most recently active first. The caller
// must hold tab.mutex.
func bucketIDs(b *bucket) []NodeID {
	ids := make([]NodeID, len(b.entries))
	for i, n := range b.entries {
		ids[i] = n.ID
	}
	return ids
}

func countID(ids []NodeID, id NodeID) int {
	n := 0
	for _, x := range ids {
		if x == id {
			n++
		}
	}
	return n
}

// A full bucket pings its least recently active entry before a newcomer may
// replace it. That ping waits up to respTimeout for the pong, and the table
// lock must not be held meanwhile: the read loop takes it to answer
// FINDNODE, so holding it would stall every inbound packet, including the
// pong the ping is waiting for. Fails if pingreplace holds tab.mutex across
// the ping.
func TestFindnodeIsAnsweredWhileReplacementPingIsOutstanding(t *testing.T) {
	u := newTestUDP(t, false)
	defer u.close(t, 5*time.Second)

	// Bond the peer through a real ping exchange, so that it is answered
	// whatever FINDNODE requires of a bond, then hold the pings the table
	// sends from here on.
	peer := newSender(t, 40000)
	u.respond(peer)
	if err := u.tab.ping(peer.id(), peer.addr); err != nil {
		t.Fatalf("bonding ping to the peer failed: %v", err)
	}
	if err := u.tab.db.updateNode(newNode(peer.id(), peer.addr.IP, uint16(peer.addr.Port), uint16(peer.addr.Port))); err != nil {
		t.Fatal(err)
	}
	tr := &heldPingTransport{started: make(chan NodeID, 1), release: make(chan error, 1), onClose: u.udp.close}
	u.tab.net = tr
	newcomer, b, oldest := bondableNewcomer(t, u.tab)

	bonded := make(chan struct{})
	go func() {
		u.tab.bond(false, newcomer.ID, newcomer.addr(), newcomer.TCP)
		close(bonded)
	}()
	select {
	case id := <-tr.started:
		if id != oldest.ID {
			t.Fatalf("refresh pinged %x, want the oldest entry %x", id[:4], oldest.ID[:4])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("refresh never pinged the oldest entry")
	}

	answered := make(chan error, 1)
	go func() {
		answered <- peer.deliver(u, findnodePacket, &findnode{
			Target:     peer.id(),
			Expiration: uint64(time.Now().Add(expiration).Unix()),
		})
	}()
	stalled := false
	select {
	case err := <-answered:
		if err != nil {
			t.Errorf("FINDNODE from a bonded peer rejected: %v", err)
		}
		if got := u.conn.count(peer.addr, neighborsPacket); got == 0 {
			t.Error("FINDNODE from a bonded peer got no NEIGHBORS reply")
		}
	case <-time.After(2 * time.Second):
		stalled = true
		t.Error("FINDNODE was not answered while the replacement ping was outstanding")
	}

	tr.release <- nil
	select {
	case <-bonded:
	case <-time.After(5 * time.Second):
		t.Fatal("bond did not return after the replacement ping was answered")
	}
	if stalled {
		select {
		case <-answered:
		case <-time.After(5 * time.Second):
			t.Fatal("FINDNODE handler did not return after the replacement ping was answered")
		}
	}
	u.tab.mutex.Lock()
	ids := bucketIDs(b)
	u.tab.mutex.Unlock()
	if countID(ids, oldest.ID) != 1 || countID(ids, newcomer.ID) != 0 {
		t.Error("the oldest entry answered the ping but was replaced")
	}
}

// bondableNewcomer returns a node that tab knows from its database, so a
// bond with it goes straight to the table, together with its bucket, which
// is filled to capacity, and that bucket's least recently active entry.
func bondableNewcomer(t *testing.T, tab *Table) (newcomer *Node, b *bucket, oldest *Node) {
	t.Helper()
	newcomer = newNode(newTestNodeID(t), net.IPv4(10, 0, 1, 1), 30303, 30303)
	if err := tab.db.updateNode(newcomer); err != nil {
		t.Fatal(err)
	}
	tab.mutex.Lock()
	defer tab.mutex.Unlock()
	b = tab.buckets[logdist(tab.self.sha, newcomer.sha)]
	b.entries = fullBucket(t).entries
	return newcomer, b, b.entries[bucketSize-1]
}

// A bond with a node that is not in its full bucket pings the oldest entry
// with the table unlocked, so the bucket can change before the outcome is
// applied. A failed ping evicts the entry
// that was pinged only if it is still the least recently active entry of a
// full bucket; otherwise the newcomer is placed by what the bucket holds
// now. Fails if the outcome is applied to a bucket that changed meanwhile.
func TestReplacementRevalidatesBucketAfterPing(t *testing.T) {
	for _, tc := range []struct {
		name string
		// change runs with tab.mutex held while the ping is outstanding.
		change func(t *testing.T, b *bucket, oldest, newcomer *Node)
		// check gets the bucket's IDs after bond returned.
		check func(t *testing.T, ids []NodeID, oldest, newcomer *Node)
	}{
		{
			name:   "unchanged bucket evicts the oldest entry",
			change: func(*testing.T, *bucket, *Node, *Node) {},
			check: func(t *testing.T, ids []NodeID, oldest, newcomer *Node) {
				if len(ids) != bucketSize || ids[0] != newcomer.ID || countID(ids, oldest.ID) != 0 {
					t.Errorf("unresponsive oldest entry was not replaced by the newcomer")
				}
			},
		},
		{
			name: "oldest entry refreshed meanwhile is kept",
			change: func(t *testing.T, b *bucket, oldest, _ *Node) {
				// A concurrent bond with the oldest entry moves it to the front.
				if !b.bump(newNode(oldest.ID, oldest.IP, oldest.UDP, oldest.TCP)) {
					t.Fatal("precondition: oldest entry not in bucket")
				}
			},
			check: func(t *testing.T, ids []NodeID, oldest, newcomer *Node) {
				if countID(ids, oldest.ID) != 1 {
					t.Errorf("entry refreshed while the ping was outstanding was evicted")
				}
				if countID(ids, newcomer.ID) != 0 {
					t.Errorf("newcomer replaced another entry than the one that was pinged")
				}
				if len(ids) != bucketSize {
					t.Errorf("bucket has %d entries, want %d", len(ids), bucketSize)
				}
			},
		},
		{
			name: "oldest entry refreshed meanwhile and last again is kept",
			change: func(t *testing.T, b *bucket, oldest, _ *Node) {
				// The refreshed entry is a new *Node with the same ID; the
				// other entries are then refreshed after it, which makes
				// it the last entry again.
				if !b.bump(newNode(oldest.ID, oldest.IP, oldest.UDP, oldest.TCP)) {
					t.Fatal("precondition: oldest entry not in bucket")
				}
				for _, n := range append([]*Node(nil), b.entries[1:]...) {
					b.bump(newNode(n.ID, n.IP, n.UDP, n.TCP))
				}
				if b.entries[bucketSize-1].ID != oldest.ID {
					t.Fatal("precondition: refreshed entry is not last again")
				}
			},
			check: func(t *testing.T, ids []NodeID, oldest, newcomer *Node) {
				if countID(ids, oldest.ID) != 1 || countID(ids, newcomer.ID) != 0 {
					t.Errorf("entry refreshed while the ping was outstanding was evicted")
				}
			},
		},
		{
			name: "oldest entry removed meanwhile frees a slot",
			change: func(_ *testing.T, b *bucket, _, _ *Node) {
				b.entries = b.entries[:bucketSize-1]
			},
			check: func(t *testing.T, ids []NodeID, oldest, newcomer *Node) {
				if len(ids) != bucketSize || ids[0] != newcomer.ID || countID(ids, oldest.ID) != 0 {
					t.Errorf("newcomer was not placed in the freed slot: %d entries", len(ids))
				}
			},
		},
		{
			name: "newcomer added meanwhile is not duplicated",
			change: func(_ *testing.T, b *bucket, _, newcomer *Node) {
				// A concurrent bond put the newcomer at the front in place
				// of another entry; the oldest entry is still last.
				copy(b.entries[1:], b.entries[:3])
				b.entries[0] = newcomer
			},
			check: func(t *testing.T, ids []NodeID, oldest, newcomer *Node) {
				if countID(ids, newcomer.ID) != 1 {
					t.Errorf("newcomer appears %d times, want 1", countID(ids, newcomer.ID))
				}
				if countID(ids, oldest.ID) != 1 {
					t.Errorf("oldest entry was evicted although the newcomer was already in the bucket")
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := &heldPingTransport{started: make(chan NodeID, 1), release: make(chan error, 1)}
			tab := newTable(tr, newTestNodeID(t), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 30303}, "")
			defer tab.Close()

			newcomer, b, oldest := bondableNewcomer(t, tab)

			done := make(chan struct{})
			go func() {
				tab.bond(false, newcomer.ID, newcomer.addr(), newcomer.TCP)
				close(done)
			}()
			select {
			case <-tr.started:
			case <-time.After(5 * time.Second):
				t.Fatal("refresh never pinged the oldest entry")
			}
			if !lockWithin(&tab.mutex, 2*time.Second) {
				tr.release <- errors.New("timeout")
				<-done
				t.Fatal("table lock is held while the replacement ping is outstanding")
			}
			tc.change(t, b, oldest, newcomer)
			tab.mutex.Unlock()

			tr.release <- errors.New("timeout")
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("bond did not return after the ping failed")
			}
			tab.mutex.Lock()
			ids := bucketIDs(b)
			tab.mutex.Unlock()
			tc.check(t, ids, oldest, newcomer)
		})
	}
}

// Only one ping of a bucket's least recently active entry is outstanding at
// a time. Otherwise a ping that failed could be applied after a later ping
// to the same entry was answered, evicting a live entry, since an answered
// ping is recorded only in the database. A second newcomer for the full
// bucket meanwhile is not admitted. Fails if a second replacement ping is
// sent while one is outstanding.
func TestReplacementPingsOneAtATimePerBucket(t *testing.T) {
	tr := &heldPingTransport{started: make(chan NodeID, 2), release: make(chan error, 2)}
	tab := newTable(tr, newTestNodeID(t), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 30303}, "")
	defer tab.Close()

	first, b, oldest := bondableNewcomer(t, tab)
	var second *Node
	for second == nil {
		n := newNode(newTestNodeID(t), net.IPv4(10, 0, 1, 2), 30303, 30303)
		if tab.buckets[logdist(tab.self.sha, n.sha)] == b {
			second = n
		}
	}
	if err := tab.db.updateNode(second); err != nil {
		t.Fatal(err)
	}

	firstDone := make(chan struct{})
	go func() {
		tab.bond(false, first.ID, first.addr(), first.TCP)
		close(firstDone)
	}()
	select {
	case <-tr.started:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh never pinged the oldest entry")
	}

	secondDone := make(chan struct{})
	go func() {
		tab.bond(false, second.ID, second.addr(), second.TCP)
		close(secondDone)
	}()
	select {
	case <-secondDone:
	case <-tr.started:
		// Let both pings fail so both bonds return, then report.
		tr.release <- errors.New("timeout")
		tr.release <- errors.New("timeout")
		for _, done := range []chan struct{}{firstDone, secondDone} {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("bond did not return after its ping failed")
			}
		}
		t.Fatal("a second replacement ping was sent while one was outstanding")
	case <-time.After(5 * time.Second):
		t.Fatal("second bond neither returned nor pinged")
	}

	tr.release <- errors.New("timeout")
	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("first bond did not return after its ping failed")
	}
	tab.mutex.Lock()
	ids := bucketIDs(b)
	tab.mutex.Unlock()
	if countID(ids, second.ID) != 0 {
		t.Error("second newcomer was admitted while the replacement ping was outstanding")
	}
	if ids[0] != first.ID || countID(ids, oldest.ID) != 0 {
		t.Error("unresponsive oldest entry was not replaced by the first newcomer")
	}
}

// sameBucketNewcomer returns a node tab knows from its database whose
// bucket is b.
func sameBucketNewcomer(t *testing.T, tab *Table, b *bucket, ip net.IP) *Node {
	t.Helper()
	for {
		n := newNode(newTestNodeID(t), ip, 30303, 30303)
		if tab.buckets[logdist(tab.self.sha, n.sha)] != b {
			continue
		}
		if err := tab.db.updateNode(n); err != nil {
			t.Fatal(err)
		}
		return n
	}
}

// bondFailingReplacement bonds n into its full bucket, fails the ping of the
// bucket's last entry, and waits for the bond to return. It fails the test
// if no replacement ping is sent.
func bondFailingReplacement(t *testing.T, tab *Table, tr *heldPingTransport, n *Node) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		tab.bond(false, n.ID, n.addr(), n.TCP)
		close(done)
	}()
	select {
	case <-tr.started:
	case <-done:
		t.Fatal("bond returned without pinging the bucket's last entry")
	case <-time.After(5 * time.Second):
		t.Fatal("bond neither returned nor pinged")
	}
	tr.release <- errors.New("timeout")
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("bond did not return after its ping failed")
	}
}

// Once a replacement ping has been answered or has failed, the bucket takes
// the next newcomer again. Fails if the bucket stays closed to newcomers
// after its first replacement.
func TestReplacementCanRunAgainAfterOne(t *testing.T) {
	tr := &heldPingTransport{started: make(chan NodeID, 1), release: make(chan error, 1)}
	tab := newTable(tr, newTestNodeID(t), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 30303}, "")
	defer tab.Close()

	first, b, _ := bondableNewcomer(t, tab)
	second := sameBucketNewcomer(t, tab, b, net.IPv4(10, 0, 1, 2))
	bondFailingReplacement(t, tab, tr, first)
	bondFailingReplacement(t, tab, tr, second)

	tab.mutex.Lock()
	ids := bucketIDs(b)
	tab.mutex.Unlock()
	if ids[0] != second.ID || ids[1] != first.ID {
		t.Error("the two newcomers did not replace the bucket's last entries in turn")
	}
}

// Several waiters of one bond share its result, so the same *Node can be
// handed to pingreplace while it is already in the bucket, and moved to the
// front and back to the last slot without a new pointer. pingreplace stores
// a copy of its own each time, so the last slot holding the pinged *Node
// still means the entry was not refreshed. Fails if a refresh with the same
// pointer leaves the entry to be evicted by a failed ping from before it.
func TestReplacementSeesRefreshWithSamePointer(t *testing.T) {
	tr := &heldPingTransport{started: make(chan NodeID, 1), release: make(chan error, 1)}
	tab := newTable(tr, newTestNodeID(t), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 30303}, "")
	defer tab.Close()

	newcomer, b, oldest := bondableNewcomer(t, tab)
	done := make(chan struct{})
	go func() {
		tab.bond(false, newcomer.ID, newcomer.addr(), newcomer.TCP)
		close(done)
	}()
	select {
	case <-tr.started:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh never pinged the oldest entry")
	}

	// The pinged entry is refreshed with the pointer the bucket holds, then
	// the others are refreshed after it, which makes it last again.
	tab.pingreplace(oldest, b)
	tab.mutex.Lock()
	for _, n := range append([]*Node(nil), b.entries[1:]...) {
		b.bump(newNode(n.ID, n.IP, n.UDP, n.TCP))
	}
	last := b.entries[bucketSize-1].ID
	tab.mutex.Unlock()
	if last != oldest.ID {
		t.Fatal("precondition: refreshed entry is not last again")
	}

	tr.release <- errors.New("timeout")
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("bond did not return after its ping failed")
	}
	tab.mutex.Lock()
	ids := bucketIDs(b)
	tab.mutex.Unlock()
	if countID(ids, oldest.ID) != 1 || countID(ids, newcomer.ID) != 0 {
		t.Error("entry refreshed while the ping was outstanding was evicted")
	}
}
