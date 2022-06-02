package app

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/tyler-smith/go-bip39"
	"github.com/zenon-network/go-zenon/chain/genesis"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/node"
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
	"gopkg.in/urfave/cli.v1"
)

var (
	GenesisBlockFlag = cli.StringSliceFlag{
		Name:  "genesis-block",
		Usage: "<address>,<ZnnAmount>,<QsrAmount>",
	}

	devnetCommand = cli.Command{
		Action:    devnetAction,
		Name:      "generate-devnet",
		Usage:     "Generates config for devnet",
		ArgsUsage: " ",
		Category:  "DEVELOPER COMMANDS",

		Flags: []cli.Flag{
			GenesisBlockFlag,
		},
	}
)

func devnetAction(ctx *cli.Context) error {

	cfg := node.DefaultNodeConfig

	// 1: Apply flags, Overwrite the configuration file configuration
	applyFlagsToConfig(ctx, &cfg)

	// 2: Make dir paths absolute
	if err := cfg.MakePathsAbsolute(); err != nil {
		return err
	}

	// 3: Check/Create dirs
	if err := checkCreatePaths(&cfg); err != nil {
		return err
	}

	// 4: Generate Producer,
	if err := createDevProducer(&cfg); err != nil {
		return err
	}

	// 5. Generate NetConfig
	// TODO add flag for IP address to generate seeders to share with others
	if err := createDevNet(&cfg); err != nil {
		return err
	}

	// 6. Generate Genesis Config
	if err := createDevGenesis(ctx, &cfg); err != nil {
		return err
	}

	// write config
	configPath := filepath.Join(cfg.DataPath, "config.json")
	file, _ := json.MarshalIndent(cfg, "", " ")
	_ = ioutil.WriteFile(configPath, file, 0700)

	return nil
}

func checkCreatePaths(cfg *node.Config) error {
	// Abort if datapath already exists
	if _, err := os.Stat(cfg.DataPath); err == nil {
		return errors.New("datapath already exists")
	}
	if err := os.MkdirAll(cfg.DataPath, 0700); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.WalletPath, 0700); err != nil {
		return err
	}
	return nil
}

func createDevProducer(cfg *node.Config) error {
	entropy, _ := bip39.NewEntropy(256)
	mnemonic, _ := bip39.NewMnemonic(entropy)

	ks := &wallet.KeyStore{
		Entropy:  entropy,
		Seed:     bip39.NewSeed(mnemonic, ""),
		Mnemonic: mnemonic,
	}
	_, kp, _ := ks.DeriveForIndexPath(0)
	ks.BaseAddress = kp.Address

	password := "Don'tTrust.Verify"
	kf, _ := ks.Encrypt(password)
	kf.Path = filepath.Join(cfg.WalletPath, ks.BaseAddress.String())
	kf.Write()

	producer := node.ProducerConfig{
		Address:     kp.Address.String(),
		Index:       0,
		KeyFilePath: kf.Path,
		Password:    password,
	}

	cfg.Producer = &producer

	return nil
}

func createDevNet(cfg *node.Config) error {
	// generate network key
	// ask for ip address via flag??

	privateKeyFile := filepath.Join(cfg.DataPath, p2p.DefaultNetPrivateKeyFile)

	key, err := crypto.GenerateKey()
	if err != nil {
		log.Crit("Failed to generate node key", "reason", err)
	}

	if err := crypto.SaveECDSA(privateKeyFile, key); err != nil {
		log.Error("Failed to persist node key", "reason", err)
	}

	cfg.Net.MinPeers = 0
	cfg.Net.MinConnectedPeers = 0
	cfg.Net.Seeders = []string{}
	return nil
}

func createDevGenesis(ctx *cli.Context, cfg *node.Config) error {
	if cfg.GenesisFile == "" {
		cfg.GenesisFile = filepath.Join(cfg.DataPath, "genesis.json")
	}

	localPillar, _ := types.ParseAddress(cfg.Producer.Address)

	znnStandard := definition.TokenInfo{
		Decimals:      8,
		IsBurnable:    true,
		IsMintable:    true,
		IsUtility:     true,
		MaxSupply:     big.NewInt(9007199254740991),
		Owner:         types.TokenContract,
		TokenDomain:   "biginches.club",
		TokenName:     "tZNN",
		TokenStandard: types.ZnnTokenStandard,
		TokenSymbol:   "tZNN",
		TotalSupply:   big.NewInt(78713599988800),
	}
	qsrStandard := definition.TokenInfo{
		Decimals:      8,
		IsBurnable:    true,
		IsMintable:    true,
		IsUtility:     true,
		MaxSupply:     big.NewInt(9007199254740991),
		Owner:         types.TokenContract,
		TokenDomain:   "biginches.club",
		TokenName:     "tQSR",
		TokenStandard: types.QsrTokenStandard,
		TokenSymbol:   "tQSR",
		TotalSupply:   big.NewInt(772135999888000),
	}

	gen := genesis.GenesisConfig{
		ChainIdentifier:     321,
		ExtraData:           "/thank_you_bich_dao",
		GenesisTimestampSec: time.Now().Unix(),
		SporkAddress:        &localPillar,

		PillarConfig: &genesis.PillarContractConfig{
			Delegations:   []*definition.DelegationInfo{},
			LegacyEntries: []*definition.LegacyPillarEntry{},
			Pillars: []*definition.PillarInfo{
				&definition.PillarInfo{
					Name:                         "Local",
					Amount:                       big.NewInt(1500000000000),
					BlockProducingAddress:        localPillar,
					StakeAddress:                 localPillar,
					RewardWithdrawAddress:        localPillar,
					PillarType:                   1,
					RevokeTime:                   0,
					GiveBlockRewardPercentage:    0,
					GiveDelegateRewardPercentage: 100,
				},
			}},
		TokenConfig: &genesis.TokenContractConfig{
			Tokens: []*definition.TokenInfo{
				&znnStandard,
				&qsrStandard,
			}},
		PlasmaConfig: &genesis.PlasmaContractConfig{
			Fusions: []*definition.FusionInfo{}},
		SwapConfig: &genesis.SwapContractConfig{
			Entries: []*definition.SwapAssets{}},
		SporkConfig: &genesis.SporkConfig{
			Sporks: []*definition.Spork{
				&definition.Spork{
					Id:                types.HexToHashPanic("6d2b1e6cb4025f2f45533f0fe22e9b7ce2014d91cc960471045fa64eee5a6ba3"),
					Name:              "AZ",
					Description:       "AZ",
					Activated:         true,
					EnforcementHeight: 0,
				},
			}},

		GenesisBlocks: &genesis.GenesisBlocksConfig{
			Blocks: []*genesis.GenesisBlockConfig{
				&genesis.GenesisBlockConfig{
					Address: types.PillarContract,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{
						types.ZnnTokenStandard: big.NewInt(1500000000000),
					},
				},
				&genesis.GenesisBlockConfig{
					Address: types.AcceleratorContract,
					BalanceList: map[types.ZenonTokenStandard]*big.Int{
						types.ZnnTokenStandard: big.NewInt(77213599988800),
						types.QsrTokenStandard: big.NewInt(772135999888000),
					},
				},
			},
		}}

	if ctx.IsSet(GenesisBlockFlag.Name) {
		input := ctx.StringSlice(GenesisBlockFlag.Name)
		for _, s := range input {
			// todo check and handle errors
			// e.g. can't use same account twice
			ss := strings.Split(s, ",")
			a, _ := types.ParseAddress(ss[0])
			z, _ := strconv.ParseInt(ss[1], 10, 64)
			q, _ := strconv.ParseInt(ss[2], 10, 64)
			znn := big.NewInt(z * constants.Decimals)
			qsr := big.NewInt(q * constants.Decimals)

			znnStandard.TotalSupply.Add(znnStandard.TotalSupply, znn)
			qsrStandard.TotalSupply.Add(qsrStandard.TotalSupply, qsr)
			block := genesis.GenesisBlockConfig{
				Address: a,
				BalanceList: map[types.ZenonTokenStandard]*big.Int{
					types.ZnnTokenStandard: znn,
					types.QsrTokenStandard: qsr,
				},
			}
			gen.GenesisBlocks.Blocks = append(gen.GenesisBlocks.Blocks, &block)

		}
	}

	// TODO add checks

	file, _ := json.MarshalIndent(gen, "", " ")
	_ = ioutil.WriteFile(cfg.GenesisFile, file, 0644)

	return nil
}
