package node

import (
	"encoding/json"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	rpc "github.com/zenon-network/go-zenon/rpc/server"
)

// capHeldWait is how long a connection beyond a listener's cap is watched
// to confirm that it is not served while the cap is held.
const capHeldWait = 300 * time.Millisecond

// The node caps the dedicated WebSocket listener by default and leaves the
// per-IP limit off: behind a reverse proxy every client shares the proxy's
// address, so a per-IP default would act as a low total limit there.
func TestRPCConfigDefaultWSConnectionLimits(t *testing.T) {
	if got := DefaultNodeConfig.RPC.MaxWSConnections; got != 1024 || DefaultMaxWSConnections != 1024 {
		t.Fatalf("default MaxWSConnections is %d (constant %d), want 1024", got, DefaultMaxWSConnections)
	}
	if got := DefaultNodeConfig.RPC.MaxWSConnectionsPerIP; got != 0 {
		t.Fatalf("default MaxWSConnectionsPerIP is %d, want 0 (off)", got)
	}
}

// Zero selects the default connection cap and a negative value removes it,
// as for the HTTP listener.
func TestEffectiveMaxWSConnections(t *testing.T) {
	for _, tc := range []struct{ in, want int }{
		{0, DefaultMaxWSConnections},
		{-1, 0},
		{1, 1},
		{5000, 5000},
	} {
		if got := effectiveMaxWSConnections(tc.in); got != tc.want {
			t.Errorf("effectiveMaxWSConnections(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// startWSCapServer starts a server on an ephemeral loopback port with
// WebSocket enabled under ws and, when httpCfg is not nil, HTTP JSON-RPC on
// the same listener under httpCfg.
func startWSCapServer(t *testing.T, ws wsConfig, httpCfg *httpConfig) *httpServer {
	t.Helper()
	h := newHTTPServer(rpc.DefaultHTTPTimeouts)
	if err := h.setListenAddr("127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if httpCfg != nil {
		cfg := *httpCfg
		cfg.Modules = []string{"probe"}
		if err := h.enableRPC(stackAPIs, cfg); err != nil {
			t.Fatal(err)
		}
	}
	ws.Modules = []string{"probe"}
	if err := h.enableWS(stackAPIs, ws); err != nil {
		t.Fatal(err)
	}
	if err := h.start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.stop)
	return h
}

// listenerCap returns the connection cap the running listener was given.
func listenerCap(h *httpServer) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.maxConns
}

// wsDialResult is the outcome of a WebSocket handshake run in the
// background.
type wsDialResult struct {
	conn *websocket.Conn
	err  error
}

// dialWSInBackground starts a WebSocket handshake with the server and
// returns a channel that delivers its outcome. The handshake waits up to
// stackTestTimeout, so one that is held in the accept queue stays pending
// rather than failing. The test waits for the handshake to end before it
// finishes and closes a connection it opened.
func dialWSInBackground(t *testing.T, h *httpServer) <-chan wsDialResult {
	t.Helper()
	addr := h.listenAddr()
	result := make(chan wsDialResult, 1)
	var opened *websocket.Conn // written before done is closed
	done := make(chan struct{})
	go func() {
		defer close(done)
		dialer := websocket.Dialer{HandshakeTimeout: stackTestTimeout}
		conn, _, err := dialer.Dial("ws://"+addr, nil)
		opened = conn
		result <- wsDialResult{conn, err}
	}()
	t.Cleanup(func() {
		<-done
		if opened != nil {
			_ = opened.Close()
		}
	})
	return result
}

// requireNotServed fails the test if the pending handshake completes while
// the cap is held.
func requireNotServed(t *testing.T, pending <-chan wsDialResult, what string) {
	t.Helper()
	select {
	case r := <-pending:
		t.Fatalf("%s: handshake finished while the cap was held (err %v)", what, r.err)
	case <-time.After(capHeldWait):
	}
}

// requireServed fails the test unless the pending handshake completes and
// the connection answers a JSON-RPC call.
func requireServed(t *testing.T, pending <-chan wsDialResult, what string) *websocket.Conn {
	t.Helper()
	select {
	case r := <-pending:
		if r.err != nil {
			t.Fatalf("%s: handshake failed: %v", what, r.err)
		}
		req := []byte(`{"jsonrpc":"2.0","id":1,"method":"probe.ping","params":[]}`)
		if err := r.conn.WriteMessage(websocket.TextMessage, req); err != nil {
			t.Fatalf("%s: write: %v", what, err)
		}
		if err := r.conn.SetReadDeadline(time.Now().Add(stackTestTimeout)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := r.conn.ReadMessage(); err != nil {
			t.Fatalf("%s: read: %v", what, err)
		}
		return r.conn
	case <-time.After(stackTestTimeout):
		t.Fatalf("%s: handshake still pending", what)
		return nil
	}
}

// A listener that serves only WebSocket holds at most MaxConnections
// connections. One beyond the cap is not served while an idle WebSocket
// holds the cap, and is served once that connection closes.
func TestWSMaxConnectionsCapsDedicatedListener(t *testing.T) {
	h := startWSCapServer(t, wsConfig{MaxConnections: 1}, nil)
	if got := listenerCap(h); got != 1 {
		t.Fatalf("listener cap is %d, want 1", got)
	}

	// first answers one call and then stays open and idle: it keeps the
	// only slot.
	first := dialStackWS(t, h)

	second := dialWSInBackground(t, h)
	requireNotServed(t, second, "second connection")

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	requireServed(t, second, "second connection after the first closed")
}

// A connection that has not upgraded holds a slot of the cap as well: the
// cap bounds what the listener has accepted, not only established
// WebSocket connections.
func TestWSMaxConnectionsCountsConnectionsBeforeUpgrade(t *testing.T) {
	h := startWSCapServer(t, wsConfig{MaxConnections: 1}, nil)

	slow, err := net.Dial("tcp", h.listenAddr())
	if err != nil {
		t.Fatal(err)
	}
	defer slow.Close()
	if _, err := slow.Write([]byte("GET / HTTP/1.1\r\nHost: 127.0.0.1\r\n")); err != nil {
		t.Fatal(err)
	}

	second := dialWSInBackground(t, h)
	requireNotServed(t, second, "connection behind a slow-header connection")

	slow.Close()
	requireServed(t, second, "connection after the slow one closed")
}

// A WebSocket-only server configured with a zero cap runs with the default
// one: with the default lowered to one, a second connection is not served
// while a first holds the cap.
func TestWSMaxConnectionsZeroSelectsDefault(t *testing.T) {
	saved := defaultMaxWSConnections
	defaultMaxWSConnections = 1
	defer func() { defaultMaxWSConnections = saved }()

	h := startWSCapServer(t, wsConfig{}, nil)
	if got := listenerCap(h); got != 1 {
		t.Fatalf("listener cap is %d, want the default (lowered to 1)", got)
	}

	first := dialStackWS(t, h)
	second := dialWSInBackground(t, h)
	requireNotServed(t, second, "zero configuration left the listener uncapped: second connection")

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	requireServed(t, second, "second connection after the first closed")
}

// With a negative MaxConnections the WebSocket listener is not capped, also
// when the default would be one. Three connections held open at once are
// all served.
func TestWSMaxConnectionsNegativeIsUncapped(t *testing.T) {
	saved := defaultMaxWSConnections
	defaultMaxWSConnections = 1
	defer func() { defaultMaxWSConnections = saved }()

	h := startWSCapServer(t, wsConfig{MaxConnections: -1}, nil)
	if got := listenerCap(h); got != 0 {
		t.Fatalf("listener cap is %d, want 0 (uncapped)", got)
	}
	for i := 0; i < 3; i++ {
		dialStackWS(t, h) // stays open until the test ends
	}
}

// When WebSocket shares the HTTP port the listener keeps the HTTP cap,
// which counts WebSocket connections too; the WebSocket cap is not used
// there, whether it is lower or higher.
func TestWSMaxConnectionsSharedPortKeepsHTTPCap(t *testing.T) {
	t.Run("lower WebSocket cap does not apply", func(t *testing.T) {
		h := startWSCapServer(t, wsConfig{MaxConnections: 1}, &httpConfig{MaxConnections: 2})
		if got := listenerCap(h); got != 2 {
			t.Fatalf("listener cap is %d, want the HTTP cap 2", got)
		}
		first := dialStackWS(t, h)
		dialStackWS(t, h) // a second WebSocket is served: the cap is 2, not 1

		// The two WebSocket connections fill the HTTP cap.
		third := dialWSInBackground(t, h)
		requireNotServed(t, third, "third connection on the shared port")
		if err := first.Close(); err != nil {
			t.Fatal(err)
		}
		requireServed(t, third, "third connection after the first closed")
	})
	t.Run("higher WebSocket cap does not apply", func(t *testing.T) {
		h := startWSCapServer(t, wsConfig{MaxConnections: 5}, &httpConfig{MaxConnections: 1})
		if got := listenerCap(h); got != 1 {
			t.Fatalf("listener cap is %d, want the HTTP cap 1", got)
		}
		first := dialStackWS(t, h)
		second := dialWSInBackground(t, h)
		requireNotServed(t, second, "second connection on the shared port")
		if err := first.Close(); err != nil {
			t.Fatal(err)
		}
		requireServed(t, second, "second connection after the first closed")
	})
	t.Run("zero HTTP cap selects the HTTP default", func(t *testing.T) {
		h := startWSCapServer(t, wsConfig{MaxConnections: 1}, &httpConfig{})
		if got := listenerCap(h); got != DefaultMaxHTTPConnections {
			t.Fatalf("listener cap is %d, want the HTTP default %d", got, DefaultMaxHTTPConnections)
		}
	})
}

// startRPC gives RPC.MaxWSConnections to the dedicated WebSocket listener
// and RPC.MaxHTTPConnections to the HTTP one. On a shared port the one
// listener takes the HTTP setting.
func TestStartRPCAppliesWSConnectionCap(t *testing.T) {
	t.Run("defaults on distinct listeners", func(t *testing.T) {
		n := startDefaultsNode(t, distinctListeners, nil)
		if got := listenerCap(n.ws); got != DefaultMaxWSConnections {
			t.Fatalf("WebSocket listener cap is %d, want the default %d", got, DefaultMaxWSConnections)
		}
		if got := listenerCap(n.http); got != DefaultMaxHTTPConnections {
			t.Fatalf("HTTP listener cap is %d, want the default %d", got, DefaultMaxHTTPConnections)
		}
	})
	t.Run("explicit settings on distinct listeners", func(t *testing.T) {
		n := startDefaultsNode(t, distinctListeners, func(cfg *RPCConfig) {
			cfg.MaxWSConnections = 7
			cfg.MaxHTTPConnections = 9
		})
		if got := listenerCap(n.ws); got != 7 {
			t.Fatalf("WebSocket listener cap is %d, want 7", got)
		}
		if got := listenerCap(n.http); got != 9 {
			t.Fatalf("HTTP listener cap is %d, want 9", got)
		}
	})
	t.Run("zero and negative on distinct listeners", func(t *testing.T) {
		n := startDefaultsNode(t, distinctListeners, func(cfg *RPCConfig) { cfg.MaxWSConnections = 0 })
		if got := listenerCap(n.ws); got != DefaultMaxWSConnections {
			t.Fatalf("zero: WebSocket listener cap is %d, want the default %d", got, DefaultMaxWSConnections)
		}
		n = startDefaultsNode(t, distinctListeners, func(cfg *RPCConfig) { cfg.MaxWSConnections = -1 })
		if got := listenerCap(n.ws); got != 0 {
			t.Fatalf("negative: WebSocket listener cap is %d, want 0 (uncapped)", got)
		}
	})
	t.Run("shared listener takes the HTTP setting", func(t *testing.T) {
		n := startDefaultsNode(t, sharedListener, func(cfg *RPCConfig) {
			cfg.MaxWSConnections = 7
			cfg.MaxHTTPConnections = 9
		})
		if wsServerOf(t, n) != n.http {
			t.Fatal("WebSocket did not share the HTTP listener")
		}
		if got := listenerCap(n.http); got != 9 {
			t.Fatalf("shared listener cap is %d, want the HTTP setting 9", got)
		}
	})
	t.Run("WebSocket without HTTP", func(t *testing.T) {
		n := startDefaultsNode(t, sharedListener, func(cfg *RPCConfig) {
			cfg.EnableHTTP = false
			cfg.MaxWSConnections = 7
			cfg.MaxHTTPConnections = 9
		})
		if wsServerOf(t, n) != n.ws {
			t.Fatal("WebSocket was not given its own server")
		}
		if got := listenerCap(n.ws); got != 7 {
			t.Fatalf("WebSocket listener cap is %d, want 7", got)
		}
	})
}

// A config.json overlays the defaults as the node loads it: a file written
// before the setting existed keeps the default cap, an explicit zero also
// selects it, a positive value is used as is and a negative one removes the
// cap. The per-IP limit stays off unless the file sets it.
func TestRPCConfigJSONMaxWSConnections(t *testing.T) {
	for _, tc := range []struct {
		name          string
		file          string
		want          int // RPC.MaxWSConnections after the overlay
		wantEffective int // the cap a dedicated listener gets
	}{
		{"absent", `{"RPC":{"WSHost":"0.0.0.0","MaxHTTPConnections":2048}}`, DefaultMaxWSConnections, DefaultMaxWSConnections},
		{"zero", `{"RPC":{"WSHost":"0.0.0.0","MaxWSConnections":0}}`, 0, DefaultMaxWSConnections},
		{"set", `{"RPC":{"WSHost":"0.0.0.0","MaxWSConnections":50}}`, 50, 50},
		{"negative", `{"RPC":{"WSHost":"0.0.0.0","MaxWSConnections":-1}}`, -1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultNodeConfig.Clone()
			if err := json.Unmarshal([]byte(tc.file), cfg); err != nil {
				t.Fatal(err)
			}
			if got := cfg.RPC.MaxWSConnections; got != tc.want {
				t.Fatalf("MaxWSConnections is %d, want %d", got, tc.want)
			}
			if got := effectiveMaxWSConnections(cfg.RPC.MaxWSConnections); got != tc.wantEffective {
				t.Fatalf("effective cap is %d, want %d", got, tc.wantEffective)
			}
			if got := cfg.RPC.MaxWSConnectionsPerIP; got != 0 {
				t.Fatalf("MaxWSConnectionsPerIP is %d, want 0", got)
			}

			// The effective configuration survives a round trip through
			// its own JSON form.
			encoded, err := json.Marshal(cfg.RPC)
			if err != nil {
				t.Fatal(err)
			}
			var decoded RPCConfig
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decoded, cfg.RPC) {
				t.Fatalf("round trip changed the RPC configuration:\n got %+v\nwant %+v", decoded, cfg.RPC)
			}
		})
	}
}
