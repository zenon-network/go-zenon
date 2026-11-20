package implementation

import (
	"math/big"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// These tests cover the electorate used by checkAcceleratorVotes:
// post-spork only votes cast by pillars in the active list count,
// so a pillar that voted and later revoked (a revoke never deletes
// votes) cannot move a tally whose turnout is measured against the
// active pillar count. Pre-spork every stored vote still counts.
//
// Revoked pillars are modelled by seeding a vote under a name that
// is absent from ctx.pillars, which is exactly what GetActivePillars
// returns after a revoke: the vote row stays, the pillar row does
// not.

var electorateStakeAddr = types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")

// makeActivePillars returns n active pillars named p1..pn.
func makeActivePillars(n int) []*definition.PillarInfo {
	pillars := make([]*definition.PillarInfo, 0, n)
	for i := 1; i <= n; i++ {
		name := "p" + string(rune('0'+i))
		pillars = append(pillars, makeTestPillar(name, electorateStakeAddr, electorateStakeAddr))
	}
	return pillars
}

// seedTurnoutFromRevoked seeds a project in voting with eight active
// pillars and three yes votes, one of them from a revoked pillar.
// Turnout needs Total*100 > 8*33 = 264, so three counted votes pass
// and two do not: the stored record passes, the live electorate
// does not.
func seedTurnoutFromRevoked(t *testing.T, ctx *testFinalTallyContext, projectId types.Hash, creationTs int64) {
	t.Helper()
	seedProject(t, ctx, projectId, creationTs, definition.VotingStatus)
	ctx.pillars = makeActivePillars(8)
	seedVote(t, ctx, projectId, "p1", definition.VoteYes)
	seedVote(t, ctx, projectId, "p2", definition.VoteYes)
	seedVote(t, ctx, projectId, "revoked-a", definition.VoteYes)
}

// seedMajorityFromRevoked seeds a project in voting with three active
// pillars voting 2 yes / 1 no and two revoked pillars voting no. The
// stored record is 2 yes / 3 no and fails; the live electorate is
// 2 yes / 1 no at full turnout and passes.
func seedMajorityFromRevoked(t *testing.T, ctx *testFinalTallyContext, projectId types.Hash, creationTs int64) {
	t.Helper()
	seedProject(t, ctx, projectId, creationTs, definition.VotingStatus)
	ctx.pillars = makeActivePillars(3)
	seedVote(t, ctx, projectId, "p1", definition.VoteYes)
	seedVote(t, ctx, projectId, "p2", definition.VoteYes)
	seedVote(t, ctx, projectId, "p3", definition.VoteNo)
	seedVote(t, ctx, projectId, "revoked-a", definition.VoteNo)
	seedVote(t, ctx, projectId, "revoked-b", definition.VoteNo)
}

func projectStatus(t *testing.T, ctx *testFinalTallyContext, projectId types.Hash) uint8 {
	t.Helper()
	project, err := definition.GetProjectEntry(ctx.Storage(), projectId)
	common.FailIfErr(t, err)
	return project.Status
}

// TestAcceleratorElectorate_PostSpork_RevokedVoteDoesNotCountTowardTurnout
// verifies that post-spork a revoked pillar's yes vote no longer
// lifts a project over the turnout threshold: in the window the
// project stays in voting, and at the final tally on an expired
// window it is closed.
func TestAcceleratorElectorate_PostSpork_RevokedVoteDoesNotCountTowardTurnout(t *testing.T) {
	frontierTime := time.Unix(2_000_000, 0)

	t.Run("InWindow", func(t *testing.T) {
		ctx := newTestFinalTallyContext(t, frontierTime, true)
		projectId := makeProjectHash(21)
		seedTurnoutFromRevoked(t, ctx, projectId, frontierTime.Unix()-100)

		common.FailIfErr(t, runAcceleratorUpdate(t, ctx))

		if got := projectStatus(t, ctx, projectId); got != definition.VotingStatus {
			t.Fatalf("post-spork: revoked vote counted toward turnout, project status %d, want voting (%d)", got, definition.VotingStatus)
		}
	})

	t.Run("ExpiredWindowFinalTally", func(t *testing.T) {
		ctx := newTestFinalTallyContext(t, frontierTime, true)
		projectId := makeProjectHash(22)
		seedTurnoutFromRevoked(t, ctx, projectId, frontierTime.Unix()-constants.AcceleratorProjectVotingPeriod-1)

		common.FailIfErr(t, runAcceleratorUpdate(t, ctx))

		if got := projectStatus(t, ctx, projectId); got != definition.ClosedStatus {
			t.Fatalf("post-spork: revoked vote counted at the final tally, project status %d, want closed (%d)", got, definition.ClosedStatus)
		}
	})
}

// TestAcceleratorElectorate_PreSpork_RevokedVoteStillCounts pins
// today's behaviour: pre-spork every stored vote counts, so the same
// seeds activate the project.
func TestAcceleratorElectorate_PreSpork_RevokedVoteStillCounts(t *testing.T) {
	frontierTime := time.Unix(2_000_000, 0)
	ctx := newTestFinalTallyContext(t, frontierTime, false)
	projectId := makeProjectHash(23)
	seedTurnoutFromRevoked(t, ctx, projectId, frontierTime.Unix()-100)

	common.FailIfErr(t, runAcceleratorUpdate(t, ctx))

	if got := projectStatus(t, ctx, projectId); got != definition.ActiveStatus {
		t.Fatalf("pre-spork: expected stored votes to activate the project, got status %d", got)
	}
}

// TestAcceleratorElectorate_RevokedNoVotesDoNotBlockMajority verifies
// the other direction: post-spork, no votes left behind by revoked
// pillars cannot outvote the live electorate's majority, while
// pre-spork they still do.
func TestAcceleratorElectorate_RevokedNoVotesDoNotBlockMajority(t *testing.T) {
	frontierTime := time.Unix(2_000_000, 0)

	t.Run("PostSpork", func(t *testing.T) {
		ctx := newTestFinalTallyContext(t, frontierTime, true)
		projectId := makeProjectHash(24)
		seedMajorityFromRevoked(t, ctx, projectId, frontierTime.Unix()-100)

		common.FailIfErr(t, runAcceleratorUpdate(t, ctx))

		if got := projectStatus(t, ctx, projectId); got != definition.ActiveStatus {
			t.Fatalf("post-spork: revoked no votes blocked the live majority, project status %d, want active (%d)", got, definition.ActiveStatus)
		}
	})

	t.Run("PreSpork", func(t *testing.T) {
		ctx := newTestFinalTallyContext(t, frontierTime, false)
		projectId := makeProjectHash(25)
		seedMajorityFromRevoked(t, ctx, projectId, frontierTime.Unix()-100)

		common.FailIfErr(t, runAcceleratorUpdate(t, ctx))

		if got := projectStatus(t, ctx, projectId); got != definition.VotingStatus {
			t.Fatalf("pre-spork: expected stored no votes to keep the project in voting, got status %d", got)
		}
	})
}

// TestAcceleratorElectorate_PhasePayout verifies the phase path: an
// active project's phase whose turnout depends on a revoked pillar's
// vote is paid pre-spork and stays in voting post-spork, with no
// payout blocks produced.
func TestAcceleratorElectorate_PhasePayout(t *testing.T) {
	frontierTime := time.Unix(2_000_000, 0)

	seed := func(t *testing.T, ctx *testFinalTallyContext, projectId, phaseId types.Hash) {
		t.Helper()
		seedProject(t, ctx, projectId, frontierTime.Unix()-1000, definition.ActiveStatus)
		seedPhase(t, ctx, phaseId, projectId, frontierTime.Unix()-100, definition.VotingStatus)
		project, err := definition.GetProjectEntry(ctx.Storage(), projectId)
		common.FailIfErr(t, err)
		project.PhaseIds = []types.Hash{phaseId}
		project.Save(ctx.Storage())
		// The contract can cover the phase (seedPhase asks for 50 of each).
		common.FailIfErr(t, ctx.SetBalance(types.ZnnTokenStandard, big.NewInt(1000)))
		common.FailIfErr(t, ctx.SetBalance(types.QsrTokenStandard, big.NewInt(1000)))

		ctx.pillars = makeActivePillars(8)
		seedVote(t, ctx, phaseId, "p1", definition.VoteYes)
		seedVote(t, ctx, phaseId, "p2", definition.VoteYes)
		seedVote(t, ctx, phaseId, "revoked-a", definition.VoteYes)
	}

	run := func(t *testing.T, ctx *testFinalTallyContext) []*nom.AccountBlock {
		t.Helper()
		method := &UpdateEmbeddedAcceleratorMethod{definition.UpdateMethodName}
		sendBlock := &nom.AccountBlock{
			Address:   electorateStakeAddr,
			ToAddress: types.AcceleratorContract,
			Data:      definition.ABIAccelerator.PackMethodPanic(definition.UpdateMethodName),
			Amount:    big.NewInt(0),
		}
		blocks, err := method.ReceiveBlock(ctx, sendBlock)
		common.FailIfErr(t, err)
		return blocks
	}

	t.Run("PostSpork", func(t *testing.T) {
		ctx := newTestFinalTallyContext(t, frontierTime, true)
		projectId, phaseId := makeProjectHash(26), makeProjectHash(27)
		seed(t, ctx, projectId, phaseId)

		blocks := run(t, ctx)

		phase, err := definition.GetPhaseEntry(ctx.Storage(), phaseId)
		common.FailIfErr(t, err)
		if phase.Status != definition.VotingStatus {
			t.Fatalf("post-spork: revoked vote paid the phase, status %d, want voting (%d)", phase.Status, definition.VotingStatus)
		}
		if len(blocks) != 0 {
			t.Fatalf("post-spork: expected no payout blocks, got %d", len(blocks))
		}
	})

	t.Run("PreSpork", func(t *testing.T) {
		ctx := newTestFinalTallyContext(t, frontierTime, false)
		projectId, phaseId := makeProjectHash(28), makeProjectHash(29)
		seed(t, ctx, projectId, phaseId)

		blocks := run(t, ctx)

		phase, err := definition.GetPhaseEntry(ctx.Storage(), phaseId)
		common.FailIfErr(t, err)
		if phase.Status != definition.PaidStatus {
			t.Fatalf("pre-spork: expected stored votes to pay the phase, status %d", phase.Status)
		}
		if len(blocks) != 2 {
			t.Fatalf("pre-spork: expected a ZNN and a QSR payout block, got %d", len(blocks))
		}
	})
}

// TestAcceleratorElectorate_ActiveVotesUnaffected verifies that when
// every vote comes from an active pillar the post-spork tally equals
// the stored record, so the filter changes nothing for honest polls.
func TestAcceleratorElectorate_ActiveVotesUnaffected(t *testing.T) {
	frontierTime := time.Unix(2_000_000, 0)
	ctx := newTestFinalTallyContext(t, frontierTime, true)
	projectId := makeProjectHash(30)
	seedProject(t, ctx, projectId, frontierTime.Unix()-100, definition.VotingStatus)
	ctx.pillars = makeActivePillars(8)
	seedVote(t, ctx, projectId, "p1", definition.VoteYes)
	seedVote(t, ctx, projectId, "p2", definition.VoteYes)
	seedVote(t, ctx, projectId, "p3", definition.VoteYes)

	common.FailIfErr(t, runAcceleratorUpdate(t, ctx))

	if got := projectStatus(t, ctx, projectId); got != definition.ActiveStatus {
		t.Fatalf("post-spork: three active yes votes of eight should activate the project, got status %d", got)
	}
}
