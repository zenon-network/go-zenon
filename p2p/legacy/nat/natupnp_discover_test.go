package nat

import (
	"errors"
	"testing"
	"time"

	"github.com/huin/goupnp"
)

// discoverUPnP waits for one result per discovery worker. A worker whose
// device search fails must still hand back a result (nil), or the
// collector, and every port mapping behind it, waits forever.
func TestDiscoverSendsResultOnSearchError(t *testing.T) {
	out := make(chan *upnp, 1)
	go discoverWith(out, "urn:test",
		func(*goupnp.RootDevice, goupnp.ServiceClient) *upnp { return nil },
		func(string) ([]goupnp.MaybeRootDevice, error) { return nil, errors.New("search failed") })
	select {
	case c := <-out:
		if c != nil {
			t.Fatalf("got a gateway from a failed search: %v", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no result handed back after the device search failed")
	}
}
