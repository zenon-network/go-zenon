package server

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"

	mapset "github.com/deckarep/golang-set"
)

func goroutinesSettled() int {
	// Give any producer goroutine that is going to exit a moment to do so,
	// so the count reflects what stays behind.
	time.Sleep(50 * time.Millisecond)
	return runtime.NumGoroutine()
}

// originIsAllowed must not leave anything behind when it returns early on a
// match that is not the last rule in the set.
func TestOriginIsAllowed_EarlyMatchLeavesNoGoroutine(t *testing.T) {
	// Both rules admit http://localhost, so whichever the set yields first
	// matches and the check returns before the second is yielded: every
	// call is an early exit.
	allowed := mapset.NewSet()
	allowed.Add("http://localhost")
	allowed.Add("localhost")

	const checks = 400
	before := goroutinesSettled()
	for i := 0; i < checks; i++ {
		if !originIsAllowed(allowed, "http://localhost") {
			t.Fatal("an allowed origin was rejected")
		}
	}
	if leaked := goroutinesSettled() - before; leaked > 5 {
		t.Fatalf("%d goroutines left behind by %d origin checks", leaked, checks)
	}
	if originIsAllowed(allowed, "http://c.example") {
		t.Fatal("a non-matching origin was accepted")
	}
	if originIsAllowed(mapset.NewSet(), "http://localhost") {
		t.Fatal("an empty rule set accepted an origin")
	}
}

// Parsing and logging now run inside the set's read lock: a malformed rule
// is skipped with a warning and the walk goes on, and concurrent checks
// against one validator neither deadlock nor disagree.
func TestOriginIsAllowed_MalformedRuleAndConcurrentChecks(t *testing.T) {
	allowed := mapset.NewSet()
	allowed.Add("http://[bad")
	allowed.Add("http://localhost")

	if !originIsAllowed(allowed, "http://localhost") {
		t.Fatal("a malformed rule stopped a valid rule from matching")
	}
	if originIsAllowed(allowed, "http://[also-bad") {
		t.Fatal("a malformed browser origin was accepted")
	}

	var wg sync.WaitGroup
	results := make([]bool, 64)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				results[i] = originIsAllowed(allowed, "http://localhost")
			} else {
				results[i] = !originIsAllowed(allowed, "http://other.example")
			}
		}(i)
	}
	wg.Wait()
	for i, ok := range results {
		if !ok {
			t.Fatalf("concurrent check %d gave the wrong answer", i)
		}
	}
}

// The same through the handshake path: a request whose Origin matches a rule
// and then fails the upgrade for another reason.
func TestWebsocketHandler_OriginCheckDoesNotLeakGoroutines(t *testing.T) {
	srv := NewServer()
	defer srv.Stop()
	handler := srv.WebsocketHandler([]string{"http://localhost", "http://127.0.0.1"})

	const requests = 200
	before := goroutinesSettled()
	for i := 0; i < requests; i++ {
		for _, origin := range []string{"http://localhost", "http://127.0.0.1"} {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Connection", "upgrade")
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Sec-WebSocket-Version", "13")
			req.Header.Set("Origin", origin)
			// No Sec-WebSocket-Key: the upgrade fails right after the origin check.
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("unexpected status %d", rec.Code)
			}
		}
	}
	if leaked := goroutinesSettled() - before; leaked > 5 {
		t.Fatalf("%d goroutines left behind by %d rejected handshakes", leaked, 2*requests)
	}
}
