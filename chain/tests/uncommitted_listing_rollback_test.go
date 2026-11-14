package tests

import (
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// The peer handshake lists the uncommitted blocks without the insert lock,
// so the listing can run while a momentum is being rolled back. It must
// return in the interval where the momentum is already popped and the pool
// has not been told yet.
//
// Each round confirms a send of User1, reads User1's frontier so the pool
// holds a manager for the address, and rolls the momentum back while other
// goroutines list the uncommitted blocks in a loop. Whether a reader lands
// in the interval is a matter of scheduling, so a single round can pass by
// luck when the walk is wrong; the rounds and readers make that unlikely.
func TestUncommittedListingDuringMomentumRollback(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	z.InsertMomentumsTo(3)

	const (
		rounds  = 10
		readers = 4
	)
	var (
		panics     atomic.Int64
		firstPanic atomic.Value
	)
	list := func() {
		defer func() {
			if r := recover(); r != nil {
				panics.Add(1)
				firstPanic.CompareAndSwap(nil, fmt.Sprint(r))
			}
		}()
		z.Chain().GetAllUncommittedAccountBlocks()
		z.Chain().GetUncommittedAccountBlocksByAddress(g.User1.Address)
	}

	for round := 0; round < rounds; round++ {
		before := z.Chain().GetFrontierMomentumStore().Identifier()
		z.InsertSendBlock(&nom.AccountBlock{
			Address: g.User1.Address, ToAddress: g.User2.Address,
			TokenStandard: types.ZnnTokenStandard, Amount: big.NewInt(1),
		}, nil, mock.SkipVmChanges)
		z.InsertNewMomentum()
		z.Chain().GetFrontierAccountStore(g.User1.Address)

		stop := make(chan struct{})
		started := sync.WaitGroup{}
		done := sync.WaitGroup{}
		for i := 0; i < readers; i++ {
			started.Add(1)
			done.Add(1)
			go func() {
				defer done.Done()
				list()
				started.Done()
				for {
					select {
					case <-stop:
						return
					default:
						list()
					}
				}
			}()
		}
		started.Wait()

		insert := z.Chain().AcquireInsert("uncommitted listing rollback test")
		err := z.Chain().RollbackTo(insert, before)
		insert.Unlock()
		close(stop)
		done.Wait()
		if err != nil {
			t.Fatal(err)
		}

		if count := panics.Load(); count != 0 {
			t.Fatalf("round %d: listing the uncommitted blocks panicked %d time(s) during the rollback: %v", round, count, firstPanic.Load())
		}
		if pending := z.Chain().GetUncommittedAccountBlocksByAddress(g.User1.Address); len(pending) != 0 {
			t.Fatalf("round %d: %d block(s) of the rolled-back account are still listed", round, len(pending))
		}
	}
}
