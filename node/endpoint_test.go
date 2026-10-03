package node

import (
	"net"
	"strconv"
	"testing"

	rpc "github.com/zenon-network/go-zenon/rpc/server"
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

// TestJoinHostPort_MalformedBrackets verifies that malformed bracketed
// input does not silently produce a wildcard (all-interface) listener.
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joinHostPort(tt.host, tt.port)
			// The malformed host should be preserved (not trimmed to
			// empty), so the result is not a bare ":port" wildcard.
			if got == ":"+strconv.Itoa(tt.port) {
				t.Errorf("joinHostPort(%q, %d) = %q, must not produce wildcard address",
					tt.host, tt.port, got)
			}
			// Malformed input must never be silently repaired into an
			// all-interface address, i.e. IPv4 0.0.0.0 or IPv6 ::.
			if addr, err := net.ResolveTCPAddr("tcp", got); err == nil && addr.IP.IsUnspecified() {
				t.Errorf("joinHostPort(%q, %d) = %q, must not become a wildcard listener",
					tt.host, tt.port, got)
			}
			// The malformed result must fail TCP resolution so that
			// downstream validation rejects it.
			if _, err := net.ResolveTCPAddr("tcp", got); err == nil {
				t.Errorf("joinHostPort(%q, %d) = %q, expected ResolveTCPAddr error for malformed input",
					tt.host, tt.port, got)
			}
		})
	}
}

// TestSetListenAddr_MalformedBrackets verifies that the runtime HTTP/WS
// listener builder preserves malformed bracketed hosts instead of repairing
// them into wildcard (all-interface) bind addresses.
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
			if err := h.setListenAddr(tt.host, tt.port); err != nil {
				t.Fatalf("setListenAddr(%q, %d) unexpected error: %v", tt.host, tt.port, err)
			}
			// A bare ":port" is the shape net.Listen resolves to every
			// interface. IsUnspecified() does not catch it: ResolveTCPAddr
			// reports a nil IP there, for which IsUnspecified() is false.
			if h.endpoint == ":"+strconv.Itoa(tt.port) {
				t.Errorf("setListenAddr(%q, %d) endpoint %q is a wildcard bind on all interfaces",
					tt.host, tt.port, h.endpoint)
			}
			if addr, err := net.ResolveTCPAddr("tcp", h.endpoint); err == nil && addr.IP.IsUnspecified() {
				t.Errorf("setListenAddr(%q, %d) endpoint %q must not become a wildcard listener",
					tt.host, tt.port, h.endpoint)
			}
		})
	}
}

// TestNormalizeListenHost covers the helper directly. setListenAddr-level
// assertions alone did not catch the empty-bracket case, because the bad
// value only becomes visible as the shape ":port" further down.
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
