package subscribe

import (
	"time"

	"github.com/inconshreveable/log15"
	rpc "github.com/zenon-network/go-zenon/rpc/server"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

type SubscriptionType byte

const (
	FirstSubscriptionType SubscriptionType = iota
	AllAccountBlocksSubscription
	AccountBlocksSubscriptionByAddress
	UnreceivedAccountBlocksSubscriptionByAddress
	MomentumsSubscription
	LastSubscriptionType
)

type subscriptionOptions struct {
	subscriptionType SubscriptionType
	createTime       time.Time
	address          types.Address
}

func newSubscription(subscriptionType SubscriptionType) *subscriptionOptions {
	return &subscriptionOptions{
		subscriptionType: subscriptionType,
		createTime:       time.Now(),
	}
}
func NewBlocksSubscription() *subscriptionOptions {
	return newSubscription(AllAccountBlocksSubscription)
}
func NewBlocksByAddressSubscription(addr types.Address) *subscriptionOptions {
	sub := newSubscription(AccountBlocksSubscriptionByAddress)
	sub.address = addr
	return sub
}
func NewToUnreceivedBlocksSubscription(addr types.Address) *subscriptionOptions {
	sub := newSubscription(UnreceivedAccountBlocksSubscriptionByAddress)
	sub.address = addr
	return sub
}
func NewMomentumsSubscription() *subscriptionOptions {
	return newSubscription(MomentumsSubscription)
}

type Subscription struct {
	log      log15.Logger
	options  *subscriptionOptions
	notifier *rpc.Notifier
	rpc      *rpc.Subscription
}

func NewSubscription(notifier *rpc.Notifier, options *subscriptionOptions) *Subscription {
	rpcSub := notifier.CreateSubscription()
	return &Subscription{
		log:      common.RPCLogger.New("module", "subscription", "id", rpcSub.ID),
		options:  options,
		notifier: notifier,
		rpc:      rpcSub,
	}
}

func (s *Subscription) Notify(data interface{}) {
	if s.Closed() {
		return
	}

	err := s.notifier.Notify(s.rpc.ID, data)
	if err != nil {
		s.log.Info("failed to notify", "reason", err)
	}
}
func (s *Subscription) Closed() bool {
	if s.notifier == nil {
		return true
	}
	select {
	case err := <-s.rpc.Err():
		s.log.Info("unsubscribing due to rpc-sub", "reason", err)
		s.notifier = nil
	case <-s.notifier.Closed():
		s.log.Info("unsubscribing", "reason", "notifier-closed")
		s.notifier = nil
	default:
	}
	return s.notifier == nil
}
