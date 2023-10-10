package app

import (
	"github.com/zenon-network/go-zenon/node"
	"github.com/zenon-network/go-zenon/p2p"

	"github.com/urfave/cli/v2"
)

var (

	// pprof

	PprofFlag = &cli.BoolFlag{
		Name:  "pprof",
		Usage: "Enable the pprof HTTP server",
	}
	PprofPortFlag = &cli.Uint64Flag{
		Name:  "pprof.port",
		Usage: "pprof HTTP server listening port",
		Value: 6060,
	}

	PprofAddrFlag = &cli.StringFlag{
		Name:  "pprof.addr",
		Usage: "pprof HTTP server listening interface",
		Value: "127.0.0.1",
	}

	// config

	ConfigFileFlag = &cli.StringFlag{
		Name:  "config",
		Usage: "Node configuration file in JSON format",
		Value: "DataPath/config.json",
	}

	// general

	DataPathFlag = &cli.StringFlag{
		Name:  "data",
		Usage: "Path to the main zenon data folder. Used for store all files.",
		Value: node.DefaultDataDir(),
	}
	WalletDirFlag = &cli.StringFlag{
		Name:  "wallet",
		Usage: "Directory for the wallet.",
		Value: "DataPath/wallet",
	}
	GenesisFileFlag = &cli.StringFlag{
		Name:  "genesis",
		Usage: "Path to genesis file. Used to override embedded genesis from the binary source-code.",
		Value: "DataPath/genesis.json",
	}
	IdentityFlag = &cli.StringFlag{
		Name:  "name", //mapping:p2p.Name
		Usage: "Node's name. Visible in the network.",
	}

	// network

	ListenHostFlag = &cli.StringFlag{
		Name:  "host",
		Usage: "Network listening host",
		Value: p2p.DefaultListenHost,
	}
	ListenPortFlag = &cli.IntFlag{
		Name:  "port",
		Usage: "Network listening port",
		Value: p2p.DefaultListenPort,
	}
	MaxPeersFlag = &cli.UintFlag{
		Name:  "max-peers",
		Usage: "Maximum number of network peers (network disabled if set to 0)",
		Value: p2p.DefaultMaxPeers,
	}
	MaxPendingPeersFlag = &cli.UintFlag{
		Name:  "max-pending-peers",
		Usage: "Maximum number of db connection attempts (defaults used if set to 0)",
		Value: p2p.DefaultMaxPeers,
	}

	// rpc

	RPCEnabledFlag = &cli.BoolFlag{
		Name:  "http",
		Usage: "Enable the HTTP-RPC server",
	}
	RPCListenAddrFlag = &cli.StringFlag{
		Name:  "http-addr",
		Usage: "HTTP-RPC server listening interface",
	}
	RPCPortFlag = &cli.IntFlag{
		Name:  "http-port",
		Usage: "HTTP-RPC server listening port",
		Value: p2p.DefaultHTTPPort,
	}
	WSEnabledFlag = &cli.BoolFlag{
		Name:  "ws",
		Usage: "Enable the WS-RPC server",
	}
	WSListenAddrFlag = &cli.StringFlag{
		Name:  "ws-addr",
		Usage: "WS-RPC server listening interface",
	}
	WSPortFlag = &cli.IntFlag{
		Name:  "ws-port",
		Usage: "WS-RPC server listening port",
		Value: p2p.DefaultWSPort,
	}

	// log

	LogLvlFlag = &cli.StringFlag{
		Name:  "loglevel",
		Usage: "log level (info,error,warn,debug)",
	}

	AllFlags = []cli.Flag{

		// config
		ConfigFileFlag,

		// pprof
		PprofFlag,
		PprofPortFlag,
		PprofAddrFlag,

		// general
		DataPathFlag,
		WalletDirFlag,
		GenesisFileFlag,
		IdentityFlag,

		// network
		ListenHostFlag,
		ListenPortFlag,
		MaxPeersFlag,
		MaxPendingPeersFlag,

		// http rpc
		RPCEnabledFlag,
		RPCListenAddrFlag,
		RPCPortFlag,

		// ws
		WSEnabledFlag,
		WSListenAddrFlag,
		WSPortFlag,

		// log
		LogLvlFlag,
	}
)
