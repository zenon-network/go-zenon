package node

import (
	"strings"
	"testing"

	rpc "github.com/zenon-network/go-zenon/rpc/server"
)

// TestStartRPC_SharedPortHostMismatch verifies that startRPC returns a
// configuration error when HTTP and WebSocket are configured to share a port
// but bind different hosts. See go-zenon issue #99.
func TestStartRPC_SharedPortHostMismatch(t *testing.T) {
	tests := []struct {
		name    string
		rpcCfg  RPCConfig
		wantErr string
	}{
		{
			name: "same port different hosts returns error",
			rpcCfg: RPCConfig{
				HTTPHost: "127.0.0.1",
				HTTPPort: 35997,
				WSHost:   "0.0.0.0",
				WSPort:   35997,
			},
			wantErr: "share port",
		},
		{
			name: "same port same host is allowed",
			rpcCfg: RPCConfig{
				HTTPHost: "127.0.0.1",
				HTTPPort: 0, // port 0 = auto-assign, avoids bind conflicts
				WSHost:   "127.0.0.1",
				WSPort:   0,
			},
			wantErr: "",
		},
		{
			name: "different ports different hosts is allowed",
			rpcCfg: RPCConfig{
				HTTPHost: "127.0.0.1",
				HTTPPort: 0,
				WSHost:   "127.0.0.1",
				WSPort:   0,
			},
			wantErr: "",
		},
		{
			name: "reverse mismatch also returns error",
			rpcCfg: RPCConfig{
				HTTPHost: "0.0.0.0",
				HTTPPort: 35997,
				WSHost:   "127.0.0.1",
				WSPort:   35997,
			},
			wantErr: "share port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &Node{
				config: &Config{RPC: tt.rpcCfg},
				http:   newHTTPServer(rpc.DefaultHTTPTimeouts),
				ws:     newHTTPServer(rpc.DefaultHTTPTimeouts),
			}

			err := node.startRPC()
			// Stop any servers that were started.
			node.stopRPC()

			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("startRPC() unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("startRPC() expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("startRPC() error = %q, want substring %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

// TestWsServerForPort verifies the server selection logic used by startRPC.
func TestWsServerForPort(t *testing.T) {
	tests := []struct {
		name     string
		rpcCfg   RPCConfig
		wantHTTP bool // true means wsServerForPort should return node.http
	}{
		{
			name: "same port returns http server",
			rpcCfg: RPCConfig{
				HTTPHost: "127.0.0.1",
				HTTPPort: 35997,
				WSHost:   "127.0.0.1",
				WSPort:   35997,
			},
			wantHTTP: true,
		},
		{
			name: "different port returns ws server",
			rpcCfg: RPCConfig{
				HTTPHost: "127.0.0.1",
				HTTPPort: 35997,
				WSHost:   "127.0.0.1",
				WSPort:   35998,
			},
			wantHTTP: false,
		},
		{
			name: "no http configured returns http server",
			rpcCfg: RPCConfig{
				HTTPHost: "",
				HTTPPort: 0,
				WSHost:   "127.0.0.1",
				WSPort:   35997,
			},
			wantHTTP: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &Node{
				config: &Config{RPC: tt.rpcCfg},
				http:   newHTTPServer(rpc.DefaultHTTPTimeouts),
				ws:     newHTTPServer(rpc.DefaultHTTPTimeouts),
			}
			// Simulate what startRPC does: set the HTTP listen addr first.
			if tt.rpcCfg.HTTPHost != "" {
				node.http.setListenAddr(tt.rpcCfg.HTTPHost, tt.rpcCfg.HTTPPort)
			}

			got := node.wsServerForPort(tt.rpcCfg.WSPort)
			if tt.wantHTTP && got != node.http {
				t.Errorf("wsServerForPort(%d) did not return node.http", tt.rpcCfg.WSPort)
			}
			if !tt.wantHTTP && got != node.ws {
				t.Errorf("wsServerForPort(%d) did not return node.ws", tt.rpcCfg.WSPort)
			}
		})
	}
}
