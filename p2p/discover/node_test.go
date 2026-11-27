package discover

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
)

// TestNodeIDPubkeyRejectsOutOfRangeAndOffCurve verifies GO-2026-4507 /
// CVE-2026-26314 directly on NodeID.Pubkey(): coordinates >= P must be rejected
// even when (x mod P, y mod P) lies on secp256k1.
func TestNodeIDPubkeyRejectsOutOfRangeAndOffCurve(t *testing.T) {
	curve := secp256k1.S256()
	p := curve.Params().P

	// Canonical small-coordinate points on secp256k1 so that adding P fits in 32 bytes.
	smallXID := MustHexID(
		"0000000000000000000000000000000000000000000000000000000000000001" +
			"4218f20ae6c646b363db68605822fb14264ca8d2587fdd6fbc750d587e76a7ee",
	)
	smallYID := MustHexID(
		"1fe1e5ef3fceb5c135ab7741333ce5a6e80d68167653f6b2b24bcbcfaaaff507" +
			"0000000000000000000000000000000000000000000000000000000000000001",
	)

	// Control: both canonical fixtures and a freshly generated key succeed.
	for name, id := range map[string]NodeID{"small-x": smallXID, "small-y": smallYID} {
		if _, err := id.Pubkey(); err != nil {
			t.Fatalf("canonical %s point rejected by Pubkey(): %v", name, err)
		}
	}
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	genID := PubkeyID(&key.PublicKey)
	gotPub, err := genID.Pubkey()
	if err != nil {
		t.Fatalf("generated NodeID rejected by Pubkey(): %v", err)
	}
	if gotPub.X.Cmp(key.PublicKey.X) != 0 || gotPub.Y.Cmp(key.PublicKey.Y) != 0 {
		t.Fatal("generated NodeID round-tripped to different coordinates")
	}

	makeID := func(x, y *big.Int) NodeID {
		var id NodeID
		x.FillBytes(id[:32])
		y.FillBytes(id[32:])
		return id
	}

	nonCanonicalX := smallXID
	xPlusP := new(big.Int).Add(new(big.Int).SetBytes(smallXID[:32]), p)
	xPlusP.FillBytes(nonCanonicalX[:32])

	nonCanonicalY := smallYID
	yPlusP := new(big.Int).Add(new(big.Int).SetBytes(smallYID[32:]), p)
	yPlusP.FillBytes(nonCanonicalY[32:])

	maxUint256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

	invalid := []struct {
		name string
		id   NodeID
	}{
		// The two "+ P" cases directly exercise GO-2026-4507 / CVE-2026-26314:
		// on go-ethereum v1.10.22, BitCurve.IsOnCurve reduced coordinates mod P
		// and accepted these non-canonical encodings of valid curve points.
		{"x = canonicalX + P (x >= P)", nonCanonicalX},
		{"y = canonicalY + P (y >= P)", nonCanonicalY},
		// Boundary and off-curve regression guards (also off-curve mod P).
		{"x == P", makeID(p, new(big.Int).SetBytes(smallXID[32:]))},
		{"y == P", makeID(new(big.Int).SetBytes(smallYID[:32]), p)},
		{"x == P and y == P", makeID(p, p)},
		{"x == 2^256-1", makeID(maxUint256, big.NewInt(1))},
		{"y == 2^256-1", makeID(big.NewInt(1), maxUint256)},
		{"zero point (0, 0)", NodeID{}},
		{"off-curve (1, 2)", makeID(big.NewInt(1), big.NewInt(2))},
	}

	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if pub, err := tc.id.Pubkey(); err == nil {
				t.Fatalf("NodeID.Pubkey() accepted invalid point (%s): got (%x, %x)", tc.name, pub.X, pub.Y)
			}
		})
	}
}
