package libp2p

import (
	"crypto/ecdsa"
	"os"
	"path/filepath"
	"testing"
	"time"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

// TestPeerstorePersistsAcrossRestart starts a libp2p Server with a
// peerstore directory, records the peer ID of a manually-added
// AddrInfo entry, stops the server, restarts it with the same
// directory, and verifies the entry survived. This is the bedrock
// invariant of the persistent peerstore feature: anything the server
// learned about peers in run N is visible to run N+1.
func TestPeerstorePersistsAcrossRestart(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "peerstore-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)
	peerstoreDir := filepath.Join(tmpDir, "ps")

	// Two distinct ECDSA keys — one for the server we restart, one
	// for the synthetic peer we record into its peerstore.
	srvKey := mustGenKey(t)
	otherKey := mustGenKey(t)

	// === First run ===
	srv := newTestServer(srvKey, peerstoreDir, t)
	if err := srv.Start(); err != nil {
		t.Fatalf("first start: %v", err)
	}

	// Inject an entry into the peerstore by computing the libp2p
	// peer.ID for otherKey and giving it a fake multiaddr. The
	// peerstore is persistent — this entry should survive the Stop +
	// re-Start cycle below.
	otherPID, err := PeerIDFromECDSA(otherKey)
	if err != nil {
		t.Fatalf("derive other peer ID: %v", err)
	}
	fakeAddrs := srv.host.Peerstore().Addrs(otherPID)
	_ = fakeAddrs
	// Use the host's Peerstore to add an address with a long TTL so
	// it isn't garbage collected during the test.
	srv.host.Peerstore().AddAddr(otherPID, srv.host.Addrs()[0], time.Hour)

	if got := srv.host.Peerstore().Addrs(otherPID); len(got) == 0 {
		t.Fatal("expected at least one address recorded for synthetic peer")
	}

	srv.Stop()

	// === Second run ===
	srv2 := newTestServer(srvKey, peerstoreDir, t)
	if err := srv2.Start(); err != nil {
		t.Fatalf("second start: %v", err)
	}
	defer srv2.Stop()

	addrs := srv2.host.Peerstore().Addrs(otherPID)
	if len(addrs) == 0 {
		t.Fatal("synthetic peer's address did not survive restart — peerstore is not persistent")
	}
}

// TestPeerstoreEmptyDirIsInMemoryFallback ensures that leaving
// PeerstoreDir unset doesn't break startup — the libp2p default
// in-memory peerstore should be used, the host comes up, and the
// peerstore is empty (no warm-bootstrap candidates).
func TestPeerstoreEmptyDirIsInMemoryFallback(t *testing.T) {
	srv := newTestServer(mustGenKey(t), "", t) // empty PeerstoreDir
	if err := srv.Start(); err != nil {
		t.Fatalf("start with empty PeerstoreDir: %v", err)
	}
	defer srv.Stop()

	// With no PeerstoreDir we expect no datastore.
	if srv.peerstoreDS != nil {
		t.Error("expected peerstoreDS to be nil with empty PeerstoreDir")
	}
	// And the in-memory peerstore should be empty of any peers
	// other than the self entry (libp2p sometimes records that).
	known := srv.host.Peerstore().PeersWithAddrs()
	for _, pid := range known {
		if pid != srv.host.ID() {
			t.Errorf("unexpected peer in fresh in-memory peerstore: %s", pid)
		}
	}
}

// newTestServer constructs a libp2p Server configured for in-process
// tests: random TCP port, optional persistent peerstore, no DHT
// (Discovery=false avoids the DHT bootstrap goroutine which would
// otherwise need network access).
func newTestServer(key *ecdsa.PrivateKey, peerstoreDir string, t *testing.T) *Server {
	t.Helper()
	return &Server{
		PrivateKey:        key,
		Name:              "peerstore-test",
		MaxPeers:          16,
		MinConnectedPeers: 0,
		MaxPendingPeers:   0,
		Discovery:         false, // skip DHT in tests; we're verifying peerstore in isolation
		NoDial:            true,  // no outbound dials
		ListenAddr:        "127.0.0.1:0",
		PeerstoreDir:      peerstoreDir,
	}
}

func mustGenKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ethcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return k
}
