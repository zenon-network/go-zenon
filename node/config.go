package node

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain/genesis"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/metadata"
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/wallet"
	"github.com/zenon-network/go-zenon/zenon"
)

type ProducerConfig struct {
	Address     string
	Index       uint32
	KeyFilePath string
	Password    string
}
type RPCConfig struct {
	EnableHTTP bool
	EnableWS   bool

	HTTPHost string
	HTTPPort int
	WSHost   string
	WSPort   int

	Endpoints []string

	HTTPVirtualHosts []string
	HTTPCors         []string
	WSOrigins        []string
}
type NetConfig struct {
	ListenHost string
	ListenPort int

	MinPeers          int
	MinConnectedPeers int
	MaxPeers          int
	MaxPendingPeers   int

	Seeders []string
}

type Config struct {
	DataPath    string // default ~/.zenon
	WalletPath  string // default DataPath/wallet
	GenesisFile string // GenesisFile is the absolute path to the genesis file

	Name string

	LogLevel string // "debug", "dbug" | "info" | "warn" | "error", "error" | "crit"

	Producer *ProducerConfig
	RPC      RPCConfig
	Net      NetConfig
}

func (c *Config) MakePathsAbsolute() error {
	if c.DataPath == "" {
		c.DataPath = DefaultDataDir()
	} else {
		absDataDir, err := filepath.Abs(c.DataPath)
		if err != nil {
			return err
		}
		c.DataPath = absDataDir
	}

	if c.WalletPath == "" {
		c.WalletPath = filepath.Join(c.DataPath, DefaultWalletDir)
	} else {
		c.WalletPath = ReplaceHomeVariable(c.WalletPath)
		absWalletDir, err := filepath.Abs(c.WalletPath)
		if err != nil {
			return err
		}
		c.WalletPath = absWalletDir
	}

	if c.GenesisFile != "" {
		c.GenesisFile = ReplaceHomeVariable(c.GenesisFile)
		absGenesisFile, err := filepath.Abs(c.GenesisFile)
		if err != nil {
			return err
		}
		c.GenesisFile = absGenesisFile
	}

	return nil
}

func (c *Config) makeZenonConfig(walletManager *wallet.Manager) (*zenon.Config, error) {
	pillarCoinbase, err := c.parseProducer(walletManager)
	if err != nil {
		return nil, err
	}

	return &zenon.Config{
		MinPeers:          c.Net.MinPeers,
		MinConnectedPeers: c.Net.MinConnectedPeers,
		ProducingKeyPair:  pillarCoinbase,
		GenesisConfig:     c.makeGenesisConfig(),
		DataDir:           c.DataPath,
	}, nil
}
func (c *Config) makeGenesisConfig() (genesisConfig store.Genesis) {
	var err error
	var path string

	if c.GenesisFile != "" {
		path = c.GenesisFile
		genesisConfig, err = genesis.ReadGenesisConfigFromFile(c.GenesisFile)
	} else {
		genesisConfig, err = genesis.MakeEmbeddedGenesisConfig()
		if err == genesis.ErrNoEmbeddedGenesis {
			log.Crit("no embedded genesis found and no genesis was specified")
			os.Exit(1)
		} else {
			log.Info("using embedded genesis")
			return
		}
	}

	if err == nil {
		fmt.Printf("Loaded a valid genesis config from path '%v'\n", path)
		log.Info("loaded a valid genesis config")
		return
	} else {
		log.Crit("no valid genesis file. Stopping ...", "reason", err)
		fmt.Printf("no valid genesis file. Reason: '%v'. Stopping ...\n", err)
		os.Exit(1)
		return
	}
}
func (c *Config) parseProducer(walletManager *wallet.Manager) (*wallet.KeyPair, error) {
	if c.Producer == nil {
		return nil, nil
	}

	// Unlock in wallet
	if _, err := walletManager.GetKeyFile(c.Producer.KeyFilePath); err != nil {
		log.Error("unable to get keyFile", "keyFilePath", c.Producer.KeyFilePath, "reason", err)
		return nil, err
	}
	if err := walletManager.Unlock(c.Producer.KeyFilePath, c.Producer.Password); err != nil {
		log.Error("unable to unlock keyFile", "keyFilePath", c.Producer.KeyFilePath, "reason", err)
		return nil, err
	}

	// check address field is set & parse it
	if c.Producer.Address == "" {
		return nil, fmt.Errorf("unable to parse producer address. Reason:missing")
	}
	address, err := types.ParseAddress(c.Producer.Address)
	if err != nil {
		return nil, fmt.Errorf("unable to parse producer address. Reason:%w", err)
	}

	// get keyStore which should already be unlocked
	keyStore, err := walletManager.GetKeyStore(c.Producer.KeyFilePath)
	if err != nil {
		return nil, err
	}

	// derive coinbase
	_, keyPair, err := keyStore.DeriveForIndexPath(c.Producer.Index)
	if err != nil {
		return nil, err
	}

	// make sure address matches
	if keyPair.Address != address {
		return nil, errors.Errorf("producer address doesn't match. Expected %v but got %v", address, keyPair.Address)
	}

	return keyPair, nil
}

func (c *Config) makeWalletConfig() *wallet.Config {
	return &wallet.Config{WalletDir: c.WalletPath}
}
func (c *Config) makeNetConfig() *p2p.Net {
	networkDataDir := filepath.Join(c.DataPath, p2p.DefaultNetDirName)
	privateKeyFile := filepath.Join(c.DataPath, p2p.DefaultNetPrivateKeyFile)

	return &p2p.Net{
		PrivateKeyFile:    privateKeyFile,
		MaxPeers:          c.Net.MaxPeers,
		MaxPendingPeers:   c.Net.MaxPendingPeers,
		MinConnectedPeers: c.Net.MinConnectedPeers,
		Name:              fmt.Sprintf("%v %v", metadata.Version, c.Name),
		Seeders:           c.Net.Seeders,
		NodeDatabase:      networkDataDir,
		ListenAddr:        c.Net.ListenHost,
		ListenPort:        c.Net.ListenPort,
	}
}
func (c *Config) HTTPEndpoint() string {
	if c.RPC.HTTPHost == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", c.RPC.HTTPHost, c.RPC.HTTPPort)
}
func (c *Config) WSEndpoint() string {
	if c.RPC.WSHost == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", c.RPC.WSHost, c.RPC.WSPort)
}
