package protocol

import (
	"errors"
	"net"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/p2p/discover"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm/constants"
)

// steppedClock replaces common.Clock for a test and is advanced by hand.
type steppedClock struct{ t time.Time }

func (c *steppedClock) Now() time.Time { return c.t }

func useSteppedClock(t *testing.T) *steppedClock {
	t.Helper()
	saved := common.Clock
	clock := &steppedClock{t: time.Unix(1_700_000_000, 0)}
	common.Clock = clock
	t.Cleanup(func() { common.Clock = saved })
	return clock
}

func hashOf(b byte) types.Hash {
	return types.NewHash([]byte{b})
}

func TestRejectedMomentumsExpireAndRefresh(t *testing.T) {
	clock := useSteppedClock(t)
	r := newRejectedMomentums(time.Hour, 16)

	if r.has(hashOf(1)) {
		t.Fatal("unknown hash reported as rejected")
	}
	r.add(hashOf(1))
	if !r.has(hashOf(1)) {
		t.Fatal("hash not remembered right after add")
	}
	clock.t = clock.t.Add(time.Hour - time.Second)
	if !r.has(hashOf(1)) {
		t.Fatal("hash forgotten before its ttl elapsed")
	}
	// A repeat offer refreshes the expiry.
	r.add(hashOf(1))
	clock.t = clock.t.Add(time.Hour - time.Second)
	if !r.has(hashOf(1)) {
		t.Fatal("refreshed hash forgotten before the new ttl elapsed")
	}
	clock.t = clock.t.Add(time.Second)
	if r.has(hashOf(1)) {
		t.Fatal("hash still remembered after its ttl elapsed")
	}
	if r.len() != 0 {
		t.Fatalf("expired entry not removed on read, %d left", r.len())
	}
}

func TestRejectedMomentumsCapEvictsOldest(t *testing.T) {
	clock := useSteppedClock(t)
	r := newRejectedMomentums(time.Hour, 3)
	for b := byte(1); b <= 3; b++ {
		r.add(hashOf(b))
		clock.t = clock.t.Add(time.Minute)
	}
	r.add(hashOf(4))
	if r.len() != 3 {
		t.Fatalf("cap not enforced, %d entries", r.len())
	}
	if r.has(hashOf(1)) {
		t.Fatal("oldest entry survived eviction")
	}
	for b := byte(2); b <= 4; b++ {
		if !r.has(hashOf(b)) {
			t.Fatalf("entry %d evicted instead of the oldest", b)
		}
	}
}

func TestValidationFailureClassification(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		validation bool
	}{
		{"verifier verdict", verifier.ErrMChangesHashInvalid, true},
		{"wrapped verdict", errors.Join(errors.New("ctx"), verifier.ErrMProducerInvalid), true},
		{"verifier internal", verifier.InternalError(errors.New("store")), false},
		{"vm panic", constants.ErrVmRunPanic, false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		got := validationFailure(c.err)
		var tagged *validationError
		if errors.As(got, &tagged) != c.validation {
			t.Fatalf("%s: tagged=%v, want %v", c.name, !c.validation, c.validation)
		}
		if c.err != nil && !errors.Is(got, c.err) {
			t.Fatalf("%s: wrapped error lost its cause", c.name)
		}
		if c.err != nil && got.Error() != c.err.Error() {
			t.Fatalf("%s: wrapped error changed its text: %q", c.name, got.Error())
		}
	}
}

func TestPeerQuarantineExpiresAndCaps(t *testing.T) {
	clock := useSteppedClock(t)
	q := newPeerQuarantine(time.Hour, 2)
	q.add("a")
	if !q.active("a") {
		t.Fatal("peer not quarantined after add")
	}
	clock.t = clock.t.Add(time.Hour)
	if q.active("a") {
		t.Fatal("quarantine did not expire")
	}
	q.add("a")
	clock.t = clock.t.Add(time.Minute)
	q.add("b")
	q.add("c")
	if q.active("a") || !q.active("b") || !q.active("c") {
		t.Fatalf("cap did not evict the entry expiring soonest: a=%v b=%v c=%v", q.active("a"), q.active("b"), q.active("c"))
	}
}

// stubP2pPeer satisfies p2p.Peer with a fixed node id.
type stubP2pPeer struct{ id discover.NodeID }

func (s *stubP2pPeer) ID() discover.NodeID       { return s.id }
func (s *stubP2pPeer) Name() string              { return "stub" }
func (s *stubP2pPeer) Caps() []p2p.Cap           { return nil }
func (s *stubP2pPeer) RemoteAddr() net.Addr      { return &net.TCPAddr{} }
func (s *stubP2pPeer) Disconnect(p2p.DiscReason) {}

type stubMsgRW struct{}

func (s *stubMsgRW) ReadMsg() (p2p.Msg, error) { return p2p.Msg{}, errors.New("no messages") }
func (s *stubMsgRW) WriteMsg(p2p.Msg) error    { return nil }

func newStubPeer(id byte) *peer {
	var nodeID discover.NodeID
	nodeID[0] = id
	return newPeer(1, 1, &stubP2pPeer{id: nodeID}, &stubMsgRW{})
}

func TestRejectPeerQuarantinesOnlyOnSideChainRejection(t *testing.T) {
	useSteppedClock(t)
	pm := NewProtocolManager(1, 1, &stubChainBridge{})
	defer pm.Stop()
	p := newStubPeer(1)
	if err := pm.peers.Register(p); err != nil {
		t.Fatal(err)
	}

	pm.rejectPeer(p.id, errors.New("store failure"))
	if pm.quarantine.active(p.id) {
		t.Fatal("a plain import failure must not quarantine the peer")
	}
	if pm.peers.Peer(p.id) == nil {
		t.Fatal("a plain import failure must leave the peer to the caller's handling")
	}

	rejected := &SideChainRejected{Momentum: types.HashHeight{Height: 7, Hash: hashOf(7)}, Cause: verifier.ErrMChangesHashInvalid}
	pm.rejectPeer(p.id, errors.Join(errors.New("Block import failed"), rejected))
	if !pm.quarantine.active(p.id) {
		t.Fatal("a side-chain rejection must quarantine the peer")
	}
	if pm.peers.Peer(p.id) != nil {
		t.Fatal("a quarantined peer must be removed")
	}
}

func TestHandleRefusesQuarantinedPeerBeforeHandshake(t *testing.T) {
	clock := useSteppedClock(t)
	pm := NewProtocolManager(1, 1, &stubChainBridge{})
	defer pm.Stop()
	p := newStubPeer(2)
	pm.quarantine.add(p.id)

	if err := pm.handle(p); !errors.Is(err, errPeerQuarantined) {
		t.Fatalf("expected errPeerQuarantined, got %v", err)
	}
	if pm.peers.Peer(p.id) != nil {
		t.Fatal("quarantined peer must not be registered")
	}

	// Once the quarantine lapses the peer reaches the handshake again,
	// which the stub stream fails on; that failure is not the quarantine.
	clock.t = clock.t.Add(peerQuarantinePeriod)
	if err := pm.handle(p); errors.Is(err, errPeerQuarantined) {
		t.Fatal("peer still refused after the quarantine expired")
	}
}
