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

const (
	bridgeCountdownNetworkClass = uint32(2)
	bridgeCountdownChainId      = uint32(123)
	bridgeCountdownDelay        = uint32(20) // confirmations to finality and redeem delay alike
)

var (
	bridgeCountdownWrapId   = types.NewHash([]byte("wrap"))
	bridgeCountdownUnwrapTx = types.NewHash([]byte("unwrap"))
)

// bridgeCountdownChain is a frontier chain reporting the momentum at
// height whose bridge account holds the orchestrator configuration, a
// network with one ZNN token pair, one wrap request created at creation
// and one unwrap request registered at creation. The account store is
// the account pool's view, so creation may lie ahead of height, as it
// does for a request whose receive executed against a frontier the
// momentum chain has not confirmed yet.
func bridgeCountdownChain(t *testing.T, height, creation uint64) *requiredPoWQuoteChain {
	t.Helper()
	accStore := account.NewAccountStore(types.BridgeContract, db.NewMemDB())
	storage := accStore.Storage()
	info := &definition.OrchestratorInfo{WindowSize: 10, KeyGenThreshold: 1, ConfirmationsToFinality: bridgeCountdownDelay, EstimatedMomentumTime: 10}
	if err := info.Save(storage); err != nil {
		t.Fatal(err)
	}
	network, err := definition.EncodeNetworkInfo(&definition.NetworkInfo{
		NetworkClass:    bridgeCountdownNetworkClass,
		Id:              bridgeCountdownChainId,
		Name:            "test",
		ContractAddress: "0x0000000000000000000000000000000000000001",
		Metadata:        "{}",
		TokenPairs: []definition.TokenPair{{
			TokenStandard: types.ZnnTokenStandard,
			TokenAddress:  "0x5fbdb2315678afecb367f032d93f642f64180aa3",
			Bridgeable:    true,
			Redeemable:    true,
			MinAmount:     big.NewInt(1),
			RedeemDelay:   bridgeCountdownDelay,
			Metadata:      "{}",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := network.Save(storage); err != nil {
		t.Fatal(err)
	}
	wrap := &definition.WrapTokenRequest{
		NetworkClass:           bridgeCountdownNetworkClass,
		ChainId:                bridgeCountdownChainId,
		Id:                     bridgeCountdownWrapId,
		ToAddress:              "0xb794f5ea0ba39494ce839613fffba74279579268",
		TokenStandard:          types.ZnnTokenStandard,
		TokenAddress:           "0x5fbdb2315678afecb367f032d93f642f64180aa3",
		Amount:                 big.NewInt(100),
		Fee:                    big.NewInt(0),
		CreationMomentumHeight: creation,
	}
	if err := wrap.Save(storage); err != nil {
		t.Fatal(err)
	}
	unwrap := &definition.UnwrapTokenRequest{
		RegistrationMomentumHeight: creation,
		NetworkClass:               bridgeCountdownNetworkClass,
		ChainId:                    bridgeCountdownChainId,
		TransactionHash:            bridgeCountdownUnwrapTx,
		LogIndex:                   0,
		ToAddress:                  types.Address{0, 0x73},
		TokenAddress:               "0x5fbdb2315678afecb367f032d93f642f64180aa3",
		TokenStandard:              types.ZnnTokenStandard,
		Amount:                     big.NewInt(100),
	}
	if err := unwrap.Save(storage); err != nil {
		t.Fatal(err)
	}
	return &requiredPoWQuoteChain{
		momentum: &requiredPoWQuoteMomentum{frontier: &nom.Momentum{Height: height}},
		account:  accStore,
	}
}

// The public getters report the countdown against the momentum the
// context quotes, read from a store that can already hold a request
// created ahead of it. Such a request must come back with a positive
// countdown, not as final or redeemable; a request behind the momentum
// counts down as before and reaches zero at the delay.
func TestBridgeGetters_CountdownStaysPositiveForRequestAheadOfMomentum(t *testing.T) {
	cases := []struct {
		name     string
		height   uint64
		creation uint64
		want     uint64
	}{
		{"request one ahead of the momentum", 10, 11, 21},
		{"request far ahead of the momentum", 1, 11, 30},
		{"request behind the momentum", 25, 11, 6},
		{"request exactly final", 31, 11, 0},
	}
	for _, c := range cases {
		bridgeApi := &BridgeApi{chain: bridgeCountdownChain(t, c.height, c.creation)}

		wrap, err := bridgeApi.GetWrapTokenRequestById(bridgeCountdownWrapId)
		if err != nil {
			t.Fatalf("%s: GetWrapTokenRequestById: %v", c.name, err)
		}
		if wrap.ConfirmationsToFinality != c.want {
			t.Errorf("%s: getWrapTokenRequestById reports %d confirmations to finality (momentum %d, created %d, threshold %d), want %d", c.name, wrap.ConfirmationsToFinality, c.height, c.creation, bridgeCountdownDelay, c.want)
		}

		unwrap, err := bridgeApi.GetUnwrapTokenRequestByHashAndLog(bridgeCountdownUnwrapTx, 0)
		if err != nil {
			t.Fatalf("%s: GetUnwrapTokenRequestByHashAndLog: %v", c.name, err)
		}
		if unwrap.RedeemableIn != c.want {
			t.Errorf("%s: getUnwrapTokenRequestByHashAndLog reports redeemable in %d (momentum %d, registered %d, delay %d), want %d", c.name, unwrap.RedeemableIn, c.height, c.creation, bridgeCountdownDelay, c.want)
		}
	}
}
