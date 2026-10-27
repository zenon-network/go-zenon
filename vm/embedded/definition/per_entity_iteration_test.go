package definition

import (
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// countingDB wraps db.DB and counts how many key-value pairs the
// iterator visits, so tests can assert that a getter reads only the
// per-entity sub-range rather than the whole table.
type countingDB struct {
	db.DB
	count int
}

func (c *countingDB) NewIterator(prefix []byte) db.StorageIterator {
	inner := c.DB.NewIterator(prefix)
	return &countingIterator{StorageIterator: inner, count: &c.count}
}

type countingIterator struct {
	db.StorageIterator
	count *int
}

func (ci *countingIterator) Next() bool {
	ok := ci.StorageIterator.Next()
	if ok {
		*ci.count++
	}
	return ok
}

// makeAddress returns a deterministic Address from a seed byte.
func makeAddress(seed byte) types.Address {
	b := make([]byte, 20)
	b[0] = seed
	b[19] = 1
	addr, err := types.BytesToAddress(b)
	if err != nil {
		panic(err)
	}
	return addr
}

// makeHash returns a deterministic Hash from a seed byte.
func makeHash(seed byte) types.Hash {
	b := make([]byte, 32)
	b[0] = seed
	b[31] = 1
	return types.NewHash(b)
}

// setupStakes inserts nStake stake entries across nAddr distinct
// addresses (round-robin), all active. Returns the addresses used.
func setupStakes(context db.DB, nAddr, nStake int) []types.Address {
	addresses := make([]types.Address, nAddr)
	for i := 0; i < nAddr; i++ {
		addresses[i] = makeAddress(byte(i + 1))
	}
	for i := 0; i < nStake; i++ {
		addr := addresses[i%nAddr]
		stake := &StakeInfo{
			Amount:         big.NewInt(int64(100 + i)),
			WeightedAmount: big.NewInt(int64(200 + i)),
			StartTime:      int64(i),
			ExpirationTime: int64(i + 1000),
			StakeAddress:   addr,
			Id:             makeHash(byte(i)),
		}
		if err := stake.Save(context); err != nil {
			panic(err)
		}
	}
	return addresses
}

// TestGetStakeListByAddressIteratesSubRange verifies that
// GetStakeListByAddress reads only the rows for the requested address,
// not the whole table, and returns the correct results.
func TestGetStakeListByAddressIteratesSubRange(t *testing.T) {
	context := db.NewMemDB()
	nAddr := 5
	nStake := 20 // 4 per address
	addresses := setupStakes(context, nAddr, nStake)

	wrapped := &countingDB{DB: context}
	list, total, weighted, err := GetStakeListByAddress(wrapped, addresses[0])
	if err != nil {
		t.Fatal(err)
	}

	// Each address has nStake/nAddr = 4 entries.
	expectedPerAddr := nStake / nAddr
	if len(list) != expectedPerAddr {
		t.Fatalf("expected %d entries, got %d", expectedPerAddr, len(list))
	}

	// The iterator must have visited exactly the entries for this
	// address, not the whole table.
	if wrapped.count != expectedPerAddr {
		t.Fatalf("iterator visited %d rows, want %d (per-entity sub-range)", wrapped.count, expectedPerAddr)
	}

	// Verify totals.
	expectedTotal := big.NewInt(0)
	expectedWeighted := big.NewInt(0)
	for i := 0; i < nStake; i++ {
		if i%nAddr == 0 {
			expectedTotal.Add(expectedTotal, big.NewInt(int64(100+i)))
			expectedWeighted.Add(expectedWeighted, big.NewInt(int64(200+i)))
		}
	}
	if total.Cmp(expectedTotal) != 0 {
		t.Fatalf("total = %s, want %s", total, expectedTotal)
	}
	if weighted.Cmp(expectedWeighted) != 0 {
		t.Fatalf("weighted = %s, want %s", weighted, expectedWeighted)
	}
}

// TestGetStakeListByAddressRevokedSkipped verifies that revoked entries
// are excluded and the iterator still stays within the sub-range.
func TestGetStakeListByAddressRevokedSkipped(t *testing.T) {
	context := db.NewMemDB()
	addr := makeAddress(1)

	active := &StakeInfo{
		Amount:         big.NewInt(100),
		WeightedAmount: big.NewInt(200),
		StartTime:      1,
		ExpirationTime: 1001,
		StakeAddress:   addr,
		Id:             makeHash(1),
	}
	if err := active.Save(context); err != nil {
		t.Fatal(err)
	}

	revoked := &StakeInfo{
		Amount:         big.NewInt(300),
		WeightedAmount: big.NewInt(400),
		StartTime:      2,
		ExpirationTime: 1002,
		RevokeTime:     500,
		StakeAddress:   addr,
		Id:             makeHash(2),
	}
	if err := revoked.Save(context); err != nil {
		t.Fatal(err)
	}

	wrapped := &countingDB{DB: context}
	list, _, _, err := GetStakeListByAddress(wrapped, addr)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 active entry, got %d", len(list))
	}
	if list[0].Id != active.Id {
		t.Fatalf("expected active entry id, got %v", list[0].Id)
	}
	// Visited both rows (active + revoked) but not beyond.
	if wrapped.count != 2 {
		t.Fatalf("iterator visited %d rows, want 2", wrapped.count)
	}
}

// setupLiquidityStakes inserts nStake liquidity entries across nAddr
// distinct addresses (round-robin).
func setupLiquidityStakes(context db.DB, nAddr, nStake int) []types.Address {
	addresses := make([]types.Address, nAddr)
	for i := 0; i < nAddr; i++ {
		addresses[i] = makeAddress(byte(i + 1))
	}
	for i := 0; i < nStake; i++ {
		addr := addresses[i%nAddr]
		entry := &LiquidityStakeEntry{
			Amount:         big.NewInt(int64(100 + i)),
			WeightedAmount: big.NewInt(int64(200 + i)),
			StartTime:      int64(i),
			ExpirationTime: int64(i + 1000),
			StakeAddress:   addr,
			Id:             makeHash(byte(i)),
		}
		if err := entry.Save(context); err != nil {
			panic(err)
		}
	}
	return addresses
}

// TestGetLiquidityStakeListByAddressIteratesSubRange verifies that
// GetLiquidityStakeListByAddress reads only the rows for the requested
// address.
func TestGetLiquidityStakeListByAddressIteratesSubRange(t *testing.T) {
	context := db.NewMemDB()
	nAddr := 5
	nStake := 20
	addresses := setupLiquidityStakes(context, nAddr, nStake)

	wrapped := &countingDB{DB: context}
	list, total, weighted, err := GetLiquidityStakeListByAddress(wrapped, addresses[1])
	if err != nil {
		t.Fatal(err)
	}

	expectedPerAddr := nStake / nAddr
	if len(list) != expectedPerAddr {
		t.Fatalf("expected %d entries, got %d", expectedPerAddr, len(list))
	}
	if wrapped.count != expectedPerAddr {
		t.Fatalf("iterator visited %d rows, want %d (per-entity sub-range)", wrapped.count, expectedPerAddr)
	}

	expectedTotal := big.NewInt(0)
	expectedWeighted := big.NewInt(0)
	for i := 0; i < nStake; i++ {
		if i%nAddr == 1 {
			expectedTotal.Add(expectedTotal, big.NewInt(int64(100+i)))
			expectedWeighted.Add(expectedWeighted, big.NewInt(int64(200+i)))
		}
	}
	if total.Cmp(expectedTotal) != 0 {
		t.Fatalf("total = %s, want %s", total, expectedTotal)
	}
	if weighted.Cmp(expectedWeighted) != 0 {
		t.Fatalf("weighted = %s, want %s", weighted, expectedWeighted)
	}
}

// setupVotes inserts nVotes pillar votes on the given id, using
// distinct pillar names.
func setupVotes(context db.DB, id types.Hash, nVotes int) {
	for i := 0; i < nVotes; i++ {
		vote := &PillarVote{
			Id:   id,
			Name: string(rune('A'+i)) + "-pillar",
			Vote: VoteYes,
		}
		if err := context.Put(vote.Key(), vote.Data()); err != nil {
			panic(err)
		}
	}
}

// TestGetAllPillarVotesIteratesSubRange verifies that
// GetAllPillarVotes reads only the rows for the requested id, not the
// whole pillarVote prefix.
func TestGetAllPillarVotesIteratesSubRange(t *testing.T) {
	context := db.NewMemDB()

	id1 := makeHash(10)
	id2 := makeHash(20)

	setupVotes(context, id1, 3)
	setupVotes(context, id2, 7)

	wrapped := &countingDB{DB: context}
	votes := GetAllPillarVotes(wrapped, id1)
	if len(votes) != 3 {
		t.Fatalf("expected 3 votes for id1, got %d", len(votes))
	}
	// The iterator must have visited only id1's 3 rows, not all 10.
	if wrapped.count != 3 {
		t.Fatalf("iterator visited %d rows, want 3 (per-id sub-range)", wrapped.count)
	}
	for _, v := range votes {
		if v.Id != id1 {
			t.Fatalf("unexpected vote id %v", v.Id)
		}
	}
}

// TestGetVoteBreakdownAfterSubRangeIteration verifies that
// GetVoteBreakdown still tallies correctly after the sub-range change.
func TestGetVoteBreakdownAfterSubRangeIteration(t *testing.T) {
	context := db.NewMemDB()

	id := makeHash(10)
	setupVotes(context, id, 4)

	breakdown := GetVoteBreakdown(context, id)
	if breakdown.Yes != 4 {
		t.Fatalf("expected 4 yes votes, got %d", breakdown.Yes)
	}
	if breakdown.No != 0 {
		t.Fatalf("expected 0 no votes, got %d", breakdown.No)
	}
}
