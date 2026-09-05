package node

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	rpc "github.com/zenon-network/go-zenon/rpc/server"
)

// probeService is the only API registered by these tests; it exists so a
// successful JSON-RPC round trip proves the handler is live.
type probeService struct{}

func (probeService) Ping() string { return "pong" }

// newRPCTestNode builds the node state startRPC works on, without the
// wallet, chain or p2p server.
func newRPCTestNode(cfg RPCConfig) *Node {
	return &Node{
		config:  &Config{RPC: cfg},
		http:    newHTTPServer(rpc.DefaultHTTPTimeouts),
		ws:      newHTTPServer(rpc.DefaultHTTPTimeouts),
		rpcAPIs: []rpc.API{{Namespace: "probe", Service: probeService{}, Public: true}},
	}
}

func startRPCTestNode(t *testing.T, cfg RPCConfig) *Node {
	t.Helper()
	n := newRPCTestNode(cfg)
	if err := n.startRPC(); err != nil {
		t.Fatalf("startRPC: %v", err)
	}
	t.Cleanup(n.stopRPC)
	return n
}

// boundAddr returns the address a server is listening on, or "" when it has
// no listener.
func boundAddr(h *httpServer) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.listener == nil {
		return ""
	}
	return h.listener.Addr().String()
}

func httpServes(t *testing.T, addr string) bool {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Post("http://"+addr, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"probe.ping","params":[]}`))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func wsServes(t *testing.T, addr string) bool {
	t.Helper()
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	conn, _, err := dialer.Dial("ws://"+addr, nil)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// A protocol whose enable flag is false must not bind a socket even though
// its host is configured; the flag is the policy and the host only says
// where an enabled protocol listens.
func TestStartRPCHonorsEnableFlags(t *testing.T) {
	cases := []struct {
		name                 string
		enableHTTP, enableWS bool
	}{
		{"both disabled", false, false},
		{"http only", true, false},
		{"ws only", false, true},
		{"both enabled", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := startRPCTestNode(t, RPCConfig{
				EnableHTTP: tc.enableHTTP,
				HTTPHost:   "127.0.0.1",
				HTTPPort:   0,
				EnableWS:   tc.enableWS,
				WSHost:     "127.0.0.1",
				WSPort:     0,
			})

			httpAddr := boundAddr(n.http)
			if tc.enableHTTP {
				if httpAddr == "" || !httpServes(t, httpAddr) {
					t.Fatalf("HTTP enabled but not served (bound %q)", httpAddr)
				}
			} else {
				if httpAddr != "" || n.http.listenAddr() != "" {
					t.Fatalf("HTTP disabled but bound to %q (endpoint %q)", httpAddr, n.http.listenAddr())
				}
			}

			// With HTTP enabled and an ephemeral port on both, WebSocket shares
			// the HTTP server; otherwise it gets its own listener.
			wsServer := n.ws
			if tc.enableHTTP && tc.enableWS {
				wsServer = n.http
			}
			wsAddr := boundAddr(wsServer)
			if tc.enableWS {
				if wsAddr == "" || !wsServes(t, wsAddr) {
					t.Fatalf("WS enabled but not served (bound %q)", wsAddr)
				}
			} else {
				if boundAddr(n.ws) != "" || n.ws.listenAddr() != "" {
					t.Fatalf("WS disabled but bound to %q", boundAddr(n.ws))
				}
				if httpAddr != "" && wsServes(t, httpAddr) {
					t.Fatal("WS disabled but the HTTP listener upgrades WebSocket connections")
				}
			}
		})
	}
}

// The existing empty-host behavior is unchanged: no host, no listener, even
// with the flag set.
func TestStartRPCEmptyHostStaysDisabled(t *testing.T) {
	n := startRPCTestNode(t, RPCConfig{EnableHTTP: true, EnableWS: true})
	if a := boundAddr(n.http); a != "" {
		t.Fatalf("HTTP bound to %q with an empty host", a)
	}
	if a := boundAddr(n.ws); a != "" {
		t.Fatalf("WS bound to %q with an empty host", a)
	}
}

// A disabled protocol must not claim its port either, so another process
// (or the other protocol) can use it.
func TestDisabledProtocolLeavesPortFree(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	n := startRPCTestNode(t, RPCConfig{
		EnableHTTP: false, HTTPHost: "127.0.0.1", HTTPPort: port,
		EnableWS: false, WSHost: "127.0.0.1", WSPort: port,
	})
	_ = n
	l, err = net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("port %d still taken after disabled RPC start: %v", port, err)
	}
	l.Close()
}
