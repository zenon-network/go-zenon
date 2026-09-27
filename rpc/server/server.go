// Copyright 2015 The go-ethereum Authors
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

package server

import (
	"context"
	"io"
	"sync"
	"sync/atomic"

	mapset "github.com/deckarep/golang-set"
	"github.com/ethereum/go-ethereum/log"
)

const MetadataApi = "rpc"

// CodecOption specifies which type of messages a codec supports.
//
// Deprecated: this option is no longer honored by Server.
type CodecOption int

const (
	// OptionMethodInvocation is an indication that the codec supports RPC method calls
	OptionMethodInvocation CodecOption = 1 << iota

	// OptionSubscriptions is an indication that the codec supports RPC notifications
	OptionSubscriptions = 1 << iota // support pub sub
)

// Server is an RPC server.
type Server struct {
	services serviceRegistry
	idgen    func() ID
	run      int32
	codecs   mapset.Set

	// mu orders codec registration against Stop. A codec that passes the
	// running check is registered before Stop can take its view of the set,
	// so Stop closes every codec that was admitted while the server was
	// running. Stop releases mu before it closes anything: a codec's close
	// runs caller-supplied code (a Conn passed to NewCodec) that may itself
	// call Stop.
	mu sync.Mutex
	// beforeRegister, when set, runs after the running check and before the
	// codec is registered. Tests use it to hold admission at that point.
	beforeRegister func()
}

// NewServer creates a new server instance with no registered handlers.
func NewServer() *Server {
	server := &Server{idgen: randomIDGenerator(), codecs: mapset.NewSet(), run: 1}
	// Register the default service providing meta information about the RPC service such
	// as the services and methods it offers.
	rpcService := &RPCService{server}
	server.RegisterName(MetadataApi, rpcService)
	return server
}

// RegisterName creates a service for the given receiver type under the given name. When no
// methods on the given receiver match the criteria to be either a RPC method or a
// subscription an error is returned. Otherwise a new service is created and added to the
// service collection this server provides to clients.
func (s *Server) RegisterName(name string, receiver interface{}) error {
	return s.services.registerName(name, receiver)
}

// ServeCodec reads incoming requests from codec, calls the appropriate callback and writes
// the response back using the given codec. It will block until the codec is closed or the
// server is stopped. In either case the codec is closed.
//
// Note that codec options are no longer supported.
func (s *Server) ServeCodec(codec ServerCodec, options CodecOption) {
	defer codec.close()

	if !s.trackCodec(codec) {
		return
	}
	defer s.untrackCodec(codec)

	c := initClient(codec, s.idgen, &s.services)
	<-codec.closed()
	c.Close()
}

// trackCodec registers codec so that Stop can close it. It reports false,
// leaving codec unregistered, when the server has stopped. The check and the
// registration happen under s.mu, which Stop also holds while it takes its
// view of the set, so a codec cannot slip in between that view and its own
// registration.
func (s *Server) trackCodec(codec ServerCodec) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Don't serve if server is stopped.
	if atomic.LoadInt32(&s.run) == 0 {
		return false
	}
	if s.beforeRegister != nil {
		s.beforeRegister()
	}
	s.codecs.Add(codec)
	return true
}

func (s *Server) untrackCodec(codec ServerCodec) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.codecs.Remove(codec)
}

// serveSingleRequest reads and processes a single RPC request from the given codec. This
// is used to serve HTTP connections. Subscriptions and reverse calls are not allowed in
// this mode.
func (s *Server) serveSingleRequest(ctx context.Context, codec ServerCodec) {
	// Don't serve if server is stopped.
	if atomic.LoadInt32(&s.run) == 0 {
		return
	}

	h := newHandler(ctx, codec, s.idgen, &s.services)
	h.allowSubscribe = false
	defer h.close(io.EOF, nil)

	reqs, batch, err := codec.readBatch()
	if err != nil {
		if err != io.EOF {
			codec.writeJSON(ctx, errorMessage(&invalidMessageError{"parse error"}))
		}
		return
	}
	if batch {
		h.handleBatch(reqs)
	} else {
		h.handleMsg(reqs[0])
	}
}

// Stop stops reading new requests, waits for stopPendingRequestTimeout to allow pending
// requests to finish, then closes all codecs which will cancel pending requests and
// subscriptions.
func (s *Server) Stop() {
	s.mu.Lock()
	if !atomic.CompareAndSwapInt32(&s.run, 1, 0) {
		s.mu.Unlock()
		return
	}
	codecs := s.codecs.ToSlice()
	s.mu.Unlock()

	// Every codec still registered at this point is in codecs: later arrivals
	// see the flag cleared under s.mu and are refused, and one that removed
	// itself has already been closed. Logging and closing happen outside the
	// lock because both run caller-supplied code (a log handler, a codec's
	// Conn) that may call back into Stop, which now returns at the flag.
	log.Debug("RPC server shutting down")
	for _, c := range codecs {
		c.(ServerCodec).close()
	}
}

// RPCService gives meta information about the server.
// e.g. gives information about the loaded modules.
type RPCService struct {
	server *Server
}

// Modules returns the list of RPC services with their version number
func (s *RPCService) Modules() map[string]string {
	s.server.services.mu.Lock()
	defer s.server.services.mu.Unlock()

	modules := make(map[string]string)
	for name := range s.server.services.services {
		modules[name] = "1.0"
	}
	return modules
}
