package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/urfave/cli.v1"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/node"
)

var defaultNodeConfigFileName = "config.json"

func MakeConfig(ctx *cli.Context) (*node.Config, error) {
	cfg := node.DefaultNodeConfig

	// 1: Load config file.
	err := readConfigFromFile(ctx, &cfg)
	if err != nil {
		return nil, err
	}

	// 2: Apply flags, Overwrite the configuration file configuration
	applyFlagsToConfig(ctx, &cfg)

	// 3: Make dir paths absolute
	if err := cfg.MakePathsAbsolute(); err != nil {
		return nil, err
	}

	// 4: Config log to file
	common.InitLogging(cfg.DataPath, cfg.LogLevel)

	// 5: Log config
	if j, err := json.MarshalIndent(cfg, "", "    "); err == nil {
		fmt.Printf("Using the following znnd config: %v\n", string(j))
	}
	log.Info("using znnd config", "config", cfg)

	return &cfg, nil
}

func applyFlagsToConfig(ctx *cli.Context, cfg *node.Config) {
	if dataDir := ctx.GlobalString(DataPathFlag.Name); ctx.GlobalIsSet(DataPathFlag.Name) && len(dataDir) > 0 {
		cfg.DataPath = dataDir
	}

	// Wallet
	if walletDir := ctx.GlobalString(WalletDirFlag.Name); ctx.GlobalIsSet(WalletDirFlag.Name) && len(walletDir) > 0 {
		cfg.WalletPath = walletDir
	}

	if genesisFile := ctx.GlobalString(GenesisFileFlag.Name); ctx.GlobalIsSet(GenesisFileFlag.Name) && len(genesisFile) > 0 {
		cfg.GenesisFile = genesisFile
	}

	// Network Config
	if identity := ctx.GlobalString(IdentityFlag.Name); ctx.GlobalIsSet(IdentityFlag.Name) && len(identity) > 0 {
		cfg.Name = identity
	}

	if ctx.GlobalIsSet(MaxPeersFlag.Name) {
		cfg.Net.MaxPeers = ctx.GlobalInt(MaxPeersFlag.Name)
	}

	if ctx.GlobalIsSet(MaxPendingPeersFlag.Name) {
		cfg.Net.MaxPendingPeers = ctx.GlobalInt(MaxPendingPeersFlag.Name)
	}

	if listenHost := ctx.GlobalString(ListenHostFlag.Name); ctx.GlobalIsSet(ListenHostFlag.Name) && len(listenHost) > 0 {
		cfg.RPC.HTTPHost = listenHost
	}

	if ctx.GlobalIsSet(ListenPortFlag.Name) {
		cfg.Net.ListenPort = ctx.GlobalInt(ListenPortFlag.Name)
	}

	// Http Config
	if ctx.GlobalIsSet(RPCEnabledFlag.Name) {
		cfg.RPC.EnableHTTP = ctx.GlobalBool(RPCEnabledFlag.Name)
	}

	if httpHost := ctx.GlobalString(RPCListenAddrFlag.Name); ctx.GlobalIsSet(RPCListenAddrFlag.Name) && len(httpHost) > 0 {
		cfg.RPC.HTTPHost = httpHost
	}

	if ctx.GlobalIsSet(RPCPortFlag.Name) {
		cfg.RPC.HTTPPort = ctx.GlobalInt(RPCPortFlag.Name)
	}

	// WS Config
	if ctx.GlobalIsSet(WSEnabledFlag.Name) {
		cfg.RPC.EnableWS = ctx.GlobalBool(WSEnabledFlag.Name)
	}

	if wsListenAddr := ctx.GlobalString(WSListenAddrFlag.Name); ctx.GlobalIsSet(WSListenAddrFlag.Name) && len(wsListenAddr) > 0 {
		cfg.RPC.WSHost = wsListenAddr
	}

	if ctx.GlobalIsSet(WSPortFlag.Name) {
		cfg.RPC.WSPort = ctx.GlobalInt(WSPortFlag.Name)
	}

	// Log Level Config
	if logLevel := ctx.GlobalString(LogLvlFlag.Name); ctx.GlobalIsSet(LogLvlFlag.Name) && len(logLevel) > 0 {
		cfg.LogLevel = logLevel
	}
}
func readConfigFromFile(ctx *cli.Context, cfg *node.Config) error {
	if file := ctx.GlobalString(ConfigFileFlag.Name); file != "" {
		if jsonConf, err := os.ReadFile(file); err == nil {
			err = json.Unmarshal(jsonConf, &cfg)
			if err == nil {
				return nil
			}

			log.Error("Config malformed: cannot unmarshal the config file content", "error", err)
			return err
		}
	}

	// second read default settings
	dataPath := cfg.DataPath
	if dataDir := ctx.GlobalString(DataPathFlag.Name); len(dataDir) > 0 {
		dataPath = dataDir
	}

	configPath := filepath.Join(dataPath, defaultNodeConfigFileName)

	if jsonConf, err := os.ReadFile(configPath); err == nil {
		err = json.Unmarshal(jsonConf, &cfg)
		if err == nil {
			return nil
		}
		log.Error("Config malformed: please check", "error", err)
		return err
	} else {
		log.Warn("Config file missing: you can provide a data path using the --data flag or provide a config file using the --config flag", "configPath", configPath)
	}
	return nil
}
