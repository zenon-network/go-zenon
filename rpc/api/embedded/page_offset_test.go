package embedded

import (
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// epochHistoryChain is a frontier chain whose contract account holds
// epochs 0..lastEpoch of reward-deposit history for user and of pillar
// epoch history for pillarName.
func epochHistoryChain(t *testing.T, contract types.Address, user types.Address, pillarName string, lastEpoch int64) *requiredPoWQuoteChain {
	t.Helper()
	accStore := account.NewAccountStore(contract, db.NewMemDB())
	if err := (&definition.LastEpochUpdate{LastEpoch: lastEpoch}).Save(accStore.Storage()); err != nil {
		t.Fatal(err)
	}
	for e := uint64(0); e <= uint64(lastEpoch); e++ {
		h := &definition.RewardDepositHistory{Epoch: e, Address: &user, Znn: big.NewInt(int64(e + 1)), Qsr: big.NewInt(0)}
		if err := h.Save(accStore.Storage()); err != nil {
			t.Fatal(err)
		}
		p := &definition.PillarEpochHistory{Name: pillarName, Epoch: e, ProducedBlockNum: int32(e + 1), Weight: big.NewInt(0)}
		if err := p.Save(accStore.Storage()); err != nil {
			t.Fatal(err)
		}
	}
	return &requiredPoWQuoteChain{
		momentum: &requiredPoWQuoteMomentum{frontier: &nom.Momentum{Height: 1}},
		account:  accStore,
	}
}

// pageIndex*pageSize was multiplied in uint32 before widening, so a page far
// past the end wrapped around and returned the newest epochs instead of an
// empty page. 4194304 * 1024 == 2^32, which wraps to 0.
func TestRewardHistoryPage_LargeIndexDoesNotWrap(t *testing.T) {
	user := types.Address{0, 0x73}
	c := epochHistoryChain(t, types.StakeContract, user, "p", 9)

	result, err := getFrontierRewardByPage(c, types.StakeContract, user, 4194304, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.List) != 0 {
		t.Fatalf("page far past the end returned %d entries (first epoch %d); want none", len(result.List), result.List[0].Epoch)
	}

	// Ordinary paging is unchanged: page 1 of size 4 is epochs 5..2.
	result, err = getFrontierRewardByPage(c, types.StakeContract, user, 1, 4)
	if err != nil || len(result.List) != 4 || result.List[0].Epoch != 5 || result.List[3].Epoch != 2 {
		t.Fatalf("page 1 size 4 = %+v, %v; want epochs 5..2", result, err)
	}
	// The last partial page and the first empty page.
	result, err = getFrontierRewardByPage(c, types.StakeContract, user, 2, 4)
	if err != nil || len(result.List) != 2 || result.List[0].Epoch != 1 {
		t.Fatalf("page 2 size 4 = %+v, %v; want epochs 1..0", result, err)
	}
	result, err = getFrontierRewardByPage(c, types.StakeContract, user, 3, 4)
	if err != nil || len(result.List) != 0 {
		t.Fatalf("page 3 size 4 = %+v, %v; want none", result, err)
	}
}

func TestPillarEpochHistoryPage_LargeIndexDoesNotWrap(t *testing.T) {
	user := types.Address{0, 0x73}
	c := epochHistoryChain(t, types.PillarContract, user, "p", 9)
	pillarApi := &PillarApi{chain: c}

	result, err := pillarApi.GetPillarEpochHistory("p", 4194304, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.List) != 0 {
		t.Fatalf("page far past the end returned %d entries (first epoch %d); want none", len(result.List), result.List[0].Epoch)
	}

	result, err = pillarApi.GetPillarEpochHistory("p", 1, 4)
	if err != nil || len(result.List) != 4 || result.List[0].Epoch != 5 || result.List[3].Epoch != 2 {
		t.Fatalf("page 1 size 4 = %+v, %v; want epochs 5..2", result, err)
	}
	result, err = pillarApi.GetPillarEpochHistory("p", 3, 4)
	if err != nil || len(result.List) != 0 {
		t.Fatalf("page 3 size 4 = %+v, %v; want none", result, err)
	}
}
