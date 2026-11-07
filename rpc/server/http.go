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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	maxRequestContentLength = 1024 * 1024 * 5
	contentType             = "application/json"
	// maxHTTPErrorBodySize bounds how much of a non-2xx response body the
	// client reads and keeps in HTTPError.Body.
	maxHTTPErrorBodySize = 64 * 1024
)

// https://www.jsonrpc.org/historical/json-rpc-over-http.html#id13
var acceptedContentTypes = []string{contentType, "application/json-rpc", "application/jsonrequest"}

type httpConn struct {
	client    *http.Client
	url       string
	closeOnce sync.Once
	closeCh   chan interface{}
	mu        sync.Mutex // protects headers
	headers   http.Header
}

// httpConn is treated specially by Client.
func (hc *httpConn) writeJSON(context.Context, interface{}) error {
	panic("writeJSON called on httpConn")
}

func (hc *httpConn) remoteAddr() string {
	return hc.url
}

func (hc *httpConn) readBatch() ([]*jsonrpcMessage, bool, error) {
	<-hc.closeCh
	return nil, false, io.EOF
}

func (hc *httpConn) close() {
	hc.closeOnce.Do(func() { close(hc.closeCh) })
}

func (hc *httpConn) closed() <-chan interface{} {
	return hc.closeCh
}

// HTTPTimeouts represents the configuration params for the HTTP RPC server.
type HTTPTimeouts struct {
	// ReadTimeout is the maximum duration for reading the entire
	// request, including the body.
	//
	// Because ReadTimeout does not let Handlers make per-request
	// decisions on each request body's acceptable deadline or
	// upload rate, most users will prefer to use
	// ReadHeaderTimeout. It is valid to use them both.
	ReadTimeout time.Duration

	// WriteTimeout is the maximum duration before timing out
	// writes of the response. It is reset whenever a new
	// request's header is read. Like ReadTimeout, it does not
	// let Handlers make decisions on a per-request basis.
	WriteTimeout time.Duration

	// IdleTimeout is the maximum amount of time to wait for the
	// next request when keep-alives are enabled. If IdleTimeout
	// is zero, the value of ReadTimeout is used. If both are
	// zero, ReadHeaderTimeout is used.
	IdleTimeout time.Duration
}

// DefaultHTTPTimeouts represents the default timeout values used if further
// configuration is not provided.
var DefaultHTTPTimeouts = HTTPTimeouts{
	ReadTimeout:  30 * time.Second,
	WriteTimeout: 30 * time.Second,
	IdleTimeout:  120 * time.Second,
}

// DialHTTPWithClient creates a new RPC client that connects to an RPC server over HTTP
// using the provided HTTP Client.
func DialHTTPWithClient(endpoint string, client *http.Client) (*Client, error) {
	// Sanity check URL so we don't end up with a client that will fail every request.
	_, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}

	initctx := context.Background()
	headers := make(http.Header, 2)
	headers.Set("accept", contentType)
	headers.Set("content-type", contentType)
	return newClient(initctx, func(context.Context) (ServerCodec, error) {
		hc := &httpConn{
			client:  client,
			headers: headers,
			url:     endpoint,
			closeCh: make(chan interface{}),
		}
		return hc, nil
	})
}

// DialHTTP creates a new RPC client that connects to an RPC server over HTTP.
func DialHTTP(endpoint string) (*Client, error) {
	return DialHTTPWithClient(endpoint, new(http.Client))
}

func (c *Client) sendHTTP(ctx context.Context, op *requestOp, msg interface{}) error {
	hc := c.writeConn.(*httpConn)
	respBody, err := hc.doRequest(ctx, msg)
	if err != nil {
		return err
	}
	defer respBody.Close()

	// A notification (Notify) has no channel to deliver to and expects no
	// reply, so whatever the server sent is not read.
	if op.resp == nil {
		return nil
	}
	var respmsg jsonrpcMessage
	if err := json.NewDecoder(respBody).Decode(&respmsg); err != nil {
		return err
	}
	op.resp <- &respmsg
	return nil
}

// sendBatchHTTP sends msgs as one batch and returns the server's reply. Each
// returned message answers a distinct element of msgs; calls the server left
// unanswered have no message.
func (c *Client) sendBatchHTTP(ctx context.Context, msgs []*jsonrpcMessage) ([]*jsonrpcMessage, error) {
	hc := c.writeConn.(*httpConn)
	respBody, err := hc.doRequest(ctx, msgs)
	if err != nil {
		return nil, err
	}
	defer respBody.Close()
	var respmsgs []*jsonrpcMessage
	if err := json.NewDecoder(respBody).Decode(&respmsgs); err != nil {
		return nil, err
	}
	if err := checkBatchResponse(msgs, respmsgs); err != nil {
		return nil, err
	}
	return respmsgs, nil
}

// checkBatchResponse reports an error unless every element of resps answers
// a distinct call of msgs. Unlike a connection's dispatch loop, which drops
// unsolicited replies, the HTTP path receives the whole reply at once and
// rejects it as a whole: the IDs are not echoed in the error because the
// server chose them.
func checkBatchResponse(msgs, resps []*jsonrpcMessage) error {
	if len(resps) > len(msgs) {
		return fmt.Errorf("%w: %d responses to %d calls", errInvalidBatchResponse, len(resps), len(msgs))
	}
	pending := make(map[string]struct{}, len(msgs))
	for _, msg := range msgs {
		pending[string(msg.ID)] = struct{}{}
	}
	for i, resp := range resps {
		if resp == nil {
			return fmt.Errorf("%w: element %d is null", errInvalidBatchResponse, i)
		}
		if _, ok := pending[string(resp.ID)]; !ok {
			return fmt.Errorf("%w: element %d answers no outstanding call", errInvalidBatchResponse, i)
		}
		delete(pending, string(resp.ID))
	}
	return nil
}

func (hc *httpConn) doRequest(ctx context.Context, msg interface{}) (io.ReadCloser, error) {
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", hc.url, io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return nil, err
	}
	req.ContentLength = int64(len(body))

	// set headers
	hc.mu.Lock()
	req.Header = hc.headers.Clone()
	hc.mu.Unlock()

	// do request
	resp, err := hc.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		var buf bytes.Buffer
		var body []byte
		if _, err := buf.ReadFrom(io.LimitReader(resp.Body, maxHTTPErrorBodySize)); err == nil {
			body = buf.Bytes()
		}

		return nil, HTTPError{
			Status:     resp.Status,
			StatusCode: resp.StatusCode,
			Body:       body,
		}
	}
	return resp.Body, nil
}

// httpServerConn turns a HTTP connection into a Conn.
type httpServerConn struct {
	io.Reader
	io.Writer
	r *http.Request
}

func newHTTPServerConn(r *http.Request, w http.ResponseWriter) ServerCodec {
	body := io.LimitReader(r.Body, maxRequestContentLength)
	conn := &httpServerConn{Reader: body, Writer: w, r: r}
	return NewCodec(conn)
}

// Close does nothing and always returns nil.
func (t *httpServerConn) Close() error { return nil }

// RemoteAddr returns the peer address of the underlying connection.
func (t *httpServerConn) RemoteAddr() string {
	return t.r.RemoteAddr
}

// SetWriteDeadline does nothing and always returns nil.
func (t *httpServerConn) SetWriteDeadline(time.Time) error { return nil }

// ServeHTTP serves JSON-RPC requests over HTTP. Every request, whatever it
// turns out to be, is admitted against the server's HTTP request limit
// first; see AdmitHTTP.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.AdmitHTTP(http.HandlerFunc(s.serveAdmittedHTTP)).ServeHTTP(w, r)
}

// serveAdmittedHTTP serves a request that holds its admission slot.
func (s *Server) serveAdmittedHTTP(w http.ResponseWriter, r *http.Request) {
	// Permit dumb empty requests for remote health-checks (AWS)
	if r.Method == http.MethodGet && r.ContentLength == 0 && r.URL.RawQuery == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if code, err := validateRequest(r); err != nil {
		http.Error(w, err.Error(), code)
		return
	}
	// OPTIONS carries no call, so its body is never dispatched. A CORS
	// preflight is answered by the CORS handler in front of this one when
	// CORS is configured.
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// All checks passed, create a codec that reads directly from the request body
	// until EOF, writes the response to w, and orders the server to process a
	// single request.
	ctx := r.Context()
	ctx = context.WithValue(ctx, "remote", r.RemoteAddr)
	ctx = context.WithValue(ctx, "scheme", r.Proto)
	ctx = context.WithValue(ctx, "local", r.Host)
	if ua := r.Header.Get("User-Agent"); ua != "" {
		ctx = context.WithValue(ctx, "User-Agent", ua)
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		ctx = context.WithValue(ctx, "Origin", origin)
	}

	w.Header().Set("content-type", contentType)
	codec := newHTTPServerConn(r, w)
	defer codec.close()
	s.serveSingleRequest(ctx, codec)
}

// refuseOverloaded answers a request refused by the HTTP request limit with
// HTTP 503 and a JSON-RPC error with code -32005 and a null id. The request
// body is never read, and the connection is closed once the answer is out:
// the read deadline set here makes the drain net/http would otherwise run
// on the unread body, for up to ReadTimeout and without a slot, fail at
// once. A ResponseWriter that cannot set deadlines still gets
// Connection: close.
func refuseOverloaded(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetReadDeadline(time.Now())
	w.Header().Set("content-type", contentType)
	w.Header().Set("Connection", "close")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(errorMessage(&serverOverloadedError{}))
}

// httpAdmittedKey marks the context of a request admitted by AdmitHTTP; its
// value is the Server whose limit holds the request's slot.
type httpAdmittedKey struct{}

// admittedHTTP reports whether r already holds a slot of s's HTTP request
// limit.
func (s *Server) admittedHTTP(r *http.Request) bool {
	admitted, _ := r.Context().Value(httpAdmittedKey{}).(*Server)
	return admitted == s
}

// AdmitHTTP returns a handler that admits each request against the server's
// HTTP request limit (see SetMaxConcurrentHTTPRequests) before passing it to
// next, and holds the slot until next has returned and the request body has
// been closed. A transport that wraps the server's ServeHTTP in handlers of
// its own, such as a compressing writer that finishes its output after the
// inner handler returns, puts AdmitHTTP outermost so that output counts as
// well; ServeHTTP then serves the request on the slot taken here instead of
// taking a second one.
//
// Admission comes before the body is read, so a refused request costs
// neither parsing nor a reply that echoes it; see refuseOverloaded. An
// admitted request's body is closed before its slot is released, so the
// bounded drain net/http performs on a body that was not read to the end
// happens while the slot is held.
func (s *Server) AdmitHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.admittedHTTP(r) {
			next.ServeHTTP(w, r)
			return
		}
		if !s.httpRequests.tryAcquire() {
			overloadedHTTPCounter.Inc(1)
			refuseOverloaded(w)
			return
		}
		defer s.httpRequests.release()
		defer r.Body.Close()
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), httpAdmittedKey{}, s)))
	})
}

// validateRequest returns a non-zero response code and error message if the
// request is invalid.
func validateRequest(r *http.Request) (int, error) {
	if r.Method == http.MethodPut || r.Method == http.MethodDelete {
		return http.StatusMethodNotAllowed, errors.New("method not allowed")
	}
	if r.ContentLength > maxRequestContentLength {
		err := fmt.Errorf("content length too large (%d>%d)", r.ContentLength, maxRequestContentLength)
		return http.StatusRequestEntityTooLarge, err
	}
	// Allow OPTIONS (regardless of content-type)
	if r.Method == http.MethodOptions {
		return 0, nil
	}
	// Check content-type
	if mt, _, err := mime.ParseMediaType(r.Header.Get("content-type")); err == nil {
		for _, accepted := range acceptedContentTypes {
			if accepted == mt {
				return 0, nil
			}
		}
	}
	// Invalid content-type
	err := fmt.Errorf("invalid content type, only %s is supported", contentType)
	return http.StatusUnsupportedMediaType, err
}
