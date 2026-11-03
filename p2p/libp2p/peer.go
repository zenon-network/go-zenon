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

package libp2p

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/p2p/discover"
)

const (
	baseProtocolVersion    = 4
	baseProtocolLength     = uint64(16)
	baseProtocolMaxMsgSize = 2 * 1024

	pingInterval     = 15 * time.Second
	discWriteTimeout = 1 * time.Second
)

const (
	// devp2p message codes
	handshakeMsg = 0x00
	discMsg      = 0x01
	pingMsg      = 0x02
	pongMsg      = 0x03
	getPeersMsg  = 0x04
	peersMsg     = 0x05
)

// protoHandshake is the RLP structure of the protocol handshake.
type protoHandshake struct {
	Version    uint64
	Name       string
	Caps       []p2p.Cap
	ListenPort uint64
	ID         discover.NodeID
}

// peerConn holds the connection state for a peer.
type peerConn struct {
	rw     *StreamRW
	stream network.Stream
	id     discover.NodeID
	caps   []p2p.Cap
	name   string
}

func (c *peerConn) close(err error) {
	if c.stream == nil {
		return
	}
	// Clean disconnect path: we know the reason and the stream is still
	// healthy enough to send a disc frame. Send the reason, then Close()
	// the stream gracefully (half-close — the remote can still drain
	// anything we already sent).
	if r, ok := err.(p2p.DiscReason); ok && r != p2p.DiscNetworkError {
		// The limit also cuts short a write still stalled on the stream,
		// which holds the write lock the disc frame needs, so both
		// writes are bounded by discWriteTimeout rather than
		// frameWriteTimeout each. The Close below is not: yamux queues
		// its FIN on the session without a deadline.
		c.rw.limitWriteDeadline(time.Now().Add(discWriteTimeout))
		p2p.Send(c.rw, discMsg, []p2p.DiscReason{r})
		c.stream.Close()
		return
	}
	// Unclean disconnect path (network error, read timeout, protocol
	// error, etc): Reset() rather than Close(). Yamux buffers Close()
	// writes for up to frameWriteTimeout (20s) on a half-dead stream,
	// during which the peer entry would stay in peerMap and count toward
	// MaxPeers — effectively leaking the peer slot until the timeout
	// fires. Reset() flushes those buffers immediately and signals the
	// remote with a RST so both sides see the disconnect promptly.
	c.stream.Reset()
}

// RemoteAddr returns the remote address of the network connection.
func (c *peerConn) RemoteAddr() net.Addr {
	if c.stream != nil {
		conn := c.stream.Conn()
		if conn != nil {
			return &multiaddrNetAddr{conn.RemoteMultiaddr()}
		}
	}
	return &net.TCPAddr{}
}

// LocalAddr returns the local address of the network connection.
func (c *peerConn) LocalAddr() net.Addr {
	if c.stream != nil {
		conn := c.stream.Conn()
		if conn != nil {
			return &multiaddrNetAddr{conn.LocalMultiaddr()}
		}
	}
	return &net.TCPAddr{}
}

// multiaddrNetAddr wraps a multiaddr as a net.Addr for compatibility.
type multiaddrNetAddr struct {
	addr ma.Multiaddr
}

func (a *multiaddrNetAddr) Network() string { return "libp2p" }
func (a *multiaddrNetAddr) String() string  { return a.addr.String() }

// Peer represents a connected remote node.
type Peer struct {
	rw      *peerConn
	running map[string]*protoRW

	wg       sync.WaitGroup
	protoErr chan error
	closed   chan struct{}
	disc     chan p2p.DiscReason

	// pongPending holds at most one pong reply that has not started yet.
	// handle puts a token in it for a ping and pingLoop writes the pong, so
	// ping and pong writes for a peer come from one goroutine: a write that
	// cannot complete holds up further replies instead of piling up
	// writers, and pings that arrive while a pong is pending are answered
	// by that pong.
	pongPending chan struct{}

	// pingC delivers the periodic ping ticks to pingLoop. It is nil for
	// production peers, which use a ticker; tests supply their own.
	pingC <-chan time.Time
}

// NewPeer returns a peer for testing purposes.
func NewPeer(id discover.NodeID, name string, caps []p2p.Cap) *Peer {
	conn := &peerConn{id: id, caps: caps, name: name}
	p := &Peer{
		rw:          conn,
		running:     make(map[string]*protoRW),
		disc:        make(chan p2p.DiscReason),
		protoErr:    make(chan error, 1),
		closed:      make(chan struct{}),
		pongPending: make(chan struct{}, 1),
	}
	close(p.closed)
	return p
}

// newPeerFromStream creates a peer from a libp2p stream.
func newPeerFromStream(rw *StreamRW, s network.Stream, id discover.NodeID, caps []p2p.Cap, name string, protocols []p2p.Protocol) *Peer {
	conn := &peerConn{
		rw:     rw,
		stream: s,
		id:     id,
		caps:   caps,
		name:   name,
	}
	protomap := matchProtocols(protocols, caps, rw)
	p := &Peer{
		rw:          conn,
		running:     protomap,
		disc:        make(chan p2p.DiscReason),
		protoErr:    make(chan error, len(protomap)+1),
		closed:      make(chan struct{}),
		pongPending: make(chan struct{}, 1),
	}
	return p
}

// ID returns the node's public key.
func (p *Peer) ID() discover.NodeID {
	return p.rw.id
}

// RemotePeerID returns the libp2p peer ID as a string.
func (p *Peer) RemotePeerID() string {
	return p.remotePeer().String()
}

// remotePeer returns the libp2p peer ID, or "" for test peers with no
// underlying stream.
func (p *Peer) remotePeer() peer.ID {
	if p.rw.stream != nil {
		return p.rw.stream.Conn().RemotePeer()
	}
	return ""
}

// Name returns the node name that the remote node advertised.
func (p *Peer) Name() string {
	return p.rw.name
}

// Caps returns the capabilities (supported subprotocols) of the remote peer.
func (p *Peer) Caps() []p2p.Cap {
	return p.rw.caps
}

// RemoteAddr returns the remote address of the network connection.
func (p *Peer) RemoteAddr() net.Addr {
	return p.rw.RemoteAddr()
}

// LocalAddr returns the local address of the network connection.
func (p *Peer) LocalAddr() net.Addr {
	return p.rw.LocalAddr()
}

// Disconnect terminates the peer connection with the given reason.
func (p *Peer) Disconnect(reason p2p.DiscReason) {
	select {
	case p.disc <- reason:
	case <-p.closed:
		return
	}
}

// String implements fmt.Stringer.
func (p *Peer) String() string {
	return fmt.Sprintf("Peer %x %v", p.rw.id[:8], p.RemoteAddr())
}

func (p *Peer) run() p2p.DiscReason {
	var (
		writeStart = make(chan struct{}, 1)
		writeErr   = make(chan error, 1)
		readErr    = make(chan error, 1)
		reason     p2p.DiscReason
		requested  bool
	)

	p.wg.Add(1)
	go func() {
		p.readLoop(readErr)
		p.wg.Done()
	}()

	p.wg.Add(1)
	go func() {
		p.pingLoop()
		p.wg.Done()
	}()

	writeStart <- struct{}{}
	p.startProtocols(writeStart, writeErr)

loop:
	for {
		select {
		case err := <-writeErr:
			if err != nil {
				common.P2PLogger.Debug(fmt.Sprintf("%v: write error: %v\n", p, err))
				reason = p2p.DiscNetworkError
				break loop
			}
			writeStart <- struct{}{}
		case err := <-readErr:
			if r, ok := err.(p2p.DiscReason); ok {
				common.P2PLogger.Debug(fmt.Sprintf("%v: remote requested disconnect: %v\n", p, r))
				requested = true
				reason = r
			} else {
				common.P2PLogger.Debug(fmt.Sprintf("%v: read error: %v\n", p, err))
				reason = p2p.DiscNetworkError
			}
			break loop
		case err := <-p.protoErr:
			reason = p2p.DiscReasonForError(err)
			common.P2PLogger.Debug(fmt.Sprintf("%v: protocol error: %v (%v)\n", p, err, reason))
			break loop
		case reason = <-p.disc:
			common.P2PLogger.Debug(fmt.Sprintf("%v: locally requested disconnect: %v\n", p, reason))
			break loop
		}
	}

	p.rw.close(reason)
	close(p.closed)
	// Join the read loop, the ping loop and every protocol goroutine
	// before returning, as the legacy backend does: runPeer frees the
	// peer's slot as soon as run returns, and a protocol handler still
	// inside Run would otherwise keep its registration past the removal,
	// which a reconnect with the same identity then trips over. The read
	// and ping loops and the protocol reads and writes unblock on the
	// closed stream or p.closed, and the final sends on protoErr, readErr
	// and writeErr have buffer space, so the join is bounded by the
	// handlers' own exit: a handler busy with local work (a chain
	// insert, say) holds the slot, and Stop, until that work returns, as
	// on the legacy backend.
	p.wg.Wait()
	common.P2PLogger.Debug("peer.run() finished")
	if requested {
		reason = p2p.DiscRequested
	}
	return reason
}

// pingLoop sends our periodic pings and the pong replies to the remote
// peer's pings, so ping and pong writes for a peer come from one goroutine
// and a write that cannot complete holds up further replies instead of
// piling up writers.
func (p *Peer) pingLoop() {
	tick := p.pingC
	if tick == nil {
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		tick = ticker.C
	}
	for {
		select {
		case <-tick:
			if err := p2p.SendItems(p.rw.rw, pingMsg); err != nil {
				p.protoErr <- err
				return
			}
		case <-p.pongPending:
			if err := p2p.SendItems(p.rw.rw, pongMsg); err != nil {
				p.protoErr <- err
				return
			}
		case <-p.closed:
			return
		}
	}
}

func (p *Peer) readLoop(errc chan<- error) {
	for {
		msg, err := p.rw.rw.ReadMsg()
		if err != nil {
			errc <- err
			return
		}
		msg.ReceivedAt = time.Now()
		if err = p.handle(msg); err != nil {
			errc <- err
			return
		}
	}
}

func (p *Peer) handle(msg p2p.Msg) error {
	switch {
	case msg.Code == pingMsg:
		msg.Discard()
		// Hand the reply to the keepalive loop. A pong that is already
		// pending answers this ping as well.
		select {
		case p.pongPending <- struct{}{}:
		default:
		}
	case msg.Code == discMsg:
		var reason [1]p2p.DiscReason
		rlp.Decode(msg.Payload, &reason)
		return reason[0]
	case msg.Code < baseProtocolLength:
		return msg.Discard()
	default:
		proto, err := p.getProto(msg.Code)
		if err != nil {
			return fmt.Errorf("msg code out of range: %v", msg.Code)
		}
		select {
		case proto.in <- msg:
			return nil
		case <-p.closed:
			return io.EOF
		}
	}
	return nil
}

func countMatchingProtocols(protocols []p2p.Protocol, caps []p2p.Cap) int {
	n := 0
	for _, cap := range caps {
		for _, proto := range protocols {
			if proto.Name == cap.Name && proto.Version == cap.Version {
				n++
			}
		}
	}
	return n
}

func matchProtocols(protocols []p2p.Protocol, caps []p2p.Cap, rw p2p.MsgReadWriter) map[string]*protoRW {
	sort.Sort(p2p.CapsByNameAndVersion(caps))
	offset := baseProtocolLength
	result := make(map[string]*protoRW)

outer:
	for _, cap := range caps {
		for _, proto := range protocols {
			if proto.Name == cap.Name && proto.Version == cap.Version {
				if old := result[cap.Name]; old != nil {
					offset -= old.Length
				}
				result[cap.Name] = &protoRW{Protocol: proto, offset: offset, in: make(chan p2p.Msg), w: rw}
				offset += proto.Length
				continue outer
			}
		}
	}
	return result
}

func (p *Peer) startProtocols(writeStart <-chan struct{}, writeErr chan<- error) {
	p.wg.Add(len(p.running))
	for _, proto := range p.running {
		proto := proto
		proto.closed = p.closed
		proto.wstart = writeStart
		proto.werr = writeErr
		common.P2PLogger.Debug(fmt.Sprintf("%v: Starting protocol %s/%d\n", p, proto.Name, proto.Version))
		go func() {
			err := proto.Run(p, proto)
			p.wg.Done()
			if err == nil {
				common.P2PLogger.Debug(fmt.Sprintf("%v: p2p.Protocol %s/%d returned\n", p, proto.Name, proto.Version))
				err = errors.New("protocol returned")
			} else if err != io.EOF {
				common.P2PLogger.Debug(fmt.Sprintf("%v: p2p.Protocol %s/%d error: %v\n", p, proto.Name, proto.Version, err))
			}
			p.protoErr <- err
		}()
	}
}

func (p *Peer) getProto(code uint64) (*protoRW, error) {
	for _, proto := range p.running {
		if code >= proto.offset && code < proto.offset+proto.Length {
			return proto, nil
		}
	}
	return nil, p2p.NewPeerError(p2p.ErrInvalidMsgCode, "%d", code)
}

type protoRW struct {
	p2p.Protocol
	in     chan p2p.Msg
	closed <-chan struct{}
	wstart <-chan struct{}
	werr   chan<- error
	offset uint64
	w      p2p.MsgWriter
}

func (rw *protoRW) WriteMsg(msg p2p.Msg) (err error) {
	if msg.Code >= rw.Length {
		return p2p.NewPeerError(p2p.ErrInvalidMsgCode, "not handled")
	}
	msg.Code += rw.offset
	select {
	case <-rw.wstart:
		err = rw.w.WriteMsg(msg)
		rw.werr <- err
	case <-rw.closed:
		err = fmt.Errorf("shutting down")
	}
	return err
}

// Closed returns a channel that is closed when the peer shuts down.
// Protocol handlers hand it to waits that are not bound to the
// connection, such as downloader deliveries, so that those waits end
// with the peer rather than with whatever they were waiting on.
func (rw *protoRW) Closed() <-chan struct{} { return rw.closed }

func (rw *protoRW) ReadMsg() (p2p.Msg, error) {
	select {
	case msg := <-rw.in:
		msg.Code -= rw.offset
		return msg, nil
	case <-rw.closed:
		return p2p.Msg{}, io.EOF
	}
}
