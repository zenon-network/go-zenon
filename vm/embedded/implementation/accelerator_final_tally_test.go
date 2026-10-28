package implementation

import (
	"math/big"
	"testing"
	"time"

	accountstore "github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// testFinalTallyContext is a minimal AccountVmContext for exercising
// the accelerator final-tally-on-expiry and late-vote-rejection
// logic. The frontier momentum timestamp is configurable so tests can
// simulate expired and non-expired voting windows.
type testFinalTallyContext struct {
	store.Account
	momentum          *nom.Momentum
	genesisMomentum   *nom.Momentum
	dynPlasmaEnforced bool
	pillars           []*definition.PillarInfo
}

func newTestFinalTallyContext(t *testing.T, frontierTime time.Time, dynPlasma bool) *testFinalTallyContext {
	t.Helper()
	genesisTime := frontierTime.Add(-time.Hour)
	return &testFinalTallyContext{
		Account: accountstore.NewAccountStore(types.AcceleratorContract, db.NewMemDB()),
		momentum: &nom.Momentum{
			ChainIdentifier: 100,
			Timestamp:       &frontierTime,
			Height:          1000,
		},
		genesisMomentum: &nom.Momentum{
			ChainIdentifier: 100,
			Timestamp:       &genesisTime,
			Height:          1,
		},
		dynPlasmaEnforced: dynPlasma,
	}
}

func (ctx *testFinalTallyContext) CacheStore() store.Cache { return nil }
func (ctx *testFinalTallyContext) MomentumStore() store.Momentum {
	return &testFinalTallyMomentumStore{pillars: ctx.pillars}
}

func (ctx *testFinalTallyContext) GetFrontierMomentum() (*nom.Momentum, error) {
	return ctx.momentum, nil
}
func (ctx *testFinalTallyContext) GetGenesisMomentum() *nom.Momentum { return ctx.genesisMomentum }

func (ctx *testFinalTallyContext) Save()  {}
func (ctx *testFinalTallyContext) Reset() {}
func (ctx *testFinalTallyContext) Done()  {}

func (ctx *testFinalTallyContext) AddBalance(ts *types.ZenonTokenStandard, amount *big.Int) {}
func (ctx *testFinalTallyContext) SubBalance(ts *types.ZenonTokenStandard, amount *big.Int) {}

func (ctx *testFinalTallyContext) IsAcceleratorSporkEnforced() bool        { return true }
func (ctx *testFinalTallyContext) IsHtlcSporkEnforced() bool               { return true }
func (ctx *testFinalTallyContext) IsBridgeAndLiquiditySporkEnforced() bool { return true }
func (ctx *testFinalTallyContext) IsDynamicPlasmaSporkEnforced() bool      { return ctx.dynPlasmaEnforced }
func (ctx *testFinalTallyContext) IsPtlcSporkEnforced() bool               { return false }

func (ctx *testFinalTallyContext) GetPillarWeights() (map[string]*big.Int, error) {
	return nil, nil
}
func (ctx *testFinalTallyContext) EpochTicker() common.Ticker {
	return common.NewTicker(time.Unix(0, 0), time.Second)
}
func (ctx *testFinalTallyContext) EpochStats(epoch uint64) (*api.EpochStats, error) {
	return nil, nil
}
func (ctx *testFinalTallyContext) GetPillarDelegationsByEpoch(epoch uint64) (map[string]*types.PillarDelegationDetail, error) {
	return nil, nil
}

// testFinalTallyMomentumStore is a minimal store.Momentum that only
// serves GetActivePillars; every other method is a nil-dereference
// guard (never called by the code under test).
type testFinalTallyMomentumStore struct {
	store.Momentum
	pillars []*definition.PillarInfo
}

func (ms *testFinalTallyMomentumStore) GetActivePillars() ([]*definition.PillarInfo, error) {
	return ms.pillars, nil
}

// seedProject stores a project with the given id, creation timestamp
// and status, plus its votable-hash marker.
func seedProject(t *testing.T, ctx *testFinalTallyContext, id types.Hash, creationTs int64, status uint8) {
	t.Helper()
	project := &definition.Project{
		Id:                id,
		Owner:             types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz"),
		Name:              "test-project",
		Description:       "test",
		Url:               "https://example.com",
		ZnnFundsNeeded:    big.NewInt(100),
		QsrFundsNeeded:    big.NewInt(100),
		CreationTimestamp: creationTs,
		Status:            status,
		PhaseIds:          []types.Hash{},
	}
	project.Save(ctx.Storage())

	votable := &definition.VotableHash{Id: id, Exists: true}
	votable.Save(ctx.Storage())
}

// seedPhase stores a phase with the given id and project id, plus its
// votable-hash marker.
func seedPhase(t *testing.T, ctx *testFinalTallyContext, id types.Hash, projectId types.Hash, creationTs int64, status uint8) {
	t.Helper()
	phase := &definition.Phase{
		Id:                id,
		ProjectId:         projectId,
		Name:              "test-phase",
		Description:       "test",
		Url:               "https://example.com",
		ZnnFundsNeeded:    big.NewInt(50),
		QsrFundsNeeded:    big.NewInt(50),
		CreationTimestamp: creationTs,
		Status:            status,
	}
	phase.Save(ctx.Storage())

	votable := &definition.VotableHash{Id: id, Exists: true}
	votable.Save(ctx.Storage())
}

// seedVote stores a single pillar vote on the given id.
func seedVote(t *testing.T, ctx *testFinalTallyContext, id types.Hash, pillarName string, vote uint8) {
	t.Helper()
	pv := &definition.PillarVote{
		Id:   id,
		Name: pillarName,
		Vote: vote,
	}
	pv.Save(ctx.Storage())
}

// seedPillarInfo creates a PillarInfo for the active pillar list.
func makeTestPillar(name string, stakeAddr, prodAddr types.Address) *definition.PillarInfo {
	return &definition.PillarInfo{
		Name:                  name,
		BlockProducingAddress: prodAddr,
		RewardWithdrawAddress: stakeAddr,
		StakeAddress:          stakeAddr,
		Amount:                big.NewInt(100),
		RegistrationTime:      0,
		RevokeTime:            0,
		PillarType:            definition.AnyPillarType,
	}
}

// makeProjectHash returns a deterministic hash for test project n.
func makeProjectHash(n byte) types.Hash {
	var h types.Hash
	h[types.HashSize-1] = n
	return h
}

// runAcceleratorUpdate calls UpdateEmbeddedAcceleratorMethod.ReceiveBlock
// with a properly formed send block.
func runAcceleratorUpdate(t *testing.T, ctx *testFinalTallyContext) error {
	t.Helper()
	method := &UpdateEmbeddedAcceleratorMethod{definition.UpdateMethodName}
	sendBlock := &nom.AccountBlock{
		Address:   types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz"),
		ToAddress: types.AcceleratorContract,
		Data:      definition.ABIAccelerator.PackMethodPanic(definition.UpdateMethodName),
		Amount:    big.NewInt(0),
	}
	_, err := method.ReceiveBlock(ctx, sendBlock)
	return err
}

// TestAcceleratorFinalTally_PostSpork_ExpiredWindow_PassingVotes_Activates
// verifies that post-spork, when the voting window has expired and the
// stored votes would pass the acceptance threshold, the project is
// activated (not closed).
func TestAcceleratorFinalTally_PostSpork_ExpiredWindow_PassingVotes_Activates(t *testing.T) {
	frontierTime := time.Unix(2_000_000, 0)
	ctx := newTestFinalTallyContext(t, frontierTime, true) // post-spork

	projectId := makeProjectHash(1)
	// Project created long ago → window expired.
	creationTs := frontierTime.Unix() - constants.AcceleratorProjectVotingPeriod - 1
	seedProject(t, ctx, projectId, creationTs, definition.VotingStatus)

	// Seed enough yes votes to pass: need Total*100 > numPillars*33 and Yes > No.
	stakeAddr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	ctx.pillars = []*definition.PillarInfo{
		makeTestPillar("p1", stakeAddr, stakeAddr),
		makeTestPillar("p2", stakeAddr, stakeAddr),
		makeTestPillar("p3", stakeAddr, stakeAddr),
	}
	seedVote(t, ctx, projectId, "p1", definition.VoteYes)
	seedVote(t, ctx, projectId, "p2", definition.VoteYes)
	seedVote(t, ctx, projectId, "p3", definition.VoteYes)

	err := runAcceleratorUpdate(t, ctx)
	common.FailIfErr(t, err)

	project, err := definition.GetProjectEntry(ctx.Storage(), projectId)
	common.FailIfErr(t, err)
	if project.Status != definition.ActiveStatus {
		t.Fatalf("expected project to be activated, got status %d", project.Status)
	}
	if project.LastUpdateTimestamp != frontierTime.Unix() {
		t.Fatalf("expected LastUpdateTimestamp %d, got %d",
			frontierTime.Unix(), project.LastUpdateTimestamp)
	}
}

// TestAcceleratorFinalTally_PostSpork_ExpiredWindow_FailingVotes_Closes
// verifies that post-spork, when the voting window has expired and the
// stored votes would NOT pass the acceptance threshold, the project is
// closed.
func TestAcceleratorFinalTally_PostSpork_ExpiredWindow_FailingVotes_Closes(t *testing.T) {
	frontierTime := time.Unix(2_000_000, 0)
	ctx := newTestFinalTallyContext(t, frontierTime, true) // post-spork

	projectId := makeProjectHash(2)
	creationTs := frontierTime.Unix() - constants.AcceleratorProjectVotingPeriod - 1
	seedProject(t, ctx, projectId, creationTs, definition.VotingStatus)

	// Seed only "no" votes → fails.
	stakeAddr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	ctx.pillars = []*definition.PillarInfo{
		makeTestPillar("p1", stakeAddr, stakeAddr),
		makeTestPillar("p2", stakeAddr, stakeAddr),
		makeTestPillar("p3", stakeAddr, stakeAddr),
	}
	seedVote(t, ctx, projectId, "p1", definition.VoteNo)
	seedVote(t, ctx, projectId, "p2", definition.VoteNo)
	seedVote(t, ctx, projectId, "p3", definition.VoteNo)

	err := runAcceleratorUpdate(t, ctx)
	common.FailIfErr(t, err)

	project, err := definition.GetProjectEntry(ctx.Storage(), projectId)
	common.FailIfErr(t, err)
	if project.Status != definition.ClosedStatus {
		t.Fatalf("expected project to be closed, got status %d", project.Status)
	}
}

// TestAcceleratorFinalTally_PreSpork_ExpiredWindow_ClosesWithoutTally
// verifies that pre-spork, an expired project is closed without any
// tally — even when the stored votes would pass.
func TestAcceleratorFinalTally_PreSpork_ExpiredWindow_ClosesWithoutTally(t *testing.T) {
	frontierTime := time.Unix(2_000_000, 0)
	ctx := newTestFinalTallyContext(t, frontierTime, false) // pre-spork

	projectId := makeProjectHash(3)
	creationTs := frontierTime.Unix() - constants.AcceleratorProjectVotingPeriod - 1
	seedProject(t, ctx, projectId, creationTs, definition.VotingStatus)

	// Seed enough yes votes to pass — but pre-spork they are ignored.
	stakeAddr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")
	ctx.pillars = []*definition.PillarInfo{
		makeTestPillar("p1", stakeAddr, stakeAddr),
		makeTestPillar("p2", stakeAddr, stakeAddr),
		makeTestPillar("p3", stakeAddr, stakeAddr),
	}
	seedVote(t, ctx, projectId, "p1", definition.VoteYes)
	seedVote(t, ctx, projectId, "p2", definition.VoteYes)
	seedVote(t, ctx, projectId, "p3", definition.VoteYes)

	err := runAcceleratorUpdate(t, ctx)
	common.FailIfErr(t, err)

	project, err := definition.GetProjectEntry(ctx.Storage(), projectId)
	common.FailIfErr(t, err)
	if project.Status != definition.ClosedStatus {
		t.Fatalf("pre-spork: expected project to be closed without tally, got status %d", project.Status)
	}
}

// TestAcceleratorFinalTally_PostSpork_VoteOnExpiredProject_Rejected
// verifies that post-spork, voting on a project whose window has
// elapsed is rejected with ErrDataNonExistent — for both VoteByName
// and VoteByProdAddress.
func TestAcceleratorFinalTally_PostSpork_VoteOnExpiredProject_Rejected(t *testing.T) {
	frontierTime := time.Unix(2_000_000, 0)
	stakeAddr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")

	t.Run("VoteByName", func(t *testing.T) {
		ctx := newTestFinalTallyContext(t, frontierTime, true)
		projectId := makeProjectHash(4)
		creationTs := frontierTime.Unix() - constants.AcceleratorProjectVotingPeriod - 1
		seedProject(t, ctx, projectId, creationTs, definition.VotingStatus)

		ctx.pillars = []*definition.PillarInfo{
			makeTestPillar("p1", stakeAddr, stakeAddr),
		}

		method := &VoteByNameMethod{definition.VoteByNameMethodName}
		sendBlock := &nom.AccountBlock{
			Address:   stakeAddr,
			ToAddress: types.AcceleratorContract,
			Data:      definition.ABICommon.PackMethodPanic(definition.VoteByNameMethodName, projectId, "p1", definition.VoteYes),
			Amount:    big.NewInt(0),
		}
		_, err := method.ReceiveBlock(ctx, sendBlock)
		if err != constants.ErrDataNonExistent {
			t.Fatalf("expected ErrDataNonExistent for expired project vote, got %v", err)
		}
	})

	t.Run("VoteByProdAddress", func(t *testing.T) {
		ctx := newTestFinalTallyContext(t, frontierTime, true)
		projectId := makeProjectHash(5)
		creationTs := frontierTime.Unix() - constants.AcceleratorProjectVotingPeriod - 1
		seedProject(t, ctx, projectId, creationTs, definition.VotingStatus)

		ctx.pillars = []*definition.PillarInfo{
			makeTestPillar("p1", stakeAddr, stakeAddr),
		}

		method := &VoteByProdAddressMethod{definition.VoteByProdAddressMethodName}
		sendBlock := &nom.AccountBlock{
			Address:   stakeAddr,
			ToAddress: types.AcceleratorContract,
			Data:      definition.ABICommon.PackMethodPanic(definition.VoteByProdAddressMethodName, projectId, definition.VoteYes),
			Amount:    big.NewInt(0),
		}
		_, err := method.ReceiveBlock(ctx, sendBlock)
		if err != constants.ErrDataNonExistent {
			t.Fatalf("expected ErrDataNonExistent for expired project vote, got %v", err)
		}
	})
}

// TestAcceleratorFinalTally_PostSpork_VoteOnNonExpiredProject_Accepted
// verifies that post-spork, voting on a project whose window is still
// open is accepted.
func TestAcceleratorFinalTally_PostSpork_VoteOnNonExpiredProject_Accepted(t *testing.T) {
	frontierTime := time.Unix(2_000_000, 0)
	stakeAddr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")

	t.Run("VoteByName", func(t *testing.T) {
		ctx := newTestFinalTallyContext(t, frontierTime, true)
		projectId := makeProjectHash(6)
		// Window still open.
		creationTs := frontierTime.Unix() - constants.AcceleratorProjectVotingPeriod + 100
		seedProject(t, ctx, projectId, creationTs, definition.VotingStatus)

		ctx.pillars = []*definition.PillarInfo{
			makeTestPillar("p1", stakeAddr, stakeAddr),
		}

		method := &VoteByNameMethod{definition.VoteByNameMethodName}
		sendBlock := &nom.AccountBlock{
			Address:   stakeAddr,
			ToAddress: types.AcceleratorContract,
			Data:      definition.ABICommon.PackMethodPanic(definition.VoteByNameMethodName, projectId, "p1", definition.VoteYes),
			Amount:    big.NewInt(0),
		}
		if _, err := method.ReceiveBlock(ctx, sendBlock); err != nil {
			t.Fatalf("expected vote on non-expired project to succeed, got %v", err)
		}
	})

	t.Run("VoteByProdAddress", func(t *testing.T) {
		ctx := newTestFinalTallyContext(t, frontierTime, true)
		projectId := makeProjectHash(7)
		creationTs := frontierTime.Unix() - constants.AcceleratorProjectVotingPeriod + 100
		seedProject(t, ctx, projectId, creationTs, definition.VotingStatus)

		ctx.pillars = []*definition.PillarInfo{
			makeTestPillar("p1", stakeAddr, stakeAddr),
		}

		method := &VoteByProdAddressMethod{definition.VoteByProdAddressMethodName}
		sendBlock := &nom.AccountBlock{
			Address:   stakeAddr,
			ToAddress: types.AcceleratorContract,
			Data:      definition.ABICommon.PackMethodPanic(definition.VoteByProdAddressMethodName, projectId, definition.VoteYes),
			Amount:    big.NewInt(0),
		}
		if _, err := method.ReceiveBlock(ctx, sendBlock); err != nil {
			t.Fatalf("expected vote on non-expired project to succeed, got %v", err)
		}
	})
}

// TestAcceleratorFinalTally_PostSpork_VoteOnPhaseId_NotAffected
// verifies that post-spork, voting on a phase id (not a project id)
// is NOT affected by the project voting-window check.
func TestAcceleratorFinalTally_PostSpork_VoteOnPhaseId_NotAffected(t *testing.T) {
	frontierTime := time.Unix(2_000_000, 0)
	stakeAddr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")

	t.Run("VoteByName", func(t *testing.T) {
		ctx := newTestFinalTallyContext(t, frontierTime, true)
		projectId := makeProjectHash(8)
		phaseId := makeProjectHash(9)
		// Project created long ago, but the phase was created recently.
		projectCreationTs := frontierTime.Unix() - constants.AcceleratorProjectVotingPeriod - 1
		phaseCreationTs := frontierTime.Unix() - 100
		seedProject(t, ctx, projectId, projectCreationTs, definition.ActiveStatus)
		seedPhase(t, ctx, phaseId, projectId, phaseCreationTs, definition.VotingStatus)

		ctx.pillars = []*definition.PillarInfo{
			makeTestPillar("p1", stakeAddr, stakeAddr),
		}

		method := &VoteByNameMethod{definition.VoteByNameMethodName}
		sendBlock := &nom.AccountBlock{
			Address:   stakeAddr,
			ToAddress: types.AcceleratorContract,
			Data:      definition.ABICommon.PackMethodPanic(definition.VoteByNameMethodName, phaseId, "p1", definition.VoteYes),
			Amount:    big.NewInt(0),
		}
		if _, err := method.ReceiveBlock(ctx, sendBlock); err != nil {
			t.Fatalf("expected vote on phase id to succeed, got %v", err)
		}
	})

	t.Run("VoteByProdAddress", func(t *testing.T) {
		ctx := newTestFinalTallyContext(t, frontierTime, true)
		projectId := makeProjectHash(10)
		phaseId := makeProjectHash(11)
		projectCreationTs := frontierTime.Unix() - constants.AcceleratorProjectVotingPeriod - 1
		phaseCreationTs := frontierTime.Unix() - 100
		seedProject(t, ctx, projectId, projectCreationTs, definition.ActiveStatus)
		seedPhase(t, ctx, phaseId, projectId, phaseCreationTs, definition.VotingStatus)

		ctx.pillars = []*definition.PillarInfo{
			makeTestPillar("p1", stakeAddr, stakeAddr),
		}

		method := &VoteByProdAddressMethod{definition.VoteByProdAddressMethodName}
		sendBlock := &nom.AccountBlock{
			Address:   stakeAddr,
			ToAddress: types.AcceleratorContract,
			Data:      definition.ABICommon.PackMethodPanic(definition.VoteByProdAddressMethodName, phaseId, definition.VoteYes),
			Amount:    big.NewInt(0),
		}
		if _, err := method.ReceiveBlock(ctx, sendBlock); err != nil {
			t.Fatalf("expected vote on phase id to succeed, got %v", err)
		}
	})
}

// TestAcceleratorFinalTally_PreSpork_VoteOnExpiredProject_Accepted
// verifies that pre-spork, voting on a project whose window has
// elapsed is still accepted (today's behavior).
func TestAcceleratorFinalTally_PreSpork_VoteOnExpiredProject_Accepted(t *testing.T) {
	frontierTime := time.Unix(2_000_000, 0)
	stakeAddr := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")

	t.Run("VoteByName", func(t *testing.T) {
		ctx := newTestFinalTallyContext(t, frontierTime, false) // pre-spork
		projectId := makeProjectHash(12)
		creationTs := frontierTime.Unix() - constants.AcceleratorProjectVotingPeriod - 1
		seedProject(t, ctx, projectId, creationTs, definition.VotingStatus)

		ctx.pillars = []*definition.PillarInfo{
			makeTestPillar("p1", stakeAddr, stakeAddr),
		}

		method := &VoteByNameMethod{definition.VoteByNameMethodName}
		sendBlock := &nom.AccountBlock{
			Address:   stakeAddr,
			ToAddress: types.AcceleratorContract,
			Data:      definition.ABICommon.PackMethodPanic(definition.VoteByNameMethodName, projectId, "p1", definition.VoteYes),
			Amount:    big.NewInt(0),
		}
		if _, err := method.ReceiveBlock(ctx, sendBlock); err != nil {
			t.Fatalf("pre-spork: expected vote on expired project to succeed, got %v", err)
		}
	})

	t.Run("VoteByProdAddress", func(t *testing.T) {
		ctx := newTestFinalTallyContext(t, frontierTime, false) // pre-spork
		projectId := makeProjectHash(13)
		creationTs := frontierTime.Unix() - constants.AcceleratorProjectVotingPeriod - 1
		seedProject(t, ctx, projectId, creationTs, definition.VotingStatus)

		ctx.pillars = []*definition.PillarInfo{
			makeTestPillar("p1", stakeAddr, stakeAddr),
		}

		method := &VoteByProdAddressMethod{definition.VoteByProdAddressMethodName}
		sendBlock := &nom.AccountBlock{
			Address:   stakeAddr,
			ToAddress: types.AcceleratorContract,
			Data:      definition.ABICommon.PackMethodPanic(definition.VoteByProdAddressMethodName, projectId, definition.VoteYes),
			Amount:    big.NewInt(0),
		}
		if _, err := method.ReceiveBlock(ctx, sendBlock); err != nil {
			t.Fatalf("pre-spork: expected vote on expired project to succeed, got %v", err)
		}
	})
}
