package protocol_test

import (
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain"
	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/wallet"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// The side-chain fixtures below are dev's (protocol/chain_bridge_sidechain_test.go
// there), cut down to what the rejection tests use. This line has no cache
// store and no branch restore, so the snapshot covers the canonical chain only.

// rollbackCountingChain records how often the bridge rolls the canonical
// chain back.
type rollbackCountingChain struct {
	chain.Chain
	rollbacks int
}

func (c *rollbackCountingChain) RollbackTo(insertLocker sync.Locker, identifier types.HashHeight) error {
	c.rollbacks++
	return c.Chain.RollbackTo(insertLocker, identifier)
}

// chainSnapshot is the canonical chain and the balances a refused batch must
// leave alone.
type chainSnapshot struct {
	frontier types.HashHeight
	hashes   map[uint64]types.Hash
	balances map[types.Address]string
}

func snapshotChain(t *testing.T, c chain.Chain) chainSnapshot {
	t.Helper()
	store := c.GetFrontierMomentumStore()
	frontier, err := store.GetFrontierMomentum()
	common.FailIfErr(t, err)
	snapshot := chainSnapshot{
		frontier: frontier.Identifier(),
		hashes:   make(map[uint64]types.Hash, frontier.Height),
	}
	for height := uint64(1); height <= frontier.Height; height++ {
		momentum, err := store.GetMomentumByHeight(height)
		common.FailIfErr(t, err)
		snapshot.hashes[height] = momentum.Hash
	}
	snapshot.balances = make(map[types.Address]string)
	for _, address := range []types.Address{g.User1.Address, g.User2.Address, g.Pillar1.Address, types.PillarContract} {
		balance, err := store.GetAccountStore(address).GetBalance(types.ZnnTokenStandard)
		common.FailIfErr(t, err)
		snapshot.balances[address] = balance.String()
	}
	return snapshot
}

func expectChainEquals(t *testing.T, c chain.Chain, expected chainSnapshot) {
	t.Helper()
	actual := snapshotChain(t, c)
	common.Expect(t, actual.frontier, expected.frontier)
	common.Expect(t, len(actual.hashes), len(expected.hashes))
	for height, hash := range expected.hashes {
		common.Expect(t, actual.hashes[height], hash)
	}
	for address, balance := range expected.balances {
		common.Expect(t, actual.balances[address], balance)
	}
}

// unsignedMomentum builds an empty momentum on top of previous, in the next
// slot, that carries a changes-hash the node cannot reproduce. It passes
// every check that runs against the state at previous and fails the first
// one that needs the state it claims to produce.
func unsignedMomentum(previous *nom.Momentum, height uint64) *nom.Momentum {
	timestamp := time.Unix(int64(previous.TimestampUnix+10), 0)
	momentum := &nom.Momentum{
		Version:         previous.Version,
		ChainIdentifier: previous.ChainIdentifier,
		PreviousHash:    previous.Hash,
		Height:          height,
		TimestampUnix:   previous.TimestampUnix + 10,
		Timestamp:       &timestamp,
		Content:         nom.NewMomentumContent(nil),
		ChangesHash:     types.NewHash([]byte("not the state this momentum produces")),
	}
	momentum.Hash = momentum.ComputeHash()
	return momentum
}

// signedBy recomputes the hash and signs the momentum with key.
func signedBy(momentum *nom.Momentum, key *wallet.KeyPair) *nom.Momentum {
	momentum.Hash = momentum.ComputeHash()
	momentum.PublicKey = key.Public
	momentum.Signature = key.Sign(momentum.Hash.Bytes())
	return momentum
}

// attackerMomentum is an unsignedMomentum signed by a key that is not an
// elected producer: internally consistent, so it costs nothing to make. On
// this line it is rejected after the rollback on its changes-hash, which the
// momentum verifier checks before the producer.
func attackerMomentum(previous *nom.Momentum, height uint64) *nom.Momentum {
	return signedBy(unsignedMomentum(previous, height), g.User1)
}

// pillarKey returns the genesis pillar key producing at address.
func pillarKey(t *testing.T, address types.Address) *wallet.KeyPair {
	t.Helper()
	for _, key := range g.PillarKeys {
		if key.Address == address {
			return key
		}
	}
	t.Fatalf("no pillar key produces at %v", address)
	return nil
}

// electedKey returns the key of the pillar elected for the slot at timestamp
// by the election z's frontier implies.
func electedKey(t *testing.T, z mock.MockZenon, timestamp time.Time) *wallet.KeyPair {
	t.Helper()
	producer, err := z.Consensus().GetMomentumProducer(timestamp)
	common.FailIfErr(t, err)
	return pillarKey(t, *producer)
}

// producerMomentum is an unsignedMomentum signed by the pillar elected for
// its slot. It passes the producer check and fails on its changes-hash once
// applied.
func producerMomentum(t *testing.T, z mock.MockZenon, previous *nom.Momentum, height uint64) *nom.Momentum {
	t.Helper()
	momentum := unsignedMomentum(previous, height)
	return signedBy(momentum, electedKey(t, z, *momentum.Timestamp))
}

func detailedOf(momentums ...*nom.Momentum) []*nom.DetailedMomentum {
	detailed := make([]*nom.DetailedMomentum, len(momentums))
	for i, momentum := range momentums {
		detailed[i] = &nom.DetailedMomentum{Momentum: momentum, AccountBlocks: []*nom.AccountBlock{}}
	}
	return detailed
}

func momentumAt(t *testing.T, c chain.Chain, height uint64) *nom.Momentum {
	t.Helper()
	momentum, err := c.GetFrontierMomentumStore().GetMomentumByHeight(height)
	common.FailIfErr(t, err)
	if momentum == nil {
		t.Fatalf("no momentum at height %d", height)
	}
	return momentum
}

// newSideChainBridge builds the bridge the way the node does, with a real
// verifier over the counting chain.
func newSideChainBridge(z mock.MockZenon) (*rollbackCountingChain, protocol.ChainBridge) {
	counting := &rollbackCountingChain{Chain: z.Chain()}
	supervisor := vm.NewSupervisor(z.Chain(), z.Consensus())
	momentumVerifier := verifier.NewVerifier(counting, z.Consensus())
	return counting, protocol.NewChainBridge(counting, z.Consensus(), momentumVerifier, supervisor)
}

// buildFork produces, on a fresh node that agrees with z up to forkBase, a
// genuine branch spanning forkBase+1..forkTop whose first momentum carries a
// send of amount from User1 to User2, so that branches built with different
// amounts differ from each other and from z's own branch. Both nodes share
// genesis and a deterministic clock, so the momentums up to forkBase are
// byte-identical on both.
func buildFork(t *testing.T, z mock.MockZenon, forkBase, forkTop uint64, amount int64) []*nom.DetailedMomentum {
	t.Helper()
	other := mock.NewMockZenon(t)
	defer other.StopPanic()
	other.InsertMomentumsTo(forkBase)
	common.Expect(t, momentumAt(t, other.Chain(), forkBase).Hash, momentumAt(t, z.Chain(), forkBase).Hash)
	other.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(amount),
	}, nil, mock.SkipVmChanges)
	other.InsertMomentumsTo(forkTop)
	return prefetchAbove(t, other, forkBase, forkTop)
}

// prefetchAbove returns the momentums of z at heights from+1..to with their
// account blocks.
func prefetchAbove(t *testing.T, z mock.MockZenon, from, to uint64) []*nom.DetailedMomentum {
	t.Helper()
	branch := make([]*nom.DetailedMomentum, 0, to-from)
	for height := from + 1; height <= to; height++ {
		detailed, err := z.Chain().GetFrontierMomentumStore().PrefetchMomentum(momentumAt(t, z.Chain(), height))
		common.FailIfErr(t, err)
		branch = append(branch, detailed)
	}
	return branch
}

// expectFrontierAt asserts the canonical frontier sits at the given momentum.
func expectFrontierAt(t *testing.T, c chain.Chain, momentum *nom.Momentum) {
	t.Helper()
	common.Expect(t, c.GetFrontierMomentumStore().Identifier(), momentum.Identifier())
}
