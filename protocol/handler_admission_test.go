package protocol

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/rlp"

	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/p2p/discover"
	"github.com/zenon-network/go-zenon/verifier"
)

// gatedStatusRW is a peer stream that answers the status handshake only
// once release is closed, and then holds every later read until hold is
// closed, so a test can act while the handshake is in flight and again
// while the connection is admitted. reading is closed when the handshake
// first blocks on the stream.
type gatedStatusRW struct {
	reading chan struct{}
	release chan struct{}
	hold    chan struct{}
	once    sync.Once
	status  []byte
	served  bool
}

func newGatedStatusRW(t *testing.T) *gatedStatusRW {
	t.Helper()
	// Matches the stubChainBridge status: zero TD, head and genesis.
	status, err := rlp.EncodeToBytes(&statusData{ProtocolVersion: 1, NetworkId: 1})
	if err != nil {
		t.Fatal(err)
	}
	return &gatedStatusRW{
		reading: make(chan struct{}),
		release: make(chan struct{}),
		hold:    make(chan struct{}),
		status:  status,
	}
}

func (g *gatedStatusRW) ReadMsg() (p2p.Msg, error) {
	if g.served {
		<-g.hold
		return p2p.Msg{}, errors.New("stream closed")
	}
	g.once.Do(func() { close(g.reading) })
	<-g.release
	g.served = true
	return p2p.Msg{Code: StatusMsg, Size: uint32(len(g.status)), Payload: bytes.NewReader(g.status)}, nil
}

func (g *gatedStatusRW) WriteMsg(p2p.Msg) error { return nil }

func newGatedPeer(id byte, rw p2p.MsgReadWriter) *peer {
	var nodeID discover.NodeID
	nodeID[0] = id
	return newPeer(1, 1, &stubP2pPeer{id: nodeID}, rw)
}

func awaitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// downloaderHas reports whether the downloader has a peer registered under
// id. Unregistering is the downloader's only report of that; a found peer
// is unregistered as a side effect, which the callers tolerate.
func downloaderHas(pm *ProtocolManager, id string) bool {
	return pm.downloader.UnregisterPeer(id) == nil
}

// awaitAdmitted returns once the connection under id has been admitted in
// full. admit publishes the peer-set entry before it registers the
// connection with the downloader, both under pm.admission, so the entry
// becoming visible is not the end of the admission; acquiring the mutex
// afterwards is, since admit still held it when the entry appeared. The
// mutex is released again before the caller observes or asserts anything.
func awaitAdmitted(t *testing.T, pm *ProtocolManager, id string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for pm.peers.Peer(id) == nil {
		if time.Now().After(deadline) {
			t.Fatal("the connection was not admitted after the handshake")
		}
		time.Sleep(time.Millisecond)
	}
	pm.admission.Lock()
	pm.admission.Unlock()
}

// A node's previous connection served a side-chain whose import outlives
// the connection; the node reconnects and the verdict lands while the new
// connection is in its status handshake, after the pre-handshake check.
// Admission must still refuse it, with the peer set and the downloader
// both left without it.
func TestAdmissionRefusesPeerQuarantinedDuringHandshake(t *testing.T) {
	useSteppedClock(t)
	pm := NewProtocolManager(1, 1, &stubChainBridge{})
	defer pm.Stop()
	rw := newGatedStatusRW(t)
	p := newGatedPeer(3, rw)

	done := make(chan error, 1)
	go func() { done <- pm.handle(p) }()
	awaitClosed(t, rw.reading, "the handshake to start")

	// The delayed verdict from the previous connection's import.
	rejected := &SideChainRejected{Momentum: types.HashHeight{Height: 9, Hash: hashOf(9)}, Cause: verifier.ErrMChangesHashInvalid}
	pm.rejectPeer(p.id, errors.Join(errors.New("Block import failed"), rejected))
	if !pm.quarantine.active(p.id) {
		t.Fatal("the node must be quarantined by the verdict")
	}
	if pm.peers.Peer(p.id) != nil {
		t.Fatal("nothing is registered yet for the reconnection")
	}

	close(rw.release)
	// Without admission-time coordination the handler admits the node and
	// settles into its message loop; the registration shows up first.
	deadline := time.Now().Add(10 * time.Second)
	for returned := false; !returned; {
		select {
		case err := <-done:
			if !errors.Is(err, errPeerQuarantined) {
				t.Fatalf("expected errPeerQuarantined after the handshake, got %v", err)
			}
			returned = true
		default:
			if pm.peers.Peer(p.id) != nil {
				close(rw.hold)
				t.Fatal("a node quarantined during its handshake was admitted")
			}
			if time.Now().After(deadline) {
				close(rw.hold)
				t.Fatal("handle did not return after the handshake completed")
			}
			time.Sleep(time.Millisecond)
		}
	}
	if pm.peers.Peer(p.id) != nil {
		t.Fatal("a node quarantined during its handshake must not be registered")
	}
	if downloaderHas(pm, p.id) {
		t.Fatal("a node quarantined during its handshake must not be admitted to the downloader")
	}
}

// The control for the test above: the same handshake with no verdict
// admits the connection to both the peer set and the downloader, so the
// refusal above is the quarantine's doing and not the stream's.
func TestAdmissionAdmitsPeerAfterHandshake(t *testing.T) {
	useSteppedClock(t)
	pm := NewProtocolManager(1, 1, &stubChainBridge{})
	defer pm.Stop()
	rw := newGatedStatusRW(t)
	p := newGatedPeer(4, rw)

	done := make(chan error, 1)
	go func() { done <- pm.handle(p) }()
	awaitClosed(t, rw.reading, "the handshake to start")
	close(rw.release)

	// Both registries are read only once the admission has completed;
	// the downloader observation unregisters the peer as a side effect.
	awaitAdmitted(t, pm, p.id)
	inPeers, inDownloader := pm.peers.Peer(p.id) != nil, downloaderHas(pm, p.id)
	if !inPeers {
		t.Fatal("an admitted connection must stay in the peer set")
	}
	if !inDownloader {
		t.Fatal("an admitted connection must be registered with the downloader")
	}

	close(rw.hold)
	select {
	case err := <-done:
		if errors.Is(err, errPeerQuarantined) {
			t.Fatalf("an unquarantined node was refused: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("handle did not return after the stream closed")
	}
	if pm.peers.Peer(p.id) != nil {
		t.Fatal("the connection must be removed when its handler returns")
	}
}

// A verdict that lands after admission finds the connection registered
// and removes it from both the peer set and the downloader.
func TestRejectPeerRemovesAdmittedConnection(t *testing.T) {
	useSteppedClock(t)
	pm := NewProtocolManager(1, 1, &stubChainBridge{})
	defer pm.Stop()
	rw := newGatedStatusRW(t)
	p := newGatedPeer(5, rw)

	done := make(chan error, 1)
	go func() { done <- pm.handle(p) }()
	awaitClosed(t, rw.reading, "the handshake to start")
	close(rw.release)
	awaitAdmitted(t, pm, p.id)

	rejected := &SideChainRejected{Momentum: types.HashHeight{Height: 9, Hash: hashOf(9)}, Cause: verifier.ErrMChangesHashInvalid}
	pm.rejectPeer(p.id, errors.Join(errors.New("Block import failed"), rejected))
	if pm.peers.Peer(p.id) != nil {
		t.Fatal("the verdict must remove the admitted connection")
	}
	if downloaderHas(pm, p.id) {
		t.Fatal("the verdict must remove the connection from the downloader")
	}
	close(rw.hold)
	awaitClosedErr(t, done)
}

func awaitClosedErr(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("handle did not return after the stream closed")
	}
}
