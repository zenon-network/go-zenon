package discover

import (
	"crypto/ecdsa"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

// fakeConn is a datagram socket that never delivers anything on its own:
// reads block until Close and writes are recorded by destination and packet
// type. Test packets are fed to the real decoder through udp.handlePacket
// instead. pingSent, when set, runs after each outbound ping is recorded so
// that a test can answer the ping from the write itself.
type fakeConn struct {
	mu        sync.Mutex
	closed    chan struct{}
	closeOnce sync.Once
	sent      map[string][]byte // destination -> packet types written
	pingSent  func(addr *net.UDPAddr)
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
	c.mu.Unlock()
	if b[headSize] == pingPacket && c.pingSent != nil {
		c.pingSent(addr)
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
