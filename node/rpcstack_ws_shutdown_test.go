package node

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"

	rpc "github.com/zenon-network/go-zenon/rpc/server"
)

// stackProbe is the only API registered by these tests.
type stackProbe struct{}

func (stackProbe) Ping() string { return "pong" }

var stackAPIs = []rpc.API{{Namespace: "probe", Service: stackProbe{}, Public: true}}

// stackTestTimeout bounds the dial and every read. A live connection with
// nothing to deliver reaches it; a closed one returns at once.
const stackTestTimeout = 5 * time.Second

// startStackServer configures an httpServer on an ephemeral loopback port
// with WebSocket enabled and, when withHTTP is set, HTTP JSON-RPC too.
func startStackServer(t *testing.T, withHTTP bool) *httpServer {
	t.Helper()
	h := newHTTPServer(rpc.DefaultHTTPTimeouts)
	if err := h.setListenAddr("127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if withHTTP {
		if err := h.enableRPC(stackAPIs, httpConfig{Modules: []string{"probe"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.enableWS(stackAPIs, wsConfig{Modules: []string{"probe"}}); err != nil {
		t.Fatal(err)
	}
	if err := h.start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.stop)
	return h
}

// dialStackWS opens a WebSocket connection to the server and proves it is
// live with one JSON-RPC round trip.
func dialStackWS(t *testing.T, h *httpServer) *websocket.Conn {
	t.Helper()
	dialer := websocket.Dialer{HandshakeTimeout: stackTestTimeout}
	conn, _, err := dialer.Dial("ws://"+h.listenAddr(), nil)
	if err != nil {
		t.Fatalf("WS dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"probe.ping","params":[]}`)
	if err := conn.WriteMessage(websocket.TextMessage, req); err != nil {
		t.Fatalf("WS write: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(stackTestTimeout)); err != nil {
		t.Fatalf("WS set read deadline: %v", err)
	}
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("WS read before stop: %v", err)
	}
	return conn
}

// Stopping the server must close established WebSocket connections. An
// upgraded connection is hijacked from the HTTP server, so closing the
// listener and shutting down net/http does not reach it; only stopping the
// WebSocket RPC server closes its codecs.
func TestHTTPServerStopClosesWebSocketConnections(t *testing.T) {
	cases := []struct {
		name     string
		withHTTP bool
	}{
		{"ws only", false},
		{"shared with http", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := startStackServer(t, tc.withHTTP)
			conn := dialStackWS(t, h)

			h.stop()

			if h.wsAllowed() {
				t.Error("WebSocket handler still installed after stop")
			}
			if h.rpcAllowed() {
				t.Error("HTTP handler still installed after stop")
			}

			// In this test the server is the only party that can close the loopback
			// connection, and a closed connection surfaces as a read error
			// that is not a timeout. A live one, with nothing to deliver,
			// blocks until the deadline and returns a timeout instead. The
			// client's own socket is still open, so setting the deadline
			// must succeed.
			if err := conn.SetReadDeadline(time.Now().Add(stackTestTimeout)); err != nil {
				t.Fatalf("WS set read deadline after stop: %v", err)
			}
			_, _, err := conn.ReadMessage()
			if err == nil {
				t.Fatal("WebSocket connection still delivering messages after stop")
			}
			if ne, ok := err.(interface{ Timeout() bool }); ok && ne.Timeout() {
				t.Fatalf("WebSocket connection still open after stop: %v", err)
			}
		})
	}
}
