package p2p

import (
	"crypto/ecdsa"

	"github.com/ethereum/go-ethereum/crypto"
	log "github.com/inconshreveable/log15"
)

const (
	DefaultNodeName = "znn-node"

	DefaultListenHost = "0.0.0.0"
	DefaultListenPort = 35995
	DefaultHTTPPort   = 35997
	DefaultWSPort     = 35998

	DefaultMinPeers          = 8
	DefaultMaxPeers          = 60
	DefaultMaxPendingPeers   = 10
	DefaultMinConnectedPeers = 16

	DefaultNetDirName        = "network"
	DefaultNetPrivateKeyFile = "network-private-key"

	// DefaultPeerstoreDirName is the conventional subdirectory under
	// the network data dir where the libp2p server keeps its peer
	// database (a LevelDB database; see p2p/libp2p/peerdb.go).
	DefaultPeerstoreDirName = "libp2p-peerstore"
)

var (
	// DefaultBootstrapPeers contains the default libp2p bootstrap peers.
	// Entries are multiaddr strings, e.g.
	// /ip4/<ip>/tcp/35995/p2p/<peer-id>.
	//
	// Ships empty by default. Operators set the list via the
	// Net.BootstrapPeers field in config.json.
	DefaultBootstrapPeers = []string{
		// populated as community operators stand up libp2p infrastructure
	}
)

type Net struct {
	// This field must be set to a valid secp256k1 private key.
	privateKey *ecdsa.PrivateKey

	// Absolute path to PrivateKeyFile.
	// (defaults to .znn/ DefaultNetPrivateKeyFile on linux).
	// If Config.DataDir changes, this reflects changes accordingly.
	PrivateKeyFile string

	// MaxPeers is the maximum number of peers that can be
	// connected. It must be greater than zero.
	MaxPeers int

	// MinConnectedPeers is the minimum number of peers that can be connected
	MinConnectedPeers int

	// MaxPendingPeers is the maximum number of inbound peers that can be
	// pending in the handshake phase. Zero defaults to preset values.
	MaxPendingPeers int

	// Name sets the node name of this server.
	Name string

	// BootstrapPeers are the libp2p bootstrap peers in multiaddr format,
	// e.g. /ip4/<ip>/tcp/35995/p2p/<peer-id>. Configs that omit it fall
	// back to DefaultBootstrapPeers.
	BootstrapPeers []string

	// NATPortMap enables UPnP / NAT-PMP port mapping. Defaults to false.
	// Home operators behind a NATting router can set this to true;
	// data-center operators should leave it false to avoid pointless
	// UPnP probes leaving their host.
	NATPortMap bool

	// PeerstoreDir is the on-disk path for the libp2p peer database.
	// When set, every peer that completes a Zenon handshake is recorded
	// (with its dialable addresses and a last-seen stamp, expiring after
	// 30 days unseen) and on startup a "warm bootstrap" redials the most
	// recently seen of them in parallel with the configured
	// BootstrapPeers, drastically reducing dependence on
	// the bootstrap-node list staying current. Empty string disables
	// peer persistence — every restart is a cold start.
	//
	// Conventionally set to <DataPath>/network/libp2p-peerstore/ by
	// node/config.go.
	PeerstoreDir string

	// If ListenAddr is set to a non-nil address, the server
	// will listen for incoming connections.
	//
	// If the port is zero, the operating system will pick a port. The
	// ListenAddr field will be updated with the actual address when
	// the server is started.
	ListenAddr string
	ListenPort int
}

// PrivateKey retrieves the currently configured private key of the node, checking
// first any manually set key, falling back to the one found in the configured
// data folder. If no key can be found, a new one is generated.
func (c *Net) PrivateKey() *ecdsa.PrivateKey {
	// Use any specifically configured key.
	if c.privateKey != nil {
		return c.privateKey
	}

	// Generate ephemeral key if no PrivateKeyFile is being used
	if c.PrivateKeyFile == "" {
		key, err := crypto.GenerateKey()
		if err != nil {
			log.Crit("Failed to generate ephemeral node key", "reason", err)
		}
		return key
	}

	if key, err := crypto.LoadECDSA(c.PrivateKeyFile); err == nil {
		return key
	}

	// No persistent key found, generate and store a new one.
	key, err := crypto.GenerateKey()
	if err != nil {
		log.Crit("Failed to generate node key", "reason", err)
	}

	if err := crypto.SaveECDSA(c.PrivateKeyFile, key); err != nil {
		log.Error("Failed to persist node key", "reason", err)
	}
	return key
}
