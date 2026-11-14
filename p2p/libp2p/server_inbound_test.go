package libp2p

import (
	"crypto/ecdsa"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// newInboundTestPeer adds one more remote identity to a capacity test
// server: its own connection, observed from addr, and a stream carrying a
// valid handshake. The stream blocks after the handshake so an adopted
// peer stays in peerMap until the test ends.
func newInboundTestPeer(t *testing.T, h *capacityTestHost, addr string) (*capacityTestConn, *capacityTestStream) {
	t.Helper()
	key := mustGenKey(t)
	id, err := PeerIDFromECDSA(key)
	if err != nil {
		t.Fatal(err)
	}
	conn := &capacityTestConn{remote: id, done: make(chan struct{}), addr: mustAddr(t, addr)}
	h.net.mu.Lock()
	if h.net.extra == nil {
		h.net.extra = make(map[peer.ID]*capacityTestConn)
	}
	h.net.extra[id] = conn
	h.net.mu.Unlock()
	hs := &protoHandshake{Version: baseProtocolVersion, Name: "inbound-test", ID: PubkeyToNodeID(&key.PublicKey)}
	s := newCapacityTestStream(t, conn, hs)
	s.readGate = make(chan struct{})
	// Registered after the server's own cleanup, so it runs first and the
	// adopted peer's read loop ends before that cleanup waits for it.
	t.Cleanup(func() { conn.Close() })
	return conn, s
}

// inboundAdopted reports whether the server holds conn's peer as an
// inbound peer.
func inboundAdopted(srv *Server, conn *capacityTestConn) bool {
	srv.peerMu.RLock()
	defer srv.peerMu.RUnlock()
	p := srv.peerMap[conn.remote.String()]
	return p != nil && p.inbound
}

func connClosed(conn *capacityTestConn) bool {
	select {
	case <-conn.done:
		return true
	default:
		return false
	}
}

// offerInbound runs one inbound handshake from addr and checks that it
// was adopted or refused as wanted. A refused peer must have its stream
// reset once and its connection closed, and must leave no pending slot.
func offerInbound(t *testing.T, srv *Server, h *capacityTestHost, addr string, wantAdopted bool) *capacityTestConn {
	t.Helper()
	conn, s := newInboundTestPeer(t, h, addr)
	srv.handleStream(s)
	if got := inboundAdopted(srv, conn); got != wantAdopted {
		t.Fatalf("inbound peer from %s adopted=%v, want %v", addr, got, wantAdopted)
	}
	if wantAdopted {
		if s.resets() != 0 || connClosed(conn) {
			t.Fatalf("adopted inbound peer from %s was reset or closed", addr)
		}
	} else if s.resets() != 1 || !connClosed(conn) {
		t.Fatalf("refused inbound peer from %s: resets=%d closed=%v, want 1 and true", addr, s.resets(), connClosed(conn))
	}
	srv.peerMu.RLock()
	pending := len(srv.pendingPeers)
	srv.peerMu.RUnlock()
	if pending != 0 {
		t.Fatalf("inbound handshake from %s left %d pending slots", addr, pending)
	}
	return conn
}

func TestOutboundReservationLimits(t *testing.T) {
	for _, tc := range []struct {
		maxPeers, minConnected int
		noDial                 bool
		reserved, inbound      int
		perOrigin              int
	}{
		{maxPeers: 60, minConnected: 16, reserved: 16, inbound: 44, perOrigin: 11}, // defaults
		{maxPeers: 60, minConnected: 16, noDial: true, reserved: 0, inbound: 60, perOrigin: 15},
		{maxPeers: 60, minConnected: 0, reserved: 0, inbound: 60, perOrigin: 15},
		{maxPeers: 60, minConnected: -1, reserved: 0, inbound: 60, perOrigin: 15},
		{maxPeers: 60, minConnected: 3, reserved: 3, inbound: 57, perOrigin: 14}, // docker devnet
		{maxPeers: 10, minConnected: 16, reserved: 5, inbound: 5, perOrigin: 2},
		{maxPeers: 16, minConnected: 16, reserved: 8, inbound: 8, perOrigin: 2},
		{maxPeers: 3, minConnected: 16, reserved: 1, inbound: 2, perOrigin: 2},
		{maxPeers: 2, minConnected: 1, reserved: 1, inbound: 1, perOrigin: 2},
		{maxPeers: 1, minConnected: 1, reserved: 0, inbound: 1, perOrigin: 2},
	} {
		srv := &Server{MaxPeers: tc.maxPeers, MinConnectedPeers: tc.minConnected, NoDial: tc.noDial}
		if got := srv.reservedOutbound(); got != tc.reserved {
			t.Errorf("%+v: reservedOutbound = %d", tc, got)
		}
		if got := srv.maxInboundPeers(); got != tc.inbound {
			t.Errorf("%+v: maxInboundPeers = %d", tc, got)
		}
		if got := srv.maxInboundPerOrigin(); got != tc.perOrigin {
			t.Errorf("%+v: maxInboundPerOrigin = %d", tc, got)
		}
		if srv.maxInboundPeers() < 1 {
			t.Errorf("%+v: no slot left for inbound peers", tc)
		}
	}
}

// TestInboundPeersLeaveReservedOutboundSlots pins the reservation:
// inbound peers fill their share of MaxPeers and no more, a dial still
// gets a slot after that, and a slot an inbound peer gives up can be
// taken by another inbound peer.
func TestInboundPeersLeaveReservedOutboundSlots(t *testing.T) {
	srv, h, _ := newCapacityTestServer(t)
	srv.MaxPeers, srv.MinConnectedPeers = 4, 2
	h.stream.readGate = make(chan struct{})

	// Loopback origins are not bounded per origin, so only the inbound
	// share (4 - 2) is in play.
	const loopback = "/ip4/127.0.0.1/tcp/1"
	first := offerInbound(t, srv, h, loopback, true)
	offerInbound(t, srv, h, loopback, true)
	offerInbound(t, srv, h, loopback, false)

	// The reserved slots are still there for a peer this node dials.
	if err := srv.dialPeer(peer.AddrInfo{ID: h.net.conn.remote}); err != nil {
		t.Fatalf("dial into a reserved slot: %v", err)
	}
	srv.peerMu.RLock()
	dialled := srv.peerMap[h.net.conn.remote.String()]
	total := len(srv.peerMap)
	srv.peerMu.RUnlock()
	if dialled == nil || dialled.inbound || total != 3 {
		t.Fatalf("dialled peer adopted=%v inbound=%v, peers=%d, want an outbound peer and 3 peers",
			dialled != nil, dialled != nil && dialled.inbound, total)
	}

	// One slot is free, but it is not an inbound slot.
	offerInbound(t, srv, h, loopback, false)

	// An inbound peer that leaves frees its place for another one.
	srv.peerMu.Lock()
	delete(srv.peerMap, first.remote.String())
	srv.peerMu.Unlock()
	offerInbound(t, srv, h, loopback, true)
	offerInbound(t, srv, h, loopback, false)
}

// TestInboundRefusalKeepsInFlightDial pins that a peer refused for the
// inbound share is treated like one refused at MaxPeers: while this node
// is dialling the same peer, only the stream is reset.
func TestInboundRefusalKeepsInFlightDial(t *testing.T) {
	srv, h, _ := newCapacityTestServer(t)
	srv.MaxPeers, srv.MinConnectedPeers = 2, 1
	offerInbound(t, srv, h, "/ip4/127.0.0.1/tcp/1", true)

	conn, s := newInboundTestPeer(t, h, "/ip4/127.0.0.1/tcp/1")
	srv.peerMu.Lock()
	srv.dialing[conn.remote.String()] = struct{}{}
	srv.peerMu.Unlock()
	srv.handleStream(s)
	if inboundAdopted(srv, conn) || s.resets() != 1 {
		t.Fatalf("adopted=%v resets=%d, want a refused and reset stream", inboundAdopted(srv, conn), s.resets())
	}
	if got, _ := h.net.closes(); got != 0 || connClosed(conn) {
		t.Fatalf("refusal closed the peer %d times while a dial to it was in flight", got)
	}
}

// TestInboundOriginBound pins the per-origin bound: one origin group
// holds at most maxInboundPerOrigin inbound peers while other groups are
// still admitted.
func TestInboundOriginBound(t *testing.T) {
	srv, h, _ := newCapacityTestServer(t)
	srv.MaxPeers = 12 // 12 inbound slots, 3 per origin group
	if got := srv.maxInboundPerOrigin(); got != 3 {
		t.Fatalf("maxInboundPerOrigin = %d, want 3", got)
	}

	// IPv4 groups are /16 prefixes.
	first := offerInbound(t, srv, h, "/ip4/198.51.100.1/tcp/4001", true)
	offerInbound(t, srv, h, "/ip4/198.51.100.2/tcp/4001", true)
	offerInbound(t, srv, h, "/ip4/198.51.7.9/tcp/4001", true)
	offerInbound(t, srv, h, "/ip4/198.51.100.3/tcp/4001", false)
	offerInbound(t, srv, h, "/ip4/198.51.250.250/tcp/4001", false)
	offerInbound(t, srv, h, "/ip4/203.0.113.5/tcp/4001", true)

	// IPv6 groups are /48 prefixes.
	offerInbound(t, srv, h, "/ip6/2001:db8:1::1/tcp/4001", true)
	offerInbound(t, srv, h, "/ip6/2001:db8:1:5::2/tcp/4001", true)
	offerInbound(t, srv, h, "/ip6/2001:db8:1:ffff::3/tcp/4001", true)
	offerInbound(t, srv, h, "/ip6/2001:db8:1::4/tcp/4001", false)
	offerInbound(t, srv, h, "/ip6/2001:db8:2::1/tcp/4001", true)

	// A place in a group is held by an adopted peer only: when one
	// leaves, the group admits another.
	srv.peerMu.Lock()
	delete(srv.peerMap, first.remote.String())
	srv.peerMu.Unlock()
	offerInbound(t, srv, h, "/ip4/198.51.100.3/tcp/4001", true)
	offerInbound(t, srv, h, "/ip4/198.51.100.4/tcp/4001", false)

	// An outbound peer does not count against its origin's group.
	srv.peerMu.Lock()
	for _, p := range srv.peerMap {
		if p.originGroup == "4:203.0" {
			p.inbound, p.originGroup = false, ""
		}
	}
	srv.peerMu.Unlock()
	offerInbound(t, srv, h, "/ip4/203.0.113.6/tcp/4001", true)
}

// TestInboundOriginBoundExemptions pins which inbound peers the
// per-origin bound leaves alone: local and private origins, which is
// what a single-host or container test network shows for every peer, and
// peers the operator listed in BootstrapPeers.
func TestInboundOriginBoundExemptions(t *testing.T) {
	for _, addr := range []string{
		"/ip4/127.0.0.1/tcp/4001",
		"/ip6/::1/tcp/4001",
		"/ip4/10.1.2.3/tcp/4001",
		"/ip4/172.30.0.10/tcp/4001", // docker/devnet
		"/ip4/192.168.1.20/tcp/4001",
		"/ip4/169.254.10.10/tcp/4001",
		"/ip6/fd00:1::1/tcp/4001",
		"/ip6/fe80::1/tcp/4001",
	} {
		t.Run(addr, func(t *testing.T) {
			srv, h, _ := newCapacityTestServer(t)
			srv.MaxPeers = 8 // 2 per origin group
			for i := 0; i < 5; i++ {
				offerInbound(t, srv, h, addr, true)
			}
		})
	}

	t.Run("shared address space is bounded", func(t *testing.T) {
		srv, h, _ := newCapacityTestServer(t)
		srv.MaxPeers = 8
		offerInbound(t, srv, h, "/ip4/100.64.0.1/tcp/4001", true)
		offerInbound(t, srv, h, "/ip4/100.64.0.2/tcp/4001", true)
		offerInbound(t, srv, h, "/ip4/100.64.0.3/tcp/4001", false)
	})

	t.Run("bootstrap peer", func(t *testing.T) {
		srv, h, _ := newCapacityTestServer(t)
		srv.MaxPeers = 8
		const addr = "/ip4/198.51.100.1/tcp/4001"
		offerInbound(t, srv, h, addr, true)
		offerInbound(t, srv, h, addr, true)

		listed, s := newInboundTestPeer(t, h, addr)
		srv.BootstrapPeers = []peer.AddrInfo{{ID: listed.remote}}
		srv.handleStream(s)
		if !inboundAdopted(srv, listed) {
			t.Fatal("a peer listed in BootstrapPeers was refused for its origin")
		}
		// It does not use up a place in the group either, and the group
		// stays closed to peers that are not listed.
		offerInbound(t, srv, h, addr, false)
	})
}

func TestInboundOriginGroup(t *testing.T) {
	srv := &Server{}
	for _, tc := range []struct{ addr, want string }{
		{"/ip4/198.51.100.7/tcp/4001", "4:198.51"},
		{"/ip4/100.64.3.4/tcp/4001", "4:100.64"},
		{"/ip6/2001:db8:1:2::3/tcp/4001", subnetKey(net.ParseIP("2001:db8:1::"))},
		{"/ip6/::ffff:198.51.100.7/tcp/4001", "4:198.51"},
		{"/ip4/203.0.113.1/tcp/4001/p2p/16Uiu2HAmRe2RazMPxYEV1A8Vs7LbVfNVMqBdiw7zgSkBLB3E2vQv/p2p-circuit", relayedOrigin},
		{"/dns4/example.org/tcp/4001", unobservedGroup},
		{"/ip4/127.0.0.1/tcp/4001", ""},
		{"/ip4/172.30.0.11/tcp/4001", ""},
		{"/ip6/::ffff:10.0.0.1/tcp/4001", ""},
	} {
		conn := &capacityTestConn{addr: mustAddr(t, tc.addr)}
		s := &capacityTestStream{conn: conn}
		if got := srv.inboundOriginGroup("some-peer", s); got != tc.want {
			t.Errorf("inboundOriginGroup(%s) = %q, want %q", tc.addr, got, tc.want)
		}
	}
	if got := srv.inboundOriginGroup("some-peer", nil); got != unobservedGroup {
		t.Errorf("inboundOriginGroup(no stream) = %q, want %q", got, unobservedGroup)
	}
}

func TestOutboundDeficit(t *testing.T) {
	for _, tc := range []struct {
		maxPeers, minConnected int
		inbound, outbound      int
		want                   int
	}{
		{maxPeers: 60, minConnected: 16, inbound: 0, outbound: 0, want: 16},
		{maxPeers: 60, minConnected: 16, inbound: 16, outbound: 0, want: 16}, // inbound peers do not count
		{maxPeers: 60, minConnected: 16, inbound: 44, outbound: 10, want: 6},
		{maxPeers: 60, minConnected: 16, inbound: 44, outbound: 16, want: 0},
		{maxPeers: 60, minConnected: 16, inbound: 3, outbound: 20, want: 0},
		{maxPeers: 10, minConnected: 16, inbound: 5, outbound: 3, want: 2}, // limited to free slots
		{maxPeers: 10, minConnected: 16, inbound: 5, outbound: 5, want: 0},
		{maxPeers: 60, minConnected: 0, inbound: 5, outbound: 0, want: 0},
	} {
		srv := &Server{MaxPeers: tc.maxPeers, MinConnectedPeers: tc.minConnected, peerMap: make(map[string]*Peer)}
		for i := 0; i < tc.inbound+tc.outbound; i++ {
			srv.peerMap[fmt.Sprint(i)] = &Peer{inbound: i < tc.inbound}
		}
		deficit, peers, outbound := srv.outboundDeficit()
		if deficit != tc.want || peers != tc.inbound+tc.outbound || outbound != tc.outbound {
			t.Errorf("%+v: outboundDeficit = %d (peers %d, outbound %d)", tc, deficit, peers, outbound)
		}
	}
}

// waitForPeerGone polls until srv no longer holds id as a peer.
func waitForPeerGone(t *testing.T, srv *Server, id peer.ID, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		srv.peerMu.RLock()
		_, connected := srv.peerMap[id.String()]
		srv.peerMu.RUnlock()
		if !connected {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s still holds peer %s after %s", srv.Name, id, timeout)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// waitForOutboundPeer polls until srv holds id as a peer it dialled.
func waitForOutboundPeer(t *testing.T, srv *Server, id peer.ID, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		srv.peerMu.RLock()
		p := srv.peerMap[id.String()]
		outbound := p != nil && !p.inbound
		srv.peerMu.RUnlock()
		if outbound {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not dial %s within %s", srv.Name, id, timeout)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func loopbackInfo(t *testing.T, key *ecdsa.PrivateKey, port int) peer.AddrInfo {
	t.Helper()
	pid, err := PeerIDFromECDSA(key)
	if err != nil {
		t.Fatalf("derive peer ID: %v", err)
	}
	return peer.AddrInfo{ID: pid, Addrs: []ma.Multiaddr{mustAddr(t, fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", port))}}
}

// TestInboundCannotTakeReservedSlotOverLoopback runs the reservation
// between real hosts. A has two slots and dials for one peer, so one slot
// is reserved: the first inbound peer is adopted, the second is refused,
// and A's own dial then takes the remaining slot.
func TestInboundCannotTakeReservedSlotOverLoopback(t *testing.T) {
	srvA := &Server{
		PrivateKey:        mustGenKey(t),
		Name:              "reservation-target",
		MaxPeers:          2,
		MinConnectedPeers: 1,
		ListenAddr:        "127.0.0.1:0",
	}
	if err := srvA.Start(); err != nil {
		t.Fatalf("start A: %v", err)
	}
	defer srvA.Stop()
	infoA := peer.AddrInfo{ID: srvA.host.ID(), Addrs: srvA.host.Addrs()}

	start := func(name string) *Server {
		srv := newTestServer(mustGenKey(t), "", t)
		srv.Name = name
		if err := srv.Start(); err != nil {
			t.Fatalf("start %s: %v", name, err)
		}
		t.Cleanup(srv.Stop)
		return srv
	}
	srvB, srvC, srvD := start("first-inbound"), start("second-inbound"), start("dial-target")

	dialUntilConnected(t, srvB, infoA, 10*time.Second)
	waitForPeerCount(t, srvA, 1, 10*time.Second)

	// A sends its handshake before it decides, so C's dial can report
	// success and then lose the session. Either way C must end up without
	// A, and A without C.
	_ = srvC.dialPeer(infoA)
	waitForPeerGone(t, srvC, srvA.host.ID(), 5*time.Second)
	srvA.peerMu.RLock()
	_, adoptedC := srvA.peerMap[srvC.host.ID().String()]
	peersA := len(srvA.peerMap)
	srvA.peerMu.RUnlock()
	if adoptedC || peersA != 1 {
		t.Fatalf("A adopted the second inbound peer=%v and has %d peers, want false and 1", adoptedC, peersA)
	}

	if err := srvA.dialPeer(peer.AddrInfo{ID: srvD.host.ID(), Addrs: srvD.host.Addrs()}); err != nil {
		t.Fatalf("A could not dial into its reserved slot: %v", err)
	}
	if got := srvA.PeerCount(); got != 2 {
		t.Fatalf("A has %d peers, want 2", got)
	}
	if got := srvA.host.Network().Connectedness(srvB.host.ID()); got != network.Connected {
		t.Fatalf("Connectedness(B) = %v, want Connected", got)
	}
}

// TestBootstrapRedialCountsOutboundOnly is the churn case: A dialled its
// bootstrap peer, gained an inbound peer, then lost the bootstrap peer.
// A still has MinConnectedPeers peers in total, and must redial all the
// same because none of them is a peer it dialled.
func TestBootstrapRedialCountsOutboundOnly(t *testing.T) {
	keyC := mustGenKey(t)
	portC := freePortTCP(t)
	infoC := loopbackInfo(t, keyC, portC)
	newC := func() *Server {
		srv := newTestServer(keyC, "", t)
		srv.Name = "bootstrap"
		srv.ListenAddr = fmt.Sprintf("127.0.0.1:%d", portC)
		return srv
	}
	srvC := newC()
	if err := srvC.Start(); err != nil {
		t.Fatalf("start C: %v", err)
	}
	stopC := srvC.Stop
	defer func() { stopC() }()

	srvA := &Server{
		PrivateKey:        mustGenKey(t),
		Name:              "redial-node",
		MaxPeers:          8,
		MinConnectedPeers: 1,
		ListenAddr:        "127.0.0.1:0",
		BootstrapPeers:    []peer.AddrInfo{infoC},
	}
	if err := srvA.Start(); err != nil {
		t.Fatalf("start A: %v", err)
	}
	defer srvA.Stop()
	dialUntilConnected(t, srvA, infoC, 10*time.Second)

	srvB := newTestServer(mustGenKey(t), "", t)
	srvB.Name = "inbound-peer"
	if err := srvB.Start(); err != nil {
		t.Fatalf("start B: %v", err)
	}
	defer srvB.Stop()
	dialUntilConnected(t, srvB, peer.AddrInfo{ID: srvA.host.ID(), Addrs: srvA.host.Addrs()}, 10*time.Second)
	waitForPeerCount(t, srvA, 2, 10*time.Second)

	// The bootstrap peer goes away and comes back.
	srvC.Stop()
	waitForPeerGone(t, srvA, infoC.ID, 10*time.Second)
	srvC = newC()
	if err := srvC.Start(); err != nil {
		t.Fatalf("restart C: %v", err)
	}
	stopC = srvC.Stop

	if deficit, peers, outbound := srvA.outboundDeficit(); deficit != 1 || peers != 1 || outbound != 0 {
		t.Fatalf("outboundDeficit = %d (peers %d, outbound %d), want 1 (1, 0)", deficit, peers, outbound)
	}
	srvA.redialBootstrapPeers()
	waitForOutboundPeer(t, srvA, infoC.ID, 10*time.Second)

	// At the outbound target a sweep has nothing left to do.
	if deficit, _, _ := srvA.outboundDeficit(); deficit != 0 {
		t.Fatalf("outboundDeficit = %d with the bootstrap peer connected, want 0", deficit)
	}
}

// TestDHTDialCountsOutboundOnly is the same gate on the discovery loop:
// with one inbound peer and MinConnectedPeers of one, A still dials a
// candidate from its routing table.
func TestDHTDialCountsOutboundOnly(t *testing.T) {
	srvA := &Server{
		PrivateKey:        mustGenKey(t),
		Name:              "discovery-node",
		MaxPeers:          8,
		MinConnectedPeers: 1,
		Discovery:         true,
		ListenAddr:        "127.0.0.1:0",
	}
	if err := srvA.Start(); err != nil {
		t.Fatalf("start A: %v", err)
	}
	defer srvA.Stop()

	srvB := newTestServer(mustGenKey(t), "", t)
	srvB.Name = "inbound-peer"
	if err := srvB.Start(); err != nil {
		t.Fatalf("start B: %v", err)
	}
	defer srvB.Stop()
	dialUntilConnected(t, srvB, peer.AddrInfo{ID: srvA.host.ID(), Addrs: srvA.host.Addrs()}, 10*time.Second)
	waitForPeerCount(t, srvA, 1, 10*time.Second)

	// C serves the DHT protocol, so it stays in A's routing table while
	// the sweep's refresh runs. Nothing has connected to it yet.
	srvC := newTestServer(mustGenKey(t), "", t)
	srvC.Name = "candidate"
	srvC.Discovery = true
	if err := srvC.Start(); err != nil {
		t.Fatalf("start C: %v", err)
	}
	defer srvC.Stop()
	srvA.host.Peerstore().AddAddrs(srvC.host.ID(), srvC.host.Addrs(), time.Hour)
	if added, err := srvA.dht.RoutingTable().TryAddPeer(srvC.host.ID(), true, false); err != nil || !added {
		t.Fatalf("add C to A's routing table: added=%v err=%v", added, err)
	}

	srvA.dialDHTCandidates()
	waitForOutboundPeer(t, srvA, srvC.host.ID(), 10*time.Second)
}
