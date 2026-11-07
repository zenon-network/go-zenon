package p2p

import (
	"crypto/ecdsa"

	"github.com/ethereum/go-ethereum/crypto"
	log "github.com/inconshreveable/log15"

	"github.com/zenon-network/go-zenon/p2p/discover"
)

// Defaults applied to the network section of the node configuration:
// the advertised node name, the listening interface and ports (35995
// for devp2p, 35997 and 35998 for the HTTP and WebSocket RPC
// endpoints), the peer-count limits and the location of the node
// database and network private key inside the data directory.
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
	// the network data dir where the libp2p backend keeps its peer
	// database (a LevelDB database; see p2p/libp2p/peerdb.go). Sits
	// alongside the legacy nodeDb but is independent from it.
	DefaultPeerstoreDirName = "libp2p-peerstore"
)

var (
	// DefaultBootstrapPeers contains the libp2p bootstrap peers used after
	// the libp2p activation spork enforces. Entries are multiaddr strings,
	// e.g. /ip4/<ip>/tcp/35995/p2p/<peer-id>.
	//
	// Ships empty by default: the community operators who run the libp2p
	// bootstrap infrastructure populate this list as part of the binary
	// release that ships ahead of the activation block. Operators may
	// override the list via the Net.BootstrapPeers field in config.json.
	DefaultBootstrapPeers = []string{
		// populated as community operators stand up libp2p infrastructure
	}

	// DefaultSeeders contains the legacy (devp2p/RLPX) mainnet bootstrap
	// peers in enode:// format. These remain in service until the libp2p
	// activation spork enforces; after that the legacy backend is torn
	// down and these entries become unused.
	//
	// They are kept here (rather than moved to a "legacy" package) so an
	// operator's existing config.json (which references the Net.Seeders
	// field) continues to work without modification across the upgrade.
	//
	// The entries feed UDP discovery (Table.Bootstrap), so the list holds
	// only nodes that answered a discovery ping and findnode when it was
	// last pruned (2026-11-07, issue #242). A Net.Seeders list in
	// config.json replaces this default rather than extending it, so an
	// operator override is not updated by changes here.
	DefaultSeeders = []string{
		"enode://142031953ebc6209604418654b3bd0c7ba61ffb588eba6ab717bc4f3234b9c05846126310afaa6ac1633b076e76cd160106f23e5200597cee2cabfb93068c709@178.18.250.224:35995",
		"enode://895a7ad858af86a0f85578d549a41b35b8565da2833a3731e6605f2efbc90c08d7a12d44f808729d703ef14f99034e57c7261d1808a021810316ecd5b9153410@135.148.121.53:35995",
		"enode://11c31b91530e839225803adf857a027edb9dd9f61217e58231335bb0d4d6c2dcdc0e03f8d60017ac19493b8aae2cefcc430c26a463142d372df9cb45f22461a2@161.97.106.183:35995",
		"enode://0c5213f89c748aab5c39e63bdc65bd97bf446a5229e640086e2784217d24731d78ab53ee5316619dc4299ab759304c5859272d35cfab74fd9106753e0db3d14e@135.148.32.194:35995",
		"enode://8da3624a825e9cc3536c6065f06f0dd38c4703bf32299f171a1d0e69d249c887220b875e1a82a38f8ecb37971d6c6d7741889abe942a1ddef75ac9dce3aa0575@62.171.148.56:35995",
		"enode://db3de09472c37eb7ce6a73e161705534719cc59e589873950d187b301ed66232a47508167a97e2ea938c64a36b15ff6465ac912f84a8f817e69da975dd927100@167.99.45.34:35995",
		"enode://916aa48165071bf1e86bc4bca272c86e092e981d210b3d7a29b099bdbda1aec362f7e085e603c3ae6a25833b44000519658e8aaa72c9ad13477a2a548ebb62ad@167.99.47.146:35995",
		"enode://e65a83e5c4b3976f80b60ce39cb461fbfffa00b3d7390c654e30816713e6567e907aeb75c59130fb4369879ad6e072ff507cd7fe4a020c8ac6732ed61024915c@178.18.250.182:35995",
		"enode://2155ec9ee05593174451fa0109224f590b039811748274ea754eff2d541c6831cec4c4ab33184e0482e249c245707f88c2588722ca329556f19bfbaf8560be72@5.9.153.75:35995",
		"enode://0316e6152bed5773850c72a79a8eed9c4dd72966d6f5101e860fa73da6c9a064701964cf3e0a09420a60c38000497af4c43c0e1374bfe5120dee0180f7bc30ed@51.81.209.53:35995",
		"enode://a0cd4a6f5dc207a00e04ab115f2c76aeb7a09059a4b0a25906734e5f094d847436fc500f809ccf32df471a2cfed62e9be7b446b21abd971312bb9e176c548dd8@135.148.149.29:35995",
		"enode://58fd515a50e3b9b68322a87ad3b262ca74441e1dd41012aa851f82d76159fab1d071c78c993e2be9d50f86bd4dd951e779df58d5b19d3631c75087bf1fba475e@178.18.251.111:35995",
	}
)

// P2PBackend selects which transport backend the switcher starts.
type P2PBackend string

const (
	// P2PBackendAuto is the default: the switcher picks legacy or libp2p
	// based on the spork oracle, and swaps when activation fires.
	P2PBackendAuto P2PBackend = "auto"
	// P2PBackendLibp2p starts the libp2p backend directly, skipping the
	// oracle check and the activation watcher. Used by fresh nodes joining
	// a network where the libp2p spork has already activated (issue #105).
	P2PBackendLibp2p P2PBackend = "libp2p"
	// P2PBackendLegacy starts the legacy backend and never swaps. Used
	// on networks where the libp2p spork has not been scheduled.
	P2PBackendLegacy P2PBackend = "legacy"
)

// Net carries the network settings from the node configuration in the
// shape the p2p layer expects. The node layer builds one with
// data-directory-relative paths filled in, then hands its fields to
// the transport switcher and its two backends, resolving the private
// key with PrivateKey, the legacy seeders with Nodes and the libp2p
// bootstrap peers with libp2p.ParseBootstrapPeers.
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

	// Seeders are the legacy (devp2p/RLPX) bootstrap peers in enode://
	// format. Used by the legacy backend until the libp2p activation
	// spork enforces. Existing operator configs populate this field.
	Seeders []string

	// BootstrapPeers are the libp2p bootstrap peers in multiaddr format,
	// e.g. /ip4/<ip>/tcp/35995/p2p/<peer-id>. Used by the libp2p backend
	// once the activation spork enforces. New field added in the
	// spork-gated rollout; existing configs that omit it fall back to
	// DefaultBootstrapPeers.
	BootstrapPeers []string

	// NATPortMap enables UPnP / NAT-PMP port mapping in the libp2p
	// backend. Defaults to false to match the pre-libp2p network's
	// behaviour: the legacy backend's NAT field was unset in the wiring
	// path, so port mapping was effectively off. Home operators behind
	// a NATting router can set this to true; data-center operators
	// should leave it false to avoid pointless UPnP probes leaving
	// their host.
	NATPortMap bool

	// PeerstoreDir is the on-disk path for the libp2p backend's peer
	// database. When set, every peer that completes a Zenon handshake
	// is recorded (with its dialable addresses and a last-seen stamp,
	// expiring after 30 days unseen) and on startup a "warm bootstrap"
	// redials the most recently seen of them in parallel with the
	// configured BootstrapPeers, drastically reducing dependence on
	// the bootstrap-node list staying current. Empty string disables
	// peer persistence — every restart is a cold start.
	//
	// Conventionally set to <DataPath>/network/libp2p-peerstore/ by
	// node/config.go, alongside the legacy nodeDb at
	// <DataPath>/network/. The two databases are independent —
	// migration of legacy entries to the libp2p format is not
	// attempted because the peer.ID encoding differs and the dial
	// endpoints (TCP vs RLPX) are incompatible.
	PeerstoreDir string

	// NodeDatabase is the path to the database containing the previously seen
	// live nodes in the network.
	NodeDatabase string

	// P2PBackend overrides the spork-oracle backend selection.
	// "auto" (default) keeps the current behavior: legacy before
	// activation, swap to libp2p when the spork enforces.
	// "libp2p" starts libp2p unconditionally — used by fresh nodes
	// joining a post-activation network (issue #105).
	// "legacy" starts legacy and never swaps.
	// Unknown values are rejected at startup.
	P2PBackend P2PBackend

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

// Nodes parses the configured seeders into discovery nodes, suitable
// for the server's bootstrap node list. It fails on the first seeder
// that is not a valid enode URL.
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
