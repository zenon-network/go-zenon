package server

import (
	"net/http"
	"net/http/httptest"
	"runtime"
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
	allowed := mapset.NewSet()
	allowed.Add("http://a.example")
	allowed.Add("http://b.example")

	const checks = 200
	before := goroutinesSettled()
	for i := 0; i < checks; i++ {
		// Whichever rule the set yields first, one of the two origins matches
		// it and returns before the other is yielded.
		if !originIsAllowed(allowed, "http://a.example") || !originIsAllowed(allowed, "http://b.example") {
			t.Fatal("an allowed origin was rejected")
		}
	}
	if leaked := goroutinesSettled() - before; leaked > 5 {
		t.Fatalf("%d goroutines left behind by %d origin checks", leaked, 2*checks)
	}
	if originIsAllowed(allowed, "http://c.example") {
		t.Fatal("a non-matching origin was accepted")
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
