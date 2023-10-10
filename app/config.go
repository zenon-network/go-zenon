package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli/v2"

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
	if dataDir := ctx.String(DataPathFlag.Name); ctx.IsSet(DataPathFlag.Name) && len(dataDir) > 0 {
		cfg.DataPath = dataDir
	}

	// Wallet
	if walletDir := ctx.String(WalletDirFlag.Name); ctx.IsSet(WalletDirFlag.Name) && len(walletDir) > 0 {
		cfg.WalletPath = walletDir
	}

	if genesisFile := ctx.String(GenesisFileFlag.Name); ctx.IsSet(GenesisFileFlag.Name) && len(genesisFile) > 0 {
		cfg.GenesisFile = genesisFile
	}

	// Network Config
	if identity := ctx.String(IdentityFlag.Name); ctx.IsSet(IdentityFlag.Name) && len(identity) > 0 {
		cfg.Name = identity
	}

	if ctx.IsSet(MaxPeersFlag.Name) {
		cfg.Net.MaxPeers = ctx.Int(MaxPeersFlag.Name)
	}

	if ctx.IsSet(MaxPendingPeersFlag.Name) {
		cfg.Net.MaxPendingPeers = ctx.Int(MaxPendingPeersFlag.Name)
	}

	if listenHost := ctx.String(ListenHostFlag.Name); ctx.IsSet(ListenHostFlag.Name) && len(listenHost) > 0 {
		cfg.RPC.HTTPHost = listenHost
	}

	if ctx.IsSet(ListenPortFlag.Name) {
		cfg.Net.ListenPort = ctx.Int(ListenPortFlag.Name)
	}

	// Http Config
	if ctx.IsSet(RPCEnabledFlag.Name) {
		cfg.RPC.EnableHTTP = ctx.Bool(RPCEnabledFlag.Name)
	}

	if httpHost := ctx.String(RPCListenAddrFlag.Name); ctx.IsSet(RPCListenAddrFlag.Name) && len(httpHost) > 0 {
		cfg.RPC.HTTPHost = httpHost
	}

	if ctx.IsSet(RPCPortFlag.Name) {
		cfg.RPC.HTTPPort = ctx.Int(RPCPortFlag.Name)
	}

	// WS Config
	if ctx.IsSet(WSEnabledFlag.Name) {
		cfg.RPC.EnableWS = ctx.Bool(WSEnabledFlag.Name)
	}

	if wsListenAddr := ctx.String(WSListenAddrFlag.Name); ctx.IsSet(WSListenAddrFlag.Name) && len(wsListenAddr) > 0 {
		cfg.RPC.WSHost = wsListenAddr
	}

	if ctx.IsSet(WSPortFlag.Name) {
		cfg.RPC.WSPort = ctx.Int(WSPortFlag.Name)
	}

	// Log Level Config
	if logLevel := ctx.String(LogLvlFlag.Name); ctx.IsSet(LogLvlFlag.Name) && len(logLevel) > 0 {
		cfg.LogLevel = logLevel
	}
}
func readConfigFromFile(ctx *cli.Context, cfg *node.Config) error {
	if file := ctx.String(ConfigFileFlag.Name); file != "" {
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
	if dataDir := ctx.String(DataPathFlag.Name); len(dataDir) > 0 {
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
