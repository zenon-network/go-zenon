package server

// DefaultMaxConcurrentHTTPRequests is the number of JSON-RPC requests a
// Server serves over HTTP at once unless SetMaxConcurrentHTTPRequests
// configures another limit. It is four times maxConcurrentCallsPerConn: HTTP
// clients together get the in-flight budget of four saturated WebSocket
// connections.
const DefaultMaxConcurrentHTTPRequests = 4 * maxConcurrentCallsPerConn

// admissionLimiter bounds how much work is in progress at once across everything
// that shares it. Unlike a connection's callSlots it never blocks: work that
// finds every slot taken is refused, so excess load is answered at once
// rather than queued behind the work already running.
type admissionLimiter struct {
	slots chan struct{}
}

func newAdmissionLimiter(n int) *admissionLimiter {
	return &admissionLimiter{slots: make(chan struct{}, n)}
}

// tryAcquire takes a slot if one is free and reports whether it did.
func (l *admissionLimiter) tryAcquire() bool {
	select {
	case l.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

// release returns a slot taken by tryAcquire.
func (l *admissionLimiter) release() {
	<-l.slots
}

// limit reports how many slots there are.
func (l *admissionLimiter) limit() int {
	return cap(l.slots)
}

// inFlight reports how many slots are taken.
func (l *admissionLimiter) inFlight() int {
	return len(l.slots)
}
