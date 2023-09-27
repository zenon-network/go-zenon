package subscribe

import (
	"context"
	"sync"

	"github.com/inconshreveable/log15"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	rpc "github.com/zenon-network/go-zenon/rpc/server"
)

const (
	acChanSize    = 100
	mChanSize     = 100
	installSize   = 100
	uninstallSize = 100
)

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
	chain     chain.Chain
	log       log15.Logger
	installCh chan *Subscription // add subscription
}
type Server struct {
	*Api

	started       bool
	uninstallCh   chan *Subscription // remove subscription
	acCh          chan []*AccountBlock
	mCh           chan *Momentum
	stopped       chan struct{}
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
			},

			acCh:          make(chan []*AccountBlock, acChanSize),
			mCh:           make(chan *Momentum, mChanSize),
			uninstallCh:   make(chan *Subscription, uninstallSize),
			stopped:       make(chan struct{}),
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
	for {
		select {
		case <-s.stopped:
			log.Info("stopped")
			s.subscriptions = nil
			return
		case sub := <-s.installCh:
			s.install(sub)
		case sub := <-s.uninstallCh:
			s.uninstall(sub)
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
	s.log.Info("uninstall", "id", subscription.rpc.ID)
	delete(s.subscriptions[subscription.options.subscriptionType], subscription.rpc.ID)
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
	subscription := NewSubscription(notifier, options)
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
