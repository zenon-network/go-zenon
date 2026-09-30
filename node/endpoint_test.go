package node

import (
	"net"
	"testing"
)

// TestHTTPEndpoint_JoinHostPort verifies that HTTPEndpoint builds valid
// listen addresses for IPv4, hostnames, and IPv6 literals (both raw and
// pre-bracketed). See go-zenon issue #109.
func TestHTTPEndpoint_JoinHostPort(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int
		want string // expected endpoint string
	}{
		{"IPv4", "127.0.0.1", 35997, "127.0.0.1:35997"},
		{"wildcard IPv4", "0.0.0.0", 35997, "0.0.0.0:35997"},
		{"hostname", "localhost", 35997, "localhost:35997"},
		{"raw IPv6", "::1", 35997, "[::1]:35997"},
		{"wildcard IPv6", "::", 35997, "[::]:35997"},
		{"bracketed IPv6", "[::1]", 35997, "[::1]:35997"},
		{"bracketed wildcard IPv6", "[::]", 35997, "[::]:35997"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{RPC: RPCConfig{HTTPHost: tt.host, HTTPPort: tt.port}}
			got := c.HTTPEndpoint()
			if got != tt.want {
				t.Errorf("HTTPEndpoint() = %q, want %q", got, tt.want)
			}
			// Verify the result is parseable as a TCP address.
			if _, err := net.ResolveTCPAddr("tcp", got); err != nil {
				t.Errorf("HTTPEndpoint() = %q, ResolveTCPAddr error: %v", got, err)
			}
		})
	}
}

// TestWSEndpoint_JoinHostPort verifies the same for WSEndpoint.
func TestWSEndpoint_JoinHostPort(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int
		want string
	}{
		{"IPv4", "127.0.0.1", 35997, "127.0.0.1:35997"},
		{"raw IPv6", "::1", 35997, "[::1]:35997"},
		{"bracketed IPv6", "[::1]", 35997, "[::1]:35997"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{RPC: RPCConfig{WSHost: tt.host, WSPort: tt.port}}
			got := c.WSEndpoint()
			if got != tt.want {
				t.Errorf("WSEndpoint() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestListenAddr_JoinHostPort verifies the P2P listen address is built
// correctly for all host forms.
func TestListenAddr_JoinHostPort(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int
		want string
	}{
		{"IPv4", "0.0.0.0", 35995, "0.0.0.0:35995"},
		{"hostname", "localhost", 35995, "localhost:35995"},
		{"raw IPv6", "::", 35995, "[::]:35995"},
		{"raw IPv6 loopback", "::1", 35995, "[::1]:35995"},
		{"bracketed IPv6", "[::]", 35995, "[::]:35995"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We test the JoinHostPort logic directly since the full
			// makeNetConfig path requires a full Config.
			got := joinHostPort(tt.host, tt.port)
			if got != tt.want {
				t.Errorf("joinHostPort(%q, %d) = %q, want %q", tt.host, tt.port, got, tt.want)
			}
		})
	}
}
