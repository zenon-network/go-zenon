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
	var gateOnce sync.Once
	releaseGate := func() { gateOnce.Do(func() { close(conn.gate) }) }
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
	// a failure releases the gate instead of hanging to timeout. The pace
	// channel has capacity 1: the consumer never blocks on it, so the
	// deadline branch and releaseGate always run.
	const total = 300
	pace := make(chan struct{}, 1) // consumer signals the producer to send the next event
	produceDone := make(chan *BroadcastStats, 1)
	go func() {
		defer releaseGate()
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
			select {
			case pace <- struct{}{}: // let the producer send the next event
			default: // pace is full; the producer has not consumed the last token
			}
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
		releaseGate()
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
		if sub.deliverCh != nil && len(sub.deliverCh) >= deliverQueueSize-1 {
			if blockedSub != nil {
				t.Fatal("multiple subscriptions have full queues")
			}
			blockedSub = sub
		}
	}
	if blockedSub == nil {
		t.Fatal("no subscription has a full delivery queue after 300 broadcasts")
	}

	// Assert retained heights to distinguish drop-newest from drop-oldest:
	// the queue must hold the earliest events (starting at height 1 or 2),
	// proving the overflow policy drops the newest arrivals, not the oldest.
	// The delivery goroutine may have consumed height 1 into a blocked write,
	// so the first queued item is at most height 2.
	retained := len(blockedSub.deliverCh)
	if retained < deliverQueueSize-1 {
		t.Fatalf("blocked subscription queue has %d events, want at least %d", retained, deliverQueueSize-1)
	}
	first := (<-blockedSub.deliverCh).([]interface{})
	firstMomentum, ok := first[0].(*Momentum)
	if !ok {
		t.Fatalf("first retained event has unexpected type %T", first[0])
	}
	if firstMomentum.Height < 1 || firstMomentum.Height > 2 {
		t.Fatalf("first retained event has height %d, want 1 or 2 (drop-newest policy violated)", firstMomentum.Height)
	}
}

// signaledGateConn is the server side of a net.Pipe whose writes pass
// freely until the test calls gate(); every write after that signals
// entered (once) and then blocks until the test closes gate. This gives
// the test a deterministic handshake: it can wait until a notification
// write is actually in flight before asserting the healthy side.
type signaledGateConn struct {
	net.Conn
	mu      sync.Mutex
	block   bool
	gate    chan struct{}
	once    sync.Once
	entered chan struct{}
}

func (c *signaledGateConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	blocked := c.block
	c.mu.Unlock()
	if blocked {
		c.once.Do(func() { close(c.entered) })
		<-c.gate
	}
	return c.Conn.Write(p)
}

func (c *signaledGateConn) blockWrites() {
	c.mu.Lock()
	c.block = true
	c.mu.Unlock()
}

// Slow-but-successful writes do not stall the worker either: a connection
// whose writes are slow but eventually complete keeps receiving events,
// and other subscribers are not delayed.
func TestSlowConnectionDoesNotStallWorker(t *testing.T) {
	server, rpcServer := startTestServer(t)

	// Slow connection: writes pass through a gate that the test opens
	// after asserting the healthy side has received the event. This
	// replaces the old fixed 40 ms timer, which could pass under
	// synchronous delivery when the healthy subscription was visited
	// first. Drain the client side so the gated writes actually complete
	// through the pipe; without a reader net.Pipe blocks forever and this
	// would be just another blocked-connection test.
	slowEnd, slowClient := net.Pipe()
	t.Cleanup(func() { slowEnd.Close(); slowClient.Close() })
	slowConn := &signaledGateConn{
		Conn:    slowEnd,
		gate:    make(chan struct{}),
		entered: make(chan struct{}),
	}
	var slowGateOnce sync.Once
	releaseSlowGate := func() { slowGateOnce.Do(func() { close(slowConn.gate) }) }
	t.Cleanup(releaseSlowGate)
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

	// Gate the slow connection's writes, then broadcast. Wait for the
	// slow write to actually enter the gate (deterministic handshake),
	// then assert the healthy subscriber still receives the event.
	// Synchronous delivery would block the worker on the gated write and
	// the healthy event would never arrive.
	slowConn.blockWrites()
	server.InsertMomentum(momentumAt(1))

	select {
	case <-slowConn.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("slow connection write never entered the gate")
	}

	select {
	case <-events:
	case <-time.After(2 * time.Second):
		t.Fatal("healthy subscriber did not receive the event " +
			"(slow connection stalled the worker)")
	}

	// Open the gate so the slow connection's write completes; the event
	// must arrive on the slow side as well.
	releaseSlowGate()
	select {
	case <-slowCh:
	case <-time.After(2 * time.Second):
		t.Fatal("slow connection did not receive the event")
	}
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

// A subscription whose notification write is blocked must still release its
// global slot when the client unsubscribes over the same connection. On dev
// the worker was the one blocked and the watcher released the slot at once;
// with per-subscription delivery goroutines the lifecycle goroutine must do
// the same even while the delivery goroutine sits inside a blocked write.
// Fails if lifecycle and delivery share one goroutine: the watcher cannot
// service unsubscribed until the write returns, and uninstall (which
// releases the slot) waits.
func TestUnsubscribeDuringBlockedWriteReclaimsSlot(t *testing.T) {
	server, rpcServer := startTestServer(t)

	serverEnd, clientEnd := net.Pipe()
	t.Cleanup(func() { serverEnd.Close(); clientEnd.Close() })
	conn := &signaledGateConn{
		Conn:    serverEnd,
		gate:    make(chan struct{}),
		entered: make(chan struct{}),
	}
	t.Cleanup(func() { close(conn.gate) })
	go rpcServer.ServeCodec(rpc.NewCodec(conn), 0)
	dec := json.NewDecoder(clientEnd)
	subID := rawSubscribe(t, clientEnd, dec, 1, `["momentums"]`)
	waitInstalled(t, server, MomentumsSubscription, 1)

	// Gate the connection's writes, then broadcast: the delivery
	// goroutine picks up the event and blocks inside the write.
	conn.blockWrites()
	server.InsertMomentum(momentumAt(1))

	// Wait until the write has actually entered the gate.
	select {
	case <-conn.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("notification write never entered the gate")
	}

	// Unsubscribe over the same (blocked) connection. The lifecycle
	// goroutine must release the slot without waiting for the write.
	if _, err := clientEnd.Write([]byte(`{"jsonrpc":"2.0","id":2,"method":"ledger.unsubscribe","params":["` + subID + `"]}`)); err != nil {
		t.Fatal(err)
	}

	waitLive(t, server, 0)

	// A replacement subscription on a different connection must be
	// admitted before the gated connection's write completes, proving
	// the slot was actually released and is reusable.
	replacement := rpc.DialInProc(rpcServer)
	t.Cleanup(replacement.Close)
	replacementEvents := make(chan interface{}, 4)
	if _, err := replacement.Subscribe(context.Background(), "ledger", replacementEvents, "momentums"); err != nil {
		t.Fatal(err)
	}
	waitLive(t, server, 1)

	// The replacement receives events while the original connection's
	// write is still gated.
	server.InsertMomentum(momentumAt(2))
	select {
	case <-replacementEvents:
	case <-time.After(3 * time.Second):
		t.Fatal("replacement subscription did not receive an event " +
			"while the original connection's write was still gated")
	}
}

// Closing the connection during a blocked write also reclaims the slot:
// the lifecycle goroutine sees the disconnect signal immediately, without
// waiting for the delivery goroutine's write to return. Fails if lifecycle
// and delivery share one goroutine: the watcher cannot service disconnected
// until the write returns.
func TestDisconnectDuringBlockedWriteReclaimsSlot(t *testing.T) {
	server, rpcServer := startTestServer(t)

	serverEnd, clientEnd := net.Pipe()
	conn := &signaledGateConn{
		Conn:    serverEnd,
		gate:    make(chan struct{}),
		entered: make(chan struct{}),
	}
	// Cleanup runs in LIFO order: close clientEnd first (if not already
	// closed), then release the gate so the delivery goroutine can exit,
	// then close serverEnd. Every failure path is covered.
	t.Cleanup(func() { serverEnd.Close() })
	t.Cleanup(func() { close(conn.gate) })
	t.Cleanup(func() { clientEnd.Close() })
	go rpcServer.ServeCodec(rpc.NewCodec(conn), 0)
	dec := json.NewDecoder(clientEnd)
	rawSubscribe(t, clientEnd, dec, 1, `["momentums"]`)
	waitInstalled(t, server, MomentumsSubscription, 1)

	// Gate writes, broadcast, wait for the delivery write to enter the gate.
	conn.blockWrites()
	server.InsertMomentum(momentumAt(1))
	select {
	case <-conn.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("notification write never entered the gate")
	}

	// Close the client end: the server-side codec sees the disconnect,
	// and the lifecycle goroutine must release the slot without waiting
	// for the gated write.
	clientEnd.Close()
	waitLive(t, server, 0)
}
