package subscribe

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	rpc "github.com/zenon-network/go-zenon/rpc/server"
)

// gateAfterConn is the server side of a net.Pipe whose writes pass freely
// until the test calls blockWrites; every write after that blocks at the
// gate. This lets the subscribe response through during setup and then
// deterministically blocks all notification writes.
type gateAfterConn struct {
	net.Conn
	mu    sync.Mutex
	block bool
	gate  chan struct{}
}

func (c *gateAfterConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	blocked := c.block
	c.mu.Unlock()
	if blocked {
		<-c.gate
	}
	return c.Conn.Write(p)
}

func (c *gateAfterConn) blockWrites() {
	c.mu.Lock()
	c.block = true
	c.mu.Unlock()
}

// A blocked connection's subscriptions are queued for async delivery, so
// the shared broadcast worker is never stalled by a slow write. A healthy
// subscriber on the same server receives events while the blocked
// connection is still waiting at the gate. Fails if broadcast reverts to
// synchronous delivery: the worker would block on the gated write and the
// healthy subscriber would not see the events before the blocked
// connection's 10-second write deadline.
//
// Two events are broadcast and both must arrive: synchronous delivery
// cannot finish the first pass if both must get through, because the first
// event's write to the gated connection would block the worker before it
// could queue anything for the healthy side.
func TestBlockedConnectionDoesNotStallHealthySubscriber(t *testing.T) {
	server, rpcServer := startTestServer(t)

	// The blocked connection: subscribe while writes pass freely, then
	// gate all subsequent writes.
	serverEnd, clientEnd := net.Pipe()
	t.Cleanup(func() { serverEnd.Close(); clientEnd.Close() })
	conn := &gateAfterConn{
		Conn: serverEnd,
		gate: make(chan struct{}),
	}
	t.Cleanup(func() { close(conn.gate) })
	go rpcServer.ServeCodec(rpc.NewCodec(conn), 0)
	dec := json.NewDecoder(clientEnd)
	rawSubscribe(t, clientEnd, dec, 1, `["momentums"]`)

	// The subscription must be installed and its notifier activated (the
	// subscribe response has been written) before we block writes.
	waitInstalled(t, server, MomentumsSubscription, 1)

	// Gate the blocked connection's writes.
	conn.blockWrites()

	// The healthy connection.
	healthy := rpc.DialInProc(rpcServer)
	t.Cleanup(healthy.Close)
	events := make(chan interface{}, 8)
	if _, err := healthy.Subscribe(context.Background(), "ledger", events, "momentums"); err != nil {
		t.Fatal(err)
	}
	waitInstalled(t, server, MomentumsSubscription, 2)

	// Broadcast two events: the blocked subscription's delivery queue
	// absorbs them; the worker must not wait on the gated write.
	server.InsertMomentum(momentumAt(1))
	server.InsertMomentum(momentumAt(2))

	// The healthy subscriber must receive both events well before the
	// blocked connection's 10-second write deadline. Synchronous delivery
	// would block on the first event's write to the gated connection and
	// never queue the second.
	for i := 0; i < 2; i++ {
		select {
		case <-events:
		case <-time.After(3 * time.Second):
			t.Fatalf("healthy subscriber did not receive event %d within 3s "+
				"(blocked connection stalled the shared worker)", i+1)
		}
	}

	// The blocked connection is still blocked; its subscription stays
	// installed because the notifier has not yet seen a write failure.
	if got := len(server.installed(MomentumsSubscription)); got != 2 {
		t.Fatalf("installed subscriptions = %d, want 2 (blocked connection still installed)", got)
	}
}

// When the blocked connection's delivery queue overflows, events are
// dropped for that subscription only — the worker continues broadcasting
// to other subscribers without stalling.
func TestBlockedConnectionQueueOverflowDropsOnlyThatSubscription(t *testing.T) {
	server, rpcServer := startTestServer(t)

	// Blocked connection.
	serverEnd, clientEnd := net.Pipe()
	t.Cleanup(func() { serverEnd.Close(); clientEnd.Close() })
	conn := &gateAfterConn{
		Conn: serverEnd,
		gate: make(chan struct{}),
	}
	gateReleased := false
	releaseGate := func() {
		if !gateReleased {
			gateReleased = true
			close(conn.gate)
		}
	}
	t.Cleanup(releaseGate)
	go rpcServer.ServeCodec(rpc.NewCodec(conn), 0)
	dec := json.NewDecoder(clientEnd)
	rawSubscribe(t, clientEnd, dec, 1, `["momentums"]`)
	waitInstalled(t, server, MomentumsSubscription, 1)
	conn.blockWrites()

	// Healthy connection.
	healthy := rpc.DialInProc(rpcServer)
	t.Cleanup(healthy.Close)
	events := make(chan interface{}, 300)
	if _, err := healthy.Subscribe(context.Background(), "ledger", events, "momentums"); err != nil {
		t.Fatal(err)
	}
	waitInstalled(t, server, MomentumsSubscription, 2)

	// Drive broadcastMomentums directly (bypassing mChan) so every event
	// reaches the worker's broadcast loop. Pace healthy delivery: receive
	// each healthy event before sending the next, because 300 back-to-back
	// broadcasts target the healthy subscription's 256-slot queue too and
	// a starved healthy watcher can legitimately drop.
	//
	// The producer runs in a goroutine with a bounded completion check so
	// a failure releases the gate instead of hanging to timeout.
	const total = 300
	pace := make(chan struct{}) // consumer signals the producer to send the next event
	produceDone := make(chan *BroadcastStats, 1)
	go func() {
		var totalStats BroadcastStats
		for h := uint64(1); h <= total; h++ {
			stats := server.broadcastMomentums(&Momentum{
				Hash:   types.HexToHashPanic("dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"),
				Height: h,
			})
			totalStats.NumNotify += stats.NumNotify
			totalStats.NumDropped += stats.NumDropped
			totalStats.NumUninstalls += stats.NumUninstalls
			// Wait for the consumer to receive this event before sending
			// the next, so the healthy subscription's queue never fills.
			select {
			case <-pace:
			case <-time.After(5 * time.Second):
				produceDone <- &totalStats
				return
			}
		}
		produceDone <- &totalStats
	}()

	// Receive each healthy event before the producer sends the next.
	received := 0
	deadline := time.Now().Add(5 * time.Second)
	for received < total {
		select {
		case <-events:
			received++
			pace <- struct{}{} // let the producer send the next event
		case <-time.After(time.Until(deadline)):
			releaseGate()
			t.Fatalf("healthy subscriber received %d of %d events: blocked connection stalled the worker",
				received, total)
		}
	}

	// Wait for the producer to finish.
	var stats *BroadcastStats
	select {
	case stats = <-produceDone:
	case <-time.After(5 * time.Second):
		t.Fatal("producer did not finish within 5s")
	}

	// The blocked subscription's queue (256 slots) absorbed the first 256
	// events; the remaining 44 were dropped for that subscription only.
	// Assert the actual drop count to pin the drop-newest policy: 43 or 44
	// dropped is acceptable (43 when one event is still in flight in the
	// delivery goroutine).
	if stats.NumDropped < 43 || stats.NumDropped > 44 {
		t.Fatalf("NumDropped = %d, want 43 or 44 (256-slot queue, 300 events)", stats.NumDropped)
	}

	// The blocked subscription's queue must be full.
	blockedSubs := server.installed(MomentumsSubscription)
	if len(blockedSubs) != 2 {
		t.Fatalf("installed subscriptions = %d, want 2", len(blockedSubs))
	}
	var blockedSub *Subscription
	for _, sub := range blockedSubs {
		if sub.deliverCh != nil && len(sub.deliverCh) == deliverQueueSize {
			if blockedSub != nil {
				t.Fatal("multiple subscriptions have full queues")
			}
			blockedSub = sub
		}
	}
	if blockedSub == nil {
		t.Fatal("no subscription has a full delivery queue after 300 broadcasts")
	}
}

// Slow-but-successful writes do not stall the worker either: a connection
// whose writes are slow but eventually complete keeps receiving events,
// and other subscribers are not delayed.
func TestSlowConnectionDoesNotStallWorker(t *testing.T) {
	server, rpcServer := startTestServer(t)

	// Slow connection: writes complete after a short delay. Drain the
	// client side so the delayed writes actually complete through the
	// pipe; without a reader net.Pipe blocks forever and this would be
	// just another blocked-connection test.
	slowEnd, slowClient := net.Pipe()
	t.Cleanup(func() { slowEnd.Close(); slowClient.Close() })
	slowConn := &delayedConn{Conn: slowEnd, delay: 50 * time.Millisecond}
	go rpcServer.ServeCodec(rpc.NewCodec(slowConn), 0)
	slowDec := json.NewDecoder(slowClient)
	rawSubscribe(t, slowClient, slowDec, 1, `["momentums"]`)
	// slowCh receives every notification the slow connection gets, so the
	// test can assert the slow side eventually receives an event without
	// racing a background drain.
	slowCh := make(chan json.RawMessage, 16)
	go func() {
		for {
			var msg json.RawMessage
			if err := slowDec.Decode(&msg); err != nil {
				return
			}
			slowCh <- msg
		}
	}()
	waitInstalled(t, server, MomentumsSubscription, 1)

	// Healthy connection.
	healthy := rpc.DialInProc(rpcServer)
	t.Cleanup(healthy.Close)
	events := make(chan interface{}, 16)
	if _, err := healthy.Subscribe(context.Background(), "ledger", events, "momentums"); err != nil {
		t.Fatal(err)
	}
	waitInstalled(t, server, MomentumsSubscription, 2)

	// Broadcast one event and synchronize on the slow write being in
	// flight: the healthy subscriber must make progress while the slow
	// write is outstanding (50ms delay). Then assert the slow side
	// received it too after the delay elapses.
	//
	// Synchronous delivery would block the worker on the slow write for
	// 50ms before it could queue anything for the healthy side; the
	// healthy event must arrive well under that bound.
	server.InsertMomentum(momentumAt(1))

	// Bound well below 450ms (9 slow writes × 50ms): the healthy event
	// must arrive while the first slow write is still in flight.
	select {
	case <-events:
	case <-time.After(40 * time.Millisecond):
		t.Fatal("healthy subscriber did not receive the event within 40ms " +
			"(slow connection stalled the worker)")
	}

	// The slow connection's write completes after its 50ms delay; the
	// event must arrive on the slow side as well.
	select {
	case <-slowCh:
	case <-time.After(2 * time.Second):
		t.Fatal("slow connection did not receive the event")
	}
}

// delayedConn is a net.Conn whose writes complete after a fixed delay.
type delayedConn struct {
	net.Conn
	delay time.Duration
}

func (c *delayedConn) Write(p []byte) (int, error) {
	time.Sleep(c.delay)
	return c.Conn.Write(p)
}

// Ordering within a subscription is preserved: events are delivered to the
// notifier in the order the worker queued them.
func TestEventOrderingPreservedPerSubscription(t *testing.T) {
	server, rpcServer := startTestServer(t)

	client := rpc.DialInProc(rpcServer)
	t.Cleanup(client.Close)
	events := make(chan interface{}, 64)
	if _, err := client.Subscribe(context.Background(), "ledger", events, "momentums"); err != nil {
		t.Fatal(err)
	}
	waitInstalled(t, server, MomentumsSubscription, 1)

	// Broadcast 10 momentums with known heights.
	const count = 10
	for h := uint64(1); h <= count; h++ {
		server.InsertMomentum(&nom.DetailedMomentum{
			Momentum: &nom.Momentum{
				Hash:   types.HexToHashPanic("eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"),
				Height: h,
			},
		})
	}

	// Collect events and verify ordering.
	received := make([]uint64, 0, count)
	deadline := time.Now().Add(5 * time.Second)
	for len(received) < count {
		select {
		case ev := <-events:
			if arr, ok := ev.([]interface{}); ok && len(arr) == 1 {
				if m, ok := arr[0].(map[string]interface{}); ok {
					if h, ok := m["height"].(float64); ok {
						received = append(received, uint64(h))
					}
				}
			}
		case <-time.After(time.Until(deadline)):
			t.Fatalf("only received %d of %d events", len(received), count)
		}
	}

	for i, h := range received {
		want := uint64(i + 1)
		if h != want {
			t.Fatalf("event %d has height %d, want %d (ordering violated)", i, h, want)
		}
	}
}
