package switcher

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/p2p/discover"
	"github.com/zenon-network/go-zenon/p2p/legacy"
	"github.com/zenon-network/go-zenon/p2p/libp2p"
)

// sporkPollInterval is how often the activation watcher checks the
// oracle once Start() has launched it. 1s is fast enough that the swap
// fires well within a single momentum slot (~10s); the check itself is
// a cached compare, so the cost is negligible.
const sporkPollInterval = 1 * time.Second

// Server is the spork-gated p2p server. It owns exactly one transport
// backend at any given time — the legacy (devp2p/RLPX) stack before
// activation, the libp2p stack after — and atomically swaps when the
// libp2p activation spork's EnforcementHeight is reached on the local
// chain.
//
// Server implements p2p.Server so callers (node, rpc) hold it via the
// interface and never reach the active backend directly.
//
// All public fields are read-only after Start().
type Server struct {
	// ---- shared config ----
	PrivateKey        *ecdsa.PrivateKey
	Name              string
	MaxPeers          int
	MinConnectedPeers int
	MaxPendingPeers   int
	ListenAddr        string // "host:port"
	Protocols         []p2p.Protocol

	// ---- legacy backend config ----
	LegacyBootstrapNodes []*discover.Node
	NodeDatabase         string

	// ---- libp2p backend config ----
	Libp2pBootstrapPeers []peer.AddrInfo
	// NATPortMap, when true, enables UPnP / NAT-PMP port mapping in
	// the libp2p backend. Default false to match the pre-libp2p
	// network's behaviour (legacy had NAT mapping unset). Operators
	// behind home routers can opt-in via the Net.NATPortMap field in
	// config.json.
	NATPortMap bool

	// ---- activation gate ----
	Oracle SporkOracle

	// ---- internal state (do not set from outside) ----
	mu       sync.RWMutex
	active   backend // currently-serving backend (nil when stopped or mid-swap)
	legacy   *legacy.Server
	libp2p   *libp2p.Server
	swapOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup // tracks the activation watcher goroutine
}

// backend is the minimal API the switcher needs from each transport
// backend. Both legacy.Server and libp2p.Server already satisfy it; this
// declaration is private documentation of the contract.
type backend interface {
	Start() error
	Stop()
	Peers() []p2p.Peer
	PeerCount() int
	AddPeer(node *discover.Node)
	Self() *discover.Node
}

// Start launches the server.
//
// The choice of backend is driven by the spork oracle:
//   - If the oracle reports the libp2p spork as already active (e.g.
//     a node syncing onto a chain where the swap happened in history),
//     libp2p is started directly. The legacy backend is never spun up.
//   - Otherwise the legacy backend is started and the activation
//     watcher goroutine is launched. It polls the oracle on a 1s
//     ticker; on the first true reading it triggers the swap.
func (srv *Server) Start() error {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.stopCh != nil {
		return errors.New("switcher: server already started")
	}
	srv.stopCh = make(chan struct{})

	libp2pActive := false
	if srv.Oracle != nil {
		libp2pActive = srv.Oracle.IsLibp2pActive()
	}

	if libp2pActive {
		common.P2PLogger.Info("libp2p spork already active on local chain; starting libp2p backend directly")
		// stdout banner so the chosen backend is visible in docker logs
		// (the p2p log file is the definitive record; this is for ops
		// visibility — see docs/libp2p/libp2p-rollout.md).
		fmt.Printf("\n===== libp2p =====\n")
		fmt.Printf("Spork already active at startup; running on libp2p backend.\n\n")
		return srv.startLibp2pLocked()
	}

	common.P2PLogger.Info("libp2p spork not active; starting legacy (devp2p/RLPX) backend")
	fmt.Printf("\n===== libp2p =====\n")
	fmt.Printf("Spork not yet active; running on legacy (devp2p/RLPX) backend.\n")
	fmt.Printf("Will swap to libp2p when the activation spork's EnforcementHeight is reached.\n\n")
	if err := srv.startLegacyLocked(); err != nil {
		return err
	}
	if srv.Oracle != nil {
		srv.wg.Add(1)
		go srv.watchActivation()
	} else {
		common.P2PLogger.Warn("no spork oracle configured; activation watcher not started")
	}
	return nil
}

// Stop terminates whichever backend is active and shuts down the
// activation watcher. Safe to call before Start() (it's a no-op) and
// safe to call multiple times (subsequent calls are no-ops).
//
// The currently-active backend's Stop() is called outside the lock so
// readers (Peers/PeerCount/RPC) aren't blocked for the duration of the
// teardown. After Stop() returns, the switcher's active reference is
// nil and all delegating methods return zero values.
func (srv *Server) Stop() {
	srv.mu.Lock()
	if srv.stopCh == nil {
		srv.mu.Unlock()
		return
	}
	close(srv.stopCh)
	srv.stopCh = nil
	legacySrv := srv.legacy
	libp2pSrv := srv.libp2p
	srv.legacy = nil
	srv.libp2p = nil
	srv.active = nil
	srv.mu.Unlock()

	if legacySrv != nil {
		legacySrv.Stop()
	}
	if libp2pSrv != nil {
		libp2pSrv.Stop()
	}
	srv.wg.Wait()
}

// activeBackend returns the currently-serving backend (or nil) under
// an RLock. The caller must not hold the result past their critical
// section since swap() can replace it concurrently.
func (srv *Server) activeBackend() backend {
	srv.mu.RLock()
	defer srv.mu.RUnlock()
	return srv.active
}

// Peers returns the currently-connected peers from the active backend.
// Returns nil if the server is stopped or mid-swap.
func (srv *Server) Peers() []p2p.Peer {
	if b := srv.activeBackend(); b != nil {
		return b.Peers()
	}
	return nil
}

// PeerCount returns the number of currently-connected peers. Returns 0
// if the server is stopped or mid-swap.
func (srv *Server) PeerCount() int {
	if b := srv.activeBackend(); b != nil {
		return b.PeerCount()
	}
	return 0
}

// AddPeer requests the active backend to dial and maintain a connection
// to the given node. Discarded if the server is stopped or mid-swap.
func (srv *Server) AddPeer(node *discover.Node) {
	if b := srv.activeBackend(); b != nil {
		b.AddPeer(node)
	}
}

// Self returns the local node's endpoint information from the active
// backend, or an empty Node if the server is stopped or mid-swap.
func (srv *Server) Self() *discover.Node {
	if b := srv.activeBackend(); b != nil {
		return b.Self()
	}
	return &discover.Node{}
}

// startLegacyLocked constructs and starts the legacy backend. Caller
// must hold srv.mu.
func (srv *Server) startLegacyLocked() error {
	srv.legacy = &legacy.Server{
		PrivateKey:        srv.PrivateKey,
		MaxPeers:          srv.MaxPeers,
		MinConnectedPeers: srv.MinConnectedPeers,
		MaxPendingPeers:   srv.MaxPendingPeers,
		Discovery:         true,
		Name:              srv.Name,
		BootstrapNodes:    srv.LegacyBootstrapNodes,
		NodeDatabase:      srv.NodeDatabase,
		Protocols:         srv.Protocols,
		ListenAddr:        srv.ListenAddr,
	}
	if err := srv.legacy.Start(); err != nil {
		srv.legacy = nil
		return fmt.Errorf("switcher: start legacy backend: %w", err)
	}
	srv.active = srv.legacy
	return nil
}

// startLibp2pLocked constructs and starts the libp2p backend. Caller
// must hold srv.mu. Only used at Start() time when there is no RPC
// caller to block — the swap() path uses buildLibp2p + Start outside
// the lock to avoid blocking concurrent Peers/PeerCount/AddPeer
// readers during libp2p initialization.
func (srv *Server) startLibp2pLocked() error {
	srv.libp2p = srv.buildLibp2p()
	if err := srv.libp2p.Start(); err != nil {
		srv.libp2p = nil
		return fmt.Errorf("switcher: start libp2p backend: %w", err)
	}
	srv.active = srv.libp2p
	return nil
}

// buildLibp2p constructs a *libp2p.Server from the switcher's
// configuration without starting it. Pure — no locks, no I/O, no field
// reads on srv beyond the immutable-after-Start config fields. Used by
// the swap() path so libp2p.New() / DHT init / NAT probe time happens
// outside the switcher mutex.
func (srv *Server) buildLibp2p() *libp2p.Server {
	return &libp2p.Server{
		PrivateKey:        srv.PrivateKey,
		Name:              srv.Name,
		MaxPeers:          srv.MaxPeers,
		MinConnectedPeers: srv.MinConnectedPeers,
		MaxPendingPeers:   srv.MaxPendingPeers,
		Discovery:         true,
		NoDial:            false,
		BootstrapPeers:    srv.Libp2pBootstrapPeers,
		NATPortMap:        srv.NATPortMap,
		ListenAddr:        srv.ListenAddr,
		Protocols:         srv.Protocols,
	}
}

// swapFailed emits the failure banner + structured log when the libp2p
// backend fails to start during the swap. Extracted so the swap()
// happy-path code stays linear; called only on the error branch.
func (srv *Server) swapFailed(err error) {
	common.P2PLogger.Crit("failed to start libp2p backend during swap; node has no active network listener", "err", err)
	// stderr so it stays separable from happy-path stdout in docker
	// logs / log aggregators.
	fmt.Fprintf(os.Stderr, "\n===== libp2p swap FAILED =====\n")
	fmt.Fprintf(os.Stderr, "Failed to start libp2p backend: %v\n", err)
	fmt.Fprintf(os.Stderr, "Node has no active network listener. Restart znnd to retry.\n\n")
}

// watchActivation polls the spork oracle until either the spork
// activates (triggering swap()) or Stop() is called.
//
// The polling cadence is sub-second so the swap fires well within a
// single momentum slot once the chain crosses EnforcementHeight. The
// check itself is cheap (one map lookup against cached frontier state),
// so polling rather than wiring into a momentum-event bus keeps the
// switcher decoupled from the chain package.
func (srv *Server) watchActivation() {
	defer srv.wg.Done()

	// Capture stopCh once so we never read a nil channel (which blocks
	// forever) if Stop() closes and nils srv.stopCh while we're waiting.
	srv.mu.RLock()
	stopCh := srv.stopCh
	srv.mu.RUnlock()
	if stopCh == nil {
		return
	}

	ticker := time.NewTicker(sporkPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			if srv.Oracle.IsLibp2pActive() {
				srv.swap()
				return
			}
		}
	}
}

// swap performs the atomic transition from the legacy backend to the
// libp2p backend. Guarded by sync.Once so concurrent triggers (e.g. if
// Stop and the activation tick race) cannot double-swap.
//
// Ordering: legacy is stopped (releasing the TCP listener port), then
// libp2p is constructed and started on the same port. There is a brief
// window — the duration of legacy.Stop() plus libp2p.Start() — during
// which the switcher has no active backend; reads in that window
// observe an empty peer set, which the protocol/handler layer tolerates
// (it sees mass disconnect followed by mass reconnect, both of which
// are normal transitions).
//
// The teardown and startup happen outside the mutex; only the brief
// state transitions (publishing the new backend reference) are
// performed under the lock. This keeps RPC readers responsive across
// the swap — libp2p.New() can take several seconds on a slow NAT path
// and we must not block Peers/PeerCount/Self/AddPeer callers for that
// duration.
//
// If libp2p startup fails the node is left without a network listener.
// We log Crit so the operator notices but do not crash: the RPC stays
// up, the chain RPC stays queryable, and a restart will retry libp2p
// startup directly (the spork is now active, so legacy is never
// reconstructed).
func (srv *Server) swap() {
	srv.swapOnce.Do(func() {
		common.P2PLogger.Info("libp2p spork EnforcementHeight reached; swapping to libp2p backend")
		fmt.Printf("\n===== libp2p swap starting =====\n")
		fmt.Printf("Spork EnforcementHeight reached. Tearing down legacy backend and bringing up libp2p.\n\n")

		// Stage 1: detach legacy from active state under the lock so
		// concurrent reads stop seeing it. Stop the actual backend
		// outside the lock since it can take a noticeable amount of
		// time.
		srv.mu.Lock()
		if srv.stopCh == nil {
			srv.mu.Unlock()
			return // Stop() was called; abort the swap
		}
		legacySrv := srv.legacy
		srv.legacy = nil
		srv.active = nil
		srv.mu.Unlock()

		if legacySrv != nil {
			legacySrv.Stop()
		}

		// Stage 2: construct and start the libp2p backend WITHOUT the
		// switcher mutex held. libp2p.New() runs through transport
		// setup, Noise key derivation, and (when enabled) UPnP/NAT-PMP
		// probes — each of which can take observable wall time. If we
		// did this under srv.mu, every Peers()/PeerCount()/Self()/
		// AddPeer() RPC call would block for that duration.
		newLibp2p := srv.buildLibp2p()
		if err := newLibp2p.Start(); err != nil {
			srv.swapFailed(err)
			return
		}

		// Stage 3: publish the new backend under the lock. If Stop()
		// arrived while libp2p was starting, tear the freshly-built
		// backend down — Stop() can't have seen it (we hadn't written
		// srv.libp2p yet), so it's our responsibility to clean up.
		srv.mu.Lock()
		if srv.stopCh == nil {
			srv.mu.Unlock()
			newLibp2p.Stop()
			return
		}
		srv.libp2p = newLibp2p
		srv.active = newLibp2p
		srv.mu.Unlock()

		common.P2PLogger.Info("libp2p swap complete")
		fmt.Printf("===== libp2p swap complete =====\n")
		fmt.Printf("Now running on libp2p transport.\n\n")
	})
}
