package legacy

import (
	"crypto/ecdsa"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/p2p/discover"
)

// startShutdownServer starts srv on a loopback listener with dialing off,
// so the only connections are the ones a test makes itself.
func startShutdownServer(t *testing.T, srv *Server) {
	t.Helper()
	if srv.PrivateKey == nil {
		key, err := crypto.GenerateKey()
		if err != nil {
			t.Fatal(err)
		}
		srv.PrivateKey = key
	}
	srv.ListenAddr = "127.0.0.1:0"
	srv.NoDial = true
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
}

// dialHandshake connects to srv as cliKey and runs both handshakes,
// advertising caps. It returns the connection on success; on failure it
// closes the connection and returns the handshake error.
func dialHandshake(t *testing.T, srv *Server, cliKey *ecdsa.PrivateKey, caps []p2p.Cap) (net.Conn, error) {
	t.Helper()
	fd, err := net.Dial("tcp", srv.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	addr := srv.listener.Addr().(*net.TCPAddr)
	dest := &discover.Node{ID: discover.PubkeyID(&srv.PrivateKey.PublicKey), IP: addr.IP, TCP: uint16(addr.Port), UDP: uint16(addr.Port)}
	cli := newRLPX(fd).(*rlpx)
	if _, err := cli.doEncHandshake(cliKey, dest); err != nil {
		fd.Close()
		return nil, err
	}
	if _, err := cli.doProtoHandshake(&protoHandshake{Version: baseProtocolVersion, Name: "probe", ID: discover.PubkeyID(&cliKey.PublicKey), Caps: caps}); err != nil {
		fd.Close()
		return nil, err
	}
	// Drain whatever the server writes so its disconnect frame never
	// blocks on a full socket.
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := fd.Read(buf); err != nil {
				return
			}
		}
	}()
	return fd, nil
}

func stopAsync(srv *Server) <-chan struct{} {
	stopped := make(chan struct{})
	go func() {
		srv.Stop()
		close(stopped)
	}()
	return stopped
}

// Stop must not return while a peer's goroutine is still inside work the
// server owns, and neither may a concurrent Stop that found the server
// already stopping. The new-peer hook runs on the runPeer goroutine, which
// the run loop counts in loopWG; holding it there models a protocol
// handler that has not finished.
func TestStopWaitsForGatedRunPeer(t *testing.T) {
	entered := make(chan struct{})
	gate := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(gate) }) }
	srv := &Server{
		MaxPeers: 1,
		newPeerHook: func(*Peer) {
			close(entered)
			<-gate
		},
	}
	startShutdownServer(t, srv)
	t.Cleanup(func() {
		release()
		srv.Stop()
	})

	cliKey, _ := crypto.GenerateKey()
	fd, err := dialHandshake(t, srv, cliKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer fd.Close()
	select {
	case <-entered:
	case <-time.After(testTimeout):
		t.Fatal("peer never reached runPeer")
	}

	// Three concurrent callers: the first closes quit, the others find the
	// server already stopping. All of them must wait for the cleanup.
	var stops []<-chan struct{}
	for i := 0; i < 3; i++ {
		stops = append(stops, stopAsync(srv))
	}
	for i, stopped := range stops {
		select {
		case <-stopped:
			t.Fatalf("Stop call %d returned while a peer goroutine was still held in the new-peer hook", i)
		case <-time.After(200 * time.Millisecond):
		}
	}
	release()
	for i, stopped := range stops {
		select {
		case <-stopped:
		case <-time.After(testTimeout):
			t.Fatalf("Stop call %d did not return after the hook was released", i)
		}
	}
}

// A checkpoint whose waiting side has already left on quit must not strand
// the run loop on the unbuffered reply. The test hands the loop a
// connection at each stage with nobody reading its reply channel, stops
// the server, and requires the loop to exit.
func TestStopDoesNotStrandRunLoopOnCheckpoint(t *testing.T) {
	for _, stage := range []string{"posthandshake", "addpeer"} {
		t.Run(stage, func(t *testing.T) {
			// MaxPeers 0 makes every stage check answer DiscTooManyPeers
			// whatever the server's state, so the loop always replies on
			// cont and never admits the connection.
			srv := &Server{MaxPeers: 0}
			startShutdownServer(t, srv)
			t.Cleanup(func() {
				// A stranded loop would hang Stop; keep cleanup bounded.
				select {
				case <-stopAsync(srv):
				case <-time.After(testTimeout):
					t.Error("Stop hung in cleanup")
				}
			})
			fd, other := net.Pipe()
			t.Cleanup(func() { fd.Close(); other.Close() })
			c := &conn{fd: fd, cont: make(chan error), id: discover.NodeID{7}}
			stageCh := srv.posthandshake
			if stage == "addpeer" {
				stageCh = srv.addpeer
			}
			select {
			case stageCh <- c:
			case <-time.After(testTimeout):
				t.Fatal("run loop did not take the checkpoint")
			}
			// Nobody reads c.cont: the checkpoint's sender left on quit.
			stopped := stopAsync(srv)
			select {
			case <-stopped:
			case <-time.After(testTimeout):
				t.Fatal("Stop did not return")
			}
			loopDone := make(chan struct{})
			go func() {
				srv.loopWG.Wait()
				close(loopDone)
			}()
			select {
			case <-loopDone:
			case <-time.After(testTimeout):
				t.Fatalf("run loop stranded on the %s reply after Stop", stage)
			}
		})
	}
}

// Peer.run must not return before the protocol goroutines it started have
// returned: the server removes the peer right after run returns, and a
// protocol handler still inside Run then holds a registration the next
// connection with the same identity trips over.
func TestPeerRunJoinsProtocolGoroutines(t *testing.T) {
	tr := newHeldTransport()
	gate := make(chan struct{})
	atGate := make(chan struct{})
	var returned int32
	proto := p2p.Protocol{
		Name: "zen", Version: 1, Length: 1,
		Run: func(_ p2p.Peer, rw p2p.MsgReadWriter) error {
			_, err := rw.ReadMsg() // io.EOF once the peer closes
			close(atGate)
			<-gate
			atomic.StoreInt32(&returned, 1)
			return err
		},
	}
	p, done := runTestPeer(t, tr, nil, []p2p.Cap{{Name: "zen", Version: 1}}, []p2p.Protocol{proto})
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	})

	go p.Disconnect(p2p.DiscQuitting)
	select {
	case <-atGate:
	case <-time.After(testTimeout):
		t.Fatal("protocol never observed the close")
	}
	select {
	case r := <-done:
		t.Fatalf("run returned %v while the protocol goroutine was still inside Run", r)
	case <-time.After(200 * time.Millisecond):
	}
	close(gate)
	expectReason(t, done, p2p.DiscQuitting)
	if atomic.LoadInt32(&returned) != 1 {
		t.Fatal("protocol goroutine had not returned when run returned")
	}
}

// A dial-history wait task is counted in loopWG, so Stop waits for it; it
// must give up on quit rather than sleep out its duration.
func TestWaitExpireTaskReturnsOnQuit(t *testing.T) {
	srv := &Server{quit: make(chan struct{})}
	close(srv.quit)
	done := make(chan struct{})
	go func() {
		waitExpireTask{Duration: time.Hour}.Do(srv)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("wait task slept through quit")
	}
}

// The protocol stream must expose the peer's close signal, so a handler
// wait that is not bound to the connection (a downloader delivery) can end
// with the peer. A handler that blocks on that signal returns once the
// peer disconnects; without the signal it would block run forever.
func TestProtoRWExposesPeerClose(t *testing.T) {
	tr := newHeldTransport()
	proto := p2p.Protocol{
		Name: "zen", Version: 1, Length: 1,
		Run: func(_ p2p.Peer, rw p2p.MsgReadWriter) error {
			c, ok := rw.(interface{ Closed() <-chan struct{} })
			if !ok {
				select {} // no signal to wait on: hold run forever
			}
			<-c.Closed()
			return nil
		},
	}
	p, done := runTestPeer(t, tr, nil, []p2p.Cap{{Name: "zen", Version: 1}}, []p2p.Protocol{proto})
	go p.Disconnect(p2p.DiscQuitting)
	expectReason(t, done, p2p.DiscQuitting)
}

// Repeated and concurrent Stop calls all return; none panics or waits on
// another.
func TestStopIsIdempotentAndConcurrent(t *testing.T) {
	srv := &Server{MaxPeers: 1}
	startShutdownServer(t, srv)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			srv.Stop()
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("concurrent Stop calls did not all return")
	}
	srv.Stop()
}

// A protocol handler models the protocol manager's peer registry: Run
// registers the identity, and its cleanup unregisters it. The first
// handler's cleanup is gated. While it is gated, a reconnect with the same
// identity must not get as far as seeing the stale registration; once the
// cleanup finishes, the same identity connects normally.
func TestSameIdentityReconnectDuringGatedCleanup(t *testing.T) {
	var mu sync.Mutex
	registry := map[string]bool{}
	var stale, runs int32
	gate := make(chan struct{})
	atGate := make(chan struct{})
	proto := p2p.Protocol{
		Name: "zen", Version: 1, Length: 1,
		Run: func(pp p2p.Peer, rw p2p.MsgReadWriter) error {
			key := fmt.Sprint(pp.ID())
			mu.Lock()
			if registry[key] {
				atomic.StoreInt32(&stale, 1)
			}
			registry[key] = true
			mu.Unlock()
			first := atomic.AddInt32(&runs, 1) == 1
			defer func() {
				if first {
					close(atGate)
					<-gate
				}
				mu.Lock()
				delete(registry, key)
				mu.Unlock()
			}()
			for {
				if _, err := rw.ReadMsg(); err != nil {
					return err
				}
			}
		},
	}
	srv := &Server{MaxPeers: 2, Protocols: []p2p.Protocol{proto}}
	startShutdownServer(t, srv)
	t.Cleanup(func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
		srv.Stop()
	})
	cliKey, _ := crypto.GenerateKey()
	caps := []p2p.Cap{{Name: "zen", Version: 1}}
	key := fmt.Sprint(discover.PubkeyID(&cliKey.PublicKey))
	registered := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return registry[key]
	}

	first, err := dialHandshake(t, srv, cliKey, caps)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first connection to register", registered)
	first.Close()
	select {
	case <-atGate:
	case <-time.After(testTimeout):
		t.Fatal("first handler never reached its gated cleanup")
	}
	// Give the server every chance to drop the peer early (it must not).
	deadline := time.Now().Add(300 * time.Millisecond)
	for srv.PeerCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if second, err := dialHandshake(t, srv, cliKey, caps); err == nil {
		// Admitted: the handler ran and must not have found the old entry.
		defer second.Close()
		time.Sleep(100 * time.Millisecond)
	} else if err != p2p.DiscAlreadyConnected {
		// Rejected: the only acceptable reason is that the first
		// connection is still registered with the transport.
		t.Fatalf("second attempt rejected with %v, want %v", err, p2p.DiscAlreadyConnected)
	}
	if atomic.LoadInt32(&stale) != 0 {
		t.Fatal("reconnect with the same identity observed the stale registration")
	}

	close(gate)
	waitFor(t, "first peer to be removed", func() bool { return srv.PeerCount() == 0 })
	waitFor(t, "registry to clear", func() bool { return !registered() })
	third, err := dialHandshake(t, srv, cliKey, caps)
	if err != nil {
		t.Fatalf("reconnect after cleanup rejected: %v", err)
	}
	defer third.Close()
	waitFor(t, "reconnect to register", registered)
	if atomic.LoadInt32(&stale) != 0 {
		t.Fatal("a later connection observed a stale registration")
	}
}
