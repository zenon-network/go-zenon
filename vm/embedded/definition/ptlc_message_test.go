package definition

import (
	"encoding/hex"
	"testing"

	"github.com/zenon-network/go-zenon/common/types"
)

func TestPtlcUnlockMessageVectors(t *testing.T) {
	const (
		entry            = "1111111111111111111111111111111111111111111111111111111111111111"
		rampEntry        = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
		destination      = "0022222222222222222222222222222222222222"
		otherDestination = "0033333333333333333333333333333333333333"
	)

	// Fixed SHA3-256 values computed outside the node over this 101-byte preimage:
	// "zenon-ptlc-unlock:v1" || uint64be(chain) || PTLC address[20] || point type[1]
	// || entry ID[32] || destination[20]. The fixed PTLC address bytes are
	// 01b3b6e5adcb4c15ff06318c6318c6318c6318c6. These are synthetic encoding
	// vectors, independent of Create admission and signature verification.
	tests := []struct {
		name        string
		chain       uint64
		pointType   uint8
		entry       string
		destination string
		want        string
	}{
		{"chain-zero", 0, PointTypeED25519, entry, destination, "ab7d242a75992f3c7333b09bd12ba82934f62a8a403fa261d27210e3d1a9bae8"},
		{"chain-one", 1, PointTypeED25519, entry, destination, "6600c6012a55fa02a3d788ca557097b6e9b1b27aeb04346423d329d6a70eeba4"},
		{"chain-69", 69, PointTypeED25519, entry, destination, "b14a5e8b2ac8efb130358e57e162f2bdf48462f3201f2ea1490adb4b6465ac8f"},
		{"chain-uint32-boundary", 4294967296, PointTypeED25519, entry, destination, "fb0c590acdf3babde0159fdd32456404de6149ee053674e56fec7f4be9a02707"},
		{"chain-high-bit", 9223372036854775808, PointTypeED25519, entry, destination, "6c1d4ec858127a5f7d0179320367016810ce46b3772fd639422d5ade2de38086"},
		{"chain-max-uint64", 18446744073709551615, PointTypeED25519, entry, destination, "bca38aa2df860696bd0b554e508232c884fbabdda4f2a8aff0504ec4c60ab489"},
		{"chain-byte-order", 0x0102030405060708, PointTypeED25519, entry, destination, "b9b5bd3774bf5edf9f06b3384c832fbba24152acd0ab147ca5a1d5d4fe27324e"},
		{"bip340", 69, PointTypeBIP340, entry, destination, "2330bc4573c8cc58161b5e2cad6945f0624de179f9321952ae79b6f91208b645"},
		{"secp256k1-point", 69, PointTypeSecp256k1Point, entry, destination, "b8fc59364c2f6f7e674eaa3f76d089debebc86ae5b1f3c53b172b1d49c692358"},
		{"entry-identifier", 69, PointTypeED25519, rampEntry, destination, "be7031dd38dea0704a1eeacee259a308293e4d6e6ff1db1bbd18f62c24171c08"},
		{"destination", 69, PointTypeED25519, entry, otherDestination, "6b23a7211b969c4de47cc43a16a80d899d35ad39cee2e8545eab30c4fe0a92fb"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			idBytes, err := hex.DecodeString(test.entry)
			if err != nil || len(idBytes) != types.HashSize {
				t.Fatal("invalid synthetic entry ID")
			}
			destinationBytes, err := hex.DecodeString(test.destination)
			if err != nil || len(destinationBytes) != types.AddressSize {
				t.Fatal("invalid synthetic destination")
			}
			var id types.Hash
			var destination types.Address
			copy(id[:], idBytes)
			copy(destination[:], destinationBytes)

			got := hex.EncodeToString(GetPtlcUnlockMessage(test.chain, test.pointType, id, destination))
			if got != test.want {
				t.Fatalf("unlock message = %s, want %s", got, test.want)
			}
		})
	}
}
