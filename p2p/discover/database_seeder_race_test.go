package discover

import (
	"sync"
	"testing"
	"time"
)

// Concurrent querySeeds and close calls must not race. querySeeds holds
// seederMu for one batch; close holds it exclusively while releasing the
// iterator and closing the store. Under -race any unsynchronised access
// to db.seeder or the store is reported.
func TestQuerySeedsCloseRace(t *testing.T) {
	db, err := newNodeDB("", Version, newTestNodeID(t))
	if err != nil {
		t.Fatal(err)
	}

	// Seed the database so querySeeds has something to iterate.
	for i := 0; i < 8; i++ {
		id := newTestNodeID(t)
		if err := db.updateLastPing(id, time.Now()); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		db.querySeeds(4)
	}()
	go func() {
		defer wg.Done()
		db.querySeeds(4)
	}()
	go func() {
		defer wg.Done()
		db.close()
	}()

	wg.Wait()
}
