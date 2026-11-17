package protocol

import (
	"errors"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/verifier"
)

// relayStub is a bridge whose frontier is parent and whose import of any
// momentum fails with a consensus verdict. inserted is closed on the
// first import.
type relayStub struct {
	stubChainBridge
	parent   *nom.Momentum
	inserted chan struct{}
}

func (s *relayStub) GetBlock(hash types.Hash) *nom.DetailedMomentum {
	if hash == s.parent.Hash {
		return &nom.DetailedMomentum{Momentum: s.parent}
	}
	return nil
}

func (s *relayStub) CurrentBlock() *nom.Momentum { return s.parent }

func (s *relayStub) InsertChain(momentums []*nom.DetailedMomentum) (int, error) {
	close(s.inserted)
	rejected := &SideChainRejected{Momentum: momentums[0].Momentum.Identifier(), Cause: verifier.ErrMSignatureInvalid}
	return 0, errors.Join(errors.New("Block import failed"), rejected)
}

// The peer that delivers a gossiped momentum is usually a relayer, not its
// author: nodes of earlier releases propagate a momentum before importing
// it. A consensus verdict on that momentum must leave the delivering peer
// connected and out of quarantine.
func TestFetcherVerdictDoesNotQuarantineRelayer(t *testing.T) {
	useSteppedClock(t)
	parent := &nom.Momentum{Height: 1, TimestampUnix: uint64(time.Now().Unix())}
	parent.Hash = parent.ComputeHash()
	block := &nom.Momentum{Height: 2, PreviousHash: parent.Hash, TimestampUnix: parent.TimestampUnix + 10}
	block.Hash = block.ComputeHash()

	stub := &relayStub{parent: parent, inserted: make(chan struct{})}
	pm := NewProtocolManager(1, 1, stub)
	defer pm.Stop()
	relayer := newStubPeer(1)
	if err := pm.peers.Register(relayer); err != nil {
		t.Fatal(err)
	}

	pm.fetcher.Start()
	if err := pm.fetcher.Enqueue(relayer.id, &nom.DetailedMomentum{Momentum: block}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stub.inserted:
	case <-time.After(10 * time.Second):
		t.Fatal("the fetcher did not import the momentum within 10s")
	}
	// Stop waits for the import, including its failure handling.
	pm.fetcher.Stop()

	if pm.quarantine.active(relayer.id) {
		t.Fatal("a verdict on a relayed momentum must not quarantine the relayer")
	}
	if pm.peers.Peer(relayer.id) == nil {
		t.Fatal("a verdict on a relayed momentum must not remove the relayer")
	}
}
