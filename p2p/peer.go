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

package p2p

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
	ma "github.com/multiformats/go-multiaddr"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/p2p/discover"
)

const (
	baseProtocolVersion    = 4
	baseProtocolLength     = uint64(16)
	baseProtocolMaxMsgSize = 2 * 1024

	pingInterval = 15 * time.Second
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
	Caps       []Cap
	ListenPort uint64
	ID         discover.NodeID
}

// peerConn holds the connection state for a peer.
type peerConn struct {
	rw     *StreamRW
	stream network.Stream
	id     discover.NodeID
	caps   []Cap
	name   string
}

func (c *peerConn) close(err error) {
	if c.stream != nil {
		c.stream.Close()
	}
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
	disc     chan DiscReason
}

// NewPeer returns a peer for testing purposes.
func NewPeer(id discover.NodeID, name string, caps []Cap) *Peer {
	conn := &peerConn{id: id, caps: caps, name: name}
	p := &Peer{
		rw:       conn,
		running:  make(map[string]*protoRW),
		disc:     make(chan DiscReason),
		protoErr: make(chan error, 1),
		closed:   make(chan struct{}),
	}
	close(p.closed)
	return p
}

// newPeerFromStream creates a peer from a libp2p stream.
func newPeerFromStream(rw *StreamRW, s network.Stream, id discover.NodeID, caps []Cap, name string, protocols []Protocol) *Peer {
	conn := &peerConn{
		rw:     rw,
		stream: s,
		id:     id,
		caps:   caps,
		name:   name,
	}
	protomap := matchProtocols(protocols, caps, rw)
	p := &Peer{
		rw:       conn,
		running:  protomap,
		disc:     make(chan DiscReason),
		protoErr: make(chan error, len(protomap)+1),
		closed:   make(chan struct{}),
	}
	return p
}

// ID returns the node's public key.
func (p *Peer) ID() discover.NodeID {
	return p.rw.id
}

// RemotePeerID returns the libp2p peer ID as a string.
func (p *Peer) RemotePeerID() string {
	if p.rw.stream != nil {
		return p.rw.stream.Conn().RemotePeer().String()
	}
	return ""
}

// Name returns the node name that the remote node advertised.
func (p *Peer) Name() string {
	return p.rw.name
}

// Caps returns the capabilities (supported subprotocols) of the remote peer.
func (p *Peer) Caps() []Cap {
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
func (p *Peer) Disconnect(reason DiscReason) {
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

func (p *Peer) run() DiscReason {
	var (
		writeStart = make(chan struct{}, 1)
		writeErr   = make(chan error, 1)
		readErr    = make(chan error, 1)
		reason     DiscReason
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
				reason = DiscNetworkError
				break loop
			}
			writeStart <- struct{}{}
		case err := <-readErr:
			if r, ok := err.(DiscReason); ok {
				common.P2PLogger.Debug(fmt.Sprintf("%v: remote requested disconnect: %v\n", p, r))
				requested = true
				reason = r
			} else {
				common.P2PLogger.Debug(fmt.Sprintf("%v: read error: %v\n", p, err))
				reason = DiscNetworkError
			}
			break loop
		case err := <-p.protoErr:
			reason = discReasonForError(err)
			common.P2PLogger.Debug(fmt.Sprintf("%v: protocol error: %v (%v)\n", p, err, reason))
			break loop
		case reason = <-p.disc:
			common.P2PLogger.Debug(fmt.Sprintf("%v: locally requested disconnect: %v\n", p, reason))
			break loop
		}
	}

	p.rw.close(reason)
	close(p.closed)
	common.P2PLogger.Debug("peer.run() finished")
	if requested {
		reason = DiscRequested
	}
	return reason
}

func (p *Peer) pingLoop() {
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	for {
		select {
		case <-ping.C:
			if err := SendItems(p.rw.rw, pingMsg); err != nil {
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

func (p *Peer) handle(msg Msg) error {
	switch {
	case msg.Code == pingMsg:
		msg.Discard()
		p.wg.Add(1)
		go func() {
			SendItems(p.rw.rw, pongMsg)
			p.wg.Done()
		}()
	case msg.Code == discMsg:
		var reason [1]DiscReason
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

func countMatchingProtocols(protocols []Protocol, caps []Cap) int {
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

func matchProtocols(protocols []Protocol, caps []Cap, rw MsgReadWriter) map[string]*protoRW {
	sort.Sort(capsByNameAndVersion(caps))
	offset := baseProtocolLength
	result := make(map[string]*protoRW)

outer:
	for _, cap := range caps {
		for _, proto := range protocols {
			if proto.Name == cap.Name && proto.Version == cap.Version {
				if old := result[cap.Name]; old != nil {
					offset -= old.Length
				}
				result[cap.Name] = &protoRW{Protocol: proto, offset: offset, in: make(chan Msg), w: rw}
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
				common.P2PLogger.Debug(fmt.Sprintf("%v: Protocol %s/%d returned\n", p, proto.Name, proto.Version))
				err = errors.New("protocol returned")
			} else if err != io.EOF {
				common.P2PLogger.Debug(fmt.Sprintf("%v: Protocol %s/%d error: %v\n", p, proto.Name, proto.Version, err))
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
	return nil, newPeerError(errInvalidMsgCode, "%d", code)
}

type protoRW struct {
	Protocol
	in     chan Msg
	closed <-chan struct{}
	wstart <-chan struct{}
	werr   chan<- error
	offset uint64
	w      MsgWriter
}

func (rw *protoRW) WriteMsg(msg Msg) (err error) {
	if msg.Code >= rw.Length {
		return newPeerError(errInvalidMsgCode, "not handled")
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

func (rw *protoRW) ReadMsg() (Msg, error) {
	select {
	case msg := <-rw.in:
		msg.Code -= rw.offset
		return msg, nil
	case <-rw.closed:
		return Msg{}, io.EOF
	}
}
