package definition

import (
	"testing"

	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

func putVote(t *testing.T, context db.DB, id types.Hash, name string, vote uint8) {
	t.Helper()
	pv := &PillarVote{Id: id, Name: name, Vote: vote}
	if err := context.Put(pv.Key(), pv.Data()); err != nil {
		t.Fatal(err)
	}
}

func expectBreakdown(t *testing.T, got *VoteBreakdown, id types.Hash, total, yes, no uint32) {
	t.Helper()
	if got.Id != id || got.Total != total || got.Yes != yes || got.No != no {
		t.Fatalf("breakdown = {total %d yes %d no %d}, want {total %d yes %d no %d}",
			got.Total, got.Yes, got.No, total, yes, no)
	}
}

// TestGetVoteBreakdownForPillars verifies that the filtered tally
// counts only the votes of the named pillars, that abstentions still
// count toward Total, and that the unfiltered tally keeps every
// stored vote.
func TestGetVoteBreakdownForPillars(t *testing.T) {
	context := db.NewMemDB()
	id := makeHash(20)
	putVote(t, context, id, "active-yes", VoteYes)
	putVote(t, context, id, "active-no", VoteNo)
	putVote(t, context, id, "active-abstain", VoteAbstain)
	putVote(t, context, id, "revoked-yes", VoteYes)
	putVote(t, context, id, "revoked-no", VoteNo)

	expectBreakdown(t, GetVoteBreakdown(context, id), id, 5, 2, 2)

	eligible := map[string]struct{}{
		"active-yes":     {},
		"active-no":      {},
		"active-abstain": {},
		"never-voted":    {},
	}
	expectBreakdown(t, GetVoteBreakdownForPillars(context, id, eligible), id, 3, 1, 1)

	// Votes on another id are not visible through this id.
	other := makeHash(21)
	putVote(t, context, other, "active-yes", VoteNo)
	expectBreakdown(t, GetVoteBreakdownForPillars(context, id, eligible), id, 3, 1, 1)
}

// TestGetVoteBreakdownForPillars_EmptyElectorate verifies that a nil
// or empty eligibility set counts nothing rather than everything.
func TestGetVoteBreakdownForPillars_EmptyElectorate(t *testing.T) {
	context := db.NewMemDB()
	id := makeHash(22)
	setupVotes(context, id, 3)

	expectBreakdown(t, GetVoteBreakdownForPillars(context, id, nil), id, 0, 0, 0)
	expectBreakdown(t, GetVoteBreakdownForPillars(context, id, map[string]struct{}{}), id, 0, 0, 0)
	expectBreakdown(t, GetVoteBreakdown(context, id), id, 3, 3, 0)
}
