package server

import (
	"context"
	"testing"
	"time"
)

// recordingWriter captures the context handed to writeJSON so a test can assert
// on the deadline a notification write is actually bounded by.
type recordingWriter struct {
	gotDeadline time.Time
	gotOK       bool
}

func (w *recordingWriter) writeJSON(ctx context.Context, _ interface{}) error {
	w.gotDeadline, w.gotOK = ctx.Deadline()
	return nil
}
func (w *recordingWriter) closed() <-chan interface{} { return nil }
func (w *recordingWriter) remoteAddr() string         { return "test" }

// A single stalled subscriber must not be able to hold the fan-out loop for the
// full 10s defaultWriteTimeout. Subscription broadcast is a serial loop
// (subscribe.Server.broadcast), so the per-write bound is the only thing
// limiting how long one unresponsive client can stall every other subscriber.
func TestNotifyBoundsWriteDeadline(t *testing.T) {
	w := &recordingWriter{}
	h := newHandler(context.Background(), w, func() ID { return "sub-1" }, &serviceRegistry{})

	n := &Notifier{h: h, namespace: "zenon"}
	sub := n.CreateSubscription()
	n.activated = true // send straight through rather than buffering

	if sub.ID != "sub-1" {
		t.Fatalf("idgen: got %q, want %q", sub.ID, ID("sub-1"))
	}

	start := time.Now()
	if err := n.Notify(sub.ID, map[string]int{"height": 42}); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	if !w.gotOK {
		t.Fatal("writeJSON received a context with no deadline; " +
			"a notification write would fall back to defaultWriteTimeout (10s) " +
			"and could stall the serial broadcast loop for that long")
	}

	bound := time.Until(w.gotDeadline)
	if bound <= 0 {
		t.Fatalf("deadline already expired: %v", bound)
	}
	// Deliberately NOT derived from notificationWriteTimeout: if the assertion
	// referenced the production constant, reverting the patch would break
	// compilation instead of failing this test, which hides the regression
	// behind a build error. The point of the check is the order of magnitude --
	// bounded at ~1s rather than inheriting defaultWriteTimeout's 10s -- so the
	// bound is asserted literally against a 2s ceiling.
	const wantCeiling = 2 * time.Second
	if bound > wantCeiling {
		t.Errorf("notification write bound too loose: got %v, want <= %v "+
			"(an unbounded context inherits defaultWriteTimeout, 10s)",
			bound, wantCeiling)
	}
	t.Logf("Notify bounded the write to %v (unbounded would be 10s); call took %v",
		bound.Round(time.Millisecond), time.Since(start).Round(time.Microsecond))
}
