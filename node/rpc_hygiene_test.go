package node

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	rpc "github.com/zenon-network/go-zenon/rpc/server"
)

// A startRPC that fails after configuring HTTP leaves a handler and an
// address on a server that never listened. stopRPC must clear those too,
// or a later start() binds an endpoint that the next configuration
// disabled.
func TestStopRPCClearsServersAfterFailedStart(t *testing.T) {
	p := freePort(t)
	n := newRPCTestNode(RPCConfig{EnableHTTP: true, EnableWS: true,
		HTTPHost: "127.0.0.1", HTTPPort: p, WSHost: "0.0.0.0", WSPort: p})
	t.Cleanup(n.stopRPC)

	if err := n.startRPC(); err == nil || !strings.Contains(err.Error(), "share port") {
		t.Fatalf("startRPC() = %v, want the shared-port host mismatch", err)
	}
	n.stopRPC()
	for name, server := range map[string]*httpServer{"http": n.http, "ws": n.ws} {
		if server.rpcAllowed() || server.wsAllowed() {
			t.Fatalf("%s server keeps a handler after stopRPC (rpc=%v ws=%v)", name, server.rpcAllowed(), server.wsAllowed())
		}
		if addr := server.listenAddr(); addr != "" {
			t.Fatalf("%s server keeps listen address %q after stopRPC", name, addr)
		}
	}

	n.config.RPC.EnableHTTP, n.config.RPC.EnableWS = false, false
	if err := n.startRPC(); err != nil {
		t.Fatalf("startRPC with both protocols disabled: %v", err)
	}
	for name, server := range map[string]*httpServer{"http": n.http, "ws": n.ws} {
		if addr := server.exposure().addr; addr != nil {
			t.Fatalf("%s server listens on %s with both protocols disabled", name, addr)
		}
	}
}

// corsPatternAdmitsAnyHost must agree with the CORS middleware: each
// pattern it calls open lets an origin on a host the operator never named
// through, and each pattern it calls closed turns that origin away.
func TestCORSPatternAdmitsAnyHost(t *testing.T) {
	cases := []struct {
		pattern string
		origin  string // an origin on a host the pattern does not name
		open    bool
	}{
		{"*", "http://attacker.invalid", true},
		{"http://*", "http://attacker.invalid", true},
		{"HTTPS://*", "https://attacker.invalid", true},
		{"http://*:8545", "http://attacker.invalid:8545", true},
		{"http://localhost*", "http://localhost.attacker.invalid", true},
		{"http://*wallet.example", "http://attackerwallet.example", true},
		{"https://*.wallet.example", "https://attacker.invalid", false},
		{"https://*.wallet.example:8545", "https://attacker.invalid:8545", false},
		{"*.wallet.example", "http://attacker.invalid", false},
		{"*://wallet.example", "http://attacker.invalid", false},
		{"https://wallet.example", "https://attacker.invalid", false},
	}
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	for _, c := range cases {
		t.Run(c.pattern, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.Header.Set("Origin", c.origin)
			rec := httptest.NewRecorder()
			newCorsHandler(ok, []string{c.pattern}).ServeHTTP(rec, req)
			granted := rec.Header().Get("Access-Control-Allow-Origin") != ""
			if granted != c.open {
				t.Fatalf("middleware granted %s under %q: %v, want %v", c.origin, c.pattern, granted, c.open)
			}
			if got := corsPatternAdmitsAnyHost(c.pattern); got != c.open {
				t.Fatalf("corsPatternAdmitsAnyHost(%q) = %v, want %v", c.pattern, got, c.open)
			}
		})
	}
}

// blockingProbe holds a JSON-RPC call open until the test releases it.
type blockingProbe struct {
	entered chan struct{}
	release chan struct{}
}

func (b blockingProbe) Block() string {
	b.entered <- struct{}{}
	<-b.release
	return "released"
}

// stop must not wait on an HTTP request that does not finish: past the
// shutdown bound it closes the connection and returns.
func TestHTTPServerStopBoundsInFlightRequest(t *testing.T) {
	probe := blockingProbe{entered: make(chan struct{}, 1), release: make(chan struct{})}
	t.Cleanup(func() { close(probe.release) })

	h := newHTTPServer(rpc.DefaultHTTPTimeouts)
	h.shutdownTimeout = 100 * time.Millisecond
	if err := h.setListenAddr("127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	apis := []rpc.API{{Namespace: "probe", Service: probe, Public: true}}
	if err := h.enableRPC(apis, httpConfig{Modules: []string{"probe"}}); err != nil {
		t.Fatal(err)
	}
	if err := h.start(); err != nil {
		t.Fatal(err)
	}

	go func() {
		body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"probe.block","params":[]}`)
		resp, err := http.Post("http://"+h.listenAddr(), "application/json", body)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	select {
	case <-probe.entered:
	case <-time.After(stackTestTimeout):
		t.Fatal("the blocking call never reached its handler")
	}

	stopped := make(chan struct{})
	go func() {
		h.stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(stackTestTimeout):
		t.Fatalf("stop did not return within %v while a request was in flight (bound %v)", stackTestTimeout, h.shutdownTimeout)
	}
	if addr := h.exposure().addr; addr != nil {
		t.Fatalf("server still listens on %s after stop", addr)
	}
}

// The open-origin warnings name the rule that leaves the host open, so the
// operator can find it in the list.
func TestOpenOriginWarningNamesTheRule(t *testing.T) {
	got := captureWarnings(t)
	n := startDefaultsNode(t, sharedListener, func(cfg *RPCConfig) {
		cfg.HTTPCors = []string{"https://wallet.example", "http://*"}
		cfg.WSOrigins = []string{"https://wallet.example", "http://"}
	})
	n.warnRPCExposure()
	want := map[string][2]string{
		httpOpenOriginWarning: {"cors", "http://*"},
		wsOpenOriginWarning:   {"origins", "http://"},
	}
	for substr, kv := range want {
		found := false
		for _, m := range got() {
			if strings.Contains(m.msg, substr) {
				found = true
				if m.ctx[kv[0]] != kv[1] {
					t.Fatalf("%q names %s=%q, want %q", m.msg, kv[0], m.ctx[kv[0]], kv[1])
				}
			}
		}
		if !found {
			t.Fatalf("no warning containing %q in %v", substr, got())
		}
	}
}
