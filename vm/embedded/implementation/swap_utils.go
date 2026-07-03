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

	// SwapRetrieveAssets selects the operation message
	// "ZNN swap retrieve assets", which CheckSwapSignature expects
	// for claiming a legacy swap balance (RetrieveAssets).
	SwapRetrieveAssets = 1
	// SwapRetrieveLegacyPillar selects the operation message
	// "ZNN swap retrieve legacy pillar", which CheckSwapSignature
	// expects for claiming a legacy pillar slot (RegisterLegacy).
	SwapRetrieveLegacyPillar = 2
)

// toOldSignature converts a go secp256k1 recovery signature
// ([R || S || V]) to the legacy znn style — the recovery byte plus 31
// moved in front of R and S — and returns it base64-encoded.
func toOldSignature(signature []byte) string {
	// transform signature in old znn-style signature
	header := signature[64]
	header += 31
	signature = append([]byte{header}, signature[0:64]...)
	return base64.StdEncoding.EncodeToString(signature)
}

// PubKeyToKeyId derives the 20-byte legacy key id from a 65-byte
// uncompressed secp256k1 public key, Bitcoin-style: RIPEMD-160 of the
// SHA-256 of the compressed public key.
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

// PubKeyToKeyIdHash returns the SHA-256 of the public key's legacy
// key id (PubKeyToKeyId) as a 32-byte hash — the key under which the
// genesis SwapAssets and LegacyPillarEntry entries are stored.
func PubKeyToKeyIdHash(pubKey []byte) types.Hash {
	keyId := PubKeyToKeyId(pubKey)
	sha := sha256.New()
	sha.Write(keyId)
	return types.BytesToHashPanic(sha.Sum(nil))
}

// SignRetrieveAssetsMessage signs the retrieve-assets swap message
// for the address with the legacy private key, returning the
// signature in the legacy base64 format CheckSwapSignature expects;
// used in contract tests.
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

// SignLegacyPillarMessage signs the legacy-pillar swap message for
// the address with the legacy private key, returning the signature in
// the legacy base64 format CheckSwapSignature expects; used in
// contract tests.
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

// serializeString prefixes the string with its single-byte length,
// the varstr framing legacy clients use when hashing messages.
func serializeString(txt string) []byte {
	y := append([]byte(""), byte(len(txt)))
	return append(y, []byte(txt)...)
}

// GetSwapMessage builds the 32-byte digest a legacy key signs to
// authorize a swap operation: the double SHA-256 of the
// length-prefixed "Zenon secp256k1 signature:" header followed by the
// length-prefixed string "<operation message> <base64 public key>
// <bech32 address>" (each string carries a single-byte length
// prefix). Embedding the recipient address makes the signature valid
// for that NoM address only.
func GetSwapMessage(operationMessage string, pubKey string, addr types.Address) []byte {
	var data []byte
	data = append(data, serializeString(hashHeader)...)
	data = append(data, serializeString(operationMessage+" "+pubKey+" "+addr.String())...)
	a := sha256.Sum256(data)
	b := sha256.Sum256(a[:])
	return b[:]
}

// CheckSwapSignature verifies a legacy swap authorization: the
// base64 signature must recover, over the swap message
// (GetSwapMessage) selected by messageType and bound to addr, to the
// base64 public key. The public key must decode to 65 uncompressed
// bytes (constants.ErrInvalidB64Decode on bad base64 or length);
// the signature must decode from base64
// (constants.ErrInvalidB64Decode) to exactly 65 bytes
// (constants.ErrInvalidSignature otherwise); an unknown messageType
// fails with constants.ErrInvalidSwapCode. The legacy header-first
// signature is converted back to [R || S || V] form (header minus
// 31) before secp256k1 recovery; a failed recovery or a mismatched
// key fails with constants.ErrInvalidSignature. The bool result is
// true exactly when the error is nil.
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
