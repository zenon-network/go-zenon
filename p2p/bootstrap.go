package p2p

import (
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// ParseBootstrapPeer parses a multiaddr string into a peer.AddrInfo.
// Expected format: /ip4/<ip>/tcp/<port>/p2p/<peer-id>
func ParseBootstrapPeer(maddrStr string) (peer.AddrInfo, error) {
	maddr, err := ma.NewMultiaddr(maddrStr)
	if err != nil {
		return peer.AddrInfo{}, fmt.Errorf("parse multiaddr %q: %w", maddrStr, err)
	}
	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return peer.AddrInfo{}, fmt.Errorf("extract peer info from %q: %w", maddrStr, err)
	}
	return *info, nil
}

// ParseBootstrapPeers parses a slice of multiaddr strings into peer.AddrInfo entries.
func ParseBootstrapPeers(maddrs []string) ([]peer.AddrInfo, error) {
	peers := make([]peer.AddrInfo, 0, len(maddrs))
	for _, s := range maddrs {
		if s == "" {
			continue
		}
		info, err := ParseBootstrapPeer(s)
		if err != nil {
			return nil, err
		}
		peers = append(peers, info)
	}
	return peers, nil
}
