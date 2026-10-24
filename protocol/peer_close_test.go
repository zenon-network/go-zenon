package protocol

import (
	"net"
	"testing"

	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/p2p/discover"
)

type stubPeer struct{}

func (stubPeer) ID() discover.NodeID       { return discover.NodeID{1} }
func (stubPeer) Name() string              { return "stub" }
func (stubPeer) Caps() []p2p.Cap           { return nil }
func (stubPeer) RemoteAddr() net.Addr      { return &net.TCPAddr{} }
func (stubPeer) Disconnect(p2p.DiscReason) {}

type plainRW struct{}

func (plainRW) ReadMsg() (p2p.Msg, error) { return p2p.Msg{}, nil }
func (plainRW) WriteMsg(p2p.Msg) error    { return nil }

type closingRW struct {
	plainRW
	closed chan struct{}
}

func (rw closingRW) Closed() <-chan struct{} { return rw.closed }

// The handler's peer carries the transport's close signal when the message
// stream exposes one, and nothing when it does not. Downloader deliveries
// select on it so a stalled session cannot hold the handler past the peer.
func TestNewPeerCarriesTransportCloseSignal(t *testing.T) {
	ch := make(chan struct{})
	p := newPeer(1, 1, stubPeer{}, closingRW{closed: ch})
	if p.closed != (<-chan struct{})(ch) {
		t.Fatal("peer does not carry the stream's close signal")
	}
	if q := newPeer(1, 1, stubPeer{}, plainRW{}); q.closed != nil {
		t.Fatal("peer carries a close signal its stream does not expose")
	}
}
