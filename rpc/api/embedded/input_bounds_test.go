package embedded

import (
	"math"
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// storeCountingChain counts the frontier store handles a handler asks
// for, so a test can assert that a rejected request read no state at
// all: every embedded read starts with api.GetFrontierContext, which
// takes both handles.
type storeCountingChain struct {
	*requiredPoWQuoteChain
	storeLookups int
}

func (c *storeCountingChain) GetFrontierMomentumStore() store.Momentum {
	c.storeLookups++
	return c.requiredPoWQuoteChain.GetFrontierMomentumStore()
}

func (c *storeCountingChain) GetFrontierAccountStore(addr types.Address) store.Account {
	c.storeLookups++
	return c.requiredPoWQuoteChain.GetFrontierAccountStore(addr)
}

// A name one byte past the pillar contract's maximum can never be
// registered, so the epoch history method rejects it before reading
// state or synthesizing rows that carry the name. The longest valid
// name is still served as before, synthesized rows included.
func TestPillarEpochHistory_RejectsOversizedNameBeforeReadingState(t *testing.T) {
	user := types.Address{0, 0x73}
	c := &storeCountingChain{requiredPoWQuoteChain: epochHistoryChain(t, types.PillarContract, user, "p", 9)}
	pillarApi := &PillarApi{chain: c}

	tooLong := strings.Repeat("n", constants.PillarNameLengthMax+1)
	result, err := pillarApi.GetPillarEpochHistory(tooLong, 0, api.RpcMaxPageSize)
	if err != api.ErrNameParamTooLong {
		t.Fatalf("oversized name: err = %v, want ErrNameParamTooLong", err)
	}
	if result != nil {
		t.Fatalf("oversized name: got a result with %d rows, want nil", len(result.List))
	}
	if c.storeLookups != 0 {
		t.Fatalf("oversized name: handler took %d store handles before rejecting, want 0", c.storeLookups)
	}

	longest := strings.Repeat("n", constants.PillarNameLengthMax)
	result, err = pillarApi.GetPillarEpochHistory(longest, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.List) != 4 || result.List[0].Name != longest || result.List[0].Epoch != 9 {
		t.Fatalf("longest valid name: page 0 size 4 = %+v, want 4 synthesized rows from epoch 9", result)
	}
	if c.storeLookups == 0 {
		t.Fatal("longest valid name: the accepted request read no state")
	}
}

// pillarVoteChain is a frontier chain whose accelerator account holds one
// vote by the named pillar on id.
func pillarVoteChain(t *testing.T, name string, id types.Hash) *storeCountingChain {
	t.Helper()
	accStore := account.NewAccountStore(types.AcceleratorContract, db.NewMemDB())
	(&definition.PillarVote{Id: id, Name: name, Vote: definition.VoteYes}).Save(accStore.Storage())
	return &storeCountingChain{requiredPoWQuoteChain: &requiredPoWQuoteChain{
		momentum: &requiredPoWQuoteMomentum{frontier: &nom.Momentum{Height: 1}},
		account:  accStore,
	}}
}

// The vote lookup hashes the name once per requested id, so both the
// name length and the list length are bounded before the first read:
// a name past the pillar maximum and a list past the page bound are
// errors; the longest name and the fullest list are served.
func TestPillarVotes_RejectsOversizedNameAndListBeforeReadingState(t *testing.T) {
	id := types.NewHash([]byte{7})
	c := pillarVoteChain(t, "p", id)
	acceleratorApi := &AcceleratorApi{chain: c}

	tooLong := strings.Repeat("n", constants.PillarNameLengthMax+1)
	result, err := acceleratorApi.GetPillarVotes(tooLong, []types.Hash{id})
	if err != api.ErrNameParamTooLong {
		t.Fatalf("oversized name: err = %v, want ErrNameParamTooLong", err)
	}
	if result != nil {
		t.Fatalf("oversized name: got %d results, want nil", len(result))
	}
	if c.storeLookups != 0 {
		t.Fatalf("oversized name: handler took %d store handles before rejecting, want 0", c.storeLookups)
	}

	tooMany := make([]types.Hash, api.RpcMaxPageSize+1)
	result, err = acceleratorApi.GetPillarVotes("p", tooMany)
	if err != api.ErrListParamTooBig {
		t.Fatalf("%d hashes: err = %v, want ErrListParamTooBig", len(tooMany), err)
	}
	if result != nil {
		t.Fatalf("%d hashes: got %d results, want nil", len(tooMany), len(result))
	}
	if c.storeLookups != 0 {
		t.Fatalf("%d hashes: handler took %d store handles before rejecting, want 0", len(tooMany), c.storeLookups)
	}

	// The fullest list the bound admits is served in order: the id the
	// pillar voted on resolves, every other entry is nil.
	fullest := make([]types.Hash, api.RpcMaxPageSize)
	fullest[7] = id
	result, err = acceleratorApi.GetPillarVotes("p", fullest)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != api.RpcMaxPageSize {
		t.Fatalf("%d hashes: got %d results", len(fullest), len(result))
	}
	if result[7] == nil || result[7].Name != "p" || result[7].Id != id || result[7].Vote != definition.VoteYes {
		t.Fatalf("vote at index 7 = %+v, want the stored yes vote", result[7])
	}
	if result[0] != nil || result[len(result)-1] != nil {
		t.Fatalf("entries without a vote are not nil: first %+v, last %+v", result[0], result[len(result)-1])
	}
	if c.storeLookups == 0 {
		t.Fatal("the accepted request read no state")
	}

	// The longest valid name is looked up, not rejected.
	longest := strings.Repeat("n", constants.PillarNameLengthMax)
	result, err = acceleratorApi.GetPillarVotes(longest, []types.Hash{id})
	if err != nil || len(result) != 1 || result[0] != nil {
		t.Fatalf("longest valid name: result = %+v, err = %v; want one nil entry and no error", result, err)
	}

	// An empty and a nil list are served as an empty result, as before.
	for _, hashes := range [][]types.Hash{{}, nil} {
		result, err = acceleratorApi.GetPillarVotes("p", hashes)
		if err != nil || result == nil || len(result) != 0 {
			t.Fatalf("%d hashes: result = %+v, err = %v; want an empty list and no error", len(hashes), result, err)
		}
	}
}

// The bound counts bytes, as the contract's registration check does, so
// a multibyte name is measured the same way on both sides: 40 bytes of
// it pass, 41 do not, whatever the rune count.
func TestPillarName_BoundCountsBytes(t *testing.T) {
	twoBytes := "é" // U+00E9, two bytes in UTF-8
	if len(twoBytes) != 2 {
		t.Fatalf("fixture rune is %d bytes", len(twoBytes))
	}
	within := strings.Repeat(twoBytes, constants.PillarNameLengthMax/2)
	if err := checkPillarName(within); err != nil {
		t.Fatalf("%d-byte multibyte name rejected: %v", len(within), err)
	}
	past := within + "n"
	if err := checkPillarName(past); err != api.ErrNameParamTooLong {
		t.Fatalf("%d-byte multibyte name: err = %v, want ErrNameParamTooLong", len(past), err)
	}
	if err := checkPillarName(""); err != nil {
		t.Fatalf("empty name rejected: %v", err)
	}
}

// The embedded handlers read contract state at the account-pool
// frontier, which can be ahead of the momentum they report against; a
// request created at a height the momentum view has not reached must
// count down from there, not wrap to zero and report "final" or
// "redeemable" for a request no momentum has confirmed.
func TestBridgeCountdowns_DoNotUnderflowWhenMomentumIsBehindRequest(t *testing.T) {
	bridgeApi := &BridgeApi{}
	cases := []struct {
		name      string
		height    uint64 // momentum.Height
		creation  uint64 // request creation / registration height
		threshold uint32 // confirmations to finality / redeem delay
		want      uint64
	}{
		{"momentum one behind the request", 10, 11, 20, 21},
		{"momentum far behind the request", 1, 11, 20, 30},
		{"momentum behind, zero threshold", 10, 11, 0, 1},
		{"same height", 10, 10, 20, 20},
		{"partway", 15, 10, 20, 15},
		{"one short of final", 29, 10, 20, 1},
		{"exactly final", 30, 10, 20, 0},
		{"past final", 100, 10, 20, 0},
		{"zero threshold at creation", 10, 10, 0, 0},
		{"request at the top of the range", math.MaxUint64, math.MaxUint64, 5, 5},
		{"momentum at the top of the range", math.MaxUint64, 10, 20, 0},
		{"remainder past the range saturates", 1, math.MaxUint64, 2, math.MaxUint64},
	}
	for _, c := range cases {
		momentum := nom.Momentum{Height: c.height}
		got := bridgeApi.getConfirmationsToFinality(definition.WrapTokenRequest{CreationMomentumHeight: c.creation}, c.threshold, momentum)
		if got != c.want {
			t.Errorf("%s: confirmationsToFinality(height %d, creation %d, threshold %d) = %d, want %d", c.name, c.height, c.creation, c.threshold, got, c.want)
		}
		got = bridgeApi.getRedeemableIn(definition.UnwrapTokenRequest{RegistrationMomentumHeight: c.creation}, definition.TokenPair{RedeemDelay: c.threshold}, momentum)
		if got != c.want {
			t.Errorf("%s: redeemableIn(height %d, registration %d, delay %d) = %d, want %d", c.name, c.height, c.creation, c.threshold, got, c.want)
		}
	}
}
