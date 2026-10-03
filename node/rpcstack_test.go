package node

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	rpc "github.com/zenon-network/go-zenon/rpc/server"
)

func TestVirtualHostHandler_CaseSensitivity(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := newVHostHandler([]string{"localhost"}, okHandler)

	tests := []struct {
		name       string
		hostHeader string
		wantStatus int
	}{
		{"lowercase", "localhost", http.StatusOK},
		{"uppercase", "LOCALHOST", http.StatusOK},
		{"mixed_case", "LocalHost", http.StatusOK},
		{"lowercase_with_port", "localhost:35997", http.StatusOK},
		{"mixed_case_with_port", "LocalHost:35997", http.StatusOK},
		{"unrelated_host", "evil.example.com", http.StatusForbidden},
		{"unrelated_host_with_port", "evil.example.com:35997", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://"+tt.hostHeader+"/", nil)
			req.Host = tt.hostHeader
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("Host %q: got status %d, want %d", tt.hostHeader, rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestVirtualHostHandler_IPLiteral(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := newVHostHandler([]string{"localhost"}, okHandler)

	// IP literals should always be served regardless of vhost config
	for _, host := range []string{"127.0.0.1", "127.0.0.1:35997", "[::1]:35997"} {
		req := httptest.NewRequest(http.MethodPost, "http://"+host+"/", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("Host %q: got status %d, want %d", host, rec.Code, http.StatusOK)
		}
	}
}

func TestVirtualHostHandler_Wildcard(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := newVHostHandler([]string{"*"}, okHandler)

	req := httptest.NewRequest(http.MethodPost, "http://anything.example.com/", nil)
	req.Host = "anything.example.com"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("wildcard: got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestVirtualHostHandler_EmptyHost(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := newVHostHandler([]string{"localhost"}, okHandler)

	// Empty Host header should be served (browser would set it)
	req := httptest.NewRequest(http.MethodPost, "http://localhost/", nil)
	req.Host = ""
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("empty host: got status %d, want %d", rec.Code, http.StatusOK)
	}
}

// --- Per-IP WebSocket connection limit (issue #122) ---

func TestWSRemoteIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		want       string
	}{
		{"ipv4 with port", "192.168.1.1:8080", "192.168.1.1"},
		{"ipv6 with port", "[::1]:8080", "::1"},
		{"ipv4 no port", "192.168.1.1", "192.168.1.1"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wsRemoteIP(tt.remoteAddr)
			if got != tt.want {
				t.Errorf("wsRemoteIP(%q) = %q, want %q", tt.remoteAddr, got, tt.want)
			}
		})
	}
}

func TestWSConnLimiter_AdmitAndRelease(t *testing.T) {
	l := &wsConnLimiter{maxPerIP: 2, counts: make(map[string]int)}

	if !l.admit("10.0.0.1") {
		t.Fatal("first admit rejected")
	}
	if !l.admit("10.0.0.1") {
		t.Fatal("second admit rejected")
	}
	if l.admit("10.0.0.1") {
		t.Fatal("third admit accepted, want rejected")
	}
	if !l.admit("10.0.0.2") {
		t.Fatal("different IP rejected")
	}

	l.release("10.0.0.1")
	if !l.admit("10.0.0.1") {
		t.Fatal("admit after release rejected")
	}
}

func TestWSConnLimiter_EmptyIPAlwaysAdmitted(t *testing.T) {
	l := &wsConnLimiter{maxPerIP: 1, counts: make(map[string]int)}

	if !l.admit("") {
		t.Fatal("empty IP rejected")
	}
	if !l.admit("") {
		t.Fatal("empty IP rejected on second call")
	}
}

func TestWSConnLimiter_ReleaseDeletesZeroEntry(t *testing.T) {
	l := &wsConnLimiter{maxPerIP: 1, counts: make(map[string]int)}

	l.admit("10.0.0.1")
	l.release("10.0.0.1")
	if len(l.counts) != 0 {
		t.Fatalf("counts map not cleaned: %v", l.counts)
	}
}

func TestMaxWSConnectionsPerIP_RefusesExcess(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	handler := maxWSConnectionsPerIP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		<-release // block until test ends
	}), 2)

	// Admit two connections from the same IP.
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		done := make(chan struct{})
		go func() {
			defer close(done)
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = "10.0.0.1:1234"
			handler.ServeHTTP(rec, req)
		}()
		// Wait for the handler to be running (admitted and blocked).
		time.Sleep(10 * time.Millisecond)
	}

	// Third connection from the same IP is refused.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1235"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("excess connection: got %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

// TestEnableWS_AppliesPerIPLimit pins the config->handler hop in enableWS.
// The limiter tests above call maxWSConnectionsPerIP directly, so without
// this test the `if config.MaxConnectionsPerIP > 0` wrap in rpcstack.go
// could be dropped and every other test would still pass.
func TestEnableWS_AppliesPerIPLimit(t *testing.T) {
	h := newHTTPServer(rpc.DefaultHTTPTimeouts)
	if err := h.enableWS(nil, wsConfig{MaxConnectionsPerIP: 1}); err != nil {
		t.Fatalf("enableWS: %v", err)
	}
	handler := h.wsHandler.Load().(*rpcHandler)
	defer handler.server.Stop()
	srv := httptest.NewServer(handler)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	// The first handshake succeeds and stays open, holding the only slot.
	first, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("first handshake: %v", err)
	}
	defer first.Close()

	// The second handshake from the same remote IP must be refused.
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("second handshake succeeded, want refusal")
	}
	if resp == nil {
		t.Fatalf("second handshake: no HTTP response: %v", err)
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second handshake: got %d, want %d", resp.StatusCode, http.StatusTooManyRequests)
	}
}

func TestMaxWSConnectionsPerIP_ZeroIsNoop(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := maxWSConnectionsPerIP(inner, 0)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if !called {
		t.Fatal("inner handler not called with maxPerIP=0")
	}
}
