package node

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// corsPatternWildcardsHost must never miss a pattern the CORS middleware
// uses to grant an origin on a host the operator did not name. It may warn
// on more than that (a subdomain wildcard), but not on less.
func TestCORSPatternWildcardsHost(t *testing.T) {
	cases := []struct {
		pattern string
		origin  string // an origin on a host the pattern does not name
		granted bool   // the middleware grants origin under pattern
		warns   bool
	}{
		{"*", "http://attacker.invalid", true, true},
		{"http://*", "http://attacker.invalid", true, true},
		{"HTTPS://*", "https://attacker.invalid", true, true},
		{"http://*:8545", "http://attacker.invalid:8545", true, true},
		{"http://localhost*", "http://localhost.attacker.invalid", true, true},
		{"http://*wallet.example", "http://attackerwallet.example", true, true},
		{"http://*.com", "http://attacker.com", true, true},
		{"https://*.co.uk", "https://attacker.co.uk", true, true},
		{"ht*", "https://attacker.invalid", true, true},
		{"https://*.wallet.example", "https://attacker.invalid", false, true},
		{"https://*.wallet.example:8545", "https://attacker.invalid:8545", false, true},
		{"*.wallet.example", "http://attacker.invalid", false, true},
		{"http://[::*]", "http://attacker.invalid", false, true},
		{"*://wallet.example", "http://attacker.invalid", false, false},
		{"http://wallet.example:*", "http://wallet.example.attacker.invalid", false, false},
		{"http://[::1]:*", "http://attacker.invalid", false, false},
		{"https://wallet.example", "https://attacker.invalid", false, false},
	}
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	for _, c := range cases {
		t.Run(c.pattern, func(t *testing.T) {
			if c.granted && !c.warns {
				t.Fatalf("table row %q: a pattern that grants an unnamed host must warn", c.pattern)
			}
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.Header.Set("Origin", c.origin)
			rec := httptest.NewRecorder()
			newCorsHandler(ok, []string{c.pattern}).ServeHTTP(rec, req)
			granted := rec.Header().Get("Access-Control-Allow-Origin") != ""
			if granted != c.granted {
				t.Fatalf("middleware granted %s under %q: %v, want %v", c.origin, c.pattern, granted, c.granted)
			}
			if got := corsPatternWildcardsHost(c.pattern); got != c.warns {
				t.Fatalf("corsPatternWildcardsHost(%q) = %v, want %v", c.pattern, got, c.warns)
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
// shutdown bound it closes the connection, so the client sees it drop while
// the handler is still blocked, and returns.
func TestHTTPServerStopBoundsInFlightRequest(t *testing.T) {
	probe := blockingProbe{entered: make(chan struct{}, 1), release: make(chan struct{})}
	h := newHTTPServer(rpc.DefaultHTTPTimeouts)
	h.shutdownTimeout = 100 * time.Millisecond
	if err := h.setListenAddr("127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	apis := []rpc.API{{Namespace: "probe", Service: probe, Public: true}}
	if err := h.enableRPC(apis, httpConfig{Modules: []string{"probe"}}); err != nil {
		t.Fatal(err)
	}
	handlerDone := signalHandlerDone(h)
	if err := h.start(); err != nil {
		t.Fatal(err)
	}
	addr := h.listenAddr()

	clientErr := make(chan error, 1)
	// Cleanups run last-in first-out: release the method first, so a stop
	// still waiting on it (if the bound regressed) can finish; then wait
	// for the request handler to return, stop the server and wait for the
	// client, so nothing outlives the test.
	t.Cleanup(func() {
		select {
		case <-clientErr:
		case <-time.After(stackTestTimeout):
			t.Error("the client goroutine did not finish")
		}
	})
	t.Cleanup(h.stop)
	t.Cleanup(func() {
		select {
		case <-handlerDone:
		case <-time.After(stackTestTimeout):
			t.Error("the request handler did not return after the method was released")
		}
	})
	t.Cleanup(func() { close(probe.release) })

	go func() {
		body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"probe.block","params":[]}`)
		resp, err := http.Post("http://"+addr, "application/json", body)
		if err == nil {
			_ = resp.Body.Close()
		}
		clientErr <- err
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

	// The handler is still blocked: only closing the connection can end
	// the client's request now.
	select {
	case err := <-clientErr:
		clientErr <- err // for the cleanup
		if err == nil {
			t.Fatal("the client got a response although the handler never returned")
		}
	case <-time.After(stackTestTimeout):
		t.Fatal("the client's connection was not closed after stop")
	}

	// stop returned without the request: its handler is still running, and
	// only the release in cleanup lets it finish. Bounded shutdown closes
	// the connection; it does not end a method that ignores its context.
	select {
	case <-handlerDone:
		t.Fatal("the request handler returned although the method was never released")
	default:
	}
}

// signalHandlerDone wraps the HTTP JSON-RPC handler installed on h so that
// the returned channel is closed once a request handler has returned: the
// whole handler stack around the method, not only the method itself. It
// must be called after enableRPC and before start.
func signalHandlerDone(h *httpServer) <-chan struct{} {
	done := make(chan struct{})
	var once sync.Once
	installed := h.httpHandler.Load().(*rpcHandler)
	h.httpHandler.Store(&rpcHandler{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer once.Do(func() { close(done) })
			installed.Handler.ServeHTTP(w, r)
		}),
		server: installed.server,
	})
	return done
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
