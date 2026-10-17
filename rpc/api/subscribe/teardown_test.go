package subscribe

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	rpc "github.com/zenon-network/go-zenon/rpc/server"
)

var errPeerGone = errors.New("peer gone")

// failableConn is the server side of a net.Pipe whose writes fail once the
// test says so. It stands in for a WebSocket whose write timed out.
type failableConn struct {
	net.Conn
	mu   sync.Mutex
	fail bool
}

func (c *failableConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	fail := c.fail
	c.mu.Unlock()
	if fail {
		return 0, errPeerGone
	}
	return c.Conn.Write(p)
}

func (c *failableConn) failWrites() {
	c.mu.Lock()
	c.fail = true
	c.mu.Unlock()
}

// rawSubscribe sends one ledger.subscribe over the raw client end and returns
// the subscription ID from the response.
func rawSubscribe(t *testing.T, client net.Conn, dec *json.Decoder, id int, params string) string {
	t.Helper()
	req := `{"jsonrpc":"2.0","id":` + strconv.Itoa(id) + `,"method":"ledger.subscribe","params":` + params + `}`
	if _, err := client.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Result string          `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := dec.Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Error) > 0 {
		t.Fatalf("subscribe failed: %s", resp.Error)
	}
	return resp.Result
}

func momentumAt(height uint64) *nom.DetailedMomentum {
	return &nom.DetailedMomentum{
		Momentum: &nom.Momentum{Hash: types.HexToHashPanic("dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"), Height: height},
	}
}

func liveCount(server *Server) int64 { return server.live.Load() }

func waitLive(t *testing.T, server *Server, want int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for liveCount(server) != want {
		if time.Now().After(deadline) {
			t.Fatalf("live subscriptions = %d, want %d", liveCount(server), want)
		}
		time.Sleep(time.Millisecond)
	}
}

// A connection whose write fails during a broadcast is torn down: every
// subscription it held is uninstalled, the global slots come back exactly
// once, and a healthy connection on the same server keeps receiving events.
// Fails if the codec no longer aborts on a terminal write error, or if the
// watchers and the worker both release a slot.
func TestTerminalWriteFailureReclaimsSubscriptionsOnce(t *testing.T) {
	server, rpcServer := startTestServer(t)

	// The failing connection: two subscriptions of different types.
	serverEnd, clientEnd := net.Pipe()
	t.Cleanup(func() { serverEnd.Close(); clientEnd.Close() })
	conn := &failableConn{Conn: serverEnd}
	go rpcServer.ServeCodec(rpc.NewCodec(conn), 0)
	dec := json.NewDecoder(clientEnd)
	rawSubscribe(t, clientEnd, dec, 1, `["momentums"]`)
	rawSubscribe(t, clientEnd, dec, 2, `["allAccountBlocks"]`)

	// The healthy connection.
	healthy := rpc.DialInProc(rpcServer)
	t.Cleanup(healthy.Close)
	events := make(chan interface{}, 8)
	if _, err := healthy.Subscribe(context.Background(), "ledger", events, "momentums"); err != nil {
		t.Fatal(err)
	}
	waitLive(t, server, 3)

	// The failing connection stops reading, then its writes fail.
	conn.failWrites()
	server.InsertMomentum(momentumAt(1))

	waitLive(t, server, 1)
	select {
	case <-events:
	case <-time.After(5 * time.Second):
		t.Fatal("healthy connection did not receive the first momentum")
	}

	// A second broadcast must not release anything twice or disturb the
	// healthy peer.
	server.InsertMomentum(momentumAt(2))
	select {
	case <-events:
	case <-time.After(5 * time.Second):
		t.Fatal("healthy connection did not receive the second momentum")
	}
	time.Sleep(50 * time.Millisecond)
	if got := liveCount(server); got != 1 {
		t.Fatalf("live subscriptions after a second broadcast = %d, want 1", got)
	}
}

// heldConn is the server side of a net.Pipe whose writes wait at a gate.
// reached is closed when the first write arrives at it.
type heldConn struct {
	net.Conn
	gate    chan struct{}
	reached chan struct{}
	once    sync.Once
}

func (c *heldConn) Write(p []byte) (int, error) {
	c.once.Do(func() { close(c.reached) })
	<-c.gate
	return c.Conn.Write(p)
}

// A subscription whose pre-activation buffer overflows on an otherwise
// healthy transport is not left installed and silent: the subscribe
// response is held, broadcasts pile up until the bound is crossed, and the
// connection is torn down so its slot comes back; a peer that subscribes
// afterwards is served. Fails if an overflow only fails the notifier.
func TestOverflowBeforeActivationTearsDownConnection(t *testing.T) {
	server, rpcServer := startTestServer(t)

	serverEnd, clientEnd := net.Pipe()
	t.Cleanup(func() { serverEnd.Close(); clientEnd.Close() })
	conn := &heldConn{Conn: serverEnd, gate: make(chan struct{}), reached: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(conn.gate) }) }
	t.Cleanup(release)
	served := make(chan struct{})
	go func() {
		rpcServer.ServeCodec(rpc.NewCodec(conn), 0)
		close(served)
	}()
	req := `{"jsonrpc":"2.0","id":1,"method":"ledger.subscribe","params":["momentums"]}`
	if _, err := clientEnd.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-conn.reached: // the response write is held; the entry is queued or installed
	case <-time.After(5 * time.Second):
		t.Fatal("the subscribe response was never written")
	}
	waitLive(t, server, 1)

	// Broadcast until the buffer overflows and the slot comes back. The
	// bound is private to rpc/server; 512 is well past it.
	for h := uint64(1); h <= 512 && liveCount(server) == 1; h++ {
		server.mCh <- &Momentum{Height: h}
	}
	waitLive(t, server, 0)
	release() // lets the held response write fail and the handler finish
	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("ServeCodec did not return after the overflow")
	}

	// A new peer is served and the count stays exact.
	healthy := rpc.DialInProc(rpcServer)
	t.Cleanup(healthy.Close)
	events := make(chan interface{}, 8)
	if _, err := healthy.Subscribe(context.Background(), "ledger", events, "momentums"); err != nil {
		t.Fatal(err)
	}
	waitLive(t, server, 1)
	server.InsertMomentum(momentumAt(1000))
	select {
	case <-events:
	case <-time.After(5 * time.Second):
		t.Fatal("a peer subscribing after the overflow did not receive the momentum")
	}
	if got := liveCount(server); got != 1 {
		t.Fatalf("live subscriptions = %d, want 1", got)
	}
}

// Subscribes, broadcasts, write failures and plain disconnects interleave
// across connections without a race or a leaked slot. Run under -race.
func TestTeardownUnderConcurrentChurn(t *testing.T) {
	server, rpcServer := startTestServer(t)

	const (
		conns  = 6
		rounds = 20
	)
	// The broadcaster runs until every connection goroutine is done: a
	// round that waits for its write to fail on the next broadcast must
	// get one. Every 5 ms keeps the momentum channel from overflowing.
	stopBroadcasting := make(chan struct{})
	broadcasterDone := make(chan struct{})
	go func() {
		defer close(broadcasterDone)
		for h := uint64(1); ; h++ {
			select {
			case <-stopBroadcasting:
				return
			default:
			}
			server.InsertMomentum(momentumAt(h))
			time.Sleep(5 * time.Millisecond)
		}
	}()

	var wg sync.WaitGroup
	for c := 0; c < conns; c++ {
		wg.Add(1)
		go func(c int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				serverEnd, clientEnd := net.Pipe()
				conn := &failableConn{Conn: serverEnd}
				codec := rpc.NewCodec(conn)
				served := make(chan struct{})
				go func() {
					rpcServer.ServeCodec(codec, 0)
					close(served)
				}()
				dec := json.NewDecoder(clientEnd)
				rawSubscribe(t, clientEnd, dec, 1, `["momentums"]`)
				// Drain notifications in the background so writes do not block.
				go func() {
					var raw json.RawMessage
					for dec.Decode(&raw) == nil {
					}
				}()
				time.Sleep(time.Duration(c+1) * time.Millisecond)
				if r%2 == 0 {
					conn.failWrites() // terminal failure on the next broadcast
				} else {
					clientEnd.Close() // normal disconnect
				}
				<-served
				serverEnd.Close()
				clientEnd.Close()
			}
		}(c)
	}
	wg.Wait()
	close(stopBroadcasting)
	<-broadcasterDone
	waitLive(t, server, 0)
}
