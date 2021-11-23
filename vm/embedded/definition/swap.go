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
	jsonSwap = `
	[
		{"type":"function","name":"RetrieveAssets", "inputs":[{"name":"publicKey","type":"string"},{"name":"signature","type":"string"}]},
		{"type":"variable","name":"swapEntry", "inputs":[
			{"name":"znn","type":"uint256"}, 
			{"name":"qsr","type":"uint256"}
		]}
	]`

	RetrieveAssetsMethodName = "RetrieveAssets"

	swapEntryVariableName = "swapEntry"
)

var (
	ABISwap = abi.JSONToABIContract(strings.NewReader(jsonSwap))
)

type ParamRetrieveAssets struct {
	PublicKey string
	Signature string
}

type SwapAssets struct {
	KeyIdHash types.Hash `json:"keyIdHash"`
	Znn       *big.Int   `json:"znn"`
	Qsr       *big.Int   `json:"qsr"`
}

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
func GetSwapAssetsByKeyIdHash(context db.DB, keyIdHash types.Hash) (*SwapAssets, error) {
	key := getSwapAssetsKey(keyIdHash)
	if data, err := context.Get(key); err != nil {
		return nil, err
	} else {
		return parseSwapAssets(data, key)
	}
}
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
