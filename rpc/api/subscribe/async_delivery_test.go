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
// healthy subscriber would not see the event before the blocked
// connection's 10-second write deadline.
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
	waitLive(t, server, 1)

	// Gate the blocked connection's writes.
	conn.blockWrites()

	// The healthy connection.
	healthy := rpc.DialInProc(rpcServer)
	t.Cleanup(healthy.Close)
	events := make(chan interface{}, 8)
	if _, err := healthy.Subscribe(context.Background(), "ledger", events, "momentums"); err != nil {
		t.Fatal(err)
	}
	waitLive(t, server, 2)

	// Broadcast: the blocked subscription's delivery queue absorbs the
	// event; the worker must not wait on it.
	server.InsertMomentum(momentumAt(1))

	// The healthy subscriber must receive the event well before the
	// blocked connection's 10-second write deadline.
	select {
	case <-events:
	case <-time.After(3 * time.Second):
		t.Fatal("healthy subscriber did not receive the momentum within 3s " +
			"(blocked connection stalled the shared worker)")
	}

	// The blocked connection is still blocked; its subscription stays
	// installed because the notifier has not yet seen a write failure.
	if got := liveCount(server); got != 2 {
		t.Fatalf("live subscriptions = %d, want 2 (blocked connection still installed)", got)
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
	t.Cleanup(func() { close(conn.gate) })
	go rpcServer.ServeCodec(rpc.NewCodec(conn), 0)
	dec := json.NewDecoder(clientEnd)
	rawSubscribe(t, clientEnd, dec, 1, `["momentums"]`)
	waitLive(t, server, 1)
	conn.blockWrites()

	// Healthy connection.
	healthy := rpc.DialInProc(rpcServer)
	t.Cleanup(healthy.Close)
	events := make(chan interface{}, 64)
	if _, err := healthy.Subscribe(context.Background(), "ledger", events, "momentums"); err != nil {
		t.Fatal(err)
	}
	waitLive(t, server, 2)

	// Fill the blocked connection's delivery queue (256) plus extra.
	// The worker must not stall; events for the healthy subscriber
	// must still be delivered.
	for h := uint64(1); h <= 300; h++ {
		server.InsertMomentum(momentumAt(h))
	}

	// The healthy subscriber should receive at least some events.
	// We don't require all 300 (the channel has capacity 64), but at
	// least the first few must arrive quickly.
	for i := 0; i < 3; i++ {
		select {
		case <-events:
		case <-time.After(3 * time.Second):
			t.Fatalf("healthy subscriber missed event %d: blocked connection stalled the worker", i+1)
		}
	}
}

// Slow-but-successful writes do not stall the worker either: a connection
// whose writes are slow but eventually complete keeps receiving events,
// and other subscribers are not delayed.
func TestSlowConnectionDoesNotStallWorker(t *testing.T) {
	server, rpcServer := startTestServer(t)

	// Slow connection: writes complete after a short delay.
	slowEnd, slowClient := net.Pipe()
	t.Cleanup(func() { slowEnd.Close(); slowClient.Close() })
	slowConn := &delayedConn{Conn: slowEnd, delay: 50 * time.Millisecond}
	go rpcServer.ServeCodec(rpc.NewCodec(slowConn), 0)
	slowDec := json.NewDecoder(slowClient)
	rawSubscribe(t, slowClient, slowDec, 1, `["momentums"]`)

	// Healthy connection.
	healthy := rpc.DialInProc(rpcServer)
	t.Cleanup(healthy.Close)
	events := make(chan interface{}, 16)
	if _, err := healthy.Subscribe(context.Background(), "ledger", events, "momentums"); err != nil {
		t.Fatal(err)
	}
	waitLive(t, server, 2)

	// Broadcast several momentums. The slow connection's writes take
	// 50ms each, but the worker should queue events asynchronously
	// and move on.
	start := time.Now()
	for h := uint64(1); h <= 10; h++ {
		server.InsertMomentum(momentumAt(h))
	}
	elapsed := time.Since(start)

	// If delivery were synchronous, 10 events × 50ms = 500ms minimum.
	// With async delivery, the worker finishes in well under 100ms.
	if elapsed > 200*time.Millisecond {
		t.Fatalf("broadcast of 10 momentums took %v, want < 200ms (slow connection stalled the worker)", elapsed)
	}

	// The healthy subscriber receives events.
	select {
	case <-events:
	case <-time.After(3 * time.Second):
		t.Fatal("healthy subscriber did not receive any event")
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
	waitLive(t, server, 1)

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
