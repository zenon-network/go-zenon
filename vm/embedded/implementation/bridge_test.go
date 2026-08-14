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

func TestChangeTssECDSAPubKey_ValidateSendBlock(t *testing.T) {
	method := &ChangeTssECDSAPubKeyMethod{definition.ChangeTssECDSAPubKeyMethodName}

	// 33 bytes that are not a point on the curve. secp256k1.DecompressPubkey
	// returns nil coordinates for these, so they must not reach any consumer
	// that dereferences the result.
	offCurveZeroes := base64.StdEncoding.EncodeToString(make([]byte, constants.CompressedECDSAPubKeyLength))

	offCurveMaxBytes := make([]byte, constants.CompressedECDSAPubKeyLength)
	offCurveMaxBytes[0] = 0x02
	for i := 1; i < len(offCurveMaxBytes); i++ {
		offCurveMaxBytes[i] = 0xff
	}
	offCurveMax := base64.StdEncoding.EncodeToString(offCurveMaxBytes)

	tests := []struct {
		name     string
		pubKey   string
		expected error
	}{
		{"valid point", validCompressedPubKey, nil},
		{"off-curve zeroes", offCurveZeroes, constants.ErrInvalidCompressedECDSAPubKey},
		{"off-curve max x", offCurveMax, constants.ErrInvalidCompressedECDSAPubKey},
		{"not base64", "!!!not-base64!!!", constants.ErrInvalidB64Decode},
		{"too short", base64.StdEncoding.EncodeToString(make([]byte, 32)), constants.ErrInvalidCompressedECDSAPubKeyLength},
		{"too long", base64.StdEncoding.EncodeToString(make([]byte, 34)), constants.ErrInvalidCompressedECDSAPubKeyLength},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := method.ValidateSendBlock(changeTssPubKeyBlock(test.pubKey)); err != test.expected {
				t.Fatalf("expected %v, got %v", test.expected, err)
			}
		})
	}
}

// An off-curve key must be reported as an error by every entry point rather than
// being passed on as a nil coordinate.
func TestChangeTssECDSAPubKey_OffCurveKeyDoesNotPanic(t *testing.T) {
	method := &ChangeTssECDSAPubKeyMethod{definition.ChangeTssECDSAPubKeyMethodName}
	offCurve := base64.StdEncoding.EncodeToString(make([]byte, constants.CompressedECDSAPubKeyLength))

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("off-curve public key panicked instead of returning an error: %v", r)
		}
	}()

	// Validation must fail before ReceiveBlock touches its context. Passing nil
	// here exercises the receive entry point that originally dereferenced the
	// invalid coordinates, while making any premature context access fail loudly.
	if _, err := method.ReceiveBlock(nil, changeTssPubKeyBlock(offCurve)); err != constants.ErrInvalidCompressedECDSAPubKey {
		t.Fatalf("expected %v, got %v", constants.ErrInvalidCompressedECDSAPubKey, err)
	}
}
