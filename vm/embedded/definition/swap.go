package definition

import (
	"math/big"
	"strings"

	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/abi"
	"github.com/zenon-network/go-zenon/vm/constants"
)

const (
	// jsonSwap is the ABI JSON of the swap embedded contract: the
	// RetrieveAssets method and the stored swapEntry variable. Parsed
	// into ABISwap.
	jsonSwap = `
	[
		{"type":"function","name":"RetrieveAssets", "inputs":[{"name":"publicKey","type":"string"},{"name":"signature","type":"string"}]},
		{"type":"variable","name":"swapEntry", "inputs":[
			{"name":"znn","type":"uint256"}, 
			{"name":"qsr","type":"uint256"}
		]}
	]`

	// RetrieveAssetsMethodName names the method that claims a legacy
	// swap balance, proving ownership of the legacy public key with a
	// signature over the claiming address.
	RetrieveAssetsMethodName = "RetrieveAssets"

	swapEntryVariableName = "swapEntry"
)

var (
	// ABISwap is the parsed ABI of the swap embedded contract.
	ABISwap = abi.JSONToABIContract(strings.NewReader(jsonSwap))
)

// ParamRetrieveAssets carries the arguments of RetrieveAssets: the
// legacy public key and the signature, both base64-encoded.
type ParamRetrieveAssets struct {
	PublicKey string
	Signature string
}

// SwapAssets is one stored legacy swap balance: the ZNN and QSR
// (smallest units) attributed to the legacy key whose key-id SHA-256
// is KeyIdHash. Claiming does not delete the entry — RetrieveAssets
// zeroes the amounts and saves the row back, so already-claimed
// entries persist with zero balances. The stored amounts never decay
// in place; the implementation discounts them by the elapsed decay
// ticks (the constants.SwapAssetDecay* schedule) when reading and
// claiming. Unlike every other entry in this package, the key is the
// bare 32-byte KeyIdHash with no prefix byte, so the entries occupy
// the contract's whole key space.
type SwapAssets struct {
	KeyIdHash types.Hash `json:"keyIdHash"`
	Znn       *big.Int   `json:"znn"`
	Qsr       *big.Int   `json:"qsr"`
}

// Save stores the two amounts under the bare key-id-hash key,
// returning any pack or put error; the hash is recovered from the key
// when parsing.
func (assets *SwapAssets) Save(context db.DB) error {
	data, err := ABISwap.PackVariable(
		swapEntryVariableName,
		assets.Znn,
		assets.Qsr)
	if err != nil {
		return err
	}
	return context.Put(getSwapAssetsKey(assets.KeyIdHash), data)
}

func getSwapAssetsKey(keyIdHash types.Hash) []byte {
	return keyIdHash[:]
}
func parseSwapAssets(data, key []byte) (*SwapAssets, error) {
	if len(data) > 0 {
		dataVar := new(SwapAssets)
		if err := ABISwap.UnpackVariable(dataVar, swapEntryVariableName, data); err != nil {
			return nil, err
		}
		if err := dataVar.KeyIdHash.SetBytes(key); err != nil {
			return nil, err
		}
		return dataVar, nil
	} else {
		return nil, constants.ErrDataNonExistent
	}
}

// GetSwapAssetsByKeyIdHash returns the stored balance of keyIdHash,
// or constants.ErrDataNonExistent if there is none. The amounts are
// the stored, undecayed values; an already-claimed entry is returned
// with zero amounts rather than an error.
func GetSwapAssetsByKeyIdHash(context db.DB, keyIdHash types.Hash) (*SwapAssets, error) {
	key := getSwapAssetsKey(keyIdHash)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseSwapAssets(data, key)
	}
}

// GetSwapAssets returns every stored balance — including
// already-claimed entries with zero amounts — in key-id-hash byte
// order, by iterating the contract's entire key space (the entries
// have no prefix byte).
func GetSwapAssets(context db.DB) ([]*SwapAssets, error) {
	iterator := context.NewIterator([]byte{})
	defer iterator.Release()
	list := make([]*SwapAssets, 0)

	for {
		if !iterator.Next() {
			if iterator.Error() != nil {
				return nil, iterator.Error()
			}
			break
		}
		if info, err := parseSwapAssets(iterator.Value(), iterator.Key()); err == nil && info != nil {
			list = append(list, info)
		} else {
			return nil, err
		}
	}

	return list, nil
}
