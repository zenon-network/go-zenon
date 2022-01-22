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

type StatsApi struct {
	z   zenon.Zenon
	p2p *p2p.Server
	log log15.Logger
}

func NewStatsApi(z zenon.Zenon, p2p *p2p.Server) *StatsApi {
	return &StatsApi{
		z:   z,
		p2p: p2p,
		log: common.RPCLogger.New("module", "net_api"),
	}
}

type OsInfoResponse struct {
	Os              string `json:"os"`
	Platform        string `json:"platform"`
	PlatformFamily  string `json:"platformFamily"`
	PlatformVersion string `json:"platformVersion"`
	KernelVersion   string `json:"kernelVersion"`
	MemoryTotal     uint64 `json:"memoryTotal"`
	MemoryFree      uint64 `json:"memoryFree"`
	NumCPU          int    `json:"numCPU"`
	NumGoroutine    int    `json:"numGoroutine"`
}

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

type ProcessInfoResponse struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

func (api *StatsApi) ProcessInfo() (*ProcessInfoResponse, error) {
	return &ProcessInfoResponse{
		Version: metadata.Version,
		Commit:  metadata.GitCommit,
	}, nil
}

type Peer struct {
	PublicKey string `json:"publicKey"`
	IP        string `json:"ip"`
	Name      string `json:"name"`
}
type NetworkInfoResponse struct {
	NumPeers int     `json:"numPeers"`
	Peers    []*Peer `json:"peers"`
	Self     *Peer   `json:"self"`
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

func (api *StatsApi) SyncInfo() (*protocol.SyncInfo, error) {
	return api.z.Broadcaster().SyncInfo(), nil
}
