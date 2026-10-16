package discover

import (
	"errors"
	"net"
	"testing"
	"time"
)

// heldPingTransport answers every ping only once the test releases it, so a
// sweep can run while the ping is outstanding.
type heldPingTransport struct {
	started chan NodeID
	release chan error
}

func (tr *heldPingTransport) ping(id NodeID, _ *net.UDPAddr) error {
	tr.started <- id
	return <-tr.release
}

func (tr *heldPingTransport) waitping(NodeID) error { return nil }

func (tr *heldPingTransport) findnode(NodeID, *net.UDPAddr, NodeID) ([]*Node, error) {
	return nil, nil
}

func (tr *heldPingTransport) close() {}

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
				if b.entries[0] != newcomer {
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
