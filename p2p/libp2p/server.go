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

	libp2ptcp "github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
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
	dialBackoffBase  = 5 * time.Second
	dialBackoffCap   = 5 * time.Minute
	dialBackoffJitter = 0.3
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

	// MaxPendingPeers is the maximum number of peers that can be pending.
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

	// If NoDial is true, the server will not dial any peers.
	NoDial bool

	// Internal state
	lock    sync.Mutex
	running bool

	host    host.Host
	dht     *dht.IpfsDHT
	ctx     context.Context
	cancel  context.CancelFunc
	peerMap map[string]*Peer
	// dialing tracks outbound dials currently in flight, keyed by the
	// remote libp2p peer.ID string. Used to collapse concurrent dial
	// attempts for the same peer down to one and avoid wasting an
	// FD + Noise handshake on the losing goroutine. Protected by peerMu.
	dialing map[string]struct{}
	peerMu  sync.RWMutex

	// backoff state for the peerMaintenanceLoop redial path. Protected
	// by backoffMu (separate from peerMu so dial/disconnect hot paths
	// don't contend with backoff bookkeeping).
	backoffs  map[string]*dialBackoff
	backoffMu sync.Mutex

	ourHandshake *protoHandshake
	delpeer      chan *Peer
	loopWG       sync.WaitGroup
	pendingCount int32 // atomic; tracks peers in handshake phase
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

	if srv.dht != nil {
		srv.dht.Close()
	}
	srv.cancel()
	if srv.host != nil {
		srv.host.Close()
	}
	srv.lock.Unlock()

	// Wait outside the lock so goroutines that need to acquire locks can finish.
	srv.loopWG.Wait()
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

	// Build libp2p options
	opts := []libp2p.Option{
		libp2p.Identity(privKey),
		libp2p.ListenAddrs(listenMaddr),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Muxer("/yamux/1.0.0", yamux.DefaultTransport),
		libp2p.Transport(libp2ptcp.NewTCPTransport),
		libp2p.NATPortMap(),
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
			common.P2PLogger.Debug(fmt.Sprintf("DHT bootstrap failed: %v", err))
		}
	}

	// Dial bootstrap peers immediately so the node has peers within seconds of
	// startup rather than waiting for the first peerMaintenanceLoop tick.
	if len(srv.BootstrapPeers) > 0 {
		for _, pi := range srv.BootstrapPeers {
			pi := pi
			go func() {
				if err := srv.dialPeer(pi); err != nil {
					common.P2PLogger.Debug(fmt.Sprintf("bootstrap dial to %s failed: %v", pi.ID, err))
				}
			}()
		}
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

	common.P2PLogger.Info(fmt.Sprintf("Listening on %s", srv.host.Addrs()))
	return nil
}

// handleStream is called when a remote peer opens a stream to us.
func (srv *Server) handleStream(s network.Stream) {
	// Enforce MaxPendingPeers
	var counted bool
	if srv.MaxPendingPeers > 0 {
		pending := atomic.AddInt32(&srv.pendingCount, 1)
		counted = true
		if int(pending) > srv.MaxPendingPeers {
			atomic.AddInt32(&srv.pendingCount, -1)
			s.Reset()
			return
		}
	}
	defer func() {
		if counted {
			atomic.AddInt32(&srv.pendingCount, -1)
		}
	}()

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
	if len(srv.Protocols) > 0 && countMatchingProtocols(srv.Protocols, phs.Caps) == 0 {
		common.P2PLogger.Debug(fmt.Sprintf("no matching protocols with %s", remotePeer))
		s.Reset()
		return
	}

	// Atomically check max peers, duplicate, and insert to eliminate the
	// TOCTOU race that two concurrent handleStream calls would otherwise create.
	p := newPeerFromStream(rw, s, remoteID, phs.Caps, phs.Name, srv.Protocols)
	srv.peerMu.Lock()
	if len(srv.peerMap) >= srv.MaxPeers {
		srv.peerMu.Unlock()
		common.P2PLogger.Debug("max peers reached, rejecting connection")
		s.Reset()
		return
	}
	if _, exists := srv.peerMap[remotePeer.String()]; exists {
		srv.peerMu.Unlock()
		common.P2PLogger.Debug(fmt.Sprintf("duplicate peer %s, rejecting", remotePeer))
		s.Reset()
		return
	}
	srv.peerMap[remotePeer.String()] = p
	srv.peerMu.Unlock()

	srv.loopWG.Add(1)
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

	// Read remote handshake
	msg, err := rw.ReadMsg()
	if err != nil {
		return nil, fmt.Errorf("read handshake: %w", err)
	}
	if msg.Size > baseProtocolMaxMsgSize {
		return nil, fmt.Errorf("handshake message too big: %d bytes (max %d)", msg.Size, baseProtocolMaxMsgSize)
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

	// Check protocol match
	if len(srv.Protocols) > 0 && countMatchingProtocols(srv.Protocols, phs.Caps) == 0 {
		s.Reset()
		return fmt.Errorf("no matching protocols")
	}

	p := newPeerFromStream(rw, s, remoteID, phs.Caps, phs.Name, srv.Protocols)

	// Final atomic check-and-insert under lock to prevent races with concurrent
	// inbound connections or other dialPeer calls for the same peer.
	srv.peerMu.Lock()
	if len(srv.peerMap) >= srv.MaxPeers {
		srv.peerMu.Unlock()
		s.Reset()
		return fmt.Errorf("max peers reached")
	}
	if _, exists := srv.peerMap[info.ID.String()]; exists {
		srv.peerMu.Unlock()
		s.Reset()
		return fmt.Errorf("duplicate peer %s", info.ID)
	}
	srv.peerMap[info.ID.String()] = p
	srv.peerMu.Unlock()

	srv.loopWG.Add(1)
	go func() {
		srv.runPeer(p)
		srv.loopWG.Done()
	}()

	return nil
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
						continue
					}
					go func() {
						if err := srv.dialPeer(pi); err != nil {
							srv.recordDialFailure(peerKey)
							common.P2PLogger.Debug(fmt.Sprintf("dial %s failed: %v", pi.ID, err))
						} else {
							srv.recordDialSuccess(peerKey)
						}
					}()
				}
			}
		}
	}
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

// parseListenAddr converts "host:port" to a multiaddr.
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
