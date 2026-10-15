package discover

import (
	"bytes"
	"crypto/ecdsa"
	"fmt"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

// fakeConn is a datagram socket that never delivers anything on its own:
// reads block until Close and writes are recorded by destination and packet
// type. Test packets are fed to the real decoder through udp.handlePacket
// instead. The packet hooks run after outbound packets are recorded so a
// test can answer from the write itself, after udp registered its reply.
type fakeConn struct {
	mu           sync.Mutex
	closed       chan struct{}
	closeOnce    sync.Once
	sent         map[string][]byte // destination -> packet types written
	pingSent     func(addr *net.UDPAddr)
	findnodeSent func(packet []byte, addr *net.UDPAddr)
}

func newFakeConn() *fakeConn {
	return &fakeConn{closed: make(chan struct{}), sent: make(map[string][]byte)}
}

func (c *fakeConn) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) {
	<-c.closed
	return 0, nil, io.EOF
}

func (c *fakeConn) WriteToUDP(b []byte, addr *net.UDPAddr) (int, error) {
	c.mu.Lock()
	c.sent[addr.String()] = append(c.sent[addr.String()], b[headSize])
	pingSent, findnodeSent := c.pingSent, c.findnodeSent
	c.mu.Unlock()
	switch b[headSize] {
	case pingPacket:
		if pingSent != nil {
			pingSent(addr)
		}
	case findnodePacket:
		if findnodeSent != nil {
			findnodeSent(b, addr)
		}
	}
	return len(b), nil
}

func (c *fakeConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *fakeConn) LocalAddr() net.Addr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 30303}
}

// count returns how many packets of the given type were written to addr.
func (c *fakeConn) count(addr *net.UDPAddr, ptype byte) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, p := range c.sent[addr.String()] {
		if p == ptype {
			n++
		}
	}
	return n
}

func (c *fakeConn) countAll(ptype byte) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, ps := range c.sent {
		for _, p := range ps {
			if p == ptype {
				n++
			}
		}
	}
	return n
}

// gate sits between the table and the real transport. With release set, it
// parks every bonding process at the point where it would send its ping
// until release is closed. Parking here rather than in the socket write
// matters: udp.ping registers its reply, and with it the response deadline,
// before it writes, so a bond held at the write could time out while parked.
// A bond held here has not started its exchange yet.
//
// Parking is independent of shutdown, so a test can observe what closing
// the table does to the processes still waiting for a slot while the parked
// ones keep holding theirs; testUDP.close lets the parked ones go afterwards.
type gate struct {
	udp     *udp
	release chan struct{} // nil when pings are not held

	// pingWanted runs once waitping has registered the ping it expects from
	// id, so that a ping delivered in response is matched rather than
	// handled as unsolicited.
	pingWanted func(id NodeID)
}

func (g *gate) ping(id NodeID, addr *net.UDPAddr) error {
	if g.release != nil {
		<-g.release
	}
	return g.udp.ping(id, addr)
}

// waitping mirrors udp.waitping, with the registration and the wait split
// around the pingWanted hook.
func (g *gate) waitping(id NodeID) error {
	errc := g.udp.pending(id, pingPacket, func(interface{}) bool { return true })
	if g.pingWanted != nil {
		g.pingWanted(id)
	}
	return <-errc
}

func (g *gate) findnode(toid NodeID, addr *net.UDPAddr, target NodeID) ([]*Node, error) {
	return g.udp.findnode(toid, addr, target)
}

func (g *gate) close() {
	g.udp.close()
}

type testUDP struct {
	t    *testing.T
	tab  *Table
	udp  *udp
	conn *fakeConn
	gate *gate

	mu         sync.Mutex
	responders map[string]*sender // destination -> identity answering pings sent there

	closeOnce   sync.Once
	releaseOnce sync.Once
}

// newTestUDP starts a transport on a fake socket. With holdPings set,
// bonding processes park before their ping until u.release is called.
func newTestUDP(t *testing.T, holdPings bool) *testUDP {
	t.Helper()
	priv, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	u := &testUDP{t: t, conn: newFakeConn(), responders: make(map[string]*sender)}
	u.conn.pingSent = u.answerPing
	u.tab, u.udp = newUDP(priv, u.conn, nil, "")
	u.gate = &gate{udp: u.udp}
	if holdPings {
		u.gate.release = make(chan struct{})
	}
	u.tab.net = u.gate
	return u
}

// release lets parked bonding processes send their pings.
func (u *testUDP) release() {
	u.releaseOnce.Do(func() {
		if u.gate.release != nil {
			close(u.gate.release)
		}
	})
}

// closeTable closes the table once.
func (u *testUDP) closeTable() {
	u.closeOnce.Do(u.tab.Close)
}

// close shuts the table down, lets any parked bonding processes go so they
// fail through the closed transport, and waits for every goroutine the
// transport started. Close does not join them itself, so this is also where
// each test checks that they all exit.
func (u *testUDP) close(t *testing.T, timeout time.Duration) {
	t.Helper()
	u.closeTable()
	u.release()
	done := make(chan struct{})
	go func() {
		u.udp.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("transport goroutines still running %v after close", timeout)
	}
}

// respond makes s answer every ping sent to it. The pong is delivered from
// the write of the ping, right after the reply was registered, so a bond
// with a responder completes as soon as it is allowed to start and never
// depends on the response timeout.
func (u *testUDP) respond(s *sender) {
	u.mu.Lock()
	u.responders[s.addr.String()] = s
	u.mu.Unlock()
}

func (u *testUDP) answerPing(addr *net.UDPAddr) {
	u.mu.Lock()
	s := u.responders[addr.String()]
	u.mu.Unlock()
	if s == nil {
		return
	}
	if err := s.deliver(u, pongPacket, &pong{
		To:         makeEndpoint(u.conn.LocalAddr().(*net.UDPAddr), 30303),
		Expiration: uint64(time.Now().Add(expiration).Unix()),
	}); err != nil {
		u.t.Errorf("pong from %v rejected: %v", s.addr, err)
	}
}

// admitted returns how many inbound bonds currently hold a permit.
func (u *testUDP) admitted() int {
	u.tab.bondmu.Lock()
	defer u.tab.bondmu.Unlock()
	return len(u.tab.inbound)
}

// freeSlots returns how many bonding slots are not held by a process.
func (u *testUDP) freeSlots() int {
	return len(u.tab.bondslots)
}

func (u *testUDP) bondingLen() int {
	u.tab.bondmu.Lock()
	defer u.tab.bondmu.Unlock()
	return len(u.tab.bonding)
}

// isBonding reports whether a bonding process is registered for id.
func (u *testUDP) isBonding(id NodeID) bool {
	u.tab.bondmu.Lock()
	defer u.tab.bondmu.Unlock()
	_, ok := u.tab.bonding[id]
	return ok
}

func (u *testUDP) bonded(s *sender) bool {
	return u.tab.db.node(s.id()) != nil
}

// waitFor polls cond until it holds or the deadline passes. It expresses
// conditions that are reached eventually rather than at a fixed delay.
func (u *testUDP) waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not reached within %v", what, timeout)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// sender is a remote discovery identity with its own key and address.
type sender struct {
	priv *ecdsa.PrivateKey
	addr *net.UDPAddr
}

func newSender(t *testing.T, port int) *sender {
	t.Helper()
	priv, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return &sender{priv: priv, addr: &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: port}}
}

func (s *sender) id() NodeID {
	return PubkeyID(&s.priv.PublicKey)
}

// deliver signs a packet from s and feeds it to the real decoder. It is
// safe to call from any goroutine.
func (s *sender) deliver(u *testUDP, ptype byte, req interface{}) error {
	packet, err := encodePacket(s.priv, ptype, req)
	if err != nil {
		return err
	}
	return u.udp.handlePacket(s.addr, packet)
}

func (s *sender) pingPacket(u *testUDP) *ping {
	return &ping{
		Version:    Version,
		From:       makeEndpoint(s.addr, uint16(s.addr.Port)),
		To:         makeEndpoint(u.conn.LocalAddr().(*net.UDPAddr), 30303),
		Expiration: uint64(time.Now().Add(expiration).Unix()),
	}
}

// ping delivers a valid, unexpired, correctly versioned ping from s.
func (s *sender) ping(t *testing.T, u *testUDP) {
	t.Helper()
	if err := s.deliver(u, pingPacket, s.pingPacket(u)); err != nil {
		t.Fatalf("ping from %v rejected: %v", s.addr, err)
	}
}

// rpcNodeTestIDs returns the secp256k1 generator and the nonzero point
// (1, 1), which does not satisfy the curve equation y^2 = x^3 + 7.
func rpcNodeTestIDs() (valid, offCurve NodeID) {
	valid = MustHexID("79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798" +
		"483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8")
	offCurve[len(offCurve)/2-1], offCurve[len(offCurve)-1] = 1, 1
	return valid, offCurve
}

// nonCanonicalRPCNodeIDs adds the field prime to X and Y, respectively,
// of two valid points with small coordinates. Both encodings fit in a
// NodeID, but neither is a canonical secp256k1 public key encoding.
func nonCanonicalRPCNodeIDs(t *testing.T) [2]NodeID {
	t.Helper()
	ids := [2]NodeID{
		MustHexID("0000000000000000000000000000000000000000000000000000000000000001" +
			"4218f20ae6c646b363db68605822fb14264ca8d2587fdd6fbc750d587e76a7ee"),
		MustHexID("1fe1e5ef3fceb5c135ab7741333ce5a6e80d68167653f6b2b24bcbcfaaaff507" +
			"0000000000000000000000000000000000000000000000000000000000000001"),
	}
	p := crypto.S256().Params().P
	for i := range ids {
		if _, err := ids[i].Pubkey(); err != nil {
			t.Fatalf("canonical point fixture %d rejected: %v", i, err)
		}
		coordinate := ids[i][i*32 : (i+1)*32]
		value := new(big.Int).SetBytes(coordinate)
		value.Add(value, p)
		if value.BitLen() > len(coordinate)*8 {
			t.Fatalf("non-canonical fixture %d does not fit in a NodeID", i)
		}
		value.FillBytes(coordinate)
	}
	return ids
}

func TestNodeFromRPCValidatesID(t *testing.T) {
	validID, offCurveID := rpcNodeTestIDs()
	nonCanonicalIDs := nonCanonicalRPCNodeIDs(t)
	if _, err := validID.Pubkey(); err != nil {
		t.Fatalf("valid secp256k1 fixture rejected: %v", err)
	}
	if _, err := offCurveID.Pubkey(); err == nil {
		t.Fatal("off-curve fixture represents a public key")
	}

	tests := []struct {
		name  string
		id    NodeID
		ip    net.IP
		udp   uint16
		tcp   uint16
		valid bool
	}{
		{"zero identity", NodeID{}, net.IPv4(192, 0, 2, 1), 30301, 30303, false},
		{"nonzero off-curve identity", offCurveID, net.IPv4(192, 0, 2, 1), 30301, 30303, false},
		{"non-canonical X coordinate", nonCanonicalIDs[0], net.IPv4(192, 0, 2, 1), 30301, 30303, false},
		{"non-canonical Y coordinate", nonCanonicalIDs[1], net.IPv4(192, 0, 2, 1), 30301, 30303, false},
		{"IPv4", validID, net.IPv4(192, 0, 2, 1), 30301, 30303, true},
		{"IPv6", validID, net.ParseIP("2001:db8::1"), 30301, 30303, true},
		{"IPv4 loopback", validID, net.IPv4(127, 0, 0, 1), 30301, 30303, true},
		{"IPv6 loopback", validID, net.IPv6loopback, 30301, 30303, true},
		{"IPv4 private", validID, net.IPv4(10, 0, 0, 1), 30301, 30303, true},
		{"IPv6 private", validID, net.ParseIP("fd00::1"), 30301, 30303, true},
		{"zero TCP port", validID, net.IPv4(192, 0, 2, 1), 30301, 0, true},
		{"IPv4 multicast", validID, net.IPv4(224, 0, 0, 1), 30301, 30303, false},
		{"IPv6 multicast", validID, net.ParseIP("ff02::1"), 30301, 30303, false},
		{"IPv4 unspecified", validID, net.IPv4zero, 30301, 30303, false},
		{"IPv6 unspecified", validID, net.IPv6zero, 30301, 30303, false},
		{"zero UDP port", validID, net.IPv4(192, 0, 2, 1), 0, 30303, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, valid := nodeFromRPC(rpcNode{ID: tt.id, IP: tt.ip, UDP: tt.udp, TCP: tt.tcp})
			if valid != tt.valid {
				t.Fatalf("nodeFromRPC valid=%v, want %v", valid, tt.valid)
			}
			if !tt.valid {
				if n != nil {
					t.Errorf("rejected RPC node returned %v", n)
				}
				return
			}
			if n == nil {
				t.Fatal("accepted RPC node returned nil")
			}
			wantIP := tt.ip
			if ip4 := wantIP.To4(); ip4 != nil {
				wantIP = ip4
			}
			if n.ID != tt.id || !bytes.Equal(n.IP, wantIP) || n.UDP != tt.udp || n.TCP != tt.tcp {
				t.Errorf("RPC fields changed: got %+v, want ID=%v IP=%v UDP=%d TCP=%d", n, tt.id, wantIP, tt.udp, tt.tcp)
			}
		})
	}
}

// discoveryBondRecorder completes valid bonds immediately. Rejected identities
// are recorded and fail immediately, so an accidental ingress acceptance is
// observable without waiting for network timeouts.
type discoveryBondRecorder struct {
	mu      sync.Mutex
	validID NodeID
	pings   map[NodeID]int
}

func (r *discoveryBondRecorder) ping(id NodeID, _ *net.UDPAddr) error {
	r.mu.Lock()
	r.pings[id]++
	r.mu.Unlock()
	if id != r.validID {
		return errUnknownNode
	}
	return nil
}

func (r *discoveryBondRecorder) waitping(id NodeID) error {
	if id != r.validID {
		return errUnknownNode
	}
	return nil
}

func (r *discoveryBondRecorder) findnode(NodeID, *net.UDPAddr, NodeID) ([]*Node, error) {
	return nil, errUnknownNode
}

func (r *discoveryBondRecorder) close() {}

func TestFindnodeRejectsInvalidNeighborIDsBeforeBonding(t *testing.T) {
	u := newTestUDP(t, false)
	defer u.close(t, 5*time.Second)
	peer := newSender(t, 40000)
	validID, offCurveID := rpcNodeTestIDs()
	nonCanonicalIDs := nonCanonicalRPCNodeIDs(t)
	validRPC := rpcNode{ID: validID, IP: net.IP{10, 0, 0, 2}, UDP: 30301, TCP: 30303}

	// A complete raw bucket includes only one usable node. Counting accepted
	// nodes instead would wait for respTimeout despite receiving every reply.
	raw := make([]rpcNode, bucketSize)
	for i := range raw {
		id := NodeID{}
		switch i % 4 {
		case 1:
			id = offCurveID
		case 2:
			id = nonCanonicalIDs[0]
		case 3:
			id = nonCanonicalIDs[1]
		}
		raw[i] = rpcNode{ID: id, IP: net.IP{192, 0, 2, byte(i + 1)}, UDP: uint16(31000 + i), TCP: uint16(32000 + i)}
	}
	raw[len(raw)-1] = validRPC
	// FINDNODE targets are arbitrary 512-bit values, including off-curve ones.
	target := offCurveID
	repliesDone := make(chan error, 1)
	u.conn.mu.Lock()
	u.conn.findnodeSent = func(buf []byte, addr *net.UDPAddr) {
		request, fromID, _, err := decodePacket(buf)
		if err != nil {
			repliesDone <- fmt.Errorf("decode outbound FINDNODE: %w", err)
			return
		}
		query, ok := request.(*findnode)
		if !ok || fromID != u.tab.self.ID || addr.String() != peer.addr.String() {
			repliesDone <- fmt.Errorf("unexpected outbound FINDNODE: packet=%T from=%v to=%v", request, fromID, addr)
			return
		}
		if query.Target != target {
			repliesDone <- fmt.Errorf("FINDNODE target changed: got %v, want %v", query.Target, target)
			return
		}
		for start := 0; start < len(raw); start += maxNeighbors {
			end := start + maxNeighbors
			if end > len(raw) {
				end = len(raw)
			}
			if err := peer.deliver(u, neighborsPacket, &neighbors{
				Nodes:      raw[start:end],
				Expiration: uint64(time.Now().Add(expiration).Unix()),
			}); err != nil {
				repliesDone <- fmt.Errorf("deliver neighbors %d:%d: %w", start, end, err)
				return
			}
		}
		repliesDone <- nil
	}
	u.conn.mu.Unlock()

	type findnodeResult struct {
		nodes []*Node
		err   error
	}
	findnodeDone := make(chan findnodeResult, 1)
	go func() {
		nodes, err := u.udp.findnode(peer.id(), peer.addr, target)
		findnodeDone <- findnodeResult{nodes, err}
	}()
	var found findnodeResult
	select {
	case found = <-findnodeDone:
	case <-time.After(5 * time.Second):
		t.Fatal("FINDNODE did not complete")
	}
	var replyErr error
	select {
	case replyErr = <-repliesDone:
	case <-time.After(5 * time.Second):
		t.Fatal("signed neighbor delivery did not complete")
	}

	// Bond the actual ingress result on an independent in-memory table. The
	// transport has no network or goroutines; bondall and its wait group are
	// both joined before checking for any attempted ping or database write.
	recorder := &discoveryBondRecorder{validID: validID, pings: make(map[NodeID]int)}
	tab := newTable(recorder, u.tab.self.ID, u.conn.LocalAddr().(*net.UDPAddr), "")
	defer tab.Close()
	versionBefore, err := tab.db.lvl.Has(nodeDBVersionKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	bondsDone := make(chan []*Node, 1)
	go func() {
		bonded := tab.bondall(found.nodes)
		tab.wg.Wait()
		bondsDone <- bonded
	}()
	var bonded []*Node
	select {
	case bonded = <-bondsDone:
	case <-time.After(5 * time.Second):
		t.Fatal("bonding ingress results did not complete")
	}

	// Check side effects first: an ingress count failure must not conceal
	// invalid identities reaching ping or the zero-ID global key namespace.
	recorder.mu.Lock()
	pingCounts := make(map[NodeID]int, len(recorder.pings))
	for id, count := range recorder.pings {
		pingCounts[id] = count
	}
	recorder.mu.Unlock()
	for _, id := range []NodeID{{}, offCurveID, nonCanonicalIDs[0], nonCanonicalIDs[1]} {
		if count := pingCounts[id]; count != 0 {
			t.Errorf("invalid neighbor %v reached bonding ping %d times", id, count)
		}
		for _, field := range []string{nodeDBDiscoverRoot, nodeDBDiscoverPing, nodeDBDiscoverPong} {
			present, err := tab.db.lvl.Has(makeKey(id, field), nil)
			if err != nil {
				t.Fatal(err)
			}
			if present {
				t.Errorf("invalid neighbor %v wrote database field %q", id, field)
			}
		}
	}
	globalPing, err := tab.db.lvl.Has([]byte(nodeDBDiscoverPing), nil)
	if err != nil {
		t.Fatal(err)
	}
	if globalPing {
		t.Error("invalid zero identity wrote lastping into the global namespace")
	}
	versionAfter, err := tab.db.lvl.Has(nodeDBVersionKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	if versionAfter != versionBefore {
		t.Errorf("database version-marker presence changed: before=%v after=%v", versionBefore, versionAfter)
	}
	for _, field := range []string{nodeDBDiscoverRoot, nodeDBDiscoverPing, nodeDBDiscoverPong} {
		present, err := tab.db.lvl.Has(makeKey(validID, field), nil)
		if err != nil {
			t.Fatal(err)
		}
		if !present {
			t.Errorf("valid neighbor has no database field %q", field)
		}
	}
	if got := tab.db.node(validID); got == nil {
		t.Error("valid neighbor was not persisted")
	} else if got.ID != validRPC.ID || !bytes.Equal(got.IP, validRPC.IP) || got.UDP != validRPC.UDP || got.TCP != validRPC.TCP {
		t.Errorf("persisted neighbor fields changed: got %+v, want %+v", got, validRPC)
	}
	if count := pingCounts[validID]; count != 1 {
		t.Errorf("valid neighbor reached bonding ping %d times, want 1", count)
	}
	if len(bonded) != 1 || bonded[0].ID != validID {
		t.Errorf("bonded neighbors=%v, want only the valid identity", bonded)
	}
	if found.err != nil {
		t.Errorf("FINDNODE returned %v after receiving a full raw bucket", found.err)
	}
	if replyErr != nil {
		t.Error(replyErr)
	}
	if len(found.nodes) != 1 || found.nodes[0].ID != validID {
		t.Errorf("FINDNODE returned %d nodes, want only the valid identity", len(found.nodes))
	}
	if count := u.conn.count(peer.addr, findnodePacket); count != 1 {
		t.Errorf("sent %d FINDNODE requests, want 1", count)
	}
}

// Every valid ping is answered with a pong, but bonding work is only started
// for as many identities as the inbound budget allows. Admission happens in
// the handler itself, so the boundary is exact: the permit count is checked
// right after each ping is delivered, before any bonding process has run.
func TestInboundBondingIsBounded(t *testing.T) {
	u := newTestUDP(t, true)
	defer u.close(t, 5*time.Second)

	const extra = 50
	senders := make([]*sender, maxInboundBonds+extra)
	for i := range senders {
		senders[i] = newSender(t, 40000+i)
	}
	for i, s := range senders {
		s.ping(t, u)
		if want := i + 1; want <= maxInboundBonds && u.admitted() != want {
			t.Fatalf("%d permits held after %d pings", u.admitted(), want)
		}
	}
	if got := u.conn.countAll(pongPacket); got != len(senders) {
		t.Fatalf("%d pongs sent for %d pings", got, len(senders))
	}
	if got := u.admitted(); got != maxInboundBonds {
		t.Fatalf("%d permits held, limit is %d", got, maxInboundBonds)
	}
	// Every admitted process registers itself and then parks, either before
	// its ping or waiting for a slot. Processes beyond the budget were never
	// started.
	u.waitFor(t, 5*time.Second, "admitted bonds registered", func() bool {
		return u.bondingLen() == maxInboundBonds
	})
	for _, s := range senders[maxInboundBonds:] {
		if u.isBonding(s.id()) {
			t.Fatalf("rejected sender %v has a bonding process", s.addr)
		}
	}
}

// Repeated pings from one identity coalesce onto the bond already in flight
// for it, so they take one permit rather than the whole budget: a different
// identity that pings while the noisy one is still bonding is admitted and
// bonded as usual.
func TestRepeatedIdentityDoesNotStarveOthers(t *testing.T) {
	u := newTestUDP(t, true)
	defer u.close(t, 5*time.Second)

	noisy := newSender(t, 40000)
	for i := 0; i < maxInboundBonds*3; i++ {
		noisy.ping(t, u)
	}
	if got := u.admitted(); got != 1 {
		t.Fatalf("%d permits held for one identity", got)
	}
	other := newSender(t, 40001)
	u.respond(other)
	other.ping(t, u)
	if got := u.admitted(); got != 2 {
		t.Fatalf("%d permits held for two identities", got)
	}
	u.waitFor(t, 5*time.Second, "one bonding process per identity", func() bool {
		return u.bondingLen() == 2
	})

	u.release()
	u.waitFor(t, 5*time.Second, "other identity bonded", func() bool {
		return u.bonded(other)
	})
	// The noisy identity's permit is released only after its one ping was
	// written and its reply timed out, so the count below is final.
	u.waitFor(t, 5*time.Second, "noisy exchange ended", func() bool {
		return u.admitted() == 0
	})
	if got := u.conn.count(noisy.addr, pingPacket); got != 1 {
		t.Fatalf("%d pings sent to the noisy identity, want one exchange", got)
	}
}

// An identity whose inbound bond ended, here by timing out, is admitted
// again on its next ping and can bond then: coalescing only spans the
// process in flight, it does not remember a failed one.
func TestIdentityIsAdmittedAgainAfterItsBondEnds(t *testing.T) {
	u := newTestUDP(t, false)
	defer u.close(t, 5*time.Second)

	s := newSender(t, 40000)
	s.ping(t, u)
	if got := u.admitted(); got != 1 {
		t.Fatalf("%d permits held after the first ping", got)
	}
	u.waitFor(t, 5*time.Second, "first bond timed out", func() bool {
		return u.admitted() == 0
	})
	if u.bonded(s) {
		t.Fatal("silent sender was bonded")
	}

	u.respond(s)
	s.ping(t, u)
	// The responder answers at once, so the worker may already have
	// released its permit here; the bond itself proves the identity was
	// admitted again, and the permit must come back afterwards.
	u.waitFor(t, 5*time.Second, "sender bonded on retry", func() bool {
		return u.bonded(s)
	})
	u.waitFor(t, 5*time.Second, "permit returned", func() bool {
		return u.admitted() == 0
	})
}

// Permits and slots are returned whether an exchange succeeds or times out,
// so a full budget drains completely and a later ping is served again. The
// answering senders complete their exchange as soon as it starts, whichever
// order the processes take slots in; the silent ones exercise the real
// response timeout.
func TestInboundBondingBudgetIsReturned(t *testing.T) {
	u := newTestUDP(t, false)
	defer u.close(t, 5*time.Second)

	senders := make([]*sender, maxInboundBonds)
	for i := range senders {
		senders[i] = newSender(t, 40000+i)
		if i%2 == 0 {
			u.respond(senders[i])
		}
	}
	for _, s := range senders {
		s.ping(t, u)
	}
	u.waitFor(t, 10*time.Second, "all permits returned", func() bool {
		return u.admitted() == 0
	})
	u.waitFor(t, 5*time.Second, "all slots returned", func() bool {
		return u.freeSlots() == maxBondingPingPongs
	})
	for i, s := range senders {
		if bonded := u.bonded(s); bonded != (i%2 == 0) {
			t.Fatalf("sender %d bonded=%v", i, bonded)
		}
	}

	late := newSender(t, 50000)
	u.respond(late)
	late.ping(t, u)
	u.waitFor(t, 5*time.Second, "late sender bonded", func() bool {
		return u.bonded(late)
	})
	u.waitFor(t, 5*time.Second, "permit returned", func() bool {
		return u.admitted() == 0
	})
}

// Outbound bonding shares the bonding slots but not the inbound budget: with
// the budget full and every slot held by an inbound exchange, an outbound
// bond still completes once slots turn over.
func TestOutboundBondProgressesUnderInboundSaturation(t *testing.T) {
	u := newTestUDP(t, true)
	defer u.close(t, 5*time.Second)

	peer := newSender(t, 50000)
	u.respond(peer)
	// An outbound bond waits for the peer to ping us back after our
	// exchange; deliver that ping once the process is waiting for it. A
	// ping back that were handled as unsolicited instead would be admitted
	// as an inbound bond of its own, which is checked right after delivery.
	var pingBackAdmitted bool
	u.gate.pingWanted = func(id NodeID) {
		if id != peer.id() {
			return
		}
		if err := peer.deliver(u, pingPacket, peer.pingPacket(u)); err != nil {
			t.Errorf("ping back from %v rejected: %v", peer.addr, err)
		}
		u.tab.bondmu.Lock()
		_, pingBackAdmitted = u.tab.inbound[peer.id()]
		u.tab.bondmu.Unlock()
	}

	senders := make([]*sender, maxInboundBonds)
	for i := range senders {
		senders[i] = newSender(t, 40000+i)
		senders[i].ping(t, u)
	}
	u.waitFor(t, 5*time.Second, "every slot held", func() bool {
		return u.freeSlots() == 0
	})

	type outcome struct {
		n   *Node
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		n, err := u.tab.bond(false, peer.id(), peer.addr, uint16(peer.addr.Port))
		done <- outcome{n, err}
	}()
	u.waitFor(t, 5*time.Second, "outbound bond registered", func() bool {
		return u.isBonding(peer.id())
	})
	if got := u.admitted(); got != maxInboundBonds {
		t.Fatalf("%d permits held, outbound bonding must not take one", got)
	}
	if got := u.freeSlots(); got != 0 {
		t.Fatalf("%d free slots, the outbound bond must be queued", got)
	}

	// The inbound remotes stay silent, so slots turn over as their
	// exchanges time out and the outbound bond takes one in its turn.
	u.release()
	select {
	case o := <-done:
		if o.err != nil || o.n == nil {
			t.Fatalf("outbound bond failed: node=%v err=%v", o.n, o.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("outbound bond did not complete")
	}
	if pingBackAdmitted {
		t.Fatal("ping back from the peer was handled as unsolicited")
	}
	if got := u.conn.count(peer.addr, pongPacket); got != 1 {
		t.Fatalf("%d pongs sent to the peer, want one for its ping back", got)
	}
}

// An outbound bond queued for a slot when the table closes returns with
// errClosed at once, without taking a slot.
func TestQueuedOutboundBondFailsOnClose(t *testing.T) {
	u := newTestUDP(t, true)
	defer u.close(t, 5*time.Second)

	for i := 0; i < maxBondingPingPongs; i++ {
		newSender(t, 40000+i).ping(t, u)
	}
	u.waitFor(t, 5*time.Second, "every slot held", func() bool {
		return u.freeSlots() == 0
	})

	peer := newSender(t, 50000)
	type outcome struct {
		n   *Node
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		n, err := u.tab.bond(false, peer.id(), peer.addr, uint16(peer.addr.Port))
		done <- outcome{n, err}
	}()
	u.waitFor(t, 5*time.Second, "outbound bond registered", func() bool {
		return u.isBonding(peer.id())
	})

	u.closeTable()
	select {
	case o := <-done:
		if o.err != errClosed || o.n != nil {
			t.Fatalf("queued outbound bond returned node=%v err=%v, want errClosed", o.n, o.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("queued outbound bond did not return after close")
	}
	if got := u.freeSlots(); got != 0 {
		t.Fatalf("%d slots returned, the parked processes still hold them", got)
	}
	if got := u.conn.count(peer.addr, pingPacket); got != 0 {
		t.Fatalf("%d pings sent to the peer after close", got)
	}
}

// Closing the table must end the wait of bonds queued for a slot: they exit
// at once, without taking a slot, rather than staying parked behind slots
// that are never returned or each failing through the closed transport in
// turn. Close does not join the workers itself, so the test also checks that
// they all exit afterwards.
func TestQueuedBondsExitOnClose(t *testing.T) {
	u := newTestUDP(t, true)
	defer u.close(t, 5*time.Second)

	for i := 0; i < maxInboundBonds; i++ {
		newSender(t, 40000+i).ping(t, u)
	}
	// Every process has started: each holds a slot at the parked ping or is
	// waiting for one.
	u.waitFor(t, 5*time.Second, "every bond parked", func() bool {
		return u.bondingLen() == maxInboundBonds && u.freeSlots() == 0
	})
	u.closeTable()
	// The parked processes keep their slots, so the queued ones can only
	// leave through the closed table.
	u.waitFor(t, 5*time.Second, "queued bonds exited", func() bool {
		return u.bondingLen() == maxBondingPingPongs
	})
	if got := u.freeSlots(); got != 0 {
		t.Fatalf("%d slots returned, the parked processes still hold them", got)
	}
}
