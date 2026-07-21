// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Package p2p implements the Zenon p2p network protocols using libp2p.
package libp2p

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/p2p/discover"

	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	libp2ptcp "github.com/libp2p/go-libp2p/p2p/transport/tcp"
)

const (
	refreshPeersInterval = 30 * time.Second
	protoID              = "/znn/eth/1"
	frameReadTimeout     = 30 * time.Second
	frameWriteTimeout    = 20 * time.Second

	// Exponential-backoff parameters for the peerMaintenanceLoop's
	// bootstrap-redial path. The schedule (in seconds, before jitter) is
	// 5, 10, 20, 40, 80, 160, 300, 300, …  — doubling each failure until
	// the cap, with ±30% jitter applied to each step so 100 nodes
	// restarting simultaneously don't all hit the bootstrap at the same
	// instant. Reset to zero on a successful connect.
	dialBackoffBase   = 5 * time.Second
	dialBackoffCap    = 5 * time.Minute
	dialBackoffJitter = 0.3

	// dhtDiscoveryInterval controls how often dhtDiscoveryLoop reads
	// the DHT routing table and attempts to dial new peers from it.
	// Faster than refreshPeersInterval (which is bootstrap-only) so the
	// node converges to MinConnectedPeers quickly post-startup, but
	// slow enough that the dial loop isn't constantly active on a
	// healthy node already at peer capacity.
	dhtDiscoveryInterval = 15 * time.Second
)

var errServerStopped = errors.New("server stopped")

// Server manages all peer connections using libp2p.
type Server struct {
	// This field must be set to a valid secp256k1 private key.
	PrivateKey *ecdsa.PrivateKey

	// MaxPeers is the maximum number of peers that can be connected.
	MaxPeers int

	// MinConnectedPeers is the minimum number of peers that can be connected.
	MinConnectedPeers int

	// MaxPendingPeers is the maximum number of peers that can be
	// pending in the handshake phase. Zero or negative defaults to
	// p2p.DefaultMaxPendingPeers.
	MaxPendingPeers int

	// Discovery specifies whether the peer discovery mechanism should be started.
	Discovery bool

	// Name sets the node name of this server.
	Name string

	// BootstrapPeers are multiaddr strings used to establish connectivity.
	BootstrapPeers []peer.AddrInfo

	// Protocols should contain the protocols supported by the server.
	Protocols []p2p.Protocol

	// ListenAddr is the address to listen on (e.g., "0.0.0.0:35995").
	ListenAddr string

	// NATPortMap controls whether libp2p attempts UPnP / NAT-PMP port
	// mapping on the listening port. Default false (matches the
	// pre-libp2p network: legacy's NAT field was nil in the standard
	// wiring path, so UPnP/PMP probes never fired). Home operators
	// behind a NATting router can flip this on; data-center operators
	// should leave it off to avoid spurious probes leaving their host.
	NATPortMap bool

	// PeerstoreDir is the on-disk directory for the node's peer
	// database. When non-empty, every peer that completes a Zenon
	// handshake is recorded (peer ID, dialable addresses learned via
	// identify, last-seen time) and the warm-bootstrap path redials
	// the most-recently-seen of them on startup, in parallel with
	// BootstrapPeers — removing the runtime dependency on the
	// bootstrap-node list staying current. Records expire on our own
	// schedule (peerDBMaxAge, 30 days), not libp2p's 30-minute
	// post-disconnect address TTL that made the earlier
	// pstoreds-backed peerstore useless across realistic downtime.
	// When empty, peers are not remembered across restarts.
	PeerstoreDir string

	// If NoDial is true, the server will not dial any peers.
	NoDial bool

	// Internal state
	lock    sync.Mutex
	running bool

	host host.Host
	dht  *dht.IpfsDHT
	// peerdb is the Zenon-owned persistent record of handshaked peers
	// (see peerdb.go) backing the warm-bootstrap path. Holds nil when
	// PeerstoreDir is empty or after Stop(). An atomic pointer rather
	// than a field guarded by srv.lock because the writers (adoption /
	// disconnect / dial-failure hooks) run on goroutines that must not
	// take the lifecycle lock — startupCleanup waits on some of them
	// while holding it. Stragglers that load the pointer right before
	// Stop swaps it to nil can still hit a closed DB; goleveldb returns
	// ErrClosed for that rather than misbehaving.
	peerdb  atomic.Pointer[peerDB]
	ctx     context.Context
	cancel  context.CancelFunc
	peerMap map[string]*Peer
	// dialing tracks outbound dials currently in flight, keyed by the
	// remote libp2p peer.ID string. Used to collapse concurrent dial
	// attempts for the same peer down to one and avoid wasting an
	// FD + Noise handshake on the losing goroutine. Protected by peerMu.
	dialing map[string]struct{}
	peerMu  sync.RWMutex
	// shuttingDown is set by Stop() (under peerMu) before it calls
	// loopWG.Wait(). dialPeer and handleStream consult it inside the
	// same peerMu critical section that inserts a newly-adopted peer
	// into peerMap, so a handshake completing in Stop()'s teardown
	// window is rejected instead of calling loopWG.Add(1) after Wait()
	// may already have returned.
	shuttingDown bool

	// backoff state for the peerMaintenanceLoop redial path. Protected
	// by backoffMu (separate from peerMu so dial/disconnect hot paths
	// don't contend with backoff bookkeeping).
	backoffs  map[string]*dialBackoff
	backoffMu sync.Mutex

	ourHandshake *protoHandshake
	delpeer      chan *Peer
	loopWG       sync.WaitGroup
	pendingCount int32 // atomic; tracks peers in handshake phase

	// discoveryWG tracks dhtDiscoveryLoop and the refresh goroutines it
	// spawns via triggerDHTRefresh, separately from loopWG. Stop() must
	// drain this group before calling srv.dht.Close() — draining loopWG
	// alone only guarantees these goroutines don't outlive Stop()
	// returning, not that they've stopped touching the DHT before it's
	// closed.
	discoveryWG sync.WaitGroup

	// refreshingDHT guards against overlapping RefreshRoutingTable calls
	// from dhtDiscoveryLoop: a refresh walks the keyspace and can take
	// longer than dhtDiscoveryInterval, so a tick that fires mid-refresh
	// must skip rather than pile up a second concurrent walk.
	refreshingDHT atomic.Bool

	// testStartHook, when non-nil, is invoked by Start() at the point
	// where every resource (context, datastore, host, DHT) is live but
	// no dial goroutines have been launched. Returning an error aborts
	// Start(), exercising the startup-cleanup path. Test-only; always
	// nil in production (same pattern as the switcher's NewLegacy /
	// NewLibp2p hooks).
	testStartHook func() error

	// testRefreshHook, when non-nil, is invoked at the start of the
	// goroutine spawned by triggerDHTRefresh, before it calls
	// srv.dht.RefreshRoutingTable(). Lets a test hold a refresh
	// in-flight to deterministically exercise the Stop()-vs-refresh
	// ordering instead of racing the real 15s ticker. Test-only; always
	// nil in production (same pattern as testStartHook).
	testRefreshHook func()

	// testDialAdoptHook, when non-nil, is invoked by dialPeer after the
	// protocol handshake completes and immediately before it takes
	// peerMu to adopt the peer. Lets a test hold an outbound dial
	// goroutine in-flight at that point to deterministically exercise
	// the Stop()-vs-adoption ordering instead of racing a real network
	// dial. Test-only; always nil in production (same pattern as
	// testStartHook).
	testDialAdoptHook func()
}

// dialBackoff carries per-peer state for exponential-backoff redialing.
// Entry is removed entirely on a successful connect so peer-churn doesn't
// leak memory.
type dialBackoff struct {
	attempts int
	nextDial time.Time
}

// Peers returns all connected peers as the application-level Peer
// interface so the result is assignable to p2p.Server.Peers()'s return
// type.
func (srv *Server) Peers() []p2p.Peer {
	srv.peerMu.RLock()
	defer srv.peerMu.RUnlock()

	ps := make([]p2p.Peer, 0, len(srv.peerMap))
	for _, p := range srv.peerMap {
		ps = append(ps, p)
	}
	return ps
}

// PeerCount returns the number of connected peers.
func (srv *Server) PeerCount() int {
	srv.peerMu.RLock()
	defer srv.peerMu.RUnlock()
	return len(srv.peerMap)
}

// AddPeer connects to the given node and maintains the connection.
//
// Failure paths are logged at Warn rather than Debug. The signature is
// void (no return value) because callers — primarily the RPC handler
// for the `admin.addPeer` method — don't currently propagate errors,
// so silently swallowing a misconfigured input would leave an operator
// wondering why their request "did nothing". Warn-level surfaces the
// reason in the normal log file without changing the API.
func (srv *Server) AddPeer(node *discover.Node) {
	if srv.host == nil {
		common.P2PLogger.Warn("AddPeer called before Server.Start; ignoring", "node", node)
		return
	}
	if srv.NoDial {
		common.P2PLogger.Warn("AddPeer called with NoDial set; ignoring", "node", node)
		return
	}
	maddr, err := nodeToMultiaddr(node)
	if err != nil {
		common.P2PLogger.Warn("AddPeer: cannot convert node to multiaddr; ignoring", "node", node, "err", err)
		return
	}
	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		common.P2PLogger.Warn("AddPeer: cannot extract peer info from multiaddr; ignoring", "maddr", maddr, "err", err)
		return
	}
	go func() {
		if err := srv.dialPeer(*info); err != nil {
			common.P2PLogger.Warn("AddPeer: dial failed", "peer", info.ID, "err", err)
		}
	}()
}

// Self returns the local node's endpoint information.
func (srv *Server) Self() *discover.Node {
	srv.lock.Lock()
	defer srv.lock.Unlock()

	if !srv.running || srv.host == nil {
		return &discover.Node{IP: net.ParseIP("0.0.0.0")}
	}

	nodeID := PubkeyToNodeID(&srv.PrivateKey.PublicKey)
	addrs := srv.host.Addrs()
	var ip net.IP
	var port uint16

	if len(addrs) > 0 {
		for _, a := range addrs {
			parsedIP, parsedPort, err := parseMultiaddrIPPort(a)
			if err == nil && !parsedIP.IsUnspecified() {
				ip = parsedIP
				port = parsedPort
				break
			}
		}
		// fallback to first address
		if ip == nil {
			parsedIP, parsedPort, err := parseMultiaddrIPPort(addrs[0])
			if err == nil {
				ip = parsedIP
				port = parsedPort
			}
		}
	}
	if ip == nil {
		ip = net.ParseIP("0.0.0.0")
	}

	return &discover.Node{
		ID:  nodeID,
		IP:  ip,
		TCP: port,
	}
}

// Stop terminates the server and all active peer connections.
// It blocks until all goroutines have exited.
func (srv *Server) Stop() {
	srv.lock.Lock()
	if !srv.running {
		srv.lock.Unlock()
		return
	}
	srv.running = false

	// Mark shutting down under peerMu before any teardown step below.
	// dialPeer/handleStream check this flag inside the same peerMu
	// critical section they use to adopt a peer, so any adoption that
	// reaches that section after this line is serialized-after it and
	// will skip loopWG.Add(1); any adoption that already completed the
	// Add is serialized-before it, so it's safely included in the
	// loopWG.Wait() below.
	srv.peerMu.Lock()
	srv.shuttingDown = true
	srv.peerMu.Unlock()

	// Cancel first and drain discoveryWG before touching the DHT: this
	// stops dhtDiscoveryLoop and waits out any in-flight
	// triggerDHTRefresh goroutine, so srv.dht.Close() below can't race a
	// concurrent RefreshRoutingTable() call. Neither of those goroutines
	// takes srv.lock, so waiting while holding it is safe.
	srv.cancel()
	srv.discoveryWG.Wait()

	if srv.dht != nil {
		srv.dht.Close()
	}
	if srv.host != nil {
		srv.host.Close()
	}
	// Detach the peer database now so hooks stop picking it up, but
	// close it only after loopWG.Wait() so peer-lifecycle hooks that
	// already hold the pointer have finished writing to it.
	peerdb := srv.peerdb.Swap(nil)
	srv.lock.Unlock()

	// Wait outside the lock so goroutines that need to acquire locks can finish.
	srv.loopWG.Wait()

	if peerdb != nil {
		if err := peerdb.Close(); err != nil {
			common.P2PLogger.Warn("error closing peer database", "err", err)
		}
	}
}

// startupCleanup releases resources acquired by a partially-completed
// Start() call. Start invokes it via defer on every error return —
// srv.running is never set on those paths, so Stop() would be a no-op
// and a mid-construction failure (e.g. DHT creation failing after the
// host is already listening) would otherwise leak the bound listener
// and the datastore lock until process exit. The switcher's swap-retry
// path depends on this: re-running Start() must not collide with a
// leaked listener from the previous attempt.
//
// Teardown order mirrors Stop(): context cancel → drain discovery
// goroutines → DHT → host → drain handler goroutines → peer database, so
// peer-lifecycle hooks finish writing before the database closes and
// nothing can touch the DHT after it's closed. Like Stop(), fields other
// than peerdb are left non-nil: an inbound stream handler caught
// mid-flight may still dereference srv.host or srv.ctx, and the next
// Start() overwrites every field anyway.
//
// Caller must hold srv.lock. In practice discoveryWG is always already
// zero here — Start() only launches dhtDiscoveryLoop after every
// fallible step, so a failure that reaches this cleanup can't have
// started it — but draining it keeps this path structurally identical
// to Stop() rather than relying on that ordering.
func (srv *Server) startupCleanup() {
	if srv.cancel != nil {
		srv.cancel()
	}
	srv.discoveryWG.Wait()
	if srv.dht != nil {
		_ = srv.dht.Close()
	}
	if srv.host != nil {
		_ = srv.host.Close()
	}
	// Inbound handlers that slipped in between SetStreamHandler and the
	// failure hold loopWG entries via runPeer; host.Close() has reset
	// their streams, so this returns promptly. None of those goroutines
	// take srv.lock, so waiting while holding it is safe.
	srv.loopWG.Wait()
	if pdb := srv.peerdb.Swap(nil); pdb != nil {
		if cerr := pdb.Close(); cerr != nil {
			common.P2PLogger.Warn("error closing peer database during startup cleanup", "err", cerr)
		}
	}
}

// Start starts running the server.
func (srv *Server) Start() (err error) {
	srv.lock.Lock()
	defer srv.lock.Unlock()

	if srv.running {
		return errors.New("server already running")
	}
	common.P2PLogger.Info("Starting Server (libp2p)")

	if srv.PrivateKey == nil {
		return fmt.Errorf("Server.PrivateKey must be set to a non-nil key")
	}

	srv.ctx, srv.cancel = context.WithCancel(context.Background())
	srv.peerMap = make(map[string]*Peer)
	srv.dialing = make(map[string]struct{})
	srv.backoffs = make(map[string]*dialBackoff)
	srv.delpeer = make(chan *Peer, 16)
	srv.peerMu.Lock()
	srv.shuttingDown = false
	srv.peerMu.Unlock()

	// Undo partial construction on any failure below. Start acquires
	// resources in sequence (context → datastore → host → DHT) and the
	// post-host failure paths would otherwise leak a bound listener and
	// a locked datastore: srv.running is never set on an error return,
	// so Stop() would be a no-op against the half-constructed server.
	defer func() {
		if err != nil {
			srv.startupCleanup()
		}
	}()

	// Convert ECDSA key to libp2p key
	privKey, err := ECDSAToLibp2pPrivKey(srv.PrivateKey)
	if err != nil {
		return fmt.Errorf("convert key: %w", err)
	}

	// Parse listen address into multiaddr
	listenMaddr, err := parseListenAddr(srv.ListenAddr)
	if err != nil {
		return fmt.Errorf("parse listen addr %q: %w", srv.ListenAddr, err)
	}

	// Build libp2p options.
	//
	// NATPortMap is gated by srv.NATPortMap (default false) so we don't
	// silently change behaviour for operators whose pre-libp2p nodes
	// had no NAT mapping configured. UPnP / NAT-PMP probes can be
	// undesirable on data-center hosts and on networks with strict
	// outbound-traffic monitoring; opt-in keeps the principle of least
	// surprise.
	opts := []libp2p.Option{
		libp2p.Identity(privKey),
		libp2p.ListenAddrs(listenMaddr),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Muxer("/yamux/1.0.0", yamux.DefaultTransport),
		libp2p.Transport(libp2ptcp.NewTCPTransport),
	}
	if srv.NATPortMap {
		opts = append(opts, libp2p.NATPortMap())
	}

	// Peer database (optional).
	//
	// When PeerstoreDir is set, peers that complete a Zenon handshake
	// are recorded on disk and the warm-bootstrap path (below) redials
	// them on the next startup, in parallel with the configured
	// BootstrapPeers — so a node that's been online before doesn't
	// depend on the bootstrap list staying current to rejoin.
	//
	// The libp2p host itself always uses its default in-memory
	// peerstore: its address records are identify-managed and expire
	// 30 minutes after disconnect, which is why they cannot serve as
	// the restart-survival mechanism (see peerdb.go).
	if srv.PeerstoreDir != "" {
		pdb, err := openPeerDB(srv.PeerstoreDir)
		if err != nil {
			return fmt.Errorf("open peer database at %q: %w", srv.PeerstoreDir, err)
		}
		srv.peerdb.Store(pdb)
		common.P2PLogger.Info("peer database opened", "path", srv.PeerstoreDir)
	}

	// Create libp2p host
	srv.host, err = libp2p.New(opts...)
	if err != nil {
		return fmt.Errorf("create libp2p host: %w", err)
	}

	// Build our protocol handshake
	srv.ourHandshake = &protoHandshake{
		Version: baseProtocolVersion,
		Name:    srv.Name,
		ID:      PubkeyToNodeID(&srv.PrivateKey.PublicKey),
	}
	for _, p := range srv.Protocols {
		srv.ourHandshake.Caps = append(srv.ourHandshake.Caps, p.Cap())
	}

	// Register stream handler
	srv.host.SetStreamHandler(protocol.ID(protoID), srv.handleStream)

	// Start discovery DHT.
	//
	// Configured for Zenon's network size (≪1000 nodes) rather than the
	// IPFS defaults (calibrated for millions):
	//
	//   - ModeServer (not ModeAutoServer): every node serves DHT queries.
	//     For a small network we'd rather have all nodes participate than
	//     have the autodetect heuristic decide for us.
	//   - ProtocolPrefix("/znn") isolates the routing table from the
	//     public IPFS DHT. Without this, well-meaning IPFS peers that
	//     dial port 35995 would fill our buckets with useless entries.
	//   - DisableProviders + DisableValues turn off the content/value
	//     routing layers — we only need peer routing.
	//   - BucketSize and RoutingTableRefreshPeriod tuned smaller/faster
	//     than IPFS defaults so the table converges quickly on a small
	//     network without hammering peers with refresh queries.
	//   - Started unconditionally rather than gated on
	//     len(BootstrapPeers) > 0, so bootstrap nodes (which have no
	//     seeders) also participate in discovery.
	if srv.Discovery {
		srv.dht, err = dht.New(srv.ctx, srv.host,
			dht.Mode(dht.ModeServer),
			dht.ProtocolPrefix("/znn"),
			dht.DisableProviders(),
			dht.DisableValues(),
			dht.BucketSize(16),
			dht.RoutingTableRefreshPeriod(1*time.Minute),
		)
		if err != nil {
			return fmt.Errorf("create DHT: %w", err)
		}
		if err := srv.dht.Bootstrap(srv.ctx); err != nil {
			common.P2PLogger.Warn(fmt.Sprintf("DHT bootstrap failed: %v", err))
		}
	}

	if srv.testStartHook != nil {
		if err := srv.testStartHook(); err != nil {
			return fmt.Errorf("start hook: %w", err)
		}
	}

	// Dial bootstrap peers immediately so the node has peers within seconds of
	// startup rather than waiting for the first peerMaintenanceLoop tick.
	if !srv.NoDial && len(srv.BootstrapPeers) > 0 {
		for _, pi := range srv.BootstrapPeers {
			pi := pi
			go func() {
				if err := srv.dialPeer(pi); err != nil {
					common.P2PLogger.Debug(fmt.Sprintf("bootstrap dial to %s failed: %v", pi.ID, err))
				}
			}()
		}
	}

	// Warm bootstrap from the peer database.
	//
	// On a fresh install (or with PeerstoreDir unset) this is a no-op.
	// On any subsequent restart it dials peers we successfully
	// handshaked with before — in parallel with the configured
	// BootstrapPeers — which lets the node rejoin the network even if
	// the entire bootstrap list has rotated since the previous run.
	// This is the primary resilience mechanism against
	// stale-bootstrap-list scenarios; the bootstrap entries become a
	// first-time-setup dependency rather than a runtime dependency.
	//
	// Fan-out is capped at MinConnectedPeers so a long-running node
	// with hundreds of recorded peers doesn't burst all its FDs on
	// startup. The DHT discovery loop covers the rest as we get going.
	warmDials := 0
	if !srv.NoDial && srv.peerdb.Load() != nil {
		warmDials = srv.warmBootstrap()
	}

	// Guardrail: with no bootstrap entries and no remembered peers the
	// node has no outbound path onto the network. That is legitimate
	// for designated bootstrap nodes (they are the seed; inbound is
	// their job) but network-fatal if it happens fleet-wide at the
	// activation swap — so make it loud. Deliberately does not refuse
	// to start: refusing would partition the node just as hard while
	// also turning away inbound connections.
	if !srv.NoDial && len(srv.BootstrapPeers) == 0 && warmDials == 0 {
		common.P2PLogger.Crit("no libp2p bootstrap peers configured and no remembered peers; node is isolated until inbound peers arrive or Net.BootstrapPeers is populated")
	}

	// Mark running only after all initialization succeeded.
	// Any error above returns before reaching here, so Stop() is safe.
	srv.running = true

	// Start peer maintenance loop
	srv.loopWG.Add(1)
	go func() {
		srv.peerMaintenanceLoop()
		srv.loopWG.Done()
	}()

	// Start peer cleanup loop
	srv.loopWG.Add(1)
	go func() {
		srv.peerCleanupLoop()
		srv.loopWG.Done()
	}()

	// Start the DHT discovery loop. The DHT's routing table is
	// populated as a side effect of the libp2p Identify exchange on
	// each connection and the bootstrap walks above; this loop is what
	// surfaces those entries into the Zenon dial loop. Without it the
	// DHT would fill its table but nothing would translate that into
	// new application-protocol streams — the network would be
	// effectively bootstrap-only after activation, which is what the
	// PR review (point 1) flagged.
	if srv.Discovery && srv.dht != nil && !srv.NoDial {
		srv.discoveryWG.Add(1)
		go func() {
			srv.dhtDiscoveryLoop()
			srv.discoveryWG.Done()
		}()
	}

	// Periodically expire stale peer-database records so a node that
	// runs for months doesn't accumulate unreachable candidates.
	if srv.peerdb.Load() != nil {
		srv.loopWG.Add(1)
		go func() {
			srv.peerdbCleanupLoop()
			srv.loopWG.Done()
		}()
	}

	common.P2PLogger.Info(fmt.Sprintf("Listening on %s", srv.host.Addrs()))
	return nil
}

// recordPeerConnected persists a peer we just completed a Zenon
// handshake with (or are parting from) so warm bootstrap can redial it
// across restarts. The stored addresses are the peer's self-advertised
// listen addresses learned via identify, read from the host's
// in-memory peerstore — NOT the connection's remote multiaddr, which
// for inbound peers is an ephemeral source port that cannot be dialed
// back. extra carries addresses already known-dialable (e.g. the addrs
// an outbound dial just used) to cover the window where identify
// hasn't completed yet.
func (srv *Server) recordPeerConnected(pid peer.ID, extra ...ma.Multiaddr) {
	pdb := srv.peerdb.Load()
	if pdb == nil {
		return
	}
	addrs := append(srv.host.Peerstore().Addrs(pid), extra...)
	pdb.recordConnected(pid, addrs)
}

// peerdbCleanupLoop runs expire() on the peer database once per
// peerDBCleanupCycle until the server context is cancelled.
func (srv *Server) peerdbCleanupLoop() {
	ticker := time.NewTicker(peerDBCleanupCycle)
	defer ticker.Stop()
	for {
		select {
		case <-srv.ctx.Done():
			return
		case <-ticker.C:
			if pdb := srv.peerdb.Load(); pdb != nil {
				pdb.expire()
			}
		}
	}
}

// handleStream is called when a remote peer opens a stream to us.
func (srv *Server) handleStream(s network.Stream) {
	// Enforce MaxPendingPeers. Zero or negative falls back to
	// p2p.DefaultMaxPendingPeers, the same preset node/defaults.go
	// applies when an operator's config.json omits the field, per the
	// documented "zero defaults to preset" semantics in p2p/config.go —
	// rather than disabling the pending-handshake throttle.
	limit := srv.MaxPendingPeers
	if limit <= 0 {
		limit = p2p.DefaultMaxPendingPeers
	}
	pending := atomic.AddInt32(&srv.pendingCount, 1)
	if int(pending) > limit {
		atomic.AddInt32(&srv.pendingCount, -1)
		s.Reset()
		return
	}
	defer atomic.AddInt32(&srv.pendingCount, -1)

	// Slowloris protection lives entirely in StreamRW: every ReadMsg /
	// WriteMsg call sets its own per-message deadline. There used to be
	// a SetReadDeadline + deferred clear here too, but it was redundant
	// (the first ReadMsg in the handshake immediately overwrites it) and
	// the deferred clear was a no-op anyway since subsequent ReadMsg
	// calls re-arm the deadline.

	rw := NewStreamRW(s)
	remotePeer := s.Conn().RemotePeer()

	// Run protocol handshake
	phs, err := srv.doProtoHandshake(rw)
	if err != nil {
		common.P2PLogger.Debug(fmt.Sprintf("proto handshake failed with %s: %v", remotePeer, err))
		s.Reset()
		return
	}

	// Verify the claimed NodeID matches the libp2p peer identity.
	// The Noise handshake cryptographically authenticates the remote key,
	// so we can derive the expected NodeID from RemotePeer() and compare.
	//
	// Reject paths Reset() the stream AND close the underlying libp2p
	// connection. Stream.Reset() alone leaves the host-level connection
	// alive, which lets an unrelated libp2p peer (e.g. a stray IPFS node
	// that resolved one of our bootstrap entries) sit on a connection
	// slot without ever speaking Zenon protocol. ClosePeer() forces the
	// host to tear that down so the FD / yamux session is reclaimed.
	remoteID := phs.ID
	expectedID, err := nodeIDFromPeerID(remotePeer)
	if err != nil {
		common.P2PLogger.Warn("rejecting non-secp256k1 peer", "peer", remotePeer, "err", err)
		s.Reset()
		_ = srv.host.Network().ClosePeer(remotePeer)
		return
	}
	if remoteID != expectedID {
		common.P2PLogger.Warn("NodeID mismatch; disconnecting peer",
			"peer", remotePeer,
			"claimed", fmt.Sprintf("%x", remoteID[:8]),
			"expected", fmt.Sprintf("%x", expectedID[:8]))
		s.Reset()
		_ = srv.host.Network().ClosePeer(remotePeer)
		return
	}

	// Check we're not connecting to ourselves
	selfID := PubkeyToNodeID(&srv.PrivateKey.PublicKey)
	if remoteID == selfID {
		common.P2PLogger.Debug("rejecting connection from self")
		s.Reset()
		_ = srv.host.Network().ClosePeer(remotePeer)
		return
	}

	// Check protocol match before taking the lock — this is not a concurrent
	// state issue and avoids holding the lock during the check.
	//
	// On reject, tear down the underlying libp2p connection as well as
	// the stream: without that, an untracked peer would hold a yamux
	// session + file descriptor without counting toward MaxPeers and
	// without ever speaking Zenon protocol. Safe here because we never
	// adopted a stream from this peer.
	if len(srv.Protocols) > 0 && countMatchingProtocols(srv.Protocols, phs.Caps) == 0 {
		common.P2PLogger.Debug(fmt.Sprintf("no matching protocols with %s", remotePeer))
		s.Reset()
		_ = srv.host.Network().ClosePeer(remotePeer)
		return
	}

	// Atomically check max peers, duplicate, and insert to eliminate the
	// TOCTOU race that two concurrent handleStream calls would otherwise create.
	p := newPeerFromStream(rw, s, remoteID, phs.Caps, phs.Name, srv.Protocols)
	srv.peerMu.Lock()
	if srv.shuttingDown {
		srv.peerMu.Unlock()
		common.P2PLogger.Debug("server shutting down, rejecting connection")
		s.Reset()
		_ = srv.host.Network().ClosePeer(remotePeer)
		return
	}
	if len(srv.peerMap) >= srv.MaxPeers {
		srv.peerMu.Unlock()
		common.P2PLogger.Debug("max peers reached, rejecting connection")
		s.Reset()
		// Same as above: drop the host-level connection so MaxPeers is
		// actually a bound on libp2p resources, not just on adopted
		// Zenon streams.
		_ = srv.host.Network().ClosePeer(remotePeer)
		return
	}
	if _, exists := srv.peerMap[remotePeer.String()]; exists {
		srv.peerMu.Unlock()
		common.P2PLogger.Debug(fmt.Sprintf("duplicate peer %s, rejecting", remotePeer))
		// Reset only the duplicate stream. Do NOT ClosePeer here —
		// the host-level connection is being used by the legitimate
		// already-adopted stream from the first concurrent handler,
		// and closing it would terminate that healthy peer.
		s.Reset()
		return
	}
	srv.peerMap[remotePeer.String()] = p
	// loopWG.Add happens inside the same peerMu critical section as the
	// shuttingDown check above so it can never race Stop()'s
	// loopWG.Wait() (see the shuttingDown field doc).
	srv.loopWG.Add(1)
	srv.peerMu.Unlock()

	// Inbound adoption: identify may not have learned the peer's
	// listen addresses yet, in which case this records lastSeen only
	// and the disconnect-time refresh in runPeer fills the addresses.
	srv.recordPeerConnected(remotePeer)

	go func() {
		srv.runPeer(p)
		srv.loopWG.Done()
	}()
}

// doProtoHandshake runs the protocol handshake over a StreamRW.
func (srv *Server) doProtoHandshake(rw *StreamRW) (*protoHandshake, error) {
	// p2p.Send our handshake
	errc := make(chan error, 1)
	go func() {
		errc <- p2p.Send(rw, handshakeMsg, srv.ourHandshake)
	}()

	// Read remote handshake. ReadMsgLimited enforces baseProtocolMaxMsgSize
	// before allocating a payload buffer, so an unauthenticated peer can't
	// force a large allocation by declaring an oversized frame.
	msg, err := rw.ReadMsgLimited(baseProtocolMaxMsgSize)
	if err != nil {
		return nil, fmt.Errorf("read handshake: %w", err)
	}
	if msg.Code == discMsg {
		var reason [1]p2p.DiscReason
		if err := msg.Decode(&reason); err != nil {
			return nil, fmt.Errorf("decode disconnect: %w", err)
		}
		return nil, reason[0]
	}
	if msg.Code != handshakeMsg {
		return nil, fmt.Errorf("expected handshake msg, got code %d", msg.Code)
	}

	var phs protoHandshake
	if err := msg.Decode(&phs); err != nil {
		return nil, fmt.Errorf("decode handshake: %w", err)
	}
	if phs.Version != srv.ourHandshake.Version {
		return nil, p2p.DiscIncompatibleVersion
	}
	if (phs.ID == discover.NodeID{}) {
		return nil, p2p.DiscInvalidIdentity
	}

	// Wait for our send to complete
	if err := <-errc; err != nil {
		return nil, fmt.Errorf("send handshake: %w", err)
	}

	return &phs, nil
}

// dialPeer connects to a remote peer and performs the handshake.
//
// Concurrency: at most one dial per remote peer.ID is in flight at a time.
// The first caller takes a slot in srv.dialing (under peerMu) and runs the
// dial; subsequent callers see the slot taken and bail. Without this, two
// peerMaintenanceLoop ticks 30s apart (or Start's bootstrap dial racing the
// first tick) could each spawn a goroutine that gets all the way through
// host.Connect + Noise handshake before the loser bails at the final
// peerMap dedup, wasting an FD and CPU.
func (srv *Server) dialPeer(info peer.AddrInfo) error {
	peerKey := info.ID.String()

	// Reserve the dialing slot under the write lock, plus run the
	// peerMap/MaxPeers pre-check atomically with it so the slot reservation
	// observes a consistent peer-set snapshot.
	srv.peerMu.Lock()
	if len(srv.peerMap) >= srv.MaxPeers {
		srv.peerMu.Unlock()
		return fmt.Errorf("max peers reached")
	}
	if _, exists := srv.peerMap[peerKey]; exists {
		srv.peerMu.Unlock()
		return fmt.Errorf("already connected to %s", info.ID)
	}
	if _, exists := srv.dialing[peerKey]; exists {
		srv.peerMu.Unlock()
		return fmt.Errorf("dial already in progress for %s", info.ID)
	}
	srv.dialing[peerKey] = struct{}{}
	srv.peerMu.Unlock()

	// Release the dialing slot on every return path so a failed dial
	// doesn't permanently block re-dial attempts.
	defer func() {
		srv.peerMu.Lock()
		delete(srv.dialing, peerKey)
		srv.peerMu.Unlock()
	}()

	if err := srv.host.Connect(srv.ctx, info); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	// Open a new stream
	s, err := srv.host.NewStream(srv.ctx, info.ID, protocol.ID(protoID))
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}

	// Slowloris protection is in StreamRW (per-message deadlines); no
	// stream-level SetReadDeadline needed here. See handleStream for
	// the same note.

	rw := NewStreamRW(s)

	// Run protocol handshake
	phs, err := srv.doProtoHandshake(rw)
	if err != nil {
		s.Reset()
		return fmt.Errorf("proto handshake: %w", err)
	}

	remoteID := phs.ID
	selfID := PubkeyToNodeID(&srv.PrivateKey.PublicKey)
	if remoteID == selfID {
		s.Reset()
		return fmt.Errorf("connected to self")
	}

	// Verify the claimed NodeID matches the libp2p peer identity.
	// The Noise handshake cryptographically authenticates the remote key,
	// so we can derive the expected NodeID from the peer.ID and compare.
	expectedID, err := nodeIDFromPeerID(info.ID)
	if err != nil {
		s.Reset()
		return fmt.Errorf("cannot derive NodeID from peer %s: %w", info.ID, err)
	}
	if remoteID != expectedID {
		s.Reset()
		return fmt.Errorf("NodeID mismatch: claimed %x, expected %x", remoteID[:8], expectedID[:8])
	}

	// Check protocol match. We never adopted a stream to this peer, so
	// tearing the host connection down too is safe — without it, the
	// libp2p host would keep the yamux session alive for a peer we've
	// just declared incompatible.
	if len(srv.Protocols) > 0 && countMatchingProtocols(srv.Protocols, phs.Caps) == 0 {
		s.Reset()
		_ = srv.host.Network().ClosePeer(info.ID)
		return fmt.Errorf("no matching protocols")
	}

	p := newPeerFromStream(rw, s, remoteID, phs.Caps, phs.Name, srv.Protocols)

	if srv.testDialAdoptHook != nil {
		srv.testDialAdoptHook()
	}

	// Final atomic check-and-insert under lock to prevent races with concurrent
	// inbound connections or other dialPeer calls for the same peer.
	srv.peerMu.Lock()
	if srv.shuttingDown {
		srv.peerMu.Unlock()
		s.Reset()
		_ = srv.host.Network().ClosePeer(info.ID)
		return errServerStopped
	}
	if len(srv.peerMap) >= srv.MaxPeers {
		srv.peerMu.Unlock()
		s.Reset()
		// We never adopted a stream to this peer, so closing the host
		// connection is safe and keeps MaxPeers bounding actual libp2p
		// resources rather than just adopted streams.
		_ = srv.host.Network().ClosePeer(info.ID)
		return fmt.Errorf("max peers reached")
	}
	if _, exists := srv.peerMap[info.ID.String()]; exists {
		srv.peerMu.Unlock()
		s.Reset()
		// Do NOT ClosePeer here — the host-level connection is shared
		// with the already-adopted stream for this peer, and closing
		// it would terminate that legitimate connection.
		return fmt.Errorf("duplicate peer %s", info.ID)
	}
	srv.peerMap[info.ID.String()] = p
	// loopWG.Add happens inside the same peerMu critical section as the
	// shuttingDown check above so it can never race Stop()'s
	// loopWG.Wait() (see the shuttingDown field doc).
	srv.loopWG.Add(1)
	srv.peerMu.Unlock()

	// Outbound adoption: the dialed addresses are known-dialable, so
	// pass them along in case identify hasn't populated the peerstore
	// for this peer yet.
	srv.recordPeerConnected(info.ID, info.Addrs...)

	go func() {
		srv.runPeer(p)
		srv.loopWG.Done()
	}()

	return nil
}

// warmBootstrap dials recently-seen peers from the peer database on
// startup and reports how many dials it launched. See the call site in
// Start() for the why.
//
// Returns immediately if the database has no candidates (fresh
// install, or every record expired). Caps fan-out at MinConnectedPeers
// so a node with many recorded peers doesn't burst its FD limit at
// startup.
func (srv *Server) warmBootstrap() int {
	pdb := srv.peerdb.Load()
	if pdb == nil {
		return 0
	}
	target := srv.MinConnectedPeers
	if target <= 0 {
		target = 8 // sane floor when MinConnectedPeers is unset
	}
	// Fetch headroom beyond the dial target since some candidates are
	// skipped below (self, backoff-suppressed).
	candidates := pdb.seeds(target * 2)
	if len(candidates) == 0 {
		return 0
	}
	selfID := srv.host.ID()

	dialed := 0
	for _, info := range candidates {
		if dialed >= target {
			break
		}
		if info.ID == selfID {
			continue
		}
		// Skip peers we've recently failed to dial; the backoff
		// machinery will let them through later via the DHT discovery
		// loop once the backoff window elapses.
		if !srv.shouldDial(info.ID.String()) {
			continue
		}
		dialed++
		go func(info peer.AddrInfo) {
			if err := srv.dialPeer(info); err != nil {
				srv.recordDialFailure(info.ID.String())
				common.P2PLogger.Debug(fmt.Sprintf("warm-bootstrap dial to %s failed: %v", info.ID, err))
			} else {
				srv.recordDialSuccess(info.ID.String())
			}
		}(info)
	}
	if dialed > 0 {
		common.P2PLogger.Info("warm-bootstrap from peer database", "dialing", dialed, "known", len(candidates))
	}
	return dialed
}

// shouldDial reports whether the per-peer backoff schedule currently
// permits a redial of the given peer. Callers should consult this before
// invoking dialPeer from a periodic loop so a peer that just failed
// gets respected breathing room.
func (srv *Server) shouldDial(peerID string) bool {
	srv.backoffMu.Lock()
	defer srv.backoffMu.Unlock()
	b := srv.backoffs[peerID]
	return b == nil || !time.Now().Before(b.nextDial)
}

// recordDialFailure bumps the per-peer attempt counter and computes the
// next earliest redial time using exponential growth (capped at
// dialBackoffCap) plus ±dialBackoffJitter jitter.
func (srv *Server) recordDialFailure(peerID string) {
	// Mirror the failure into the peer database's consecutive-failure
	// counter (a no-op for peers that never completed a handshake).
	// Benign dial races ("already connected", "dial in progress")
	// currently count too; the counter resets on every successful
	// connect and the prune threshold is generous, so the pollution is
	// tolerable until dialPeer grows sentinel errors to distinguish
	// them.
	if pdb := srv.peerdb.Load(); pdb != nil {
		if pid, err := peer.Decode(peerID); err == nil {
			pdb.recordDialFail(pid)
		}
	}

	srv.backoffMu.Lock()
	defer srv.backoffMu.Unlock()
	b := srv.backoffs[peerID]
	if b == nil {
		b = &dialBackoff{}
		srv.backoffs[peerID] = b
	}
	b.attempts++

	// Exponential growth: dialBackoffBase * 2^(attempts-1), capped.
	// shift cap prevents overflow on absurdly large attempt counts.
	shift := uint(b.attempts - 1)
	if shift > 16 {
		shift = 16
	}
	base := time.Duration(int64(dialBackoffBase) << shift)
	if base > dialBackoffCap {
		base = dialBackoffCap
	}

	// Symmetric jitter in [-jitter*base, +jitter*base].
	jitterRange := int64(float64(base) * dialBackoffJitter * 2)
	if jitterRange < 1 {
		jitterRange = 1
	}
	jitter := time.Duration(rand.Int63n(jitterRange)) - time.Duration(jitterRange/2)
	b.nextDial = time.Now().Add(base + jitter)
}

// recordDialSuccess clears the per-peer backoff so subsequent
// disconnects start the schedule fresh rather than picking up where
// the last failure streak left off.
func (srv *Server) recordDialSuccess(peerID string) {
	srv.backoffMu.Lock()
	delete(srv.backoffs, peerID)
	srv.backoffMu.Unlock()
}

// peerMaintenanceLoop periodically checks peer count and dials bootstrap peers if needed.
func (srv *Server) peerMaintenanceLoop() {
	ticker := time.NewTicker(refreshPeersInterval)
	defer ticker.Stop()

	for {
		select {
		case <-srv.ctx.Done():
			return
		case <-ticker.C:
			if srv.NoDial {
				continue
			}
			srv.peerMu.RLock()
			numPeers := len(srv.peerMap)
			srv.peerMu.RUnlock()

			if numPeers < srv.MinConnectedPeers && len(srv.BootstrapPeers) > 0 {
				dialing, suppressed := 0, 0
				for _, pi := range srv.BootstrapPeers {
					pi := pi
					peerKey := pi.ID.String()
					srv.peerMu.RLock()
					_, connected := srv.peerMap[peerKey]
					srv.peerMu.RUnlock()
					if connected {
						continue
					}
					// Respect the exponential-backoff schedule so 100
					// nodes restarting after an outage don't all hit
					// the bootstrap at the same 30s cadence.
					if !srv.shouldDial(peerKey) {
						suppressed++
						continue
					}
					dialing++
					go func() {
						if err := srv.dialPeer(pi); err != nil {
							srv.recordDialFailure(peerKey)
							common.P2PLogger.Debug(fmt.Sprintf("dial %s failed: %v", pi.ID, err))
						} else {
							srv.recordDialSuccess(peerKey)
						}
					}()
				}
				// One Info line per tick while below the peer minimum,
				// so an operator can see redial progress (and backoff
				// suppression) without enabling debug logging. Silent
				// on healthy nodes — this branch isn't reached at all
				// once numPeers >= MinConnectedPeers.
				if dialing > 0 || suppressed > 0 {
					common.P2PLogger.Info("bootstrap redial sweep",
						"peers", numPeers, "min", srv.MinConnectedPeers,
						"dialing", dialing, "backoff-suppressed", suppressed)
				}
			}
		}
	}
}

// dhtDiscoveryLoop translates the DHT's routing table into Zenon
// application-protocol dials.
//
// The DHT (created in Start) populates its routing table passively as a
// side effect of bootstrap walks and the libp2p Identify exchange on
// each connection. On a small/private network that passive trickle can
// be too slow: a node below MinConnectedPeers has no way to reach peers
// its table hasn't heard about yet. So each tick below MinConnectedPeers
// also kicks off an active RefreshRoutingTable() walk (fire-and-forget,
// deduplicated by refreshingDHT) that issues real FIND_NODE queries
// against the DHT to pull in peers beyond whatever Identify happened to
// deliver — the result lands in the routing table for a later tick to
// dial. We can't use provider-record rendezvous discovery
// (routing.NewRoutingDiscovery) here since the DHT is started with
// DisableProviders/DisableValues; RefreshRoutingTable operates purely at
// the peer-routing layer, which stays enabled.
//
// Without any of this, the Zenon peer set would be effectively
// bootstrap-only after activation; if a node's configured bootstrap
// entries went down, it would never discover replacements.
//
// The loop is bounded:
//   - Only runs when len(peerMap) < MinConnectedPeers, so a healthy
//     node at capacity doesn't dial or query unnecessarily.
//   - Defers to dialPeer's own dedup (peerMap + dialing set) and
//     respects the per-peer backoff machinery via shouldDial, so a
//     peer that just failed isn't immediately retried.
//   - Caps work per tick at (MinConnectedPeers - peerCount) so we
//     don't fan out to the entire routing table on a fresh node.
func (srv *Server) dhtDiscoveryLoop() {
	ticker := time.NewTicker(dhtDiscoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-srv.ctx.Done():
			return
		case <-ticker.C:
			if srv.NoDial {
				continue
			}

			srv.peerMu.RLock()
			numPeers := len(srv.peerMap)
			srv.peerMu.RUnlock()
			if numPeers >= srv.MinConnectedPeers {
				continue
			}
			needed := srv.MinConnectedPeers - numPeers

			srv.triggerDHTRefresh()

			// Snapshot the routing table. ListPeers returns peer.IDs
			// the DHT has been able to talk to; we still need a full
			// libp2p Connect for each (handled inside dialPeer).
			candidates := srv.dht.RoutingTable().ListPeers()
			selfID := srv.host.ID()
			dialed := 0
			for _, pid := range candidates {
				if dialed >= needed {
					break
				}
				if pid == selfID {
					continue
				}
				peerKey := pid.String()

				srv.peerMu.RLock()
				_, connected := srv.peerMap[peerKey]
				_, dialing := srv.dialing[peerKey]
				srv.peerMu.RUnlock()
				if connected || dialing {
					continue
				}
				if !srv.shouldDial(peerKey) {
					continue
				}

				// Pull the multiaddrs the libp2p peerstore knows for
				// this peer — populated by Identify on first contact.
				addrs := srv.host.Peerstore().Addrs(pid)
				if len(addrs) == 0 {
					// No addresses known yet; the DHT will fill these
					// in soon via its own routing-table refresh.
					continue
				}
				info := peer.AddrInfo{ID: pid, Addrs: addrs}
				dialed++
				go func(info peer.AddrInfo) {
					if err := srv.dialPeer(info); err != nil {
						srv.recordDialFailure(info.ID.String())
						common.P2PLogger.Debug(fmt.Sprintf("dht-discovered dial to %s failed: %v", info.ID, err))
					} else {
						srv.recordDialSuccess(info.ID.String())
					}
				}(info)
			}
			// Mirror of the maintenance loop's summary: visible at the
			// default log level only while the node is hunting for
			// peers, so DHT-driven discovery progress is observable.
			if dialed > 0 {
				common.P2PLogger.Info("dht discovery: dialing candidates",
					"dialing", dialed, "table", len(candidates),
					"peers", numPeers, "min", srv.MinConnectedPeers)
			}
		}
	}
}

// triggerDHTRefresh kicks off an active Kademlia routing-table refresh in
// the background if one isn't already running. The walk can take longer
// than dhtDiscoveryInterval, and its only effect is a fuller routing
// table for a later dhtDiscoveryLoop tick to read, so the caller doesn't
// block on completion — but it's tracked by discoveryWG (the same group
// dhtDiscoveryLoop itself is tracked in) so Stop() can drain it before
// calling srv.dht.Close(), rather than merely before Stop() returns.
func (srv *Server) triggerDHTRefresh() {
	if !srv.refreshingDHT.CompareAndSwap(false, true) {
		return
	}
	srv.discoveryWG.Add(1)
	go func() {
		defer srv.discoveryWG.Done()
		defer srv.refreshingDHT.Store(false)
		if srv.testRefreshHook != nil {
			srv.testRefreshHook()
		}
		for err := range srv.dht.RefreshRoutingTable() {
			if err != nil {
				common.P2PLogger.Debug(fmt.Sprintf("dht routing table refresh: %v", err))
			}
		}
	}()
}

// peerCleanupLoop waits for peer disconnects and removes them from the map.
func (srv *Server) peerCleanupLoop() {
	for {
		select {
		case <-srv.ctx.Done():
			return
		case p := <-srv.delpeer:
			peerID := p.RemotePeerID()
			srv.peerMu.Lock()
			delete(srv.peerMap, peerID)
			srv.peerMu.Unlock()
			common.P2PLogger.Debug(fmt.Sprintf("Removed peer %s", peerID))
		}
	}
}

// runPeer runs the peer lifecycle.
func (srv *Server) runPeer(p *Peer) {
	common.P2PLogger.Debug(fmt.Sprintf("Added %v", p))
	reason := p.run()

	// Refresh the peer's database record on the way out: lastSeen is
	// what ranks warm-bootstrap candidates, and by now identify has had
	// the whole connection lifetime to learn the peer's listen
	// addresses.
	if pid := p.remotePeer(); pid != "" {
		srv.recordPeerConnected(pid)
	}

	// Notify the cleanup loop. If the context is already done (server is
	// stopping and peerCleanupLoop has exited), handle removal here directly
	// so this goroutine doesn't block and loopWG.Wait() can complete.
	peerID := p.RemotePeerID()
	select {
	case srv.delpeer <- p:
	case <-srv.ctx.Done():
		srv.peerMu.Lock()
		delete(srv.peerMap, peerID)
		srv.peerMu.Unlock()
	}
	common.P2PLogger.Debug(fmt.Sprintf("Removed %v (%v)", p, reason))
}

// parseListenAddr converts "host:port" to a multiaddr. host must be
// empty, "0.0.0.0", or a literal IP address — DNS hostnames (e.g.
// "localhost") are not resolved and return an error.
func parseListenAddr(addr string) (ma.Multiaddr, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if host == "" || host == "0.0.0.0" {
		return ma.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%s", port))
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP: %s", host)
	}
	if ip.To4() != nil {
		return ma.NewMultiaddr(fmt.Sprintf("/ip4/%s/tcp/%s", host, port))
	}
	return ma.NewMultiaddr(fmt.Sprintf("/ip6/%s/tcp/%s", host, port))
}

// nodeToMultiaddr converts a discover.Node to a multiaddr.
func nodeToMultiaddr(node *discover.Node) (ma.Multiaddr, error) {
	var ipStr string
	if node.IP.To4() != nil {
		ipStr = fmt.Sprintf("/ip4/%s/tcp/%d", node.IP.String(), node.TCP)
	} else {
		ipStr = fmt.Sprintf("/ip6/%s/tcp/%d", node.IP.String(), node.TCP)
	}
	maddr, err := ma.NewMultiaddr(ipStr)
	if err != nil {
		return nil, err
	}
	// We need to append the peer ID for libp2p.
	// Convert the ECDSA pubkey to a libp2p public key, then derive peer.ID.
	pub, err := node.ID.Pubkey()
	if err != nil || pub == nil {
		return nil, fmt.Errorf("invalid node ID: %v", err)
	}
	compressed := ethcrypto.CompressPubkey(pub) // 33-byte compressed
	lp2pPub, err := libp2pcrypto.UnmarshalSecp256k1PublicKey(compressed)
	if err != nil {
		return nil, fmt.Errorf("convert pubkey: %w", err)
	}
	pid, err := peer.IDFromPublicKey(lp2pPub)
	if err != nil {
		return nil, err
	}
	return ma.NewMultiaddr(fmt.Sprintf("%s/p2p/%s", maddr.String(), pid))
}

// parseMultiaddrIPPort extracts IP and port from a multiaddr.
func parseMultiaddrIPPort(maddr ma.Multiaddr) (net.IP, uint16, error) {
	ip, err := maddr.ValueForProtocol(ma.P_IP4)
	if err != nil {
		ip, err = maddr.ValueForProtocol(ma.P_IP6)
		if err != nil {
			return nil, 0, err
		}
	}
	portStr, err := maddr.ValueForProtocol(ma.P_TCP)
	if err != nil {
		return nil, 0, err
	}
	var port uint16
	fmt.Sscanf(portStr, "%d", &port)
	return net.ParseIP(ip), port, nil
}
