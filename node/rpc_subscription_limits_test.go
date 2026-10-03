package node

import (
	"testing"

	"github.com/zenon-network/go-zenon/rpc/api/subscribe"
	rpc "github.com/zenon-network/go-zenon/rpc/server"
)

// The built-in defaults are the limits the RPC servers used before they
// became configurable.
func TestRPCConfigDefaultSubscriptionLimits(t *testing.T) {
	if got := DefaultNodeConfig.RPC.MaxSubscriptionsPerConn; got != 64 || rpc.DefaultMaxSubscriptionsPerConn != 64 {
		t.Fatalf("default MaxSubscriptionsPerConn is %d, want 64", got)
	}
	if got := DefaultNodeConfig.RPC.MaxSubscriptions; got != 4096 || subscribe.DefaultMaxSubscriptions != 4096 {
		t.Fatalf("default MaxSubscriptions is %d, want 4096", got)
	}
}

// The per-connection limit configured for an endpoint reaches the RPC server
// that serves it, for HTTP and WebSocket alike.
func TestRPCServersTakeConfiguredPerConnectionLimit(t *testing.T) {
	h := newHTTPServer(rpc.DefaultHTTPTimeouts)
	if err := h.enableRPC(nil, httpConfig{MaxSubscriptionsPerConn: 2}); err != nil {
		t.Fatal(err)
	}
	if got := h.httpHandler.Load().(*rpcHandler).server.MaxSubscriptionsPerConn(); got != 2 {
		t.Fatalf("HTTP server limit is %d, want 2", got)
	}
	if err := h.enableWS(nil, wsConfig{MaxSubscriptionsPerConn: 3}); err != nil {
		t.Fatal(err)
	}
	if got := h.wsHandler.Load().(*rpcHandler).server.MaxSubscriptionsPerConn(); got != 3 {
		t.Fatalf("WebSocket server limit is %d, want 3", got)
	}

	// Zero in the config means the default, as it does for an omitted key.
	unset := newHTTPServer(rpc.DefaultHTTPTimeouts)
	if err := unset.enableWS(nil, wsConfig{}); err != nil {
		t.Fatal(err)
	}
	if got := unset.wsHandler.Load().(*rpcHandler).server.MaxSubscriptionsPerConn(); got != rpc.DefaultMaxSubscriptionsPerConn {
		t.Fatalf("unset limit is %d, want the default %d", got, rpc.DefaultMaxSubscriptionsPerConn)
	}
}
