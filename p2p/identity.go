package p2p

import (
	"crypto/ecdsa"
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zenon-network/go-zenon/p2p/discover"
)

// ECDSAToLibp2pPrivKey converts a secp256k1 ECDSA private key to a libp2p private key.
// The underlying key material is identical; only the encoding changes.
func ECDSAToLibp2pPrivKey(ecdsaKey *ecdsa.PrivateKey) (libp2pcrypto.PrivKey, error) {
	// secp256k1 serialized private key is the raw 32-byte scalar
	raw := ecdsaKey.D.Bytes()
	// pad to 32 bytes if needed
	if len(raw) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(raw):], raw)
		raw = padded
	}
	privKey, err := libp2pcrypto.UnmarshalSecp256k1PrivateKey(raw)
	if err != nil {
		return nil, fmt.Errorf("unmarshal secp256k1 key: %w", err)
	}
	return privKey, nil
}

// PeerIDFromECDSA derives a libp2p peer.ID from an ECDSA private key.
func PeerIDFromECDSA(ecdsaKey *ecdsa.PrivateKey) (peer.ID, error) {
	privKey, err := ECDSAToLibp2pPrivKey(ecdsaKey)
	if err != nil {
		return "", err
	}
	return peer.IDFromPrivateKey(privKey)
}

// PubkeyToNodeID converts an ECDSA public key to a discover.NodeID (64-byte raw uncompressed key).
// Same logic as discover.PubkeyID but avoids importing discover.
func PubkeyToNodeID(pub *ecdsa.PublicKey) discover.NodeID {
	var id discover.NodeID
	// crypto.FromECDSAPub returns 65-byte uncompressed key with 0x04 prefix
	copy(id[:], crypto.FromECDSAPub(pub)[1:])
	return id
}

// nodeIDFromPeerID extracts the expected NodeID from a libp2p peer.ID.
// The peer.ID is a multihash of the compressed public key. We extract
// the public key, convert to ECDSA, and derive the NodeID.
func nodeIDFromPeerID(pid peer.ID) (discover.NodeID, error) {
	pubKey, err := pid.ExtractPublicKey()
	if err != nil {
		return discover.NodeID{}, fmt.Errorf("extract pubkey from peer ID: %w", err)
	}
	raw, err := pubKey.Raw()
	if err != nil {
		return discover.NodeID{}, fmt.Errorf("get raw pubkey: %w", err)
	}
	// raw is 33-byte compressed secp256k1; convert to uncompressed
	ecdsaPub, err := crypto.DecompressPubkey(raw)
	if err != nil {
		return discover.NodeID{}, fmt.Errorf("decompress pubkey: %w", err)
	}
	return PubkeyToNodeID(ecdsaPub), nil
}
