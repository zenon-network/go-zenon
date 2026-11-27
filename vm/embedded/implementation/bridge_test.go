package implementation

import (
	"encoding/base64"
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// validCompressedPubKey is a compressed secp256k1 point taken from the bridge
// integration fixtures, where it round-trips through a real key rotation.
const validCompressedPubKey = "AhOiqdjx002Cj8o1jxTM5LqywbgNFZwUPJuR9ffdQwFP"

func changeTssPubKeyBlock(pubKey string) *nom.AccountBlock {
	return &nom.AccountBlock{
		Address:       types.PillarContract,
		ToAddress:     types.BridgeContract,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(0),
		Data: definition.ABIBridge.PackMethodPanic(definition.ChangeTssECDSAPubKeyMethodName,
			pubKey, "", ""),
	}
}

func invalidCompressedPubKeyCases() []struct {
	name   string
	pubKey string
} {
	zeroes := make([]byte, constants.CompressedECDSAPubKeyLength)

	maxX := make([]byte, constants.CompressedECDSAPubKeyLength)
	maxX[0] = 0x02
	for i := 1; i < len(maxX); i++ {
		maxX[i] = 0xff
	}

	return []struct {
		name   string
		pubKey string
	}{
		{
			name:   "off-curve zeroes",
			pubKey: base64.StdEncoding.EncodeToString(zeroes),
		},
		{
			name:   "off-curve max x",
			pubKey: base64.StdEncoding.EncodeToString(maxX),
		},
	}
}

func TestChangeTssECDSAPubKey_ValidateSendBlock(t *testing.T) {
	method := &ChangeTssECDSAPubKeyMethod{definition.ChangeTssECDSAPubKeyMethodName}
	invalidCases := invalidCompressedPubKeyCases()

	tests := []struct {
		name     string
		pubKey   string
		amount   *big.Int
		expected error
	}{
		{"valid point", validCompressedPubKey, big.NewInt(0), nil},
		// Send-side validation preserves v0.0.10 rules: any 33-byte base64 payload
		// is accepted on send, and curve validation is enforced in ReceiveBlock.
		{"off-curve zeroes accepted on send", invalidCases[0].pubKey, big.NewInt(0), nil},
		{"off-curve max x accepted on send", invalidCases[1].pubKey, big.NewInt(0), nil},
		{"not base64", "!!!not-base64!!!", big.NewInt(0), constants.ErrInvalidB64Decode},
		{"too short", base64.StdEncoding.EncodeToString(make([]byte, 32)), big.NewInt(0), constants.ErrInvalidCompressedECDSAPubKeyLength},
		{"too long", base64.StdEncoding.EncodeToString(make([]byte, 34)), big.NewInt(0), constants.ErrInvalidCompressedECDSAPubKeyLength},
		{"non-zero amount", validCompressedPubKey, big.NewInt(1), constants.ErrInvalidTokenOrAmount},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			block := changeTssPubKeyBlock(test.pubKey)
			block.Amount = test.amount
			if err := method.ValidateSendBlock(block); err != test.expected {
				t.Fatalf("expected %v, got %v", test.expected, err)
			}
		})
	}
}

// An invalid compressed secp256k1 key must be rejected cleanly in ReceiveBlock
// before touching context state or dereferencing nil coordinates.
func TestChangeTssECDSAPubKey_OffCurveKeyDoesNotPanic(t *testing.T) {
	method := &ChangeTssECDSAPubKeyMethod{definition.ChangeTssECDSAPubKeyMethodName}

	for _, tc := range invalidCompressedPubKeyCases() {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("off-curve public key panicked instead of returning an error: %v", r)
				}
			}()

			blocks, err := method.ReceiveBlock(nil, changeTssPubKeyBlock(tc.pubKey))
			if err != constants.ErrInvalidCompressedECDSAPubKey {
				t.Fatalf("expected %v, got %v", constants.ErrInvalidCompressedECDSAPubKey, err)
			}
			if blocks != nil {
				t.Fatalf("expected nil descendant blocks, got %v", blocks)
			}
		})
	}
}
