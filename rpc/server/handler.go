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
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/log"
)

// handler handles JSON-RPC messages. There is one handler per connection. Note that
// handler is not safe for concurrent use. Message handling never blocks indefinitely
// because RPCs are processed on background goroutines launched by handler.
//
// The entry points for incoming messages are:
//
//    h.handleMsg(message)
//    h.handleBatch(message)
//
// Outgoing calls use the requestOp struct. Register the request before sending it
// on the connection:
//
//    op := &requestOp{ids: ...}
//    h.addRequestOp(op)
//
// Now send the request, then wait for the reply to be delivered through handleMsg:
//
//    if err := op.wait(...); err != nil {
//        h.removeRequestOp(op) // timeout, etc.
//    }
//
type handler struct {
	reg            *serviceRegistry
	unsubscribeCb  *callback
	idgen          func() ID                      // subscription ID generator
	respWait       map[string]*requestOp          // active client requests
	clientSubs     map[string]*ClientSubscription // active client subscriptions
	callWG         sync.WaitGroup                 // pending call goroutines
	callsPending   int32                          // accepted but unfinished messages, see startCallProc
	answers        chan interface{}               // answers produced on the dispatch loop, see writeAnswer
	answerBytes    int64                          // bytes retained by queued and in-flight answers
	answerOnce     sync.Once                      // starts the answer writer
	directAnswers  bool                           // write answers on the caller instead; see writeAnswer
	writerDone     chan struct{}                  // closed when the answer writer has exited; nil if never started
	closing        chan struct{}                  // closed by close(); stops the answer writer
	closeAt        int64                          // unix nanoseconds; set by close() before closing is closed
	rootCtx        context.Context                // canceled by close()
	cancelRoot     func()                         // cancel function for rootCtx
	conn           jsonWriter                     // where responses will be sent
	log            log.Logger
	allowSubscribe bool

	subLock    sync.Mutex
	serverSubs map[ID]*Subscription
}

type callProc struct {
	ctx       context.Context
	notifiers []*Notifier
}

func newHandler(connCtx context.Context, conn jsonWriter, idgen func() ID, reg *serviceRegistry) *handler {
	rootCtx, cancelRoot := context.WithCancel(connCtx)
	h := &handler{
		reg:            reg,
		idgen:          idgen,
		conn:           conn,
		respWait:       make(map[string]*requestOp),
		clientSubs:     make(map[string]*ClientSubscription),
		rootCtx:        rootCtx,
		cancelRoot:     cancelRoot,
		allowSubscribe: true,
		answers:        make(chan interface{}, maxQueuedAnswers),
		closing:        make(chan struct{}),
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
func (h *handler) handleBatch(msgs []*jsonrpcMessage) {
	// Emit error response for empty batches:
	if len(msgs) == 0 {
		started := h.startCallProc(func(cp *callProc) {
			h.conn.writeJSON(cp.ctx, errorMessage(&invalidRequestError{"empty batch"}))
		})
		if !started {
			h.writeAnswer(errorMessage(&invalidRequestError{"empty batch"}))
		}
		return
	}

	// Handle non-call messages first. Unsubscribe requests are answered
	// here as well, without going through admission; see handleTeardown.
	calls := make([]*jsonrpcMessage, 0, len(msgs))
	var immediate []*jsonrpcMessage
	for _, msg := range msgs {
		if handled := h.handleImmediate(msg); handled {
			continue
		}
		if answer, handled := h.handleTeardown(msg); handled {
			if answer != nil {
				immediate = append(immediate, answer)
			}
			continue
		}
		calls = append(calls, msg)
	}
	if len(calls) == 0 {
		if len(immediate) > 0 {
			h.writeAnswer(immediate)
		}
		return
	}
	// Process calls on a goroutine because they may block indefinitely:
	started := h.startCallProc(func(cp *callProc) {
		answers := make([]*jsonrpcMessage, 0, len(msgs))
		answers = append(answers, immediate...)
		responseBytes := 0
		for i, msg := range calls {
			if answer := h.handleCallMsg(cp, msg); answer != nil {
				answers = append(answers, answer)
				responseBytes += answer.payloadSize()
			}
			// The answer that crosses the budget is still delivered; the
			// calls after it are answered without being executed.
			if responseBytes > maxBatchResponseBytes {
				h.log.Warn("Batch response too large", "responseBytes", responseBytes, "skipped", len(calls)-i-1)
				for _, rest := range calls[i+1:] {
					if answer := skippedAnswer(rest, new(batchResponseTooLargeError)); answer != nil {
						answers = append(answers, answer)
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
	if !started {
		answers := make([]*jsonrpcMessage, 0, len(calls)+len(immediate))
		answers = append(answers, immediate...)
		for _, msg := range calls {
			if answer := skippedAnswer(msg, new(tooManyPendingError)); answer != nil {
				answers = append(answers, answer)
			}
		}
		if len(answers) > 0 {
			h.writeAnswer(answers)
		}
	}
}

// handleMsg handles a single message.
func (h *handler) handleMsg(msg *jsonrpcMessage) {
	if ok := h.handleImmediate(msg); ok {
		return
	}
	if answer, handled := h.handleTeardown(msg); handled {
		if answer != nil {
			h.writeAnswer(answer)
		}
		return
	}
	started := h.startCallProc(func(cp *callProc) {
		answer := h.handleCallMsg(cp, msg)
		h.addSubscriptions(cp.notifiers)
		if answer != nil {
			h.conn.writeJSON(cp.ctx, answer)
		}
		for _, n := range cp.notifiers {
			n.activate()
		}
	})
	if !started {
		if answer := skippedAnswer(msg, new(tooManyPendingError)); answer != nil {
			h.writeAnswer(answer)
		}
	}
}

// handleBatchTooLarge answers a batch the codec refused to decode because it
// carried more than maxBatchRequests elements. The whole value has been
// consumed from the stream, so the connection keeps serving.
func (h *handler) handleBatchTooLarge() {
	h.writeAnswer(errorMessage(errBatchTooLarge))
}

// handleTeardown runs an unsubscribe request on the caller's goroutine and
// returns its answer (nil for the notification form). Unsubscribe does not
// count against the pending-message limit and, inside a batch, is not
// subject to the response budget either: it only removes an entry from
// this connection's own subscription table, so it is bounded work, and a
// client must be able to release subscriptions from a connection it has
// saturated. The connection's subscription table bounds how much such work
// there is to do. handled is false for every other message, which the
// caller then submits to startCallProc.
func (h *handler) handleTeardown(msg *jsonrpcMessage) (answer *jsonrpcMessage, handled bool) {
	if !msg.isUnsubscribe() || (!msg.isCall() && !msg.isNotification()) {
		return nil, false
	}
	return h.handleCallMsg(&callProc{ctx: h.rootCtx}, msg), true
}

// skippedAnswer is the answer for a message of a batch or connection that
// is not going to be executed: a call is answered with err, a message that
// is not a call gets the invalid-request answer handleCallMsg would have
// given it, and a notification gets none.
func skippedAnswer(msg *jsonrpcMessage, err error) *jsonrpcMessage {
	switch {
	case msg.isNotification():
		return nil
	case msg.isCall():
		return msg.errorResponse(err)
	case msg.hasValidID():
		return msg.errorResponse(&invalidRequestError{"invalid request"})
	default:
		return errorMessage(&invalidRequestError{"invalid request"})
	}
}

// close cancels all requests except for inflightReq and waits for
// call goroutines to shut down.
func (h *handler) close(err error, inflightReq *requestOp) {
	h.cancelAllRequests(err, inflightReq)
	atomic.StoreInt64(&h.closeAt, time.Now().Add(defaultWriteTimeout).UnixNano())
	close(h.closing)
	h.callWG.Wait()
	h.waitAnswerWriter()
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
		if sub := n.takeSubscription(); sub != nil {
			h.serverSubs[sub.ID] = sub
		}
	}
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
//
// The connection may have at most maxPendingCalls messages accepted but not
// finished; beyond that startCallProc returns false without starting
// anything and the caller answers the message with an overload error. An
// accepted message runs at once rather than waiting in a queue: a call that
// waited for a call awaiting a reply from the peer could wait forever when
// the peer's calls are in the same state (mutual recursion through reverse
// calls), whereas a rejected message returns an error the chain unwinds
// on. The caller is the connection's dispatch loop, which running calls
// need to deliver replies to their reverse calls, so nothing here blocks.
func (h *handler) startCallProc(fn func(*callProc)) bool {
	if atomic.AddInt32(&h.callsPending, 1) > maxPendingCalls {
		atomic.AddInt32(&h.callsPending, -1)
		return false
	}
	h.callWG.Add(1)
	go func() {
		defer h.callWG.Done()
		defer atomic.AddInt32(&h.callsPending, -1)
		ctx, cancel := context.WithCancel(h.rootCtx)
		defer cancel()
		fn(&callProc{ctx: ctx})
	}()
	return true
}

// writeAnswer queues an answer produced on the dispatch loop itself (an
// overload rejection, an oversized-batch rejection, an unsubscribe result)
// for the connection's answer writer, so the loop never waits on the
// transport: call goroutines need it to deliver the replies to their
// reverse calls. The queue is bounded by count and by the bytes it
// retains, since an answer echoes the request's id. It never blocks: a peer
// whose answers overflow either bound keeps sending while not reading, and
// its connection is closed instead. The first answer is queued whatever its
// size, so a single oversized id costs the peer nothing but its own bytes.
//
// A single-request handler (HTTP) writes on the caller instead: its codec
// belongs to the request, whose handler must not return while the answer
// is still being written, and there is no dispatch loop to keep free.
func (h *handler) writeAnswer(v interface{}) {
	if h.directAnswers {
		h.writeOne(v)
		return
	}
	h.answerOnce.Do(func() {
		h.writerDone = make(chan struct{})
		go h.writeAnswers()
	})
	size := answerSize(v)
	if queued := atomic.AddInt64(&h.answerBytes, size); queued > maxQueuedAnswerBytes && queued != size {
		atomic.AddInt64(&h.answerBytes, -size)
		h.shed("bytes", queued)
		return
	}
	select {
	case h.answers <- v:
	default:
		atomic.AddInt64(&h.answerBytes, -size)
		h.shed("answers", int64(len(h.answers)))
	}
}

// shed closes the connection of a peer that keeps sending while not
// reading its answers. Waiting would not serve the peer, and the answers it
// leaves unread would otherwise accumulate without bound.
func (h *handler) shed(what string, queued int64) {
	h.log.Warn("Closing RPC connection, peer is not reading its answers", "queued"+what, queued)
	h.conn.close()
}

// writeAnswers is the connection's answer writer. When the handler closes
// it writes what is still queued and exits.
func (h *handler) writeAnswers() {
	defer close(h.writerDone)
	for {
		select {
		case v := <-h.answers:
			h.writeOne(v)
		case <-h.closing:
			for {
				select {
				case v := <-h.answers:
					h.writeOne(v)
				default:
					return
				}
			}
		}
	}
}

// writeOne writes one answer. Once close() has recorded its deadline, every
// write uses that deadline instead of a fresh one, whether the write was
// picked before or after the close signal; on a transport that honours
// deadlines the only write that can outlast it is one that was already in
// progress, and on one that does not, waitAnswerWriter bounds the close
// instead. A failed write means the transport is gone or the peer has
// stopped reading, and closes the connection.
func (h *handler) writeOne(v interface{}) {
	ctx := h.rootCtx
	if at := atomic.LoadInt64(&h.closeAt); at != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(context.Background(), time.Unix(0, at))
		defer cancel()
	}
	err := h.conn.writeJSON(ctx, v)
	atomic.AddInt64(&h.answerBytes, -answerSize(v))
	if err != nil {
		h.log.Debug("Closing RPC connection, failed to write answer", "err", err)
		h.conn.close()
	}
}

// waitAnswerWriter waits for the answer writer to finish, until the
// deadline close() recorded. A transport that ignores write deadlines can
// keep the writer inside a write past that point; the connection is then
// closed under it, which interrupts the write on every transport whose
// close does that, and the writer is left to exit on its own so that close
// itself stays bounded whatever the transport.
func (h *handler) waitAnswerWriter() {
	if h.writerDone == nil {
		return
	}
	timer := time.NewTimer(time.Until(time.Unix(0, atomic.LoadInt64(&h.closeAt))))
	defer timer.Stop()
	select {
	case <-h.writerDone:
	case <-timer.C:
		h.log.Warn("Closing RPC connection under an answer write that did not finish in time")
		h.conn.close()
	}
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

	// Install notifier in context so the subscription handler can find it.
	n := &Notifier{h: h, namespace: namespace}
	cp.notifiers = append(cp.notifiers, n)
	ctx := context.WithValue(cp.ctx, notifierKey{}, n)

	return h.runMethod(ctx, msg, callb, args)
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
