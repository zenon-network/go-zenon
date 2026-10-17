// Copyright 2016 The go-ethereum Authors
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
	"container/list"
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/rand"
	"reflect"
	"strings"
	"sync"
	"time"
)

var (
	// ErrNotificationsUnsupported is returned when the connection doesn't support notifications
	ErrNotificationsUnsupported = errors.New("notifications not supported")
	// ErrSubscriptionNotFound is returned when no subscription matches
	// the given id, for example on an unsubscribe request.
	ErrSubscriptionNotFound = errors.New("subscription not found")
	// ErrTooManySubscriptions is returned when a connection already holds
	// DefaultMaxSubscriptionsPerConn server subscriptions unless the
	// server configures another limit; see Server.SetMaxSubscriptionsPerConn.
	ErrTooManySubscriptions = errors.New("too many subscriptions on this connection")
	// ErrNotificationBufferFull is returned by Notify when more than
	// maxPendingNotifications arrive before the subscription is activated.
	// The notifier is then failed and the connection is torn down: nothing
	// it buffered is delivered, and the subscription is reclaimed.
	ErrNotificationBufferFull = errors.New("too many notifications buffered before the subscription was activated")
)

// maxPendingNotifications bounds the notifications a notifier holds between
// the subscription's creation and the moment its ID has been written to the
// client. The window is a response write long; the bound is a safety net.
const maxPendingNotifications = 128

// notifierState is the delivery state of a Notifier.
type notifierState uint8

const (
	notifierBuffering notifierState = iota // before activation: messages wait in buffer
	notifierFlushing                       // activate is writing the buffer; new messages join it
	notifierActive                         // messages are written as they arrive
	notifierFailed                         // terminal: nothing is written again
)

// DefaultMaxSubscriptionsPerConn bounds the server subscriptions one connection can
// hold at a time; a client that needs more must unsubscribe first.
const DefaultMaxSubscriptionsPerConn = 64

var globalGen = randomIDGenerator()

// ID defines a pseudo random number that is used to identify RPC subscriptions.
type ID string

// NewID returns a new, random ID.
func NewID() ID {
	return globalGen()
}

// randomIDGenerator returns a function generates a random IDs.
func randomIDGenerator() func() ID {
	var buf = make([]byte, 8)
	var seed int64
	if _, err := crand.Read(buf); err == nil {
		seed = int64(binary.BigEndian.Uint64(buf))
	} else {
		seed = int64(time.Now().Nanosecond())
	}

	var (
		mu  sync.Mutex
		rng = rand.New(rand.NewSource(seed))
	)
	return func() ID {
		mu.Lock()
		defer mu.Unlock()
		id := make([]byte, 16)
		rng.Read(id)
		return encodeID(id)
	}
}

func encodeID(b []byte) ID {
	id := hex.EncodeToString(b)
	id = strings.TrimLeft(id, "0")
	if id == "" {
		id = "0" // ID's are RPC quantities, no leading zero's and 0 is 0x0.
	}
	return ID("0x" + id)
}

type notifierKey struct{}

// NotifierFromContext returns the Notifier value stored in ctx, if any.
func NotifierFromContext(ctx context.Context) (*Notifier, bool) {
	n, ok := ctx.Value(notifierKey{}).(*Notifier)
	return n, ok
}

// Notifier is tied to a RPC connection that supports subscriptions.
// Server callbacks use the notifier to send notifications.
type Notifier struct {
	h         *handler
	namespace string

	mu           sync.Mutex
	sub          *Subscription
	buffer       []json.RawMessage
	callReturned bool
	state        notifierState
	failErr      error // the error that moved state to notifierFailed
}

// CreateSubscription returns a new subscription that is coupled to the
// RPC connection. By default subscriptions are inactive and notifications
// are dropped until the subscription is marked as active. This is done
// by the RPC server after the subscription ID is send to the client.
func (n *Notifier) CreateSubscription() *Subscription {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.sub != nil {
		panic("can't create multiple subscriptions with Notifier")
	} else if n.callReturned {
		panic("can't create subscription after subscribe call has returned")
	}
	n.sub = &Subscription{ID: n.h.idgen(), namespace: n.namespace, err: make(chan error, 1)}
	return n.sub
}

// Notify sends a notification to the client with the given data as payload.
//
// Before the subscription is activated the encoded message is buffered, up to
// maxPendingNotifications; the buffer is written by activate, after the
// subscription ID has gone to the client, and before any later message. Once
// active, Notify writes the message and returns the write error, if any.
//
// A terminal failure (a failed flush, an overflowed buffer, or a write error
// once active) moves the notifier to a failed state: every later call returns
// that error without writing. The connection is torn down by the codec on
// its first terminal transport failure, and by Notify on an overflow; the
// subscription is uninstalled by the connection's lifecycle, never here.
//
// Notify does not hold the mutex across a write: it is held only to decide
// the state, so a flush in progress does not block it. Once active, the
// write is serialized with the connection's other writes by the codec, as
// any write is. Notify panics if called before a subscription has been
// created, or with an ID that does not match the subscription.
func (n *Notifier) Notify(id ID, data interface{}) error {
	enc, err := json.Marshal(data)
	if err != nil {
		return err
	}

	n.mu.Lock()
	if n.sub == nil {
		n.mu.Unlock()
		panic("can't Notify before subscription is created")
	} else if n.sub.ID != id {
		n.mu.Unlock()
		panic("Notify with wrong ID")
	}
	switch n.state {
	case notifierFailed:
		err := n.failErr
		n.mu.Unlock()
		return err
	case notifierBuffering, notifierFlushing:
		if len(n.buffer) >= maxPendingNotifications {
			n.fail(ErrNotificationBufferFull)
			n.mu.Unlock()
			// The client holds an ID that can never deliver and cannot be
			// told so, and the connection's writer has fallen this far
			// behind on its activation (the subscribe response, or the
			// buffered messages after it): the connection is treated as
			// failed and torn down, outside the notifier's lock, so the
			// subscription is reclaimed like any other terminal failure.
			if c, ok := n.h.conn.(interface{ abort(error) }); ok {
				c.abort(ErrNotificationBufferFull)
			}
			return ErrNotificationBufferFull
		}
		n.buffer = append(n.buffer, enc)
		n.mu.Unlock()
		return nil
	}
	sub := n.sub
	n.mu.Unlock()

	if err := n.send(sub, enc); err != nil {
		n.mu.Lock()
		if n.state != notifierFailed {
			n.fail(err)
		}
		err = n.failErr // the first failure, if another landed meanwhile
		n.mu.Unlock()
		return err
	}
	return nil
}

// fail moves the notifier to its terminal state. The caller holds n.mu.
func (n *Notifier) fail(err error) {
	n.state = notifierFailed
	n.failErr = err
	n.buffer = nil
}

// Closed returns a channel that is closed when the RPC connection is closed.
// Deprecated: use subscription error channel
func (n *Notifier) Closed() <-chan interface{} {
	return n.h.conn.closed()
}

// takeSubscription returns the subscription (if one has been created). No subscription can
// be created after this call.
func (n *Notifier) takeSubscription() *Subscription {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.callReturned = true
	return n.sub
}

// activate is called after the subscription ID was sent to the client. It
// writes everything buffered so far, including messages that arrive while
// it is writing, and only then lets Notify write directly, so no message
// overtakes an earlier one. The buffer is written without holding n.mu. A
// write error is terminal: the rest of the buffer is dropped and the error
// is returned; the codec has already begun tearing the connection down.
func (n *Notifier) activate() error {
	n.mu.Lock()
	if n.state != notifierBuffering {
		err := n.failErr
		n.mu.Unlock()
		return err
	}
	n.state = notifierFlushing
	for len(n.buffer) > 0 {
		pending, sub := n.buffer, n.sub
		n.buffer = nil
		n.mu.Unlock()
		for _, data := range pending {
			if err := n.send(sub, data); err != nil {
				// The first failure stands: an overflow meanwhile has
				// already failed the notifier and torn the connection
				// down, and this write failed because of that.
				n.mu.Lock()
				if n.state != notifierFailed {
					n.fail(err)
				}
				err = n.failErr
				n.mu.Unlock()
				return err
			}
		}
		n.mu.Lock()
		// A Notify that overflowed the buffer while the batch was being
		// written has failed the notifier and cleared the buffer; the
		// failure stands, the empty buffer does not mean the flush is done.
		if n.state != notifierFlushing {
			err := n.failErr
			n.mu.Unlock()
			return err
		}
	}
	n.state = notifierActive
	n.mu.Unlock()
	return nil
}

func (n *Notifier) send(sub *Subscription, data json.RawMessage) error {
	params, _ := json.Marshal(&subscriptionResult{ID: string(sub.ID), Result: data})
	ctx := context.Background()
	return n.h.conn.writeJSON(ctx, &jsonrpcMessage{
		Version: vsn,
		Method:  n.namespace + notificationMethodSuffix,
		Params:  params,
	})
}

// A Subscription is created by a notifier and tied to that notifier. The client can use
// this subscription to wait for an unsubscribe request for the client, see Err().
type Subscription struct {
	ID        ID
	namespace string
	err       chan error // closed on unsubscribe
}

// Err returns a channel that is closed when the client send an unsubscribe request.
func (s *Subscription) Err() <-chan error {
	return s.err
}

// MarshalJSON marshals a subscription as its ID.
func (s *Subscription) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.ID)
}

// ClientSubscription is a subscription established through the Client's Subscribe or
// EthSubscribe methods.
type ClientSubscription struct {
	client    *Client
	etype     reflect.Type
	channel   reflect.Value
	namespace string
	subid     string

	// The in channel receives notification values from client dispatcher.
	in chan json.RawMessage

	// The error channel receives the error from the forwarding loop.
	// It is closed by Unsubscribe.
	err     chan error
	errOnce sync.Once

	// Closing of the subscription is requested by sending on 'quit'. This is handled by
	// the forwarding loop, which closes 'forwardDone' when it has stopped sending to
	// sub.channel. Finally, 'unsubDone' is closed after unsubscribing on the server side.
	quit        chan error
	forwardDone chan struct{}
	unsubDone   chan struct{}
}

// This is the sentinel value sent on sub.quit when Unsubscribe is called.
var errUnsubscribed = errors.New("unsubscribed")

func newClientSubscription(c *Client, namespace string, channel reflect.Value) *ClientSubscription {
	sub := &ClientSubscription{
		client:      c,
		namespace:   namespace,
		etype:       channel.Type().Elem(),
		channel:     channel,
		in:          make(chan json.RawMessage),
		quit:        make(chan error),
		forwardDone: make(chan struct{}),
		unsubDone:   make(chan struct{}),
		err:         make(chan error, 1),
	}
	return sub
}

// Err returns the subscription error channel. The intended use of Err is to schedule
// resubscription when the client connection is closed unexpectedly.
//
// The error channel receives a value when the subscription has ended due to an error. The
// received error is nil if Close has been called on the underlying client and no other
// error has occurred.
//
// The error channel is closed when Unsubscribe is called on the subscription.
func (sub *ClientSubscription) Err() <-chan error {
	return sub.err
}

// Unsubscribe unsubscribes the notification and closes the error channel.
// It can safely be called more than once.
func (sub *ClientSubscription) Unsubscribe() {
	sub.errOnce.Do(func() {
		select {
		case sub.quit <- errUnsubscribed:
			<-sub.unsubDone
		case <-sub.unsubDone:
		}
		close(sub.err)
	})
}

// deliver is called by the client's message dispatcher to send a notification value.
func (sub *ClientSubscription) deliver(result json.RawMessage) (ok bool) {
	select {
	case sub.in <- result:
		return true
	case <-sub.forwardDone:
		return false
	}
}

// close is called by the client's message dispatcher when the connection is closed.
func (sub *ClientSubscription) close(err error) {
	select {
	case sub.quit <- err:
	case <-sub.forwardDone:
	}
}

// run is the forwarding loop of the subscription. It runs in its own goroutine and
// is launched by the client's handler after the subscription has been created.
func (sub *ClientSubscription) run() {
	defer close(sub.unsubDone)

	unsubscribe, err := sub.forward()

	// The client's dispatch loop won't be able to execute the unsubscribe call if it is
	// blocked in sub.deliver() or sub.close(). Closing forwardDone unblocks them.
	close(sub.forwardDone)

	// Call the unsubscribe method on the server.
	if unsubscribe {
		sub.requestUnsubscribe()
	}

	// Send the error.
	if err != nil {
		if err == ErrClientQuit {
			// ErrClientQuit gets here when Client.Close is called. This is reported as a
			// nil error because it's not an error, but we can't close sub.err here.
			err = nil
		}
		sub.err <- err
	}
}

// forward is the forwarding loop. It takes in RPC notifications and sends them
// on the subscription channel.
func (sub *ClientSubscription) forward() (unsubscribeServer bool, err error) {
	cases := []reflect.SelectCase{
		{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(sub.quit)},
		{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(sub.in)},
		{Dir: reflect.SelectSend, Chan: sub.channel},
	}
	buffer := list.New()

	for {
		var chosen int
		var recv reflect.Value
		if buffer.Len() == 0 {
			// Idle, omit send case.
			chosen, recv, _ = reflect.Select(cases[:2])
		} else {
			// Non-empty buffer, send the first queued item.
			cases[2].Send = reflect.ValueOf(buffer.Front().Value)
			chosen, recv, _ = reflect.Select(cases)
		}

		switch chosen {
		case 0: // <-sub.quit
			if !recv.IsNil() {
				err = recv.Interface().(error)
			}
			if err == errUnsubscribed {
				// Exiting because Unsubscribe was called, unsubscribe on server.
				return true, nil
			}
			return false, err

		case 1: // <-sub.in
			val, err := sub.unmarshal(recv.Interface().(json.RawMessage))
			if err != nil {
				return true, err
			}
			if buffer.Len() == maxClientSubscriptionBuffer {
				return true, ErrSubscriptionQueueOverflow
			}
			buffer.PushBack(val)

		case 2: // sub.channel<-
			cases[2].Send = reflect.Value{} // Don't hold onto the value.
			buffer.Remove(buffer.Front())
		}
	}
}

func (sub *ClientSubscription) unmarshal(result json.RawMessage) (interface{}, error) {
	val := reflect.New(sub.etype)
	err := json.Unmarshal(result, val.Interface())
	return val.Elem().Interface(), err
}

func (sub *ClientSubscription) requestUnsubscribe() error {
	var result interface{}
	return sub.client.Call(&result, sub.namespace+unsubscribeMethodSuffix, sub.subid)
}
