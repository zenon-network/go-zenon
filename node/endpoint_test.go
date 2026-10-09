package node

import (
	"net"
	"testing"

	rpc "github.com/zenon-network/go-zenon/rpc/server"
)

// TestHTTPEndpoint_JoinHostPort verifies that HTTPEndpoint builds valid
// listen addresses for IPv4, hostnames, and IPv6 literals (both raw and
// pre-bracketed).
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

// TestJoinHostPort_MalformedBrackets verifies that malformed bracketed input
// is rejected with an error rather than returned for the caller to bind.
func TestJoinHostPort_MalformedBrackets(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int
	}{
		{"double-nested brackets", "[[]]", 35997},
		{"only open bracket", "[[", 35997},
		{"only close bracket", "]]", 35997},
		{"nested IPv6 wildcard", "[[::]]", 35997},
		{"unclosed IPv6 wildcard", "[::", 35997},
		{"truncated IPv4 wildcard", "[0.0.0.0", 35997},
		{"trailing bracket on IPv4 wildcard", "0.0.0.0]", 35997},
		{"empty brackets", "[]", 35997},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := joinHostPort(tt.host, tt.port)
			if err == nil {
				t.Fatalf("joinHostPort(%q, %d) = %q, nil error; want rejection",
					tt.host, tt.port, got)
			}
			// No address may be handed back alongside the error, so a
			// caller that ignores the error still cannot bind a wildcard.
			if got != "" {
				t.Errorf("joinHostPort(%q, %d) returned %q with an error; want empty",
					tt.host, tt.port, got)
			}
		})
	}
}

// TestSetListenAddr_MalformedBrackets verifies that the runtime HTTP/WS
// listener builder rejects a malformed host outright and leaves the server
// unconfigured, instead of storing an address that binds every interface.
func TestSetListenAddr_MalformedBrackets(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int
	}{
		{"nested IPv6 wildcard", "[[::]]", 35997},
		{"truncated IPv4 wildcard", "[0.0.0.0", 35997},
		{"trailing bracket on IPv4 wildcard", "0.0.0.0]", 35997},
		{"double-nested brackets", "[[]]", 35997},
		{"empty brackets", "[]", 35997},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &httpServer{}
			if err := h.setListenAddr(tt.host, tt.port); err == nil {
				t.Fatalf("setListenAddr(%q, %d) = nil error, endpoint %q; want rejection",
					tt.host, tt.port, h.endpoint)
			}
			// A rejected host must not leave a usable address behind.
			if h.endpoint != "" {
				t.Errorf("setListenAddr(%q, %d) left endpoint %q after rejecting the host",
					tt.host, tt.port, h.endpoint)
			}
		})
	}
}

// TestSetListenAddr_MalformedHostKeepsPreviousEndpoint verifies that a rejected
// host does not partially apply: a server already pointed somewhere keeps that
// address.
func TestSetListenAddr_MalformedHostKeepsPreviousEndpoint(t *testing.T) {
	h := newHTTPServer(rpc.DefaultHTTPTimeouts)
	if err := h.setListenAddr("127.0.0.1", 35997); err != nil {
		t.Fatalf("setListenAddr(good) error: %v", err)
	}
	if err := h.setListenAddr("[]", 35998); err == nil {
		t.Fatal("setListenAddr(\"[]\") = nil error; want rejection")
	}
	if h.endpoint != "127.0.0.1:35997" {
		t.Errorf("endpoint = %q after a rejected host, want the previous %q",
			h.endpoint, "127.0.0.1:35997")
	}
	if h.host != "127.0.0.1" || h.port != 35997 {
		t.Errorf("host/port = %q/%d after a rejected host, want 127.0.0.1/35997", h.host, h.port)
	}
}

// TestNormalizeListenHost covers the helper directly.
func TestNormalizeListenHost(t *testing.T) {
	rejected := []string{"[]", "[[]]", "[[::]]", "[", "]]", "[[", "[::", "[0.0.0.0", "0.0.0.0]"}
	for _, host := range rejected {
		t.Run("reject "+host, func(t *testing.T) {
			got, ok := normalizeListenHost(host)
			if ok {
				t.Errorf("normalizeListenHost(%q) = (%q, true), want rejected", host, got)
			}
			if got != "" {
				t.Errorf("normalizeListenHost(%q) = %q on failure, want empty string", host, got)
			}
		})
	}

	accepted := map[string]string{
		"":          "",
		"127.0.0.1": "127.0.0.1",
		"localhost": "localhost",
		"::1":       "::1",
		"[::1]":     "::1",
		"::":        "::",
		"[::]":      "::",
		"0.0.0.0":   "0.0.0.0",
	}
	for host, want := range accepted {
		t.Run("accept "+host, func(t *testing.T) {
			got, ok := normalizeListenHost(host)
			if !ok {
				t.Fatalf("normalizeListenHost(%q) rejected, want accepted", host)
			}
			if got != want {
				t.Errorf("normalizeListenHost(%q) = %q, want %q", host, got, want)
			}
		})
	}
}

// TestSetListenAddr_JoinHostPort verifies that httpServer.setListenAddr
// builds correct endpoints for all host forms, including IPv6.
func TestSetListenAddr_JoinHostPort(t *testing.T) {
	tests := []struct {
		name string
		host string
		port int
		want string
	}{
		{"IPv4", "127.0.0.1", 35997, "127.0.0.1:35997"},
		{"hostname", "localhost", 35997, "localhost:35997"},
		{"raw IPv6", "::1", 35997, "[::1]:35997"},
		{"bracketed IPv6", "[::1]", 35997, "[::1]:35997"},
		{"wildcard IPv6", "::", 35997, "[::]:35997"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHTTPServer(rpc.DefaultHTTPTimeouts)
			if err := h.setListenAddr(tt.host, tt.port); err != nil {
				t.Fatalf("setListenAddr(%q, %d) error: %v", tt.host, tt.port, err)
			}
			if h.endpoint != tt.want {
				t.Errorf("setListenAddr(%q, %d) endpoint = %q, want %q",
					tt.host, tt.port, h.endpoint, tt.want)
			}
			// Verify the result is parseable as a TCP address.
			if _, err := net.ResolveTCPAddr("tcp", h.endpoint); err != nil {
				t.Errorf("setListenAddr(%q, %d) endpoint = %q, ResolveTCPAddr error: %v",
					tt.host, tt.port, h.endpoint, err)
			}
		})
	}
}
