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
func (b *broadcaster) CreateMomentum(momentumTransaction *nom.MomentumTransaction, detailed *nom.DetailedMomentum) {
	b.log.Info("creating own momentum", "identifier", momentumTransaction.Momentum.Identifier())
	insert := b.chain.AcquireInsert(fmt.Sprintf("zenon - create momentum %v", momentumTransaction.Momentum.Identifier()))
	err := b.chain.UpdateCache(insert, detailed, momentumTransaction.Changes)
	if err != nil {
		insert.Unlock()
		b.log.Error("failed to insert own momentum to chain cache", "reason", err)
		return
	}
	err = b.chain.AddMomentumTransaction(insert, momentumTransaction)
	insert.Unlock()
	if err != nil {
		b.log.Error("failed to insert own momentum", "reason", err)
		return
	}

	store := b.chain.GetFrontierMomentumStore()
	detailed, err = store.PrefetchMomentum(momentumTransaction.Momentum)
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
