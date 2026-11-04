package momentum

import (
	"testing"
	"time"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/common/db"
)

var errReadBudget = errors.New("read budget exhausted")

// boundedReadDB fails every read after the first left, so a lookup that
// keeps reading the same entries stops with an error instead of hanging.
type boundedReadDB struct {
	db.DB
	left int
}

func (b *boundedReadDB) Get(key []byte) ([]byte, error) {
	if b.left <= 0 {
		return nil, errReadBudget
	}
	b.left--
	return b.DB.Get(key)
}

// A timestamp inside the same second as the momentum below it gives the
// low-boundary step a zero height gap. The step must still move up, or the
// lookup reads that height forever.
func TestGetMomentumBeforeTime_SubsecondTargetTerminates(t *testing.T) {
	const count = 50
	genesisTime := time.Unix(1_700_000_000, 0)
	for _, c := range []struct {
		name   string
		height uint64
		want   uint64
	}{
		{"half a second after genesis", 1, 1},
		{"half a second after an interior momentum", 20, 20},
		{"half a second after the frontier", count, count},
	} {
		t.Run(c.name, func(t *testing.T) {
			ms := timedChain(t, count, genesisTime)
			ms.DB = &boundedReadDB{DB: ms.DB, left: 1000}
			target := genesisTime.Add(time.Duration(c.height-1)*10*time.Second + 500*time.Millisecond)
			got, err := ms.GetMomentumBeforeTime(&target)
			if err != nil {
				t.Fatalf("GetMomentumBeforeTime(%v): %v", target, err)
			}
			if got == nil || got.Height != c.want {
				t.Fatalf("GetMomentumBeforeTime(%v) = %v, want height %d", target, got, c.want)
			}
		})
	}
}
