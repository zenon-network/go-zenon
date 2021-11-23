package p2p

import (
	"crypto/ecdsa"

	"github.com/ethereum/go-ethereum/crypto"
	log "github.com/inconshreveable/log15"

	"github.com/zenon-network/go-zenon/p2p/discover"
)

const (
	DefaultNodeName = "znn-node"

	DefaultListenHost = "0.0.0.0"
	DefaultListenPort = 35995
	DefaultHTTPPort   = 35997
	DefaultWSPort     = 35998

	DefaultMinPeers        = 10
	DefaultMaxPeers        = 60
	DefaultMaxPendingPeers = 10

	DefaultNetDirName        = "network"
	DefaultNetPrivateKeyFile = "network-private-key"
)

var (
	DefaultSeeders []string
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

	// MaxPendingPeers is the maximum number of peers that can be pending in the
	// handshake phase, counted separately for inbound and outbound connections.
	// Zero defaults to preset values.
	MaxPendingPeers int

	// Name sets the node name of this server.
	Name string

	Seeders []string

	// NodeDatabase is the path to the database containing the previously seen
	// live nodes in the network.
	NodeDatabase string

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
func (c *Net) Nodes() ([]*discover.Node, error) {
	var err error
	nodes := make([]*discover.Node, len(c.Seeders))
	for index, nodeAddress := range c.Seeders {
		nodes[index], err = discover.ParseNode(nodeAddress)
		if err != nil {
			return nil, err
		}
	}

	return nodes, nil
}
