// Copyright 2019 The go-ethereum Authors
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
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/log"
)

// handler handles JSON-RPC messages. There is one handler per connection. Note that
// handler is not safe for concurrent use. Message handling never blocks indefinitely
// because RPCs are processed on background goroutines launched by handler.
//
// The entry points for incoming messages are:
//
//	h.handleMsg(message)
//	h.handleBatch(message)
//
// Outgoing calls use the requestOp struct. Register the request before sending it
// on the connection:
//
//	op := &requestOp{ids: ...}
//	h.addRequestOp(op)
//
// Now send the request, then wait for the reply to be delivered through handleMsg:
//
//	if err := op.wait(...); err != nil {
//	    h.removeRequestOp(op) // timeout, etc.
//	}
type handler struct {
	reg            *serviceRegistry
	unsubscribeCb  *callback
	idgen          func() ID                      // subscription ID generator
	respWait       map[string]*requestOp          // active client requests
	clientSubs     map[string]*ClientSubscription // active client subscriptions
	callWG         sync.WaitGroup                 // pending call goroutines
	rootCtx        context.Context                // canceled by close()
	cancelRoot     func()                         // cancel function for rootCtx
	conn           jsonWriter                     // where responses will be sent
	log            log.Logger
	allowSubscribe bool

	// callSlots bounds the number of concurrently executing calls per
	// connection. Slots are acquired by Client.read before dispatching
	// call messages and released here when the call goroutines complete.
	// The handler never blocks on this semaphore; acquisition in the read
	// goroutine applies TCP backpressure without stalling the dispatch
	// loop.
	callSlots chan struct{}

	subLock    sync.Mutex
	serverSubs map[ID]*Subscription
	// pendingSubs counts subscribe calls that have been accepted against the
	// per-connection limit but whose notifier has not been collected by
	// addSubscriptions yet. Guarded by subLock together with serverSubs.
	pendingSubs int
	// maxServerSubs is the connection's subscription budget, taken from the
	// Server that created the handler.
	maxServerSubs int
}

// maxConcurrentCallsPerConn bounds how many calls may be executing at once
// on a single connection. Slots are acquired in Client.read before a call
// message is dispatched; when all slots are taken the read goroutine blocks,
// which stops reading from the network and applies TCP backpressure to the
// peer. The dispatch loop is never blocked by this limit.
//
// A slot is held until the method returns and its reply write finishes or
// times out (defaultWriteTimeout). A non-reading peer is not disconnected —
// closing the codec on a write error is a separate change, and aggregate and
// idle limits belong to the deployment. When the read goroutine is blocked
// waiting for a slot it cannot read subsequent messages, including
// .unsubscribe. A client that has saturated the connection cannot shed its
// own load until at least one in-flight call completes.
const maxConcurrentCallsPerConn = 64

type callProc struct {
	ctx       context.Context
	notifiers []*Notifier
}

// releaseCallSlot returns one call slot to the semaphore. It is a no-op
// when callSlots is nil, which is the case for handlers created outside a
// Client (e.g. HTTP serveSingleRequest) where the semaphore does not apply.
func (h *handler) releaseCallSlot() {
	if h.callSlots == nil {
		return
	}
	<-h.callSlots
}

func newHandler(connCtx context.Context, conn jsonWriter, idgen func() ID, reg *serviceRegistry, maxServerSubs int) *handler {
	if maxServerSubs < 1 {
		maxServerSubs = DefaultMaxSubscriptionsPerConn
	}
	rootCtx, cancelRoot := context.WithCancel(connCtx)
	h := &handler{
		reg:            reg,
		idgen:          idgen,
		maxServerSubs:  maxServerSubs,
		conn:           conn,
		respWait:       make(map[string]*requestOp),
		clientSubs:     make(map[string]*ClientSubscription),
		rootCtx:        rootCtx,
		cancelRoot:     cancelRoot,
		allowSubscribe: true,
		serverSubs:     make(map[ID]*Subscription),
		log:            log.Root(),
	}
	if conn.remoteAddr() != "" {
		h.log = h.log.New("conn", conn.remoteAddr())
	}
	h.unsubscribeCb = newCallback(reflect.Value{}, reflect.ValueOf(h.unsubscribe))
	return h
}

// handleBatch executes all messages in a batch and returns the responses.
// slotAcquired reports whether Client.read acquired a batch-level call slot
// for this batch (true when at least one message needed one).
func (h *handler) handleBatch(msgs []*jsonrpcMessage, slotAcquired bool) {
	// Emit error response for empty batches:
	if len(msgs) == 0 {
		h.startCallProc(func(cp *callProc) {
			h.conn.writeJSON(cp.ctx, errorMessage(&invalidRequestError{"empty batch"}))
		})
		return
	}

	// Handle non-call messages first:
	calls := make([]*jsonrpcMessage, 0, len(msgs))
	for _, msg := range msgs {
		if handled := h.handleImmediate(msg); !handled {
			calls = append(calls, msg)
		}
	}
	if len(calls) == 0 {
		return
	}
	// Process calls on a goroutine because they may block indefinitely.
	// Release the batch-level slot when the batch completes, but only if
	// Client.read actually acquired one (slotAcquired).
	h.startCallProc(func(cp *callProc) {
		if slotAcquired {
			defer h.releaseCallSlot()
		}
		answers := make([]*jsonrpcMessage, 0, len(msgs))
		responseBytes := 0
		for i, msg := range calls {
			if answer := h.handleCallMsg(cp, msg); answer != nil {
				answers = append(answers, answer)
				responseBytes += answer.payloadSize()
			}
			// The answer that crosses the budget is still delivered; the
			// elements after it are answered the way handleCallMsg would
			// have answered them, except that a call is not executed and
			// gets the budget error instead of its result.
			if responseBytes > maxBatchResponseBytes {
				h.log.Warn("Batch response too large", "responseBytes", responseBytes, "skipped", len(calls)-i-1)
				for _, rest := range calls[i+1:] {
					switch {
					case rest.isNotification():
					case rest.isCall():
						answers = append(answers, rest.errorResponse(new(batchResponseTooLargeError)))
					case rest.hasValidID():
						answers = append(answers, rest.errorResponse(&invalidRequestError{"invalid request"}))
					default:
						answers = append(answers, errorMessage(&invalidRequestError{"invalid request"}))
					}
				}
				break
			}
		}
		h.addSubscriptions(cp.notifiers)
		if len(answers) > 0 {
			h.conn.writeJSON(cp.ctx, answers)
		}
		for _, n := range cp.notifiers {
			n.activate()
		}
	})
}

// handleMsg handles a single message. slotAcquired reports whether
// Client.read acquired a call slot for this message.
func (h *handler) handleMsg(msg *jsonrpcMessage, slotAcquired bool) {
	if ok := h.handleImmediate(msg); ok {
		return
	}
	h.startCallProc(func(cp *callProc) {
		defer func() {
			if slotAcquired {
				h.releaseCallSlot()
			}
		}()
		answer := h.handleCallMsg(cp, msg)
		h.addSubscriptions(cp.notifiers)
		if answer != nil {
			h.conn.writeJSON(cp.ctx, answer)
		}
		for _, n := range cp.notifiers {
			n.activate()
		}
	})
}

// close cancels all requests except for inflightReq and waits for
// call goroutines to shut down.
func (h *handler) close(err error, inflightReq *requestOp) {
	h.cancelAllRequests(err, inflightReq)
	h.callWG.Wait()
	h.cancelRoot()
	h.cancelServerSubscriptions(err)
}

// addRequestOp registers a request operation.
func (h *handler) addRequestOp(op *requestOp) {
	for _, id := range op.ids {
		h.respWait[string(id)] = op
	}
}

// removeRequestOps stops waiting for the given request IDs.
func (h *handler) removeRequestOp(op *requestOp) {
	for _, id := range op.ids {
		delete(h.respWait, string(id))
	}
}

// cancelAllRequests unblocks and removes pending requests and active subscriptions.
func (h *handler) cancelAllRequests(err error, inflightReq *requestOp) {
	didClose := make(map[*requestOp]bool)
	if inflightReq != nil {
		didClose[inflightReq] = true
	}

	for id, op := range h.respWait {
		// Remove the op so that later calls will not close op.resp again.
		delete(h.respWait, id)

		if !didClose[op] {
			op.err = err
			close(op.resp)
			didClose[op] = true
		}
	}
	for id, sub := range h.clientSubs {
		delete(h.clientSubs, id)
		sub.close(err)
	}
}

func (h *handler) addSubscriptions(nn []*Notifier) {
	h.subLock.Lock()
	defer h.subLock.Unlock()

	for _, n := range nn {
		// Every notifier collected by handleSubscribe holds one reservation
		// and a subscription to convert it into.
		h.pendingSubs--
		if sub := n.takeSubscription(); sub != nil {
			h.serverSubs[sub.ID] = sub
		}
	}
}

// releaseSubscription returns a reservation taken by reserveSubscription for
// a call that ended without creating a subscription.
func (h *handler) releaseSubscription() {
	h.subLock.Lock()
	defer h.subLock.Unlock()
	h.pendingSubs--
}

// reserveSubscription claims one slot of the connection's subscription budget
// for a subscribe call that is about to run. Slots held by calls still in
// flight count as well, so neither a batch nor concurrent single requests can
// exceed the connection's limit. The slot is released by releaseSubscription
// if the call creates no subscription, converted by addSubscriptions when
// the call's notifier is collected, and the installed subscription's slot is
// released by unsubscribe or cancelServerSubscriptions.
func (h *handler) reserveSubscription() error {
	h.subLock.Lock()
	defer h.subLock.Unlock()

	if len(h.serverSubs)+h.pendingSubs >= h.maxServerSubs {
		return ErrTooManySubscriptions
	}
	h.pendingSubs++
	return nil
}

// cancelServerSubscriptions removes all subscriptions and closes their error channels.
func (h *handler) cancelServerSubscriptions(err error) {
	h.subLock.Lock()
	defer h.subLock.Unlock()

	for id, s := range h.serverSubs {
		s.err <- err
		close(s.err)
		delete(h.serverSubs, id)
	}
}

// startCallProc runs fn in a new goroutine and starts tracking it in the h.calls wait group.
func (h *handler) startCallProc(fn func(*callProc)) {
	h.callWG.Add(1)
	go func() {
		ctx, cancel := context.WithCancel(h.rootCtx)
		defer h.callWG.Done()
		defer cancel()
		fn(&callProc{ctx: ctx})
	}()
}

// handleImmediate executes non-call messages. It returns false if the message is a
// call or requires a reply.
func (h *handler) handleImmediate(msg *jsonrpcMessage) bool {
	start := time.Now()
	switch {
	case msg.isNotification():
		if strings.HasSuffix(msg.Method, notificationMethodSuffix) {
			h.handleSubscriptionResult(msg)
			return true
		}
		return false
	case msg.isResponse():
		h.handleResponse(msg)
		h.log.Trace("Handled RPC response", "reqid", idForLog{msg.ID}, "t", time.Since(start))
		return true
	default:
		return false
	}
}

// handleSubscriptionResult processes subscription notifications.
func (h *handler) handleSubscriptionResult(msg *jsonrpcMessage) {
	var result subscriptionResult
	if err := json.Unmarshal(msg.Params, &result); err != nil {
		h.log.Debug("Dropping invalid subscription message")
		return
	}
	if h.clientSubs[result.ID] != nil {
		h.clientSubs[result.ID].deliver(result.Result)
	}
}

// handleResponse processes method call responses.
func (h *handler) handleResponse(msg *jsonrpcMessage) {
	op := h.respWait[string(msg.ID)]
	if op == nil {
		h.log.Debug("Unsolicited RPC response", "reqid", idForLog{msg.ID})
		return
	}
	delete(h.respWait, string(msg.ID))
	// For normal responses, just forward the reply to Call/BatchCall.
	if op.sub == nil {
		op.resp <- msg
		return
	}
	// For subscription responses, start the subscription if the server
	// indicates success. EthSubscribe gets unblocked in either case through
	// the op.resp channel.
	defer close(op.resp)
	if msg.Error != nil {
		op.err = msg.Error
		return
	}
	if op.err = json.Unmarshal(msg.Result, &op.sub.subid); op.err == nil {
		h.callWG.Add(1)
		go func() {
			op.sub.run()
			h.callWG.Done()
		}()
		h.clientSubs[op.sub.subid] = op.sub
	}
}

// handleCallMsg executes a call message and returns the answer.
func (h *handler) handleCallMsg(ctx *callProc, msg *jsonrpcMessage) *jsonrpcMessage {
	start := time.Now()
	switch {
	case msg.isNotification():
		h.handleCall(ctx, msg)
		h.log.Debug("Served "+msg.Method, "t", time.Since(start))
		return nil
	case msg.isCall():
		resp := h.handleCall(ctx, msg)
		var ctx []interface{}
		ctx = append(ctx, "reqid", idForLog{msg.ID}, "t", time.Since(start))
		if resp.Error != nil {
			ctx = append(ctx, "err", resp.Error.Message)
			if resp.Error.Data != nil {
				ctx = append(ctx, "errdata", resp.Error.Data)
			}
			h.log.Warn("Served "+msg.Method, ctx...)
		} else {
			h.log.Debug("Served "+msg.Method, ctx...)
		}
		return resp
	case msg.hasValidID():
		return msg.errorResponse(&invalidRequestError{"invalid request"})
	default:
		return errorMessage(&invalidRequestError{"invalid request"})
	}
}

// handleCall processes method calls.
func (h *handler) handleCall(cp *callProc, msg *jsonrpcMessage) *jsonrpcMessage {
	if msg.isSubscribe() {
		return h.handleSubscribe(cp, msg)
	}
	var callb *callback
	if msg.isUnsubscribe() {
		callb = h.unsubscribeCb
	} else {
		callb = h.reg.callback(msg.Method)
	}
	if callb == nil {
		return msg.errorResponse(&methodNotFoundError{method: msg.Method})
	}
	args, err := parsePositionalArguments(msg.Params, callb.argTypes)
	if err != nil {
		return msg.errorResponse(&invalidParamsError{err.Error()})
	}
	start := time.Now()
	answer := h.runMethod(cp.ctx, msg, callb, args)

	// Collect the statistics for RPC calls if metrics is enabled.
	// We only care about pure rpc call. Filter out subscription.
	if callb != h.unsubscribeCb {
		rpcRequestGauge.Inc(1)
		if answer.Error != nil {
			failedReqeustGauge.Inc(1)
		} else {
			successfulRequestGauge.Inc(1)
		}
		rpcServingTimer.UpdateSince(start)
		newRPCServingTimer(msg.Method, answer.Error == nil).UpdateSince(start)
	}
	return answer
}

// handleSubscribe processes *_subscribe method calls.
func (h *handler) handleSubscribe(cp *callProc, msg *jsonrpcMessage) *jsonrpcMessage {
	if !h.allowSubscribe {
		return msg.errorResponse(ErrNotificationsUnsupported)
	}

	// Subscription method name is first argument.
	name, err := parseSubscriptionName(msg.Params)
	if err != nil {
		return msg.errorResponse(&invalidParamsError{err.Error()})
	}
	namespace := msg.namespace()
	callb := h.reg.subscription(namespace, name)
	if callb == nil {
		return msg.errorResponse(&subscriptionNotFoundError{namespace, name})
	}

	// Parse subscription name arg too, but remove it before calling the callback.
	argTypes := append([]reflect.Type{stringType}, callb.argTypes...)
	args, err := parsePositionalArguments(msg.Params, argTypes)
	if err != nil {
		return msg.errorResponse(&invalidParamsError{err.Error()})
	}
	args = args[1:]

	// Reserve the connection's budget only for well-formed requests, and
	// before the notifier exists: the notifier is collected by
	// addSubscriptions unconditionally, so a reservation and a notifier must
	// always be created together.
	if err := h.reserveSubscription(); err != nil {
		return msg.errorResponse(err)
	}

	// Install notifier in context so the subscription handler can find it.
	n := &Notifier{h: h, namespace: namespace}
	ctx := context.WithValue(cp.ctx, notifierKey{}, n)

	answer := h.runMethod(ctx, msg, callb, args)

	// A call that returned without creating a subscription has nothing to
	// install, so its reservation is returned now rather than when the whole
	// batch has run: later elements of the same batch must not be rejected
	// on behalf of slots that nothing holds.
	if n.takeSubscription() == nil {
		h.releaseSubscription()
		return answer
	}
	cp.notifiers = append(cp.notifiers, n)
	return answer
}

// runMethod runs the Go callback for an RPC method.
func (h *handler) runMethod(ctx context.Context, msg *jsonrpcMessage, callb *callback, args []reflect.Value) *jsonrpcMessage {
	result, err := callb.call(ctx, msg.Method, args)
	if err != nil {
		return msg.errorResponse(err)
	}
	return msg.response(result)
}

// unsubscribe is the callback function for all *_unsubscribe calls.
func (h *handler) unsubscribe(ctx context.Context, id ID) (bool, error) {
	h.subLock.Lock()
	defer h.subLock.Unlock()

	s := h.serverSubs[id]
	if s == nil {
		return false, ErrSubscriptionNotFound
	}
	close(s.err)
	delete(h.serverSubs, id)
	return true, nil
}

type idForLog struct{ json.RawMessage }

func (id idForLog) String() string {
	if s, err := strconv.Unquote(string(id.RawMessage)); err == nil {
		return s
	}
	return string(id.RawMessage)
}
