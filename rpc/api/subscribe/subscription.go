package subscribe

import (
	"time"

	"github.com/inconshreveable/log15"
	rpc "github.com/zenon-network/go-zenon/rpc/server"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

// SubscriptionType identifies the kind of event stream a subscription
// delivers and selects the bucket the server files it under when
// broadcasting.
type SubscriptionType byte

const (
	// FirstSubscriptionType is the lower bound of the valid range. It is
	// only used together with LastSubscriptionType to initialize and
	// iterate the server's per-type subscription tables; it is not a
	// subscribable type itself.
	FirstSubscriptionType SubscriptionType = iota
	// AllAccountBlocksSubscription streams every account block committed
	// to the chain.
	AllAccountBlocksSubscription
	// AccountBlocksSubscriptionByAddress streams the committed account
	// blocks of a single address.
	AccountBlocksSubscriptionByAddress
	// UnreceivedAccountBlocksSubscriptionByAddress streams committed send
	// blocks destined for a single address.
	UnreceivedAccountBlocksSubscriptionByAddress
	// MomentumsSubscription streams the hash and height of every inserted
	// momentum.
	MomentumsSubscription
	// LastSubscriptionType is the upper bound of the valid range; like
	// FirstSubscriptionType it is a marker, not a subscribable type.
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

// NewBlocksSubscription returns the options for a subscription to all
// committed account blocks.
func NewBlocksSubscription() *subscriptionOptions {
	return newSubscription(AllAccountBlocksSubscription)
}

// NewBlocksByAddressSubscription returns the options for a subscription
// to the committed account blocks of addr.
func NewBlocksByAddressSubscription(addr types.Address) *subscriptionOptions {
	sub := newSubscription(AccountBlocksSubscriptionByAddress)
	sub.address = addr
	return sub
}

// NewToUnreceivedBlocksSubscription returns the options for a
// subscription to committed send blocks destined for addr.
func NewToUnreceivedBlocksSubscription(addr types.Address) *subscriptionOptions {
	sub := newSubscription(UnreceivedAccountBlocksSubscriptionByAddress)
	sub.address = addr
	return sub
}

// NewMomentumsSubscription returns the options for a subscription to
// inserted momentum headers (hash and height).
func NewMomentumsSubscription() *subscriptionOptions {
	return newSubscription(MomentumsSubscription)
}

// Subscription pairs an installed event stream with the rpc/server
// subscription that delivers it: the options select which events match,
// the notifier pushes payloads onto the originating websocket or ipc
// connection, and the wrapped rpc subscription carries the id the
// client uses to unsubscribe.
type Subscription struct {
	log      log15.Logger
	options  *subscriptionOptions
	notifier *rpc.Notifier
	rpc      *rpc.Subscription
}

// NewSubscription creates a Subscription on the given notifier with a
// freshly allocated rpc subscription id. The result delivers nothing
// until the server installs it; the rpc/server layer buffers any
// notifications sent before the id has reached the client.
func NewSubscription(notifier *rpc.Notifier, options *subscriptionOptions) *Subscription {
	rpcSub := notifier.CreateSubscription()
	return &Subscription{
		log:      common.RPCLogger.New("module", "subscription", "id", rpcSub.ID),
		options:  options,
		notifier: notifier,
		rpc:      rpcSub,
	}
}

// Notify pushes data to the client as a JSON-RPC notification carrying
// the subscription id. It is a no-op when the subscription is closed,
// and a delivery failure is only logged: the rpc/server layer already
// closes the connection when a write fails.
func (s *Subscription) Notify(data interface{}) {
	if s.Closed() {
		return
	}

	err := s.notifier.Notify(s.rpc.ID, data)
	if err != nil {
		s.log.Info("failed to notify", "reason", err)
	}
}

// Closed reports whether the subscription has ended, which happens when
// the client sends an unsubscribe request or the underlying connection
// closes. The check is non-blocking and latches: once either signal
// fires the notifier reference is dropped and Closed keeps returning
// true. The server uses it to lazily uninstall dead subscriptions
// during broadcasts.
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
