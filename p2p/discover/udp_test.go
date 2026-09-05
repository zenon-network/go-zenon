package discover

import (
	"crypto/ecdsa"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

// fakeConn is a datagram socket that never delivers anything: reads block
// until Close and writes are recorded by destination and packet type. Test
// packets are fed to the real decoder through udp.handlePacket instead.
type fakeConn struct {
	mu        sync.Mutex
	closed    chan struct{}
	closeOnce sync.Once
	sent      map[string][]byte // destination -> packet types written
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
	defer c.mu.Unlock()
	c.sent[addr.String()] = append(c.sent[addr.String()], b[headSize])
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

type testUDP struct {
	tab  *Table
	udp  *udp
	conn *fakeConn
}

func newTestUDP(t *testing.T) *testUDP {
	t.Helper()
	priv, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn()
	tab, u := newUDP(priv, conn, nil, "")
	return &testUDP{tab: tab, udp: u, conn: conn}
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

// ping delivers a valid, unexpired, correctly versioned ping from s.
func (s *sender) ping(t *testing.T, u *testUDP) {
	t.Helper()
	packet, err := encodePacket(s.priv, pingPacket, &ping{
		Version:    Version,
		From:       makeEndpoint(s.addr, uint16(s.addr.Port)),
		To:         makeEndpoint(u.conn.LocalAddr().(*net.UDPAddr), 30303),
		Expiration: uint64(time.Now().Add(expiration).Unix()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := u.udp.handlePacket(s.addr, packet); err != nil {
		t.Fatalf("ping from %v rejected: %v", s.addr, err)
	}
}

func (u *testUDP) bondingLen() int {
	u.tab.bondmu.Lock()
	defer u.tab.bondmu.Unlock()
	return len(u.tab.bonding)
}

// waitDone waits for every goroutine the transport started, or fails.
func (u *testUDP) waitDone(t *testing.T, timeout time.Duration) {
	t.Helper()
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

// Every valid ping is answered with a pong, but bonding work is only started
// for as many senders as the inbound bonding budget allows: the rest are
// dropped instead of being queued. Remotes never answer here, so each
// admitted bond occupies a slot until its ping times out.
func TestInboundBondingIsBounded(t *testing.T) {
	u := newTestUDP(t)
	defer u.tab.Close()

	const extra = 50
	senders := make([]*sender, maxInboundBonds+extra)
	for i := range senders {
		senders[i] = newSender(t, 40000+i)
	}

	before := runtime.NumGoroutine()
	for _, s := range senders {
		s.ping(t, u)
	}
	if got := u.conn.countAll(pongPacket); got != len(senders) {
		t.Fatalf("%d pongs sent for %d pings", got, len(senders))
	}

	// Give admitted goroutines time to register their bonding process.
	time.Sleep(100 * time.Millisecond)
	if got := u.bondingLen(); got > maxInboundBonds {
		t.Fatalf("%d bonding processes registered, limit is %d", got, maxInboundBonds)
	}
	if grew := runtime.NumGoroutine() - before; grew > maxInboundBonds+maxBondingPingPongs+4 {
		t.Fatalf("goroutines grew by %d for %d pings", grew, len(senders))
	}
}

// Repeated pings from one identity share a single bonding process and the
// handler goroutines waiting on it are bounded by the same budget.
func TestRepeatedIdentityBondingIsBounded(t *testing.T) {
	u := newTestUDP(t)
	defer u.tab.Close()

	s := newSender(t, 40000)
	before := runtime.NumGoroutine()
	for i := 0; i < maxInboundBonds*3; i++ {
		s.ping(t, u)
	}
	time.Sleep(100 * time.Millisecond)
	if got := u.bondingLen(); got > 1 {
		t.Fatalf("%d bonding processes for one identity", got)
	}
	if grew := runtime.NumGoroutine() - before; grew > maxInboundBonds+maxBondingPingPongs+4 {
		t.Fatalf("goroutines grew by %d", grew)
	}
}

// Once admitted bonds time out, the budget is returned and a later ping is
// served again.
func TestInboundBondingBudgetIsReturned(t *testing.T) {
	u := newTestUDP(t)
	defer u.tab.Close()

	for i := 0; i < maxInboundBonds+maxBondingPingPongs; i++ {
		newSender(t, 40000+i).ping(t, u)
	}
	// Every queued bond needs one respTimeout once it holds a slot.
	rounds := (maxInboundBonds+maxBondingPingPongs)/maxBondingPingPongs + 1
	time.Sleep(time.Duration(rounds) * respTimeout)

	late := newSender(t, 50000)
	late.ping(t, u)
	deadline := time.Now().Add(2 * time.Second)
	for u.conn.count(late.addr, pingPacket) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("late sender was not bonded after the budget drained")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Closing the table must release bonds waiting for a slot; they may not stay
// parked forever behind slots that will never be returned.
func TestQueuedBondsExitOnClose(t *testing.T) {
	u := newTestUDP(t)

	for i := 0; i < maxInboundBonds; i++ {
		newSender(t, 40000+i).ping(t, u)
	}
	time.Sleep(50 * time.Millisecond)
	u.tab.Close()
	u.waitDone(t, 3*time.Second)
}
