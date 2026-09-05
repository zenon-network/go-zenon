package subscribe

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

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

// ErrSubscriptionLimitReached is returned when the server already serves
// maxSubscriptions live subscriptions across all connections.
var ErrSubscriptionLimitReached = errors.New("subscribe server subscription limit reached")

const (
	acChanSize  = 100
	mChanSize   = 100
	installSize = 100

	// maxSubscriptions bounds the subscriptions kept installed across all
	// connections. Every matching chain event is delivered to each of them
	// from the single worker goroutine, so this also bounds the work one
	// event can cost. Each connection is separately limited by the RPC
	// server.
	maxSubscriptions = 4096
)

// sweepInterval is how often the worker drops subscriptions whose client has
// unsubscribed or disconnected. Broadcasts only visit subscriptions their
// event matches, so without the sweep an address-filtered subscription
// would stay installed until a block for that address arrives.
var sweepInterval = 10 * time.Second

var (
	oneSingleton sync.Mutex
	singleton    *Server
)

type Momentum struct {
	Hash   types.Hash `json:"hash"`
	Height uint64     `json:"height"`
}
type AccountBlock struct {
	BlockType uint64        `json:"blockType"`
	Hash      types.Hash    `json:"hash"`
	Height    uint64        `json:"height"`
	Address   types.Address `json:"address"`
	ToAddress types.Address `json:"toAddress"`
	FromHash  types.Hash    `json:"fromHash"`
}

func newAccountBlock(block *nom.AccountBlock) []*AccountBlock {
	all := make([]*AccountBlock, 1, len(block.DescendantBlocks)+1)
	all[0] = &AccountBlock{
		BlockType: block.BlockType,
		Hash:      block.Hash,
		Height:    block.Height,
		Address:   block.Address,
		ToAddress: block.ToAddress,
		FromHash:  block.FromBlockHash,
	}
	for _, dBlock := range block.DescendantBlocks {
		all = append(all, newAccountBlock(dBlock)...)
	}
	return all
}

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

	// live counts subscriptions that hold a slot of maxSubscriptions: those
	// waiting in installCh plus those installed by the worker. Only subscribe
	// increments it, serialized by stopLock, and only the worker's uninstall
	// decrements it, so the check-then-increment in subscribe cannot
	// overshoot.
	live atomic.Int64
}
type Server struct {
	*Api

	started       bool
	acCh          chan []*AccountBlock
	mCh           chan *Momentum
	subscriptions map[SubscriptionType]map[rpc.ID]*Subscription

	wg sync.WaitGroup
}

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
	}
	return singleton
}
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

func (s *Server) Init() error {
	s.log.Info("init")
	defer s.log.Info("finish init")
	for i := FirstSubscriptionType; i < LastSubscriptionType; i++ {
		s.subscriptions[i] = make(map[rpc.ID]*Subscription)
	}
	return nil
}
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

func (s *Server) InsertMomentum(detailed *nom.DetailedMomentum) {
	select {
	case s.mCh <- &Momentum{
		Hash:   detailed.Momentum.Hash,
		Height: detailed.Momentum.Height,
	}:
	default:
		s.log.Error("can't insert momentum for broadcast", "reason", "channel is full", "momentum-identifier", detailed.Momentum.Identifier())
	}

	abEvents := make([]*AccountBlock, 0, len(detailed.AccountBlocks))
	for _, block := range detailed.AccountBlocks {
		abEvents = append(abEvents, newAccountBlock(block)...)
	}
	select {
	case s.acCh <- abEvents:
	default:
		s.log.Error("can't insert account-blocks for broadcast", "reason", "channel is full", "momentum-identifier", detailed.Momentum.Identifier())
	}
	return
}
func (s *Server) DeleteMomentum(*nom.DetailedMomentum) {
}

func (s *Server) work() {
	log := s.log.New("module", "worker")
	defer common.RecoverStack()
	log.Info("start event loop")
	defer log.Info("stop event loop")
	sweep := time.NewTicker(sweepInterval)
	defer sweep.Stop()
	for {
		select {
		case <-s.stopped:
			log.Info("stopped")
			s.subscriptions = nil
			return
		case sub := <-s.installCh:
			s.install(sub)
		case <-sweep.C:
			s.sweep()
		case momentums := <-s.mCh:
			s.broadcastMomentums(momentums)
		case blocks := <-s.acCh:
			s.broadcastBlocks(blocks)
		}
	}
}

type BroadcastStats struct {
	NumNotify     int
	NumUninstalls int
}

func (s *Server) install(subscription *Subscription) {
	s.log.Info("install", "id", subscription.rpc.ID)
	s.subscriptions[subscription.options.subscriptionType][subscription.rpc.ID] = subscription
}
func (s *Server) uninstall(subscription *Subscription) {
	installed := s.subscriptions[subscription.options.subscriptionType]
	if _, ok := installed[subscription.rpc.ID]; !ok {
		return
	}
	s.log.Info("uninstall", "id", subscription.rpc.ID)
	delete(installed, subscription.rpc.ID)
	// Released exactly once, together with the map entry the slot was held for.
	s.live.Add(-1)
}

// sweep uninstalls every subscription whose client has unsubscribed or
// disconnected, regardless of whether an event for it has arrived.
func (s *Server) sweep() {
	startTime := common.Clock.Now()
	stats := &BroadcastStats{}
	for _, installed := range s.subscriptions {
		for _, subscription := range installed {
			if subscription.Closed() {
				stats.NumUninstalls += 1
				s.uninstall(subscription)
			}
		}
	}
	if stats.NumUninstalls > 0 {
		s.log.Info("finish sweeping subscriptions", "elapsed", common.Clock.Now().Sub(startTime), "stats", stats)
	}
}
func (s *Server) broadcast(subscription *Subscription, data interface{}, stats *BroadcastStats) {
	if subscription.Closed() {
		stats.NumUninstalls += 1
		s.uninstall(subscription)
	} else {
		stats.NumNotify += 1
		subscription.Notify(data)
	}
}
func (s *Server) broadcastMomentums(momentum *Momentum) {
	if momentum == nil {
		return
	}
	startTime := common.Clock.Now()
	stats := &BroadcastStats{}

	for _, f := range s.subscriptions[MomentumsSubscription] {
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

	for _, f := range s.subscriptions[AllAccountBlocksSubscription] {
		s.broadcast(f, blocks, stats)
	}
	for _, f := range s.subscriptions[AccountBlocksSubscriptionByAddress] {
		if blocks, ok := byAddress[f.options.address]; ok {
			s.broadcast(f, blocks, stats)
		}
	}
	for _, f := range s.subscriptions[UnreceivedAccountBlocksSubscriptionByAddress] {
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
	// the count covers queued and installed subscriptions alike; the worker
	// returns it when it uninstalls the entry.
	if s.live.Load() >= maxSubscriptions {
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

func (s *Api) Momentums(ctx context.Context) (*rpc.Subscription, error) {
	s.log.Info("new subscription", "type", "Momentums")
	return s.subscribe(ctx, NewMomentumsSubscription())
}
func (s *Api) AllAccountBlocks(ctx context.Context) (*rpc.Subscription, error) {
	s.log.Info("new subscription", "type", "AllAccountBlocks")
	return s.subscribe(ctx, NewBlocksSubscription())
}
func (s *Api) AccountBlocksByAddress(ctx context.Context, address types.Address) (*rpc.Subscription, error) {
	s.log.Info("new subscription", "type", "AccountBlocksByAddress")
	return s.subscribe(ctx, NewBlocksByAddressSubscription(address))
}
func (s *Api) UnreceivedAccountBlocksByAddress(ctx context.Context, address types.Address) (*rpc.Subscription, error) {
	s.log.Info("new subscription", "type", "UnreceivedAccountBlocksByAddress")
	return s.subscribe(ctx, NewToUnreceivedBlocksSubscription(address))
}
