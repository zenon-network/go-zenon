// Package protocol keeps the dual ledger synchronised with the rest
// of the network and relays locally produced blocks to it.
//
// The package speaks the eth/61 wire protocol (protocol.go) on top of
// the devp2p transport from package p2p. After exchanging StatusMsg
// handshakes — which must agree on protocol version, network id and
// genesis hash — peers gossip account blocks (TxMsg), announce new
// momentums by hash (NewBlockHashesMsg), propagate full momentums
// (NewBlockMsg) and serve batched history (GetBlockHashesMsg,
// GetBlockHashesFromNumberMsg, GetBlocksMsg and their replies). The
// handshake's TD (total difficulty) field carries the sender's
// frontier momentum height; the protocol treats the chain with the
// highest frontier as the best one.
//
// ProtocolManager (handler.go) runs one handler loop per peer and
// splits synchronisation between two helpers: package downloader
// performs bulk batch downloads from the best peer whenever its
// advertised height exceeds the local frontier, while package fetcher
// recovers individual announced momentums — hash announcements at any
// time, directly propagated full momentums only once the node has
// caught up. ChainBridge (chain_bridge.go) adapts the chain for both
// directions — serving momentums to remote peers and inserting
// downloaded ones under the chain insert lock — and Broadcaster
// (broadcaster.go) inserts momentums and account blocks produced by
// this node before handing them to the manager for propagation.
package protocol

import (
	"fmt"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
)

type broadcaster struct {
	log      common.Logger
	chain    chain.Chain
	protocol *ProtocolManager
}

// NewBroadcaster returns a Broadcaster which inserts blocks produced
// by this node into chain and relays them through protocol.
func NewBroadcaster(chain chain.Chain, protocol *ProtocolManager) Broadcaster {
	return &broadcaster{
		log:      common.ProtocolLogger.New("submodule", "broadcaster"),
		chain:    chain,
		protocol: protocol,
	}
}

func (b *broadcaster) SyncInfo() *SyncInfo {
	return b.protocol.SyncInfo()
}

// CreateMomentum is called when our node created a momentum.
// The momentum will be inserted in the chain and broadcasted.
func (b *broadcaster) CreateMomentum(momentumTransaction *nom.MomentumTransaction) {
	b.log.Info("creating own momentum", "identifier", momentumTransaction.Momentum.Identifier())
	insert := b.chain.AcquireInsert(fmt.Sprintf("zenon - create momentum %v", momentumTransaction.Momentum.Identifier()))
	err := b.chain.AddMomentumTransaction(insert, momentumTransaction)
	insert.Unlock()
	if err != nil {
		b.log.Error("failed to insert own momentum", "reason", err)
		return
	}

	store := b.chain.GetFrontierMomentumStore()
	detailed, err := store.PrefetchMomentum(momentumTransaction.Momentum)
	if err != nil {
		b.log.Error("failed to insert own momentum", "reason", err)
		return
	}

	b.log.Info("broadcasting own momentum", "identifier", momentumTransaction.Momentum.Identifier())
	b.protocol.BroadcastMomentum(detailed, true)
}

// CreateAccountBlock is called when our node created an account block.
// The account-block will be inserted in the chain and broadcasted.
func (b *broadcaster) CreateAccountBlock(accountBlockTransaction *nom.AccountBlockTransaction) {
	insert := b.chain.AcquireInsert(fmt.Sprintf("zenon - create account-block %v", accountBlockTransaction.Block.Header()))
	err := b.chain.AddAccountBlockTransaction(insert, accountBlockTransaction)
	insert.Unlock()
	if err != nil {
		b.log.Error("failed to insert own account-block", "reason", err)
		return
	}

	b.protocol.BroadcastAccountBlock(accountBlockTransaction.Block)
}
