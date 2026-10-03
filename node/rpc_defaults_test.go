package node

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"syscall"
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

const defaultsPingRequest = `{"jsonrpc":"2.0","id":3,"method":"probe.ping","params":[]}`

// requirePong fails the test unless raw is a successful reply to
// defaultsPingRequest: same id, "pong" result, no error.
func requirePong(t *testing.T, what string, raw []byte) {
	t.Helper()
	var resp struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("%s: undecodable response %q: %v", what, raw, err)
	}
	if string(resp.ID) != "3" {
		t.Fatalf("%s: response id = %s, want 3", what, resp.ID)
	}
	if len(resp.Error) != 0 && string(resp.Error) != "null" {
		t.Fatalf("%s: unexpected RPC error %s", what, resp.Error)
	}
	if string(resp.Result) != `"pong"` {
		t.Fatalf("%s: result = %s, want \"pong\"", what, resp.Result)
	}
}

// defaultsAddrInUse reports whether a listen failure was an address-in-use
// error (Unix EADDRINUSE or Winsock 10048).
func defaultsAddrInUse(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	return errno == syscall.EADDRINUSE || errno == 10048
}

// defaultsFreePort returns a loopback TCP port that was free a moment ago.
func defaultsFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// listenerLayout decides whether the two protocols share one listener or
// get one each. startRPC shares the HTTP listener when both protocols are
// configured on the same port, which is what happens when both ports are
// zero, so a distinct-listener run has to pick a concrete WebSocket port.
type listenerLayout int

const (
	sharedListener listenerLayout = iota
	distinctListeners
)

func (l listenerLayout) String() string {
	if l == sharedListener {
		return "shared listener"
	}
	return "distinct listeners"
}

var listenerLayouts = []listenerLayout{sharedListener, distinctListeners}

// startDefaultsNode starts the RPC stack with the shipped defaults, except
// that ports are ephemeral so the test never collides with a running node.
// mutate, if given, adjusts the configuration before startup and is called
// again on each retry. A distinct-listener run picks a fresh WebSocket port
// per attempt and retries when another process took it in the meantime.
func startDefaultsNode(t *testing.T, layout listenerLayout, mutate func(*RPCConfig)) *Node {
	t.Helper()
	for attempt := 0; attempt < 10; attempt++ {
		cfg := DefaultNodeConfig.RPC
		cfg.HTTPPort, cfg.WSPort = 0, 0
		if layout == distinctListeners {
			cfg.WSPort = defaultsFreePort(t)
		}
		if mutate != nil {
			mutate(&cfg)
		}
		n := &Node{
			config:  &Config{RPC: cfg},
			http:    newHTTPServer(rpc.DefaultHTTPTimeouts),
			ws:      newHTTPServer(rpc.DefaultHTTPTimeouts),
			rpcAPIs: []rpc.API{{Namespace: "probe", Service: defaultsProbe{}, Public: true}},
		}
		err := n.startRPC()
		if err == nil {
			t.Cleanup(n.stopRPC)
			return n
		}
		n.stopRPC()
		if layout != distinctListeners || !defaultsAddrInUse(err) {
			t.Fatalf("startRPC: %v", err)
		}
	}
	t.Fatal("startRPC: the chosen WebSocket port was taken on every attempt")
	return nil
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

// postJSONRPC sends a ping over HTTP with the given extra headers; a "Host"
// entry replaces the request's Host header. It returns the response with its
// body already read.
func postJSONRPC(t *testing.T, addr string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://"+addr, strings.NewReader(defaultsPingRequest))
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
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, body
}

// requireHTTPPong asserts that a ping with the given headers is answered.
func requireHTTPPong(t *testing.T, addr string, headers map[string]string) {
	t.Helper()
	resp, body := postJSONRPC(t, addr, headers)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST with %v: status %d, body %q", headers, resp.StatusCode, body)
	}
	requirePong(t, "POST with "+describeHeaders(headers), body)
}

func describeHeaders(headers map[string]string) string {
	if len(headers) == 0 {
		return "no extra headers"
	}
	parts := make([]string, 0, len(headers))
	for k, v := range headers {
		parts = append(parts, k+": "+v)
	}
	return strings.Join(parts, ", ")
}

// originHeader builds the handshake headers for a browser-style client. The
// value may be empty, which sends the header with no value; a nil header
// (no Origin at all) is what non-browser clients send.
func originHeader(origin string) http.Header {
	return http.Header{"Origin": {origin}}
}

// dialWS attempts a WebSocket handshake with the given headers and, if it
// succeeds, a JSON-RPC ping over the connection. It returns the handshake
// error, so a refused upgrade is reported as such and an accepted one is
// proven live.
func dialWS(t *testing.T, addr string, header http.Header) error {
	t.Helper()
	origin := "<none>"
	if header != nil {
		origin = header.Get("Origin")
	}
	conn, _, err := (&websocket.Dialer{HandshakeTimeout: 2 * time.Second}).Dial("ws://"+addr, header)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if err := conn.WriteMessage(websocket.TextMessage, []byte(defaultsPingRequest)); err != nil {
		t.Fatalf("WS write (origin %q): %v", origin, err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("WS read (origin %q): %v", origin, err)
	}
	requirePong(t, "WS ping (origin "+origin+")", msg)
	return nil
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

// Under the defaults every listener is loopback-only, HTTP answers local
// hostnames and IP literals with a decoded result but issues no cross-origin
// grant, and WebSocket answers non-browser clients and local origins. The
// checks run once with both protocols on one listener and once with a
// listener each, so each server's binding is inspected on its own.
func TestDefaultRPCServesLocalClientsOnly(t *testing.T) {
	for _, layout := range listenerLayouts {
		t.Run(layout.String(), func(t *testing.T) {
			n := startDefaultsNode(t, layout, nil)

			wsServer := wsServerOf(t, n)
			if layout == distinctListeners {
				if wsServer != n.ws {
					t.Fatal("WebSocket was not given its own server")
				}
				if n.http.wsAllowed() {
					t.Fatal("HTTP server also has the WebSocket handler")
				}
				if n.http.listenAddr() == n.ws.listenAddr() {
					t.Fatalf("both servers report listener %s", n.http.listenAddr())
				}
			} else if wsServer != n.http {
				t.Fatal("WebSocket did not share the HTTP listener")
			}
			if ip := listenerIP(t, n.http); !ip.IsLoopback() {
				t.Fatalf("HTTP bound to %v", ip)
			}
			if ip := listenerIP(t, wsServer); !ip.IsLoopback() {
				t.Fatalf("WS bound to %v", ip)
			}
			httpAddr := n.http.listenAddr()
			wsAddr := wsServer.listenAddr()

			requireHTTPPong(t, httpAddr, nil)
			for _, host := range []string{"localhost", "localhost:35997", "127.0.0.1:35997"} {
				requireHTTPPong(t, httpAddr, map[string]string{"Host": host})
			}
			for _, host := range []string{"node.example", "node.example:35997"} {
				if resp, body := postJSONRPC(t, httpAddr, map[string]string{"Host": host}); resp.StatusCode != http.StatusForbidden {
					t.Fatalf("Host %s: status %d, body %q, want 403", host, resp.StatusCode, body)
				}
			}

			// Without a CORS list the middleware is not installed, so no
			// origin, local or foreign, is granted a cross-origin read.
			for _, origin := range []string{"https://site.example", "http://localhost:3000", "http://localhost"} {
				resp, body := postJSONRPC(t, httpAddr, map[string]string{"Origin": origin})
				if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
					t.Fatalf("origin %s was granted Access-Control-Allow-Origin %q", origin, got)
				}
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("origin %s: status %d, body %q", origin, resp.StatusCode, body)
				}
				requirePong(t, "POST with Origin "+origin, body)
			}

			if err := dialWS(t, wsAddr, nil); err != nil {
				t.Fatalf("WebSocket without Origin rejected: %v", err)
			}
			if err := dialWS(t, wsAddr, originHeader("http://localhost")); err != nil {
				t.Fatalf("WebSocket from http://localhost rejected: %v", err)
			}
			if err := dialWS(t, wsAddr, originHeader("https://site.example")); err == nil {
				t.Fatal("WebSocket from https://site.example accepted")
			}
		})
	}
}

// With no WebSocket origin list the validator falls back to two rules,
// http://localhost and http://<machine hostname>, that fix the scheme and
// hostname but not the port; clients that send no Origin header are not
// checked at all. This table documents exactly what that admits.
func TestDefaultWSOriginPolicy(t *testing.T) {
	type originCase struct {
		name    string
		header  http.Header
		allowed bool
	}
	browser := func(origin string, allowed bool) originCase {
		return originCase{origin, originHeader(origin), allowed}
	}
	cases := []originCase{
		{"no Origin header", nil, true}, // wallets, CLI tools, SDKs
		{"empty Origin header", originHeader(""), false},
		browser("http://localhost", true),
		browser("http://localhost:3000", true), // fallback rules carry no port
		browser("HTTP://LocalHost:3000", true), // origins are compared case-insensitively
		browser("http://127.0.0.1", false),     // the loopback IP literal is not localhost
		browser("http://127.0.0.1:3000", false),
		browser("https://localhost", false), // the fallback rule is http only
		browser("https://localhost:3000", false),
		browser("http://localhost.example", false), // whole-hostname match only
		browser("https://site.example", false),
		browser("null", false), // opaque origin (sandboxed iframe, file://)
	}
	machine := ""
	if hostname, err := os.Hostname(); err == nil {
		machine = strings.ToLower(hostname)
		if u, err := url.Parse("http://" + machine); err == nil && u.Hostname() == machine {
			cases = append(cases,
				browser("http://"+hostname+":8080", true),
				browser("https://"+hostname, false),
			)
		}
	}

	n := startDefaultsNode(t, distinctListeners, nil)
	wsAddr := wsServerOf(t, n).listenAddr()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// A rejected http origin is admitted after all on a machine
			// whose hostname happens to equal it.
			if !c.allowed && c.header != nil {
				if u, err := url.Parse(strings.ToLower(c.header.Get("Origin"))); err == nil && u.Scheme == "http" && u.Hostname() == machine {
					t.Skipf("the machine hostname is %q, which the fallback admits", machine)
				}
			}
			err := dialWS(t, wsAddr, c.header)
			switch {
			case c.allowed && err != nil:
				t.Fatalf("%s rejected: %v", c.name, err)
			case !c.allowed && err == nil:
				t.Fatalf("%s accepted", c.name)
			}
		})
	}
}

// warning is one warning-level record: its message and its key/value
// context rendered as strings.
type warning struct {
	msg string
	ctx map[string]string
}

func (w warning) String() string { return w.msg + " " + fmt.Sprint(w.ctx) }

// captureWarnings routes the node logger through a recorder for the test.
// The recorder is locked because the logger is process-global and a server
// goroutine may log while the test reads.
func captureWarnings(t *testing.T) func() []warning {
	t.Helper()
	var (
		mu   sync.Mutex
		msgs []warning
	)
	old := log.GetHandler()
	log.SetHandler(log15.FuncHandler(func(r *log15.Record) error {
		if r.Lvl != log15.LvlWarn {
			return nil
		}
		w := warning{msg: r.Msg, ctx: map[string]string{}}
		for i := 0; i+1 < len(r.Ctx); i += 2 {
			w.ctx[fmt.Sprint(r.Ctx[i])] = fmt.Sprint(r.Ctx[i+1])
		}
		mu.Lock()
		msgs = append(msgs, w)
		mu.Unlock()
		return nil
	}))
	t.Cleanup(func() { log.SetHandler(old) })
	return func() []warning {
		mu.Lock()
		defer mu.Unlock()
		return append([]warning(nil), msgs...)
	}
}

const (
	httpBindWarning   = "HTTP-RPC listens on a non-loopback address"
	httpOriginWarning = "HTTP-RPC accepts any browser origin"
	wsBindWarning     = "WS-RPC listens on a non-loopback address"
	wsOriginWarning   = "WS-RPC accepts any browser origin"
)

func countWarnings(msgs []warning, substr string) int {
	n := 0
	for _, m := range msgs {
		if strings.Contains(m.msg, substr) {
			n++
		}
	}
	return n
}

// exposureWarnings is the number of bind and wildcard-origin warnings a
// configuration is expected to produce for each protocol.
type exposureWarnings struct {
	httpBind, httpOrigin, wsBind, wsOrigin int
}

func requireExposureWarnings(t *testing.T, msgs []warning, want exposureWarnings) {
	t.Helper()
	got := exposureWarnings{
		httpBind:   countWarnings(msgs, httpBindWarning),
		httpOrigin: countWarnings(msgs, httpOriginWarning),
		wsBind:     countWarnings(msgs, wsBindWarning),
		wsOrigin:   countWarnings(msgs, wsOriginWarning),
	}
	if got != want {
		t.Fatalf("exposure warnings = %+v, want %+v; messages: %v", got, want, msgs)
	}
	if extra := len(msgs) - (got.httpBind + got.httpOrigin + got.wsBind + got.wsOrigin); extra != 0 {
		t.Fatalf("%d unexpected warnings in %v", extra, msgs)
	}
}

// defaultsBoundAddr returns the server's live listener address, or nil when it is
// not listening.
func defaultsBoundAddr(h *httpServer) *net.TCPAddr {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.listener == nil {
		return nil
	}
	addr, _ := h.listener.Addr().(*net.TCPAddr)
	return addr
}

// requireBindEndpoints asserts that each bind warning names the listener of
// the server that carries that protocol's handler, whichever server that is.
func requireBindEndpoints(t *testing.T, n *Node, msgs []warning) {
	t.Helper()
	for _, server := range []*httpServer{n.http, n.ws} {
		addr := defaultsBoundAddr(server)
		if addr == nil || addr.IP.IsLoopback() {
			continue
		}
		if server.rpcAllowed() {
			requireWarningEndpoint(t, msgs, httpBindWarning, addr.String())
		}
		if server.wsAllowed() {
			requireWarningEndpoint(t, msgs, wsBindWarning, addr.String())
		}
	}
}

// requireWarningEndpoint asserts that the single warning containing substr
// names addr as its endpoint.
func requireWarningEndpoint(t *testing.T, msgs []warning, substr, addr string) {
	t.Helper()
	for _, m := range msgs {
		if strings.Contains(m.msg, substr) {
			if m.ctx["endpoint"] != addr {
				t.Fatalf("%q names endpoint %q, want %q", m.msg, m.ctx["endpoint"], addr)
			}
			return
		}
	}
	t.Fatalf("no warning containing %q in %v", substr, msgs)
}

// Listeners that reach beyond the local machine are announced once they are
// up, based on what is actually bound rather than on the configuration: one
// warning per protocol per live non-loopback listener and one per wildcard
// origin list, whether the protocols share a listener or have one each. The
// defaults are silent.
func TestRPCExposureWarning(t *testing.T) {
	wildcard := func(cfg *RPCConfig) { cfg.HTTPCors, cfg.WSOrigins = []string{"*"}, []string{"*"} }
	public := func(cfg *RPCConfig) { cfg.HTTPHost, cfg.WSHost = "0.0.0.0", "0.0.0.0" }
	both := func(fns ...func(*RPCConfig)) func(*RPCConfig) {
		return func(cfg *RPCConfig) {
			for _, fn := range fns {
				fn(cfg)
			}
		}
	}

	cases := []struct {
		name   string
		mutate func(*RPCConfig)
		want   exposureWarnings
		// distinctOnly marks cases whose two hosts differ; on a shared
		// port there is only one bind, so they have no shared reading.
		distinctOnly bool
	}{
		{"defaults are silent", nil, exposureWarnings{}, false},
		{"non-loopback binds warn per protocol", public,
			exposureWarnings{httpBind: 1, wsBind: 1}, false},
		{"wildcard origins warn per protocol on a loopback bind", wildcard,
			exposureWarnings{httpOrigin: 1, wsOrigin: 1}, false},
		{"non-loopback binds with wildcard origins warn for both", both(public, wildcard),
			exposureWarnings{httpBind: 1, httpOrigin: 1, wsBind: 1, wsOrigin: 1}, false},
		{"explicit origin lists on a non-loopback bind warn for the bind only",
			both(public, func(cfg *RPCConfig) {
				cfg.HTTPCors, cfg.WSOrigins = []string{"https://wallet.example"}, []string{"https://wallet.example"}
			}),
			exposureWarnings{httpBind: 1, wsBind: 1}, false},
		{"a wildcard on one protocol warns for that protocol only",
			func(cfg *RPCConfig) {
				cfg.HTTPCors, cfg.WSOrigins = []string{"*"}, []string{"https://wallet.example"}
			},
			exposureWarnings{httpOrigin: 1}, false},
		{"HTTP only", both(public, wildcard, func(cfg *RPCConfig) { cfg.WSHost = "" }),
			exposureWarnings{httpBind: 1, httpOrigin: 1}, false},
		{"WS only", both(public, wildcard, func(cfg *RPCConfig) { cfg.HTTPHost = "" }),
			exposureWarnings{wsBind: 1, wsOrigin: 1}, false},
		{"protocols without a listener do not warn",
			both(wildcard, func(cfg *RPCConfig) { cfg.HTTPHost, cfg.WSHost = "", "" }),
			exposureWarnings{}, false},
		{"a non-loopback HTTP bind beside a loopback WS bind warns for HTTP only",
			both(wildcard, func(cfg *RPCConfig) { cfg.HTTPHost = "0.0.0.0" }),
			exposureWarnings{httpBind: 1, httpOrigin: 1, wsOrigin: 1}, true},
		{"a non-loopback WS bind beside a loopback HTTP bind warns for WS only",
			func(cfg *RPCConfig) { cfg.WSHost = "0.0.0.0" },
			exposureWarnings{wsBind: 1}, true},
	}
	for _, layout := range listenerLayouts {
		for _, c := range cases {
			if c.distinctOnly && layout != distinctListeners {
				continue
			}
			t.Run(layout.String()+"/"+c.name, func(t *testing.T) {
				got := captureWarnings(t)
				n := startDefaultsNode(t, layout, c.mutate)
				n.warnRPCExposure()
				msgs := got()
				requireExposureWarnings(t, msgs, c.want)
				requireBindEndpoints(t, n, msgs)
			})
		}
	}

	t.Run("distinct listeners are each named by their own warning", func(t *testing.T) {
		got := captureWarnings(t)
		n := startDefaultsNode(t, distinctListeners, public)
		n.warnRPCExposure()
		msgs := got()
		requireExposureWarnings(t, msgs, exposureWarnings{httpBind: 1, wsBind: 1})
		requireWarningEndpoint(t, msgs, httpBindWarning, n.http.listenAddr())
		requireWarningEndpoint(t, msgs, wsBindWarning, n.ws.listenAddr())
		if n.http.listenAddr() == n.ws.listenAddr() {
			t.Fatalf("both servers report listener %s", n.http.listenAddr())
		}
	})

	t.Run("a shared listener is named by both warnings", func(t *testing.T) {
		got := captureWarnings(t)
		n := startDefaultsNode(t, sharedListener, public)
		n.warnRPCExposure()
		msgs := got()
		requireExposureWarnings(t, msgs, exposureWarnings{httpBind: 1, wsBind: 1})
		requireWarningEndpoint(t, msgs, httpBindWarning, n.http.listenAddr())
		requireWarningEndpoint(t, msgs, wsBindWarning, n.http.listenAddr())
		if n.ws.listenAddr() != "" {
			t.Fatalf("the WS server was started on %s although the port is shared", n.ws.listenAddr())
		}
	})

	t.Run("WS only starts one listener with no HTTP handler", func(t *testing.T) {
		n := startDefaultsNode(t, distinctListeners, func(cfg *RPCConfig) { cfg.HTTPHost = "" })
		wsServer := wsServerOf(t, n)
		listeners := 0
		for _, server := range []*httpServer{n.http, n.ws} {
			if server.rpcAllowed() {
				t.Fatal("an HTTP handler is installed although the HTTP host is empty")
			}
			if addr, _, _ := server.exposure(); addr != nil {
				listeners++
			}
		}
		if listeners != 1 {
			t.Fatalf("%d listeners started, want 1", listeners)
		}
		if err := dialWS(t, wsServer.listenAddr(), nil); err != nil {
			t.Fatalf("WebSocket rejected: %v", err)
		}
	})

	t.Run("a public listener warns whatever the flags say", func(t *testing.T) {
		got := captureWarnings(t)
		n := startDefaultsNode(t, sharedListener, func(cfg *RPCConfig) {
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
			if bound[proto] != (countWarnings(msgs, proto+" listens on a non-loopback address") == 1) {
				t.Errorf("%s: listener bound=%v but warnings were %v", proto, bound[proto], msgs)
			}
		}
	})
}
