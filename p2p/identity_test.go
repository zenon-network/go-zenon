package p2p

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

func TestECDSAToLibp2pPrivKey(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	lp2pKey, err := ECDSAToLibp2pPrivKey(key)
	if err != nil {
		t.Fatal(err)
	}

	if lp2pKey == nil {
		t.Fatal("expected non-nil libp2p key")
	}

	// Verify the raw bytes match
	raw := key.D.Bytes()
	if len(raw) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(raw):], raw)
		raw = padded
	}
	lp2pRaw, err := lp2pKey.Raw()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, lp2pRaw) {
		t.Fatalf("key bytes mismatch:\n  ecdsa:   %x\n  libp2p:  %x", raw, lp2pRaw)
	}
}

func TestPeerIDFromECDSA_Deterministic(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	id1, err := PeerIDFromECDSA(key)
	if err != nil {
		t.Fatal(err)
	}

	id2, err := PeerIDFromECDSA(key)
	if err != nil {
		t.Fatal(err)
	}

	if id1 != id2 {
		t.Fatalf("peer IDs should be deterministic: %s != %s", id1, id2)
	}
}

func TestPeerIDFromECDSA_DifferentKeys(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()

	id1, _ := PeerIDFromECDSA(key1)
	id2, _ := PeerIDFromECDSA(key2)

	if id1 == id2 {
		t.Fatal("different keys should produce different peer IDs")
	}
}

func TestPubkeyToNodeID(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	nodeID := PubkeyToNodeID(&key.PublicKey)

	// NodeID should be 64 bytes
	if len(nodeID) != 64 {
		t.Fatalf("expected 64-byte NodeID, got %d bytes", len(nodeID))
	}

	// Verify it matches the uncompressed pubkey without 0x04 prefix
	uncompressed := crypto.FromECDSAPub(&key.PublicKey) // 65 bytes with 0x04 prefix
	expected := uncompressed[1:]                          // strip prefix

	if !bytes.Equal(nodeID[:], expected) {
		t.Fatalf("NodeID mismatch:\n  got:  %x\n  want: %x", nodeID[:], expected)
	}
}

func TestPubkeyToNodeID_Deterministic(t *testing.T) {
	key, _ := crypto.GenerateKey()

	id1 := PubkeyToNodeID(&key.PublicKey)
	id2 := PubkeyToNodeID(&key.PublicKey)

	if id1 != id2 {
		t.Fatal("PubkeyToNodeID should be deterministic")
	}
}

func TestNodeIDFromPeerID(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	// Derive peer.ID from the ECDSA key
	pid, err := PeerIDFromECDSA(key)
	if err != nil {
		t.Fatal(err)
	}

	// Extract NodeID from the peer.ID
	nodeID, err := nodeIDFromPeerID(pid)
	if err != nil {
		t.Fatal(err)
	}

	// Should match the direct conversion
	expected := PubkeyToNodeID(&key.PublicKey)
	if nodeID != expected {
		t.Fatalf("NodeID mismatch:\n  from peer: %x\n  direct:    %x", nodeID[:], expected[:])
	}
}

func TestNodeIDFromPeerID_DifferentKey(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()

	pid1, _ := PeerIDFromECDSA(key1)
	pid2, _ := PeerIDFromECDSA(key2)

	id1, _ := nodeIDFromPeerID(pid1)
	id2, _ := nodeIDFromPeerID(pid2)

	if id1 == id2 {
		t.Fatal("different peer IDs should produce different NodeIDs")
	}
}
