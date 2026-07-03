package api

import (
	"runtime"
	"strings"

	"github.com/inconshreveable/log15"
	"github.com/shirou/gopsutil/host"
	"github.com/shirou/gopsutil/mem"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/metadata"
	"github.com/zenon-network/go-zenon/p2p"
	"github.com/zenon-network/go-zenon/p2p/discover"
	"github.com/zenon-network/go-zenon/protocol"
	"github.com/zenon-network/go-zenon/zenon"
)

// StatsApi implements the "stats" JSON-RPC namespace, which reports
// node-level status: host OS and memory, build version, connected
// peers and momentum sync progress. Every exported method is served as
// stats.<lowerCamelMethodName>.
type StatsApi struct {
	z   zenon.Zenon
	p2p *p2p.Server
	log log15.Logger
}

// NewStatsApi returns a StatsApi bound to the given node and its p2p
// server. It is called by the RPC server when the "stats" namespace is
// enabled; it is not itself an RPC method.
func NewStatsApi(z zenon.Zenon, p2p *p2p.Server) *StatsApi {
	return &StatsApi{
		z:   z,
		p2p: p2p,
		log: common.RPCLogger.New("module", "net_api"),
	}
}

// OsInfoResponse describes the host machine the node runs on, as
// returned by OsInfo.
type OsInfoResponse struct {
	// Os, Platform, PlatformFamily, PlatformVersion and KernelVersion
	// identify the host operating system as reported by the OS itself,
	// for example "linux" / "ubuntu" / "debian" / "20.04" / "5.4.0".
	Os              string `json:"os"`
	Platform        string `json:"platform"`
	PlatformFamily  string `json:"platformFamily"`
	PlatformVersion string `json:"platformVersion"`
	KernelVersion   string `json:"kernelVersion"`
	// MemoryTotal is the host's total physical memory in bytes.
	MemoryTotal uint64 `json:"memoryTotal"`
	// MemoryFree is the memory in bytes available for allocation
	// without swapping (the OS "available" figure, which includes
	// reclaimable caches, not strictly free memory).
	MemoryFree uint64 `json:"memoryFree"`
	// NumCPU is the number of logical CPUs usable by the node process.
	NumCPU int `json:"numCPU"`
	// NumGoroutine is the number of goroutines currently running in
	// the node process.
	NumGoroutine int `json:"numGoroutine"`
}

// OsInfo reports the host's operating system, memory and CPU details
// together with the node's current goroutine count. Failures while
// querying the OS are not reported: the affected fields are left at
// their zero values and the returned error is always nil.
//
// JSON-RPC: stats.osInfo
func (api *StatsApi) OsInfo() (*OsInfoResponse, error) {
	result := &OsInfoResponse{}
	stat, e := host.Info()
	if e == nil {
		result.Os = stat.OS
		result.Platform = stat.Platform
		result.PlatformFamily = stat.PlatformFamily
		result.PlatformVersion = stat.PlatformVersion
		result.KernelVersion = stat.KernelVersion
	}

	memO, e := mem.VirtualMemory()
	if e == nil {
		result.MemoryFree = memO.Available
		result.MemoryTotal = memO.Total
	}

	result.NumCPU = runtime.NumCPU()
	result.NumGoroutine = runtime.NumGoroutine()
	return result, nil
}

// ProcessInfoResponse identifies the node build serving the RPC, as
// returned by ProcessInfo.
type ProcessInfoResponse struct {
	// Version is the release version compiled into the binary, for
	// example "v0.0.8"; shared-library (libznn) builds carry a
	// "-libznn" suffix.
	Version string `json:"version"`
	// Commit is the git revision the binary was built from, or empty
	// when the build carries no VCS information.
	Commit string `json:"commit"`
}

// ProcessInfo returns the version and git commit of the running node
// binary, both fixed at build time.
//
// JSON-RPC: stats.processInfo
func (api *StatsApi) ProcessInfo() (*ProcessInfoResponse, error) {
	return &ProcessInfoResponse{
		Version: metadata.Version,
		Commit:  metadata.GitCommit,
	}, nil
}

// Peer describes one node on the p2p network as seen by NetworkInfo.
type Peer struct {
	// PublicKey is the peer's discovery node id - its 512-bit public
	// key hex-encoded without a 0x prefix.
	PublicKey string `json:"publicKey"`
	// IP is the peer's remote IP address as observed on the live
	// connection. For the Self entry it is the fixed placeholder
	// "127.0.0.1".
	IP string `json:"ip"`
	// Name is the node name the peer advertised during the protocol
	// handshake. For the Self entry it is the fixed placeholder
	// "*self*".
	Name string `json:"name"`
}

// NetworkInfoResponse describes the node's current view of the p2p
// network, as returned by NetworkInfo.
type NetworkInfoResponse struct {
	// NumPeers is the number of connected peers. It is sampled
	// separately from Peers, so it can momentarily differ from the
	// length of that list.
	NumPeers int `json:"numPeers"`
	// Peers lists the currently connected peers.
	Peers []*Peer `json:"peers"`
	// Self describes the local node; only its PublicKey is real, the
	// other fields hold the placeholders documented on Peer.
	Self *Peer `json:"self"`
}

func p2pPeerToPeer(peer *p2p.Peer) (*Peer, error) {
	ip := peer.RemoteAddr().String()
	splits := strings.Split(ip, ":")
	return &Peer{
		PublicKey: peer.ID().String(),
		IP:        splits[0],
		Name:      peer.Name(),
	}, nil
}
func selfToPeer(node *discover.Node) *Peer {
	return &Peer{
		PublicKey: node.ID.String(),
		IP:        "127.0.0.1",
		Name:      "*self*",
	}
}

// NetworkInfo reports the peers the node is currently connected to and
// the node's own network identity.
//
// JSON-RPC: stats.networkInfo
func (api *StatsApi) NetworkInfo() (*NetworkInfoResponse, error) {
	peersRaw := api.p2p.Peers()
	peers := make([]*Peer, 0, len(peersRaw))
	for _, raw := range peersRaw {
		peer, err := p2pPeerToPeer(raw)
		if err != nil {
			return nil, err
		}
		peers = append(peers, peer)
	}

	return &NetworkInfoResponse{
		NumPeers: api.p2p.PeerCount(),
		Peers:    peers,
		Self:     selfToPeer(api.p2p.Self()),
	}, nil
}

// SyncInfo reports the node's momentum sync progress. In the result,
// CurrentHeight is the local frontier momentum height and TargetHeight
// is the frontier height advertised by the best-known peer (0 when no
// peers are connected). State is 1 (Syncing) while CurrentHeight is
// behind TargetHeight and 2 (SyncDone) once it has caught up; both are
// overridden by 3 (NotEnoughPeers) when fewer peers are connected than
// the node's configured minimum.
//
// JSON-RPC: stats.syncInfo
func (api *StatsApi) SyncInfo() (*protocol.SyncInfo, error) {
	return api.z.Broadcaster().SyncInfo(), nil
}
