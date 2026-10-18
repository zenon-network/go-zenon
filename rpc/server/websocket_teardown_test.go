package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// bigResult returns a payload large enough that a peer which stopped reading
// makes the server's write block until its deadline. Loopback socket buffers
// can absorb several MiB (and grow under load), so the reply is well past
// that: 4 MiB fitted entirely in one full-suite run and no timeout fired.
type bigResult struct{}

func (bigResult) Blob() string { return strings.Repeat("x", 32<<20) }

func setWriteTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := defaultWriteTimeout
	defaultWriteTimeout = d
	t.Cleanup(func() { defaultWriteTimeout = prev })
}

// wsServer serves srv over WebSocket and reports every codec it creates, so
// a test can watch the server side of a connection.
func wsServer(t *testing.T, srv *Server) (url string, codecs <-chan *websocketCodec) {
	t.Helper()
	ch := make(chan *websocketCodec, 8)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		codec := newWebsocketCodec(conn).(*websocketCodec)
		ch <- codec
		srv.ServeCodec(codec, 0)
	})
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return "ws" + strings.TrimPrefix(ts.URL, "http"), ch
}

func dialRaw(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func awaitCodec(t *testing.T, codecs <-chan *websocketCodec) *websocketCodec {
	t.Helper()
	select {
	case c := <-codecs:
		return c
	case <-time.After(teardownTestTimeout):
		t.Fatal("server never created a codec for the connection")
		return nil
	}
}

// The same plain encoding error over WebSocket: the codec stays open, no
// frame is sent for the failed message, and the next message reaches the
// peer. Fails if the WebSocket encoder serializes into an open frame and
// its error is then read as a transport failure.
func TestWebsocketPlainEncodingErrorDoesNotTearDownConnection(t *testing.T) {
	srv := NewServer()
	defer srv.Stop()
	url, codecs := wsServer(t, srv)
	client := dialRaw(t, url)
	codec := awaitCodec(t, codecs)

	err := codec.writeJSON(context.Background(), map[string]interface{}{"n": json.Number("not-a-number")})
	if err == nil {
		t.Fatal("encoding an invalid json.Number succeeded")
	}
	if isTerminalWriteError(err) {
		t.Fatalf("encoding error %v was classified as a terminal write error", err)
	}
	select {
	case <-codec.closed():
		t.Fatal("codec was closed by an encoding error")
	default:
	}

	done := make(chan error, 1)
	go func() { done <- codec.writeJSON(context.Background(), &jsonrpcMessage{Version: vsn, Method: "ok"}) }()
	client.SetReadDeadline(time.Now().Add(teardownTestTimeout))
	var msg jsonrpcMessage
	if err := client.ReadJSON(&msg); err != nil {
		t.Fatalf("reading the message after the encoding error: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if msg.Method != "ok" {
		t.Fatalf("got method %q, want ok", msg.Method)
	}
}

// A WebSocket peer that stops reading makes the server's reply write time
// out. That is a terminal failure: the codec closes, ServeCodec returns,
// and a second connection is unaffected. Fails if the write timeout no
// longer aborts the codec.
func TestWebsocketWriteTimeoutTearsDownConnection(t *testing.T) {
	setWriteTimeout(t, 200*time.Millisecond)
	srv := NewServer()
	if err := srv.RegisterName("big", bigResult{}); err != nil {
		t.Fatal(err)
	}
	if err := srv.RegisterName("test", issue116Probe{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	url, codecs := wsServer(t, srv)

	stuck := dialRaw(t, url)
	codec := awaitCodec(t, codecs)
	// Request a large reply and never read it.
	if err := stuck.WriteMessage(websocket.TextMessage, []byte(`{"jsonrpc":"2.0","id":1,"method":"big.blob"}`)); err != nil {
		t.Fatal(err)
	}
	awaitClosed(t, codec.closed(), "server codec closed after the reply write timed out")
	if err := codec.failed(); err == nil {
		t.Fatal("codec closed without a recorded terminal error")
	}

	// A healthy peer on the same server is served normally. Its reply is
	// small: the shortened write timeout is for the stuck peer's 4 MiB
	// reply, and must not decide this peer's fate.
	client, err := DialWebsocket(context.Background(), url, "")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var pong string
	ctx, cancel := context.WithTimeout(context.Background(), teardownTestTimeout)
	defer cancel()
	if err := client.CallContext(ctx, &pong, "test.ping"); err != nil {
		t.Fatalf("healthy connection failed after another was torn down: %v", err)
	}
	if pong != "pong" {
		t.Fatalf("healthy connection answered %q, want pong", pong)
	}
}

// A ping write that fails aborts the codec even though the peer's own
// direction is still open: the server half-closes its write side, so the
// next ping gets a broken pipe while reads would still block. Fails if
// pingLoop keeps discarding ping errors.
func TestWebsocketPingFailureTearsDownConnection(t *testing.T) {
	prevInterval, prevTimeout := wsPingInterval, wsPingWriteTimeout
	wsPingInterval, wsPingWriteTimeout = 20*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { wsPingInterval, wsPingWriteTimeout = prevInterval, prevTimeout })

	srv := NewServer()
	if err := srv.RegisterName("test", issue116Probe{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	url, codecs := wsServer(t, srv)

	peer := dialRaw(t, url)
	codec := awaitCodec(t, codecs)
	// Keep the peer reading so its side never errors first.
	go func() {
		for {
			if _, _, err := peer.ReadMessage(); err != nil {
				return
			}
		}
	}()
	tcp, ok := codec.conn.NetConn().(*net.TCPConn)
	if !ok {
		t.Fatalf("server connection is %T, want *net.TCPConn", codec.conn.NetConn())
	}
	if err := tcp.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	awaitClosed(t, codec.closed(), "server codec closed after a ping write failed")
	if err := codec.failed(); err == nil {
		t.Fatal("codec closed without a recorded terminal error")
	}
}

// stopOnCloseListener hands out connections whose Close calls Server.Stop,
// the way an embedding's connection type might (issue #116). The WebSocket
// upgrade hijacks exactly these connections.
type stopOnCloseListener struct {
	net.Listener
	srv *Server
}

func (l *stopOnCloseListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &stopOnCloseNetConn{Conn: c, srv: l.srv}, nil
}

type stopOnCloseNetConn struct {
	net.Conn
	srv *Server
}

func (c *stopOnCloseNetConn) Close() error {
	c.srv.Stop()
	return c.Conn.Close()
}

// wsServerStopOnClose is wsServer over connections whose Close re-enters
// Server.Stop, and reports when each ServeCodec returns.
func wsServerStopOnClose(t *testing.T, srv *Server) (url string, codecs <-chan *websocketCodec, served <-chan struct{}) {
	t.Helper()
	codecCh := make(chan *websocketCodec, 8)
	servedCh := make(chan struct{}, 8)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		codec := newWebsocketCodec(conn).(*websocketCodec)
		codecCh <- codec
		srv.ServeCodec(codec, 0)
		servedCh <- struct{}{}
	})
	ts := httptest.NewUnstartedServer(h)
	ts.Listener = &stopOnCloseListener{Listener: ts.Listener, srv: srv}
	ts.Start()
	t.Cleanup(ts.Close)
	return "ws" + strings.TrimPrefix(ts.URL, "http"), codecCh, servedCh
}

// The ping goroutine's abort closes the connection; if that Close re-enters
// Server.Stop, Stop closes this codec too, and the WebSocket close joins
// the ping goroutine. That goroutine must already count as finished, or it
// waits for itself and ServeCodec never returns. Fails if the ping
// goroutine aborts before it has left the wait group.
func TestWebsocketPingAbortSurvivesReentrantStop(t *testing.T) {
	prevInterval, prevTimeout := wsPingInterval, wsPingWriteTimeout
	wsPingInterval, wsPingWriteTimeout = 20*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { wsPingInterval, wsPingWriteTimeout = prevInterval, prevTimeout })

	srv := NewServer()
	if err := srv.RegisterName("test", issue116Probe{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	url, codecs, served := wsServerStopOnClose(t, srv)

	peer := dialRaw(t, url)
	codec := awaitCodec(t, codecs)
	go func() {
		for {
			if _, _, err := peer.ReadMessage(); err != nil {
				return
			}
		}
	}()
	wrapped, ok := codec.conn.NetConn().(*stopOnCloseNetConn)
	if !ok {
		t.Fatalf("server connection is %T, want *stopOnCloseNetConn", codec.conn.NetConn())
	}
	if err := wrapped.Conn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	awaitClosed(t, codec.closed(), "server codec closed after a ping write failed")
	select {
	case <-served:
	case <-time.After(teardownTestTimeout):
		t.Fatal("ServeCodec did not return: the ping goroutine joined itself through the re-entered Stop")
	}
}
