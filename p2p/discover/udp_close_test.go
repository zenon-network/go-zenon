package discover

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

// closeTestConn is a conn whose reads block until it is closed and whose
// local address is not a loopback address, so that newUDP starts the NAT
// mapping worker for it.
type closeTestConn struct {
	addr   *net.UDPAddr
	closed chan struct{}
	once   sync.Once
}

func newCloseTestConn() *closeTestConn {
	return &closeTestConn{addr: &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 30303}, closed: make(chan struct{})}
}

func (c *closeTestConn) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) {
	<-c.closed
	return 0, nil, errors.New("closed")
}
func (c *closeTestConn) WriteToUDP(b []byte, addr *net.UDPAddr) (int, error) { return len(b), nil }
func (c *closeTestConn) Close() error                                        { c.once.Do(func() { close(c.closed) }); return nil }
func (c *closeTestConn) LocalAddr() net.Addr                                 { return c.addr }

// gatedNAT is a nat.Interface whose DeleteMapping blocks until the test
// releases it, and which signals when each call is entered and done.
type gatedNAT struct {
	added    chan struct{}
	deleting chan struct{}
	deleted  chan struct{}
	release  chan struct{}
	once     sync.Once
}

func newGatedNAT() *gatedNAT {
	return &gatedNAT{
		added:    make(chan struct{}, 1),
		deleting: make(chan struct{}, 1),
		deleted:  make(chan struct{}, 1),
		release:  make(chan struct{}),
	}
}

// unblock lets a held DeleteMapping return. Safe to call more than once, so
// a failing assertion can release the worker before the test ends.
func (n *gatedNAT) unblock() { n.once.Do(func() { close(n.release) }) }

func (n *gatedNAT) AddMapping(protocol string, extport, intport int, name string, lifetime time.Duration) error {
	select {
	case n.added <- struct{}{}:
	default:
	}
	return nil
}
func (n *gatedNAT) DeleteMapping(protocol string, extport, intport int) error {
	select {
	case n.deleting <- struct{}{}:
	default:
	}
	<-n.release
	select {
	case n.deleted <- struct{}{}:
	default:
	}
	return nil
}
func (n *gatedNAT) ExternalIP() (net.IP, error) { return net.IPv4(203, 0, 113, 7), nil }
func (n *gatedNAT) String() string              { return "gated" }

// TestTableCloseWaitsForNATUnmapping pins that Close does not return until
// the NAT mapping worker's deletion attempt has completed. Without that, a
// caller that closes a table and immediately opens a new one on the same
// port (the legacy server retrying after a failed start) can have its fresh
// mapping removed by the old worker's deferred cleanup. The packet and read
// loops are deliberately not part of this guarantee.
func TestTableCloseWaitsForNATUnmapping(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	natm := newGatedNAT()
	tab, _ := newUDP(key, newCloseTestConn(), natm, "")

	select {
	case <-natm.added:
	case <-time.After(5 * time.Second):
		t.Fatal("the NAT mapping worker did not add a mapping")
	}

	defer natm.unblock() // never leave the worker blocked, even on failure

	closed := make(chan struct{})
	go func() {
		tab.Close()
		close(closed)
	}()

	// Close signals the worker, whose deferred unmapping then enters the
	// gate. Only from that point is "Close is still waiting" a meaningful
	// check; before it, scheduling alone could keep both from running.
	select {
	case <-natm.deleting:
	case <-closed:
		t.Fatal("Close returned before the NAT unmapping was even attempted")
	case <-time.After(5 * time.Second):
		t.Fatal("the NAT unmapping was not attempted after Close")
	}
	select {
	case <-closed:
		t.Fatal("Close returned while the NAT unmapping was still in progress")
	case <-time.After(200 * time.Millisecond):
	}

	natm.unblock()
	select {
	case <-natm.deleted:
	case <-time.After(5 * time.Second):
		t.Fatal("the NAT mapping was not removed after release")
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return after the NAT mapping was removed")
	}
}
