// Package subscribe implements the publish/subscribe half of the "ledger"
// JSON-RPC namespace: push notifications about momentums and account
// blocks as they are committed to the chain.
//
// Subscriptions are only available on connection-oriented transports.
// Websocket and ipc connections are served by the rpc/server package
// with subscription support enabled, while plain HTTP requests are
// handled in single-request mode with subscriptions disabled, so a
// subscribe call over HTTP fails with ErrNotificationsUnsupported from
// the rpc/server package.
//
// A client subscribes by calling the ledger.subscribe method with the
// subscription name as first positional parameter, followed by the
// arguments of that subscription, if any. The names are the lower-camel
// forms of the exported methods of Api: momentums, allAccountBlocks,
// accountBlocksByAddress and unreceivedAccountBlocksByAddress. The call
// returns a subscription id; from then on every event is pushed to the
// client as a JSON-RPC notification with method ledger.subscription
// whose params object carries the subscription id and the event payload.
// Calling ledger.unsubscribe with the id cancels the subscription, as
// does closing the connection.
//
// Internally a process-wide Server registers with the chain as a
// momentum event listener and a single worker goroutine fans each
// inserted momentum out to the installed subscriptions. The hand-off
// from chain insertion to the worker uses bounded buffers, and events
// are dropped (with an error log) when a buffer is full, so delivery is
// best-effort under sustained load. There is no historical backfill:
// a new subscription receives what the worker broadcasts from then on,
// which may include a momentum committed shortly before installation
// that was still buffered, so clients must tolerate an overlap with
// what they fetched beforehand.
package subscribe

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/inconshreveable/log15"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	rpc "github.com/zenon-network/go-zenon/rpc/server"
)

// ErrSubscribeBacklogFull is returned when the worker's install backlog is
// full; the caller should retry.
var ErrSubscribeBacklogFull = errors.New("subscribe server backlog is full")

// ErrSubscriptionLimitReached is returned when the server already serves its
// configured number of live subscriptions across all connections; see
// Server.SetMaxSubscriptions.
var ErrSubscriptionLimitReached = errors.New("subscribe server subscription limit reached")

// Buffer sizes of the Server's internal channels. The momentum and
// account-block channels drop events when full (see
// Server.InsertMomentum); a subscribe call fails with
// ErrSubscribeBacklogFull once installSize installations are pending.
const (
	acChanSize  = 100
	mChanSize   = 100
	installSize = 100

	// deliverQueueSize bounds the events queued per subscription between
	// the broadcast worker and the subscription's own delivery goroutine.
	// It must be at least rpc/server's maxPendingNotifications so that
	// pre-activation buffering can still overflow the notifier and tear
	// the connection down (see TestOverflowBeforeActivationTearsDownConnection).
	deliverQueueSize = 256

	// DefaultMaxSubscriptions bounds the subscriptions kept installed across
	// all connections unless Server.SetMaxSubscriptions configures another
	// limit. Every matching chain event is delivered to each of them from the
	// single worker goroutine, so this also bounds the work one event can
	// cost. Each connection is separately limited by the RPC server.
	DefaultMaxSubscriptions = 4096
)

var (
	oneSingleton sync.Mutex
	singleton    *Server
)

// Momentum is the notification payload of the momentums subscription:
// the hash and height of a momentum that was just inserted into the
// chain. Each notification carries a one-element array of these
// objects.
type Momentum struct {
	Hash   types.Hash `json:"hash"`
	Height uint64     `json:"height"`
}

// AccountBlock is the notification payload of the account-block
// subscriptions: a trimmed-down view of a block committed by a
// momentum. ToAddress is the destination of a send block; FromHash is
// the hash of the send block a receive block accepts (zero otherwise).
// Each notification carries an array of these objects covering one
// momentum, in the momentum's content order; descendant blocks that
// the momentum does not list separately are expanded into the array
// right after their parent, while descendants listed in the content
// keep their own position.
type AccountBlock struct {
	BlockType uint64        `json:"blockType"`
	Hash      types.Hash    `json:"hash"`
	Height    uint64        `json:"height"`
	Address   types.Address `json:"address"`
	ToAddress types.Address `json:"toAddress"`
	FromHash  types.Hash    `json:"fromHash"`
}

func newAccountBlockFiltered(block *nom.AccountBlock, topLevelBlocks map[types.Hash]bool) []*AccountBlock {
	all := make([]*AccountBlock, 1, len(block.DescendantBlocks)+1)
	all[0] = &AccountBlock{
		BlockType: block.BlockType,
		Hash:      block.Hash,
		Height:    block.Height,
		Address:   block.Address,
		ToAddress: block.ToAddress,
		FromHash:  block.FromBlockHash,
	}
	// A descendant the momentum also lists is emitted at its own entry, so it
	// is skipped here. An unlisted descendant is expanded after its parent, so
	// the stream stays complete for either content layout.
	for _, dBlock := range block.DescendantBlocks {
		if !topLevelBlocks[dBlock.Hash] {
			all = append(all, newAccountBlockFiltered(dBlock, topLevelBlocks)...)
		}
	}
	return all
}

// accountBlockEvents flattens the account blocks a momentum lists into the
// events to broadcast, in content order, each block once. The pool commits a
// contract receive's descendants at their own heights and the producer lists
// every pending height, so a descendant normally appears as its own entry
// next to its parent; the verifier accepts a content that lists only the
// parent as well, in which case the descendant is reached through the
// parent's batch.
func accountBlockEvents(blocks []*nom.AccountBlock) []*AccountBlock {
	listed := make(map[types.Hash]bool, len(blocks))
	for _, block := range blocks {
		listed[block.Hash] = true
	}
	events := make([]*AccountBlock, 0, len(blocks))
	for _, block := range blocks {
		events = append(events, newAccountBlockFiltered(block, listed)...)
	}
	return events
}

// Api implements the subscription handlers of the "ledger" JSON-RPC
// namespace. Because each exported method takes a context and returns a
// subscription, the RPC server registers it as a subscription callback
// instead of a regular method: it is invoked through ledger.subscribe
// with the method's lower-camel name as first parameter and streams its
// events as ledger.subscription notifications. New subscriptions are
// handed to the shared Server, which owns the event fan-out.
type Api struct {
	chain chain.Chain
	log   log15.Logger

	// stopLock orders subscribe against Stop: Stop sets isStopped under the
	// lock before closing stopped, so a subscriber that observed
	// isStopped == false holds the lock and keeps the worker alive until its
	// subscription is enqueued.
	stopLock  sync.Mutex
	isStopped bool
	installCh chan *Subscription // add subscription
	stopped   chan struct{}

	// maxSubscriptions is the global limit live is checked against; see
	// Server.SetMaxSubscriptions.
	maxSubscriptions atomic.Int64
	// live counts subscriptions that hold a slot of maxSubscriptions: those
	// waiting in installCh plus those installed. Only subscribe increments
	// it, serialized by stopLock, and only uninstall decrements it, under
	// subsMu together with the map entry, so the check-then-increment in
	// subscribe cannot overshoot and a slot is never released twice.
	live atomic.Int64
}

// Server is the singleton behind Api that connects the chain to the
// active subscriptions. It registers with the chain as a momentum event
// listener, converts each inserted momentum into Momentum and
// AccountBlock events on buffered channels, and fans them out to the
// installed subscriptions from a single worker goroutine. Its Init,
// Start and Stop methods tie it into the node lifecycle.
type Server struct {
	*Api

	started bool
	acCh    chan []*AccountBlock
	mCh     chan *Momentum

	// subsMu guards subscriptions. The worker installs entries and takes
	// snapshots to broadcast to; each entry's watcher goroutine removes it
	// when its client unsubscribes or disconnects, so capacity returns
	// without waiting for the worker or for an event matching the entry.
	subsMu        sync.Mutex
	subscriptions map[SubscriptionType]map[rpc.ID]*Subscription

	wg sync.WaitGroup
}

// GetSubscribeServer returns the process-wide subscription Server,
// creating it bound to the given chain on first call. Later calls
// return the same instance regardless of the argument until Stop clears
// the singleton.
func GetSubscribeServer(chain chain.Chain) *Server {
	oneSingleton.Lock()
	defer oneSingleton.Unlock()

	if singleton == nil {
		singleton = &Server{
			Api: &Api{
				chain:     chain,
				log:       common.RPCLogger.New("module", "subscribe_api"),
				installCh: make(chan *Subscription, installSize),
				stopped:   make(chan struct{}),
			},

			acCh:          make(chan []*AccountBlock, acChanSize),
			mCh:           make(chan *Momentum, mChanSize),
			subscriptions: make(map[SubscriptionType]map[rpc.ID]*Subscription),
		}
		singleton.maxSubscriptions.Store(DefaultMaxSubscriptions)
	}
	return singleton
}

// SetMaxSubscriptions sets how many live subscriptions the server keeps
// across all connections. Subscriptions already installed are kept when the
// limit is lowered; only new ones are refused. A value below one restores
// DefaultMaxSubscriptions.
func (s *Server) SetMaxSubscriptions(n int) {
	if n < 1 {
		n = DefaultMaxSubscriptions
	}
	s.maxSubscriptions.Store(int64(n))
}

// MaxSubscriptions reports the global limit on live subscriptions.
func (s *Server) MaxSubscriptions() int {
	return int(s.maxSubscriptionsLimit())
}

// maxSubscriptionsLimit is the configured limit, or the default while none
// has been set, so an Api built without GetSubscribeServer is bounded too.
func (a *Api) maxSubscriptionsLimit() int64 {
	if limit := a.maxSubscriptions.Load(); limit > 0 {
		return limit
	}
	return DefaultMaxSubscriptions
}

// GetSubscribeApi returns the Api of the singleton Server so the RPC
// layer can register it under the "ledger" namespace. It panics if
// GetSubscribeServer has not been called or the server has not been
// started yet, making wrong startup order fail fast.
func GetSubscribeApi() *Api {
	oneSingleton.Lock()
	defer oneSingleton.Unlock()
	if singleton == nil {
		panic("must call GetSubscribeServer once before calling GetSubscribeApi")
	}
	if !singleton.started {
		panic("must start SubscribeServer before calling GetSubscribeApi")
	}
	return singleton.Api
}

// Init allocates one subscription table per subscription type. It must
// run before Start and always returns nil.
func (s *Server) Init() error {
	s.log.Info("init")
	defer s.log.Info("finish init")
	for i := FirstSubscriptionType; i < LastSubscriptionType; i++ {
		s.subscriptions[i] = make(map[rpc.ID]*Subscription)
	}
	return nil
}

// Start registers the server as a momentum event listener on the chain
// and launches the worker goroutine that serves installations and
// broadcasts. It always returns nil.
func (s *Server) Start() error {
	s.log.Info("start")
	defer s.log.Info("finish start")
	s.started = true
	s.chain.Register(s)
	s.wg.Add(1)
	go func() {
		s.work()
		s.wg.Done()
	}()
	return nil
}

// Stop unregisters from the chain, signals the worker goroutine to exit
// and waits for it, and clears the package singleton so a new server
// can be created. Active subscriptions are not notified; the worker
// simply drops its tables on exit. It always returns nil.
func (s *Server) Stop() error {
	s.log.Info("stop")
	defer s.log.Info("finish stop")
	s.started = false
	s.chain.UnRegister(s)
	s.stopLock.Lock()
	s.isStopped = true
	s.stopLock.Unlock()
	close(s.stopped)
	singleton = nil
	s.log.Debug("wg.Wait() api Server.Stop()")
	s.wg.Wait()
	s.log.Debug("wg.Wait() api Server.Stop() finish")
	return nil
}

// InsertMomentum implements the chain's momentum event listener
// interface. It converts the inserted momentum into a Momentum event
// and a flattened slice of AccountBlock events (descendant blocks
// included) and queues them for the worker goroutine. The queues are
// non-blocking: when one is full the event is dropped and an error is
// logged, so subscribers can miss events under sustained load.
func (s *Server) InsertMomentum(detailed *nom.DetailedMomentum) {
	select {
	case s.mCh <- &Momentum{
		Hash:   detailed.Momentum.Hash,
		Height: detailed.Momentum.Height,
	}:
	default:
		s.log.Error("can't insert momentum for broadcast", "reason", "channel is full", "momentum-identifier", detailed.Momentum.Identifier())
	}

	abEvents := accountBlockEvents(detailed.AccountBlocks)
	select {
	case s.acCh <- abEvents:
	default:
		s.log.Error("can't insert account-blocks for broadcast", "reason", "channel is full", "momentum-identifier", detailed.Momentum.Identifier())
	}
	return
}

// DeleteMomentum implements the chain's momentum event listener
// interface as a no-op: rollbacks are not broadcast to subscribers.
func (s *Server) DeleteMomentum(*nom.DetailedMomentum) {
}

func (s *Server) work() {
	log := s.log.New("module", "worker")
	defer common.RecoverStack()
	log.Info("start event loop")
	defer log.Info("stop event loop")
	for {
		select {
		case <-s.stopped:
			log.Info("stopped")
			s.subsMu.Lock()
			s.subscriptions = nil
			s.subsMu.Unlock()
			return
		case sub := <-s.installCh:
			s.install(sub)
		case momentums := <-s.mCh:
			s.broadcastMomentums(momentums)
		case blocks := <-s.acCh:
			s.broadcastBlocks(blocks)
		}
	}
}

// BroadcastStats counts, for one broadcast pass, how many subscriptions
// were queued, how many events were dropped because the subscription's
// delivery queue was full, and how many subscriptions were found closed
// and uninstalled. It only feeds the log line emitted after each pass.
type BroadcastStats struct {
	NumNotify     int
	NumDropped    int
	NumUninstalls int
}

func (s *Server) install(subscription *Subscription) {
	s.log.Debug("install", "id", subscription.rpc.ID)
	// The watcher's signals and the notifier are captured here, on the
	// worker, which owns subscription.notifier: the next broadcast may find
	// the client gone and clear that field in Closed before the goroutine
	// launched below has run, so the watcher is handed the channels and the
	// notifier reference and never reads the field.
	unsubscribed, disconnected := subscription.rpc.Err(), subscription.notifier.Closed()
	notifier := subscription.notifier
	s.subsMu.Lock()
	s.subscriptions[subscription.options.subscriptionType][subscription.rpc.ID] = subscription
	s.subsMu.Unlock()
	s.wg.Add(1)
	go s.watch(subscription, notifier, unsubscribed, disconnected)
}

// watch delivers queued events to the subscription and removes it as soon
// as its client unsubscribes or its connection closes, the same two signals
// Closed reports, captured by install together with the notifier reference.
// Running delivery and cleanup in one goroutine per subscription isolates
// the shared broadcast worker from slow writes: a blocked connection only
// delays its own delivery goroutine, never other subscribers. Of the
// subscription itself it only uses the immutable options and rpc fields to
// find the entry; the notifier is the reference captured at install time.
// A client that went away while the entry was still queued is removed right
// after install.
func (s *Server) watch(subscription *Subscription, notifier *rpc.Notifier, unsubscribed <-chan error, disconnected <-chan interface{}) {
	defer s.wg.Done()
	for {
		select {
		case <-s.stopped:
			return
		case <-unsubscribed:
			s.uninstall(subscription)
			return
		case <-disconnected:
			s.uninstall(subscription)
			return
		case data := <-subscription.deliverCh:
			err := notifier.Notify(subscription.rpc.ID, data)
			if err != nil {
				subscription.log.Info("failed to notify", "reason", err)
			}
		}
	}
}

func (s *Server) uninstall(subscription *Subscription) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	installed := s.subscriptions[subscription.options.subscriptionType]
	if _, ok := installed[subscription.rpc.ID]; !ok {
		return
	}
	s.log.Debug("uninstall", "id", subscription.rpc.ID)
	delete(installed, subscription.rpc.ID)
	// Released exactly once, together with the map entry the slot was held for.
	s.live.Add(-1)
}

// installed returns the subscriptions of one type for the worker to
// broadcast to. Notifications are network writes, so they must not run
// under subsMu or a slow client would block every watcher's uninstall.
func (s *Server) installed(subscriptionType SubscriptionType) []*Subscription {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	entries := s.subscriptions[subscriptionType]
	snapshot := make([]*Subscription, 0, len(entries))
	for _, subscription := range entries {
		snapshot = append(snapshot, subscription)
	}
	return snapshot
}

// broadcast queues data for one subscription's own delivery goroutine.
// The send is non-blocking: when the subscription's queue is full the
// event is dropped for that subscription only, so a slow or blocked
// connection never stalls the shared worker or delays other subscribers.
func (s *Server) broadcast(subscription *Subscription, data interface{}, stats *BroadcastStats) {
	if subscription.Closed() {
		stats.NumUninstalls += 1
		s.uninstall(subscription)
	} else {
		stats.NumNotify += 1
		select {
		case subscription.deliverCh <- data:
		default:
			stats.NumDropped += 1
		}
	}
}
func (s *Server) broadcastMomentums(momentum *Momentum) {
	if momentum == nil {
		return
	}
	startTime := common.Clock.Now()
	stats := &BroadcastStats{}

	for _, f := range s.installed(MomentumsSubscription) {
		s.broadcast(f, []interface{}{momentum}, stats)
	}

	s.log.Info("finish broadcasting momentum", "identifier", momentum, "elapsed", common.Clock.Now().Sub(startTime), "stats", stats)
}

func (s *Server) broadcastBlocks(blocks []*AccountBlock) {
	if len(blocks) == 0 {
		return
	}
	startTime := common.Clock.Now()
	stats := &BroadcastStats{}

	byAddress := make(map[types.Address][]*AccountBlock)
	unreceivedByAddress := make(map[types.Address][]*AccountBlock)
	for _, block := range blocks {
		if _, ok := byAddress[block.Address]; !ok {
			byAddress[block.Address] = make([]*AccountBlock, 0)
		}
		byAddress[block.Address] = append(byAddress[block.Address], block)
		if nom.IsSendBlock(block.BlockType) {
			if _, ok := unreceivedByAddress[block.ToAddress]; !ok {
				unreceivedByAddress[block.ToAddress] = make([]*AccountBlock, 0)
			}
			unreceivedByAddress[block.ToAddress] = append(unreceivedByAddress[block.ToAddress], block)
		}
	}

	for _, f := range s.installed(AllAccountBlocksSubscription) {
		s.broadcast(f, blocks, stats)
	}
	for _, f := range s.installed(AccountBlocksSubscriptionByAddress) {
		if blocks, ok := byAddress[f.options.address]; ok {
			s.broadcast(f, blocks, stats)
		}
	}
	for _, f := range s.installed(UnreceivedAccountBlocksSubscriptionByAddress) {
		if blocks, ok := unreceivedByAddress[f.options.address]; ok {
			s.broadcast(f, blocks, stats)
		}
	}

	s.log.Info("finish broadcasting account-blocks", "elapsed", common.Clock.Now().Sub(startTime), "stats", stats)
}

func (s *Api) subscribe(ctx context.Context, options *subscriptionOptions) (*rpc.Subscription, error) {
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return nil, rpc.ErrNotificationsUnsupported
	}
	s.stopLock.Lock()
	defer s.stopLock.Unlock()
	// Checked under stopLock, and the notifier subscription is created only on
	// the accepting path: creating it before deciding would leave a
	// subscription registered on the RPC connection that no worker serves,
	// because the handler registers whatever the notifier holds even when this
	// method returns an error.
	if s.isStopped {
		return nil, errors.New("subscribe server is stopped")
	}
	// The enqueue must not block while stopLock is held: the worker drains
	// installCh only between broadcasts, and a broadcast stalled on a slow
	// client would otherwise hold every other subscribe call and Stop behind
	// this lock. A full backlog is reported to the caller instead.
	//
	// Installation itself is not awaited: if Stop lands right after the lock
	// is released, the worker may exit with this entry still buffered, which
	// is indistinguishable from installing it and then stopping — either way
	// no events are delivered.
	//
	// Capacity is checked before NewSubscription: creating the notifier
	// subscription first would leave it registered on the RPC connection
	// (the handler takes it even when this method errors) with an ID the
	// client never learns and cannot unsubscribe. stopLock serializes
	// producers and the worker only drains, so once there is room the send
	// below cannot block.
	// The slot is claimed here, before the entry is handed to the worker, so
	// the count covers queued and installed subscriptions alike; it is
	// returned when the entry is uninstalled, which the entry's watcher does
	// as soon as the client unsubscribes or disconnects.
	if s.live.Load() >= s.maxSubscriptionsLimit() {
		return nil, ErrSubscriptionLimitReached
	}
	if len(s.installCh) == cap(s.installCh) {
		return nil, ErrSubscribeBacklogFull
	}
	subscription := NewSubscription(notifier, options)
	s.live.Add(1)
	s.installCh <- subscription
	return subscription.rpc, nil
}

// Momentums creates a subscription that streams the hash and height of
// every momentum inserted into the chain. Each notification carries an
// array holding a single Momentum object.
//
// JSON-RPC: ledger.subscribe with first parameter "momentums". The
// reply is the subscription id; ErrNotificationsUnsupported is returned
// on transports without notification support (plain HTTP).
func (s *Api) Momentums(ctx context.Context) (*rpc.Subscription, error) {
	s.log.Debug("new subscription", "type", "Momentums")
	return s.subscribe(ctx, NewMomentumsSubscription())
}

// AllAccountBlocks creates a subscription that streams every account
// block committed to the chain. Each notification carries the blocks of
// one momentum as an array of AccountBlock objects, descendant blocks
// included.
//
// JSON-RPC: ledger.subscribe with first parameter "allAccountBlocks".
// The reply is the subscription id; ErrNotificationsUnsupported is
// returned on transports without notification support (plain HTTP).
func (s *Api) AllAccountBlocks(ctx context.Context) (*rpc.Subscription, error) {
	s.log.Debug("new subscription", "type", "AllAccountBlocks")
	return s.subscribe(ctx, NewBlocksSubscription())
}

// AccountBlocksByAddress creates a subscription that streams the
// committed account blocks of a single account chain, i.e. blocks whose
// address field equals the given address. Each notification carries the
// matching blocks of one momentum as an array of AccountBlock objects;
// momentums without matching blocks produce no notification.
//
// JSON-RPC: ledger.subscribe with parameters "accountBlocksByAddress"
// and the address. The reply is the subscription id;
// ErrNotificationsUnsupported is returned on transports without
// notification support (plain HTTP).
func (s *Api) AccountBlocksByAddress(ctx context.Context, address types.Address) (*rpc.Subscription, error) {
	s.log.Debug("new subscription", "type", "AccountBlocksByAddress")
	return s.subscribe(ctx, NewBlocksByAddressSubscription(address))
}

// UnreceivedAccountBlocksByAddress creates a subscription that streams
// send blocks destined for the given address as they are committed,
// that is, transfers the address has yet to receive. Each notification
// carries the matching blocks of one momentum as an array of
// AccountBlock objects. There is no backfill: transfers already pending
// must be fetched with ledger.getUnreceivedBlocksByAddress, and a
// momentum still buffered at installation may be delivered as well.
//
// JSON-RPC: ledger.subscribe with parameters
// "unreceivedAccountBlocksByAddress" and the address. The reply is the
// subscription id; ErrNotificationsUnsupported is returned on
// transports without notification support (plain HTTP).
func (s *Api) UnreceivedAccountBlocksByAddress(ctx context.Context, address types.Address) (*rpc.Subscription, error) {
	s.log.Debug("new subscription", "type", "UnreceivedAccountBlocksByAddress")
	return s.subscribe(ctx, NewToUnreceivedBlocksSubscription(address))
}
