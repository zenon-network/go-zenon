package tests

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/implementation"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

type Height struct {
	Height uint64 `json:"height"`
}
type listToCount struct {
	count int
}

func (a *listToCount) UnmarshalJSON(data []byte) error {
	aux := make([]interface{}, 0)
	err := json.Unmarshal(data, &aux)
	if err != nil {
		return err
	}

	a.count = len(aux)
	return nil
}
func (a *listToCount) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%v", a.count)), nil
}

type genericListOf struct {
	generator func() interface{}
	Count     int           `json:"count"`
	List      []interface{} `json:"list"`
}

func (a *genericListOf) UnmarshalJSON(data []byte) error {
	aux := struct {
		Count int           `json:"count"`
		List  []interface{} `json:"list"`
	}{}
	err := json.Unmarshal(data, &aux)
	if err != nil {
		return err
	}

	a.Count = aux.Count
	a.List = make([]interface{}, len(aux.List))
	for index := range aux.List {
		data, err := json.Marshal(aux.List[index])
		if err != nil {
			return err
		}
		a.List[index] = a.generator()
		err = json.Unmarshal(data, a.List[index])
		if err != nil {
			return err
		}
	}
	return nil
}

func ListOf(generator func() interface{}) interface{} {
	return &genericListOf{
		generator: generator,
	}
}
func ListOfHeight() interface{} {
	return ListOf(func() interface{} {
		return new(Height)
	})
}
func ListOfName() interface{} {
	return ListOf(func() interface{} {
		return new(struct {
			Name string `json:"name"`
		})
	})
}

func init() {
	// set local time to UTC for logging purposes
	time.Local = time.UTC

	constants.SentinelLockTimeWindow = 40   // 40 momentums
	constants.SentinelRevokeTimeWindow = 20 // 20 momentums
	constants.RewardTimeLimit = 0           // 0 seconds
	constants.UpdateMinNumMomentums = 360   // exactly one hour
	constants.FuseExpiration = 100
	constants.StakeTimeUnitSec = 60 * 60
	constants.StakeTimeMinSec = constants.StakeTimeUnitSec * 1
	constants.StakeTimeMaxSec = constants.StakeTimeUnitSec * 12

}

func autoreceive(t *testing.T, z mock.MockZenon, address types.Address) {
	ledgerApi := api.NewLedgerApi(z)
	unreceived, err := ledgerApi.GetUnreceivedBlocksByAddress(address, 0, 50)
	common.FailIfErr(t, err)
	for _, block := range unreceived.List {
		z.InsertReceiveBlock(block.AccountBlock.Header(), nil, nil, mock.SkipVmChanges)
	}
}

func signRetrieveAssetsMessage(t *testing.T, address types.Address, prv []byte, pub string) string {
	signature, err := implementation.SignRetrieveAssetsMessage(address, prv, pub)
	common.FailIfErr(t, err)
	return signature
}
