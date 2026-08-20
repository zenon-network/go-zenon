package libp2p

import (
	"fmt"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/zenon-network/go-zenon/common"
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

// ParseBootstrapPeers parses a slice of bootstrap entries.
//
// Entries are expected to be in libp2p multiaddr format. Two recoverable
// conditions are tolerated rather than failing startup:
//
//   - Empty strings are skipped silently.
//   - Legacy enode:// strings are skipped with a warning. This is
//     defensive: an operator who pastes their old Seeders list into
//     the new BootstrapPeers field shouldn't take their node offline;
//     they get a clear log line pointing at the misconfiguration and
//     the rest of the (valid) entries continue to be used.
//
// A genuinely malformed multiaddr (neither empty nor an enode:// URL)
// is still treated as a hard error, so typos are surfaced.
func ParseBootstrapPeers(maddrs []string) ([]peer.AddrInfo, error) {
	peers := make([]peer.AddrInfo, 0, len(maddrs))
	for _, s := range maddrs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "enode://") {
			common.P2PLogger.Warn(
				"ignoring legacy enode:// entry in BootstrapPeers; this field expects libp2p multiaddr format",
				"entry", s,
			)
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
