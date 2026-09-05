package node

import (
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/inconshreveable/log15"

	rpc "github.com/zenon-network/go-zenon/rpc/server"
)

// defaultsProbe is the only API registered by these tests so a successful
// JSON-RPC round trip proves the handler is live.
type defaultsProbe struct{}

func (defaultsProbe) Ping() string { return "pong" }

// startDefaultsNode starts the RPC stack with the shipped defaults, except
// that ports are ephemeral so the test never collides with a running node.
func startDefaultsNode(t *testing.T, mutate func(*RPCConfig)) *Node {
	t.Helper()
	cfg := DefaultNodeConfig.RPC
	cfg.HTTPPort, cfg.WSPort = 0, 0
	if mutate != nil {
		mutate(&cfg)
	}
	n := &Node{
		config:  &Config{RPC: cfg},
		http:    newHTTPServer(rpc.DefaultHTTPTimeouts),
		ws:      newHTTPServer(rpc.DefaultHTTPTimeouts),
		rpcAPIs: []rpc.API{{Namespace: "probe", Service: defaultsProbe{}, Public: true}},
	}
	if err := n.startRPC(); err != nil {
		t.Fatalf("startRPC: %v", err)
	}
	t.Cleanup(n.stopRPC)
	return n
}

// wsServerOf returns the server that has the WebSocket handler installed,
// whichever one startRPC chose; it fails if there is none.
func wsServerOf(t *testing.T, n *Node) *httpServer {
	t.Helper()
	for _, h := range []*httpServer{n.http, n.ws} {
		if h.wsAllowed() {
			return h
		}
	}
	t.Fatal("WebSocket is not enabled on either server")
	return nil
}

func listenerIP(t *testing.T, h *httpServer) net.IP {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.listener == nil {
		t.Fatal("server has no listener")
	}
	return h.listener.Addr().(*net.TCPAddr).IP
}

func postJSONRPC(t *testing.T, addr string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://"+addr, strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"probe.ping","params":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func dialWS(t *testing.T, addr string, origin string) error {
	t.Helper()
	var header http.Header
	if origin != "" {
		header = http.Header{"Origin": {origin}}
	}
	conn, _, err := (&websocket.Dialer{HandshakeTimeout: 2 * time.Second}).Dial("ws://"+addr, header)
	if err == nil {
		conn.Close()
	}
	return err
}

// The shipped defaults must not expose RPC beyond the local machine or to
// arbitrary browser origins; an operator opts into either explicitly.
func TestDefaultRPCConfigIsLocalOnly(t *testing.T) {
	cfg := DefaultNodeConfig.RPC
	for name, host := range map[string]string{"HTTPHost": cfg.HTTPHost, "WSHost": cfg.WSHost} {
		if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
			t.Errorf("%s defaults to %q, want a loopback address", name, host)
		}
	}
	if !cfg.EnableHTTP || !cfg.EnableWS {
		t.Error("HTTP and WebSocket RPC should stay enabled for local clients")
	}
	if len(cfg.HTTPCors) != 0 {
		t.Errorf("HTTPCors defaults to %v, want none", cfg.HTTPCors)
	}
	if len(cfg.WSOrigins) != 0 {
		t.Errorf("WSOrigins defaults to %v, want none", cfg.WSOrigins)
	}
	if len(cfg.HTTPVirtualHosts) != 1 || cfg.HTTPVirtualHosts[0] != "localhost" {
		t.Errorf("HTTPVirtualHosts defaults to %v, want [localhost]", cfg.HTTPVirtualHosts)
	}
}

// Under the defaults the listeners are loopback-only, HTTP serves local
// hostnames and IP literals but grants no cross-origin access, and the
// WebSocket upgrade accepts local origins and non-browser clients only.
func TestDefaultRPCServesLocalClientsOnly(t *testing.T) {
	n := startDefaultsNode(t, nil)

	if ip := listenerIP(t, n.http); !ip.IsLoopback() {
		t.Fatalf("HTTP bound to %v", ip)
	}
	wsServer := wsServerOf(t, n)
	if ip := listenerIP(t, wsServer); !ip.IsLoopback() {
		t.Fatalf("WS bound to %v", ip)
	}
	httpAddr := n.http.listenAddr()
	wsAddr := wsServer.listenAddr()

	if resp := postJSONRPC(t, httpAddr, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("IP-literal host: status %d", resp.StatusCode)
	}
	if resp := postJSONRPC(t, httpAddr, map[string]string{"Host": "localhost"}); resp.StatusCode != http.StatusOK {
		t.Fatalf("Host localhost: status %d", resp.StatusCode)
	}
	if resp := postJSONRPC(t, httpAddr, map[string]string{"Host": "node.example"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Host node.example: status %d, want 403", resp.StatusCode)
	}

	resp := postJSONRPC(t, httpAddr, map[string]string{"Origin": "https://site.example"})
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("cross-origin request was granted Access-Control-Allow-Origin %q", got)
	}

	if err := dialWS(t, wsAddr, ""); err != nil {
		t.Fatalf("WebSocket without Origin rejected: %v", err)
	}
	if err := dialWS(t, wsAddr, "http://localhost"); err != nil {
		t.Fatalf("WebSocket from http://localhost rejected: %v", err)
	}
	if err := dialWS(t, wsAddr, "https://site.example"); err == nil {
		t.Fatal("WebSocket from https://site.example accepted")
	}
}

// captureWarnings routes the node logger through a recorder for the test.
func captureWarnings(t *testing.T) func() []string {
	t.Helper()
	var msgs []string
	old := log.GetHandler()
	log.SetHandler(log15.FuncHandler(func(r *log15.Record) error {
		if r.Lvl == log15.LvlWarn {
			msgs = append(msgs, r.Msg)
		}
		return nil
	}))
	t.Cleanup(func() { log.SetHandler(old) })
	return func() []string { return msgs }
}

func hasWarning(msgs []string, substr string) bool {
	for _, m := range msgs {
		if strings.Contains(m, substr) {
			return true
		}
	}
	return false
}

// Listeners that reach beyond the local machine are announced once they are
// up, based on what is actually bound rather than on the configuration; the
// defaults are silent.
func TestRPCExposureWarning(t *testing.T) {
	t.Run("defaults are silent", func(t *testing.T) {
		got := captureWarnings(t)
		n := startDefaultsNode(t, nil)
		n.warnRPCExposure()
		if msgs := got(); len(msgs) != 0 {
			t.Fatalf("unexpected warnings: %v", msgs)
		}
	})
	t.Run("non-loopback binds and wildcard origins warn", func(t *testing.T) {
		got := captureWarnings(t)
		n := startDefaultsNode(t, func(cfg *RPCConfig) {
			cfg.HTTPHost, cfg.WSHost = "0.0.0.0", "0.0.0.0"
			cfg.HTTPCors, cfg.WSOrigins = []string{"*"}, []string{"*"}
		})
		n.warnRPCExposure()
		msgs := got()
		for _, want := range []string{"HTTP-RPC", "WS-RPC"} {
			if !hasWarning(msgs, want+" listens on a non-loopback address") {
				t.Errorf("missing bind warning for %s in %v", want, msgs)
			}
			if !hasWarning(msgs, want+" accepts any browser origin") {
				t.Errorf("missing origin warning for %s in %v", want, msgs)
			}
		}
	})
	t.Run("protocols without a listener do not warn", func(t *testing.T) {
		got := captureWarnings(t)
		n := startDefaultsNode(t, func(cfg *RPCConfig) {
			cfg.HTTPHost, cfg.WSHost = "", ""
			cfg.HTTPCors, cfg.WSOrigins = []string{"*"}, []string{"*"}
		})
		n.warnRPCExposure()
		if msgs := got(); len(msgs) != 0 {
			t.Fatalf("unexpected warnings: %v", msgs)
		}
	})
	t.Run("a public listener warns whatever the flags say", func(t *testing.T) {
		got := captureWarnings(t)
		n := startDefaultsNode(t, func(cfg *RPCConfig) {
			cfg.EnableHTTP, cfg.HTTPHost = false, "0.0.0.0"
			cfg.EnableWS, cfg.WSHost = false, "0.0.0.0"
		})
		n.warnRPCExposure()
		msgs := got()
		// Whether these listeners exist depends on whether startRPC honors
		// the enable flags; the warning must track the listener either way.
		bound := map[string]bool{}
		for _, server := range []*httpServer{n.http, n.ws} {
			addr, cors, origins := server.exposure()
			if addr != nil && cors != nil {
				bound["HTTP-RPC"] = true
			}
			if addr != nil && origins != nil {
				bound["WS-RPC"] = true
			}
		}
		for _, proto := range []string{"HTTP-RPC", "WS-RPC"} {
			if bound[proto] != hasWarning(msgs, proto+" listens on a non-loopback address") {
				t.Errorf("%s: listener bound=%v but warnings were %v", proto, bound[proto], msgs)
			}
		}
	})
}
