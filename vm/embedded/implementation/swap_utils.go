package implementation

import (
	"crypto/sha256"
	"encoding/base64"
	"math/big"
	"reflect"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"
	"golang.org/x/crypto/ripemd160"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
)

var swapUtilsLog = common.EmbeddedLogger.New("contract", "swap-utils-log")

const (
	hashHeader          = "Zenon secp256k1 signature:"
	assetsMessage       = "ZNN swap retrieve assets"
	legacyPillarMessage = "ZNN swap retrieve legacy pillar"

	SwapRetrieveAssets       = 1
	SwapRetrieveLegacyPillar = 2
)

func toOldSignature(signature []byte) string {
	// transform signature in old znn-style signature
	header := signature[64]
	header += 31
	signature = append([]byte{header}, signature[0:64]...)
	return base64.StdEncoding.EncodeToString(signature)
}

func PubKeyToKeyId(pubKey []byte) []byte {
	A := new(big.Int).SetBytes(pubKey[1:33])
	B := new(big.Int).SetBytes(pubKey[33:])
	compressed := secp256k1.CompressPubkey(A, B)
	sha := sha256.New()
	sha.Write(compressed)
	ripe := ripemd160.New()
	ripe.Write(sha.Sum(nil))
	return ripe.Sum(nil)
}

func PubKeyToKeyIdHash(pubKey []byte) types.Hash {
	keyId := PubKeyToKeyId(pubKey)
	sha := sha256.New()
	sha.Write(keyId)
	return types.BytesToHashPanic(sha.Sum(nil))
}

// SignRetrieveAssetsMessage is used for in contract tests
func SignRetrieveAssetsMessage(address types.Address, prv []byte, pub string) (string, error) {
	// config message & verify against expected message
	message := GetSwapMessage(assetsMessage, pub, address)

	// sign message
	signature, err := secp256k1.Sign(message, prv)
	if err != nil {
		return "", err
	}
	return toOldSignature(signature), nil
}

// SignLegacyPillarMessage is used for in contract tests
func SignLegacyPillarMessage(address types.Address, prv []byte, pub string) (string, error) {
	// config message & verify against expected message
	message := GetSwapMessage(legacyPillarMessage, pub, address)

	// sign message
	signature, err := secp256k1.Sign(message, prv)
	if err != nil {
		return "", err
	}
	return toOldSignature(signature), nil
}

func serializeString(txt string) []byte {
	y := append([]byte(""), byte(len(txt)))
	return append(y, []byte(txt)...)
}

func GetSwapMessage(operationMessage string, pubKey string, addr types.Address) []byte {
	var data []byte
	data = append(data, serializeString(hashHeader)...)
	data = append(data, serializeString(operationMessage+" "+pubKey+" "+addr.String())...)
	a := sha256.Sum256(data)
	b := sha256.Sum256(a[:])
	return b[:]
}

func CheckSwapSignature(messageType int, addr types.Address, pubKeyStr string, signatureStr string) (bool, error) {
	pubKey, err := base64.StdEncoding.DecodeString(pubKeyStr)
	if err != nil {
		swapUtilsLog.Debug("swap-utils-error", "reason", "malformed-pubKey")
		return false, constants.ErrInvalidB64Decode
	}
	if len(pubKey) != 65 {
		swapUtilsLog.Debug("swap-utils-error", "reason", "invalid-pubKey-length")
		return false, constants.ErrInvalidB64Decode
	}

	sig, err := base64.StdEncoding.DecodeString(signatureStr)
	if err != nil {
		swapUtilsLog.Debug("swap-utils-error", "reason", "malformed-signature")
		return false, constants.ErrInvalidB64Decode
	}
	if len(sig) != 65 {
		swapUtilsLog.Debug("swap-utils-error", "reason", "invalid-signature-length")
		return false, constants.ErrInvalidSignature
	}

	var operationMessage string
	if messageType == SwapRetrieveAssets {
		operationMessage = assetsMessage
	} else if messageType == SwapRetrieveLegacyPillar {
		operationMessage = legacyPillarMessage
	} else {
		swapUtilsLog.Debug("swap-utils-error", "reason", "invalid-operation")
		return false, constants.ErrInvalidSwapCode
	}

	message := GetSwapMessage(operationMessage, pubKeyStr, addr)
	swapUtilsLog.Debug("swap-utils-log", "expected-message", hexutil.Encode(message))

	// Transform signature from Old Znn-style to go secp256k1 signature
	header := sig[0]
	header -= 31
	sig = append(sig, header)
	sig = sig[1:]

	recoveredPubKey, err := secp256k1.RecoverPubkey(message, sig)
	if err != nil {
		swapUtilsLog.Debug("swap-utils-error", "reason", err)
		return false, constants.ErrInvalidSignature
	}
	if !reflect.DeepEqual(pubKey, recoveredPubKey) {
		swapUtilsLog.Debug("swap-utils-error", "reason", "invalid-signature")
		return false, constants.ErrInvalidSignature
	}

	return true, nil
}
