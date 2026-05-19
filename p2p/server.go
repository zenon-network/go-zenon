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
package p2p

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
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
	Protocols []Protocol

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
	peerMu  sync.RWMutex

	ourHandshake  *protoHandshake
	delpeer       chan *Peer
	loopWG        sync.WaitGroup
	pendingCount  int32 // atomic; tracks peers in handshake phase
}

// Peers returns all connected peers.
func (srv *Server) Peers() []*Peer {
	srv.peerMu.RLock()
	defer srv.peerMu.RUnlock()

	ps := make([]*Peer, 0, len(srv.peerMap))
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
func (srv *Server) AddPeer(node *discover.Node) {
	if srv.host == nil {
		return
	}
	// Convert discover.Node to multiaddr
	maddr, err := nodeToMultiaddr(node)
	if err != nil {
		common.P2PLogger.Debug(fmt.Sprintf("AddPeer: invalid node: %v", err))
		return
	}
	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		common.P2PLogger.Debug(fmt.Sprintf("AddPeer: %v", err))
		return
	}
	go func() {
		if err := srv.host.Connect(srv.ctx, *info); err != nil {
			common.P2PLogger.Debug(fmt.Sprintf("AddPeer connect failed: %v", err))
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
	srv.running = true
	common.P2PLogger.Info("Starting Server (libp2p)")

	if srv.PrivateKey == nil {
		return fmt.Errorf("Server.PrivateKey must be set to a non-nil key")
	}

	srv.ctx, srv.cancel = context.WithCancel(context.Background())
	srv.peerMap = make(map[string]*Peer)
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
		srv.ourHandshake.Caps = append(srv.ourHandshake.Caps, p.cap())
	}

	// Register stream handler
	srv.host.SetStreamHandler(protocol.ID(protoID), srv.handleStream)

	// Start discovery DHT if configured
	if srv.Discovery && len(srv.BootstrapPeers) > 0 {
		srv.dht, err = dht.New(srv.ctx, srv.host, dht.Mode(dht.ModeAutoServer))
		if err != nil {
			return fmt.Errorf("create DHT: %w", err)
		}
	}

	// Dial bootstrap peers immediately so the node has peers within seconds of
	// startup rather than waiting for the first peerMaintenanceLoop tick (30s).
	if len(srv.BootstrapPeers) > 0 {
		for _, pi := range srv.BootstrapPeers {
			pi := pi
			go func() {
				if err := srv.dialPeer(pi); err != nil {
					common.P2PLogger.Debug(fmt.Sprintf("bootstrap dial to %s failed: %v", pi.ID, err))
				}
			}()
		}
		// Bootstrap the DHT
		if srv.dht != nil {
			if err := srv.dht.Bootstrap(srv.ctx); err != nil {
				common.P2PLogger.Debug(fmt.Sprintf("DHT bootstrap failed: %v", err))
			}
		}
	}

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

	// Apply read deadline to prevent slowloris
	s.SetReadDeadline(time.Now().Add(frameReadTimeout))
	defer s.SetReadDeadline(time.Time{}) // clear deadline after handshake

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
	remoteID := phs.ID
	expectedID, err := nodeIDFromPeerID(remotePeer)
	if err != nil {
		common.P2PLogger.Debug(fmt.Sprintf("cannot derive NodeID from peer %s: %v", remotePeer, err))
		s.Reset()
		return
	}
	if remoteID != expectedID {
		common.P2PLogger.Debug(fmt.Sprintf("NodeID mismatch: claimed %x, expected %x", remoteID[:8], expectedID[:8]))
		s.Reset()
		return
	}

	// Check we're not connecting to ourselves
	selfID := PubkeyToNodeID(&srv.PrivateKey.PublicKey)
	if remoteID == selfID {
		common.P2PLogger.Debug("rejecting connection from self")
		s.Reset()
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
	// Send our handshake
	errc := make(chan error, 1)
	go func() {
		errc <- Send(rw, handshakeMsg, srv.ourHandshake)
	}()

	// Read remote handshake
	msg, err := rw.ReadMsg()
	if err != nil {
		return nil, fmt.Errorf("read handshake: %w", err)
	}
	if msg.Code != handshakeMsg {
		return nil, fmt.Errorf("expected handshake msg, got code %d", msg.Code)
	}

	var phs protoHandshake
	if err := msg.Decode(&phs); err != nil {
		return nil, fmt.Errorf("decode handshake: %w", err)
	}

	// Wait for our send to complete
	if err := <-errc; err != nil {
		return nil, fmt.Errorf("send handshake: %w", err)
	}

	return &phs, nil
}

// dialPeer connects to a remote peer and performs the handshake.
func (srv *Server) dialPeer(info peer.AddrInfo) error {
	// Pre-check before dialing to avoid unnecessary connections.
	srv.peerMu.RLock()
	numPeers := len(srv.peerMap)
	_, alreadyConnected := srv.peerMap[info.ID.String()]
	srv.peerMu.RUnlock()
	if numPeers >= srv.MaxPeers {
		return fmt.Errorf("max peers reached")
	}
	if alreadyConnected {
		return fmt.Errorf("already connected to %s", info.ID)
	}

	if err := srv.host.Connect(srv.ctx, info); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	// Open a new stream
	s, err := srv.host.NewStream(srv.ctx, info.ID, protocol.ID(protoID))
	if err != nil {
		return fmt.Errorf("open stream: %w", err)
	}

	// Apply read deadline to prevent a stalled remote from blocking indefinitely.
	s.SetReadDeadline(time.Now().Add(frameReadTimeout))
	defer s.SetReadDeadline(time.Time{})

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
					srv.peerMu.RLock()
					_, connected := srv.peerMap[pi.ID.String()]
					srv.peerMu.RUnlock()
					if !connected {
						go func() {
							if err := srv.dialPeer(pi); err != nil {
								common.P2PLogger.Debug(fmt.Sprintf("dial %s failed: %v", pi.ID, err))
							}
						}()
					}
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
