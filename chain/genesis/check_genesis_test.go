package genesis

import (
	"encoding/json"
	"math/big"
	"os"
	"path"
	"testing"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

var (
	checkUserA = types.ParseAddressPanic("z1qq6eg8n43g032hanpsfp02qcdmv7zfj3y2lt5d")
	checkUserB = types.ParseAddressPanic("z1qqleq9qc2u3039fly4ld5qgngdeapa3yks0e3l")
	checkSpork = types.ParseAddressPanic("z1qq9n7fpaqd8lpcljandzmx4xtku9w4ftwyg0mq")
)

// validCheckConfig returns a small config that passes CheckGenesis and
// exercises every section the per-entry checks look at: one pillar at
// the standard stake, one legacy entry, one fusion, one swap entry and
// a user balance in both tokens.
func validCheckConfig() *GenesisConfig {
	stake := new(big.Int).Set(constants.PillarStakeAmount)
	return &GenesisConfig{
		ChainIdentifier:     100,
		GenesisTimestampSec: 1000000000,
		SporkAddress:        &checkSpork,
		PillarConfig: &PillarContractConfig{
			Pillars: []*definition.PillarInfo{{
				Name:                         "pillar",
				BlockProducingAddress:        checkUserA,
				RewardWithdrawAddress:        checkUserA,
				StakeAddress:                 checkUserA,
				Amount:                       new(big.Int).Set(stake),
				GiveDelegateRewardPercentage: 100,
				PillarType:                   definition.LegacyPillarType,
			}},
			Delegations: []*definition.DelegationInfo{},
			LegacyEntries: []*definition.LegacyPillarEntry{{
				KeyIdHash:   types.HexToHashPanic("01e209fc055989dbcc2b31f28aac892a0d1f4dd3f6dfb5921835537d0e11fbb1"),
				PillarCount: 1,
			}},
		},
		TokenConfig: &TokenContractConfig{
			Tokens: []*definition.TokenInfo{
				{
					Owner:         types.PillarContract,
					TokenName:     "ZNN",
					TokenSymbol:   "ZNN",
					TotalSupply:   new(big.Int).Add(stake, big.NewInt(100)),
					MaxSupply:     big.NewInt(4611686018427387903),
					Decimals:      8,
					TokenStandard: types.ZnnTokenStandard,
				},
				{
					Owner:         types.StakeContract,
					TokenName:     "QSR",
					TokenSymbol:   "QSR",
					TotalSupply:   big.NewInt(60),
					MaxSupply:     big.NewInt(4611686018427387903),
					Decimals:      8,
					TokenStandard: types.QsrTokenStandard,
				},
			},
		},
		PlasmaConfig: &PlasmaContractConfig{
			Fusions: []*definition.FusionInfo{{
				Owner:       checkUserA,
				Id:          types.HexToHashPanic("85b559d52ef7e23b358cbceb12aeadcf5397bad860d8d50c4cd348246379e913"),
				Amount:      big.NewInt(10),
				Beneficiary: checkUserA,
			}},
		},
		SwapConfig: &SwapContractConfig{
			Entries: []*definition.SwapAssets{{
				KeyIdHash: types.HexToHashPanic("04ceaccbda764cbcdb57ec9d5bdbb4af062582691ef16f1ce1aaf4bc912303ce"),
				Znn:       big.NewInt(5),
				Qsr:       big.NewInt(5),
			}},
		},
		GenesisBlocks: &GenesisBlocksConfig{
			Blocks: []*GenesisBlockConfig{
				{
					Address:     types.PillarContract,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{types.ZnnTokenStandard: new(big.Int).Set(stake)},
				},
				{
					Address:     types.PlasmaContract,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{types.QsrTokenStandard: big.NewInt(10)},
				},
				{
					Address: checkUserA,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{
						types.ZnnTokenStandard: big.NewInt(100),
						types.QsrTokenStandard: big.NewInt(50),
					},
				},
			},
		},
	}
}

// aggregateChecks are the checks CheckGenesis ran before the per-entry
// checks were added. Each rejected case below must still pass all of
// them, which shows the config was accepted before.
func aggregateChecks(g *GenesisConfig) error {
	for _, check := range []func(*GenesisConfig) error{CheckFieldsExist, CheckPlasmaInfo, CheckSwapAccount, CheckPillarBalance, CheckTokenTotalSupply} {
		if err := check(g); err != nil {
			return err
		}
	}
	return nil
}

func writeConfig(t *testing.T, g *GenesisConfig) string {
	t.Helper()
	data, err := json.Marshal(g)
	common.FailIfErr(t, err)
	file := path.Join(t.TempDir(), "genesis.json")
	common.FailIfErr(t, os.WriteFile(file, data, 0600))
	return file
}

func TestCheckGenesisAcceptsValidConfig(t *testing.T) {
	g := validCheckConfig()
	common.FailIfErr(t, CheckGenesis(g))
	store, err := ReadGenesisConfigFromFile(writeConfig(t, g))
	common.FailIfErr(t, err)
	if store == nil {
		t.Fatal("expected a genesis store")
	}
}

// Each case was accepted by the aggregate checks alone and is now
// rejected with its own named error, both by CheckGenesis and by
// ReadGenesisConfigFromFile.
func TestCheckGenesisRejectsInvalidEntries(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(g *GenesisConfig)
		want   error
	}{
		{
			// -1 and +101 sum to the same supply as 100, but
			// SetBalance stores the absolute value.
			name: "negative balance",
			mutate: func(g *GenesisConfig) {
				g.GenesisBlocks.Blocks[2].BalanceList[types.ZnnTokenStandard] = big.NewInt(-1)
				g.GenesisBlocks.Blocks = append(g.GenesisBlocks.Blocks, &GenesisBlockConfig{
					Address:     checkUserB,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{types.ZnnTokenStandard: big.NewInt(101)},
				})
			},
			want: ErrGenesisNegativeBalance,
		},
		{
			// 11 and -1 sum to the plasma balance of 10.
			name: "negative fusion",
			mutate: func(g *GenesisConfig) {
				g.PlasmaConfig.Fusions[0].Amount = big.NewInt(11)
				g.PlasmaConfig.Fusions = append(g.PlasmaConfig.Fusions, &definition.FusionInfo{
					Owner:       checkUserB,
					Id:          types.HexToHashPanic("8d3a261582f0f7d01b0a7c6024da87e5637b5929cb71bc0f954cf14093e3059e"),
					Amount:      big.NewInt(-1),
					Beneficiary: checkUserB,
				})
			},
			want: ErrGenesisNegativeFusion,
		},
		{
			name: "negative swap amount",
			mutate: func(g *GenesisConfig) {
				g.SwapConfig.Entries[0].Qsr = big.NewInt(-1)
			},
			want: ErrGenesisNegativeSwapAmount,
		},
		{
			// The supply check counts both entries; state keeps
			// only the second.
			name: "duplicate address",
			mutate: func(g *GenesisConfig) {
				g.GenesisBlocks.Blocks[2].BalanceList = map[types.ZenonTokenStandard]*big.Int{
					types.ZnnTokenStandard: big.NewInt(50),
					types.QsrTokenStandard: big.NewInt(25),
				}
				g.GenesisBlocks.Blocks = append(g.GenesisBlocks.Blocks, &GenesisBlockConfig{
					Address: checkUserA,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{
						types.ZnnTokenStandard: big.NewInt(50),
						types.QsrTokenStandard: big.NewInt(25),
					},
				})
			},
			want: ErrGenesisDuplicateAddress,
		},
		{
			name: "zero legacy slots",
			mutate: func(g *GenesisConfig) {
				g.PillarConfig.LegacyEntries[0].PillarCount = 0
			},
			want: ErrGenesisZeroLegacySlots,
		},
		{
			// The pillar contract balance matches the one-unit
			// stake, but revoke would refund PillarStakeAmount.
			name: "non-standard pillar stake",
			mutate: func(g *GenesisConfig) {
				g.PillarConfig.Pillars[0].Amount = big.NewInt(1)
				g.GenesisBlocks.Blocks[0].BalanceList[types.ZnnTokenStandard] = big.NewInt(1)
				g.TokenConfig.Tokens[0].TotalSupply = big.NewInt(101)
			},
			want: ErrGenesisPillarStakeAmount,
		},
		{
			name: "pillar stake above the standard amount",
			mutate: func(g *GenesisConfig) {
				more := new(big.Int).Add(constants.PillarStakeAmount, big.NewInt(1))
				g.PillarConfig.Pillars[0].Amount = more
				g.GenesisBlocks.Blocks[0].BalanceList[types.ZnnTokenStandard] = new(big.Int).Set(more)
				g.TokenConfig.Tokens[0].TotalSupply = new(big.Int).Add(more, big.NewInt(100))
			},
			want: ErrGenesisPillarStakeAmount,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := validCheckConfig()
			tc.mutate(g)
			if err := aggregateChecks(g); err != nil {
				t.Fatalf("case should pass the aggregate checks, got %v", err)
			}
			if err := CheckGenesis(g); !errors.Is(err, tc.want) {
				t.Fatalf("CheckGenesis: expected %v, got %v", tc.want, err)
			}
			if _, err := ReadGenesisConfigFromFile(writeConfig(t, g)); err != ErrInvalidGenesisConfig {
				t.Fatalf("ReadGenesisConfigFromFile: expected ErrInvalidGenesisConfig, got %v", err)
			}
		})
	}
}

// Nil entries and nil amounts used to reach a nil dereference inside
// the aggregate checks or state construction; they are now rejected
// with a named error before either runs.
func TestCheckGenesisRejectsNilEntries(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(g *GenesisConfig)
		want   error
	}{
		{"nil genesis block", func(g *GenesisConfig) { g.GenesisBlocks.Blocks[1] = nil }, ErrGenesisNilEntry},
		{"nil balance", func(g *GenesisConfig) { g.GenesisBlocks.Blocks[2].BalanceList[types.ZnnTokenStandard] = nil }, ErrGenesisNilAmount},
		{"nil fusion", func(g *GenesisConfig) { g.PlasmaConfig.Fusions[0] = nil }, ErrGenesisNilEntry},
		{"nil fusion amount", func(g *GenesisConfig) { g.PlasmaConfig.Fusions[0].Amount = nil }, ErrGenesisNilAmount},
		{"nil swap entry", func(g *GenesisConfig) { g.SwapConfig.Entries[0] = nil }, ErrGenesisNilEntry},
		{"nil swap amount", func(g *GenesisConfig) { g.SwapConfig.Entries[0].Znn = nil }, ErrGenesisNilAmount},
		{"nil token", func(g *GenesisConfig) { g.TokenConfig.Tokens[1] = nil }, ErrGenesisNilEntry},
		{"nil token supply", func(g *GenesisConfig) { g.TokenConfig.Tokens[1].TotalSupply = nil }, ErrGenesisNilAmount},
		{"nil legacy entry", func(g *GenesisConfig) { g.PillarConfig.LegacyEntries[0] = nil }, ErrGenesisNilEntry},
		{"nil pillar", func(g *GenesisConfig) { g.PillarConfig.Pillars[0] = nil }, ErrGenesisNilEntry},
		{"nil pillar amount", func(g *GenesisConfig) { g.PillarConfig.Pillars[0].Amount = nil }, ErrGenesisPillarStakeAmount},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := validCheckConfig()
			tc.mutate(g)
			if err := CheckGenesis(g); !errors.Is(err, tc.want) {
				t.Fatalf("CheckGenesis: expected %v, got %v", tc.want, err)
			}
		})
	}
}

// The devnet genesis shipped under docker/devnet must keep loading.
func TestDevnetGenesisFixturePasses(t *testing.T) {
	store, err := ReadGenesisConfigFromFile(path.Join("..", "..", "docker", "devnet", "genesis.json"))
	common.FailIfErr(t, err)
	if store == nil {
		t.Fatal("expected a genesis store")
	}
}
