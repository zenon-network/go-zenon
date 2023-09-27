package momentum

import (
	"fmt"
	"math/big"
	"sort"

	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

func (ms *momentumStore) GetActivePillars() ([]*definition.PillarInfo, error) {
	sd, err := ms.getEmbeddedStore(types.PillarContract)
	if err != nil {
		return nil, fmt.Errorf("getEmbeddedStore failed: %w", err)
	}

	return definition.GetPillarsList(sd.Storage(), true, definition.AnyPillarType)
}
func (ms *momentumStore) getAllDelegations() ([]*definition.DelegationInfo, error) {
	sd, err := ms.getEmbeddedStore(types.PillarContract)
	if err != nil {
		return nil, fmt.Errorf("getEmbeddedStore failed: %w", err)
	}

	return definition.GetDelegationsList(sd.Storage())
}
func (ms *momentumStore) computeBackers(infos []*definition.DelegationInfo) (*map[string]map[types.Address]*big.Int, error) {
	result := map[string]map[types.Address]*big.Int{}

	addresses := make([]types.Address, 0, len(infos))
	balanceMap := make(map[types.Address]*big.Int)
	for _, delegation := range infos {
		balance, err := ms.getZnnBalance(delegation.Backer)
		if err != nil {
			return nil, err
		}
		balanceMap[delegation.Backer] = balance
		addresses = append(addresses, delegation.Backer)
	}

	for _, delegation := range infos {
		balance, ok := balanceMap[delegation.Backer]
		if !ok {
			balance = big.NewInt(0)
		}

		delegators, ok := result[delegation.Name]
		if !ok {
			delegators = map[types.Address]*big.Int{}
		}

		delegators[delegation.Backer] = balance
		result[delegation.Name] = delegators
	}
	return &result, nil
}
func (ms *momentumStore) ComputePillarDelegations() ([]*types.PillarDelegationDetail, error) {
	delegations, _ := ms.getAllDelegations()
	backers, err := ms.computeBackers(delegations)
	if err != nil {
		return nil, err
	}

	// query register info
	registerList, _ := ms.GetActivePillars()
	pillarDelegationDetails := make([]*types.PillarDelegationDetail, 0, len(registerList))
	for _, registration := range registerList {
		pillarDelegationDetails = append(pillarDelegationDetails, &types.PillarDelegationDetail{
			PillarDelegation: types.PillarDelegation{
				Name:      registration.Name,
				Producing: registration.BlockProducingAddress,
				Weight:    big.NewInt(0),
			},
			Backers: make(map[types.Address]*big.Int, 0),
		})
	}

	for pillarName, delegators := range *backers {
		// Get registration
		var delegation *types.PillarDelegationDetail
		for _, r := range pillarDelegationDetails {
			if r.Name == pillarName {
				delegation = r
			}
		}

		if delegation == nil {
			continue
		}

		totalBalance := big.NewInt(0)
		for _, balance := range delegators {
			totalBalance.Add(totalBalance, balance)
		}

		delegation.Weight.Set(totalBalance)
		delegation.Backers = delegators
	}

	sort.Sort(types.SortPDDByWeight(pillarDelegationDetails))
	return pillarDelegationDetails, nil
}

func (ms *momentumStore) GetStakeBeneficialAmount(addr types.Address) (*big.Int, error) {
	sd, err := ms.getEmbeddedStore(types.PlasmaContract)
	if err != nil {
		return nil, fmt.Errorf("getEmbeddedStore failed: %w", err)
	}

	fused, err := definition.GetFusedAmount(sd.Storage(), addr)
	if err != nil {
		return nil, err
	}
	return fused.Amount, nil
}
func (ms *momentumStore) GetTokenInfoByTs(ts types.ZenonTokenStandard) (*definition.TokenInfo, error) {
	sd, err := ms.getEmbeddedStore(types.TokenContract)
	if err != nil {
		return nil, fmt.Errorf("getEmbeddedStore failed: %w", err)
	}

	return definition.GetTokenInfo(sd.Storage(), ts)
}
func (ms *momentumStore) GetAllDefinedSporks() ([]*definition.Spork, error) {
	sd, err := ms.getEmbeddedStore(types.SporkContract)
	if err != nil {
		return nil, fmt.Errorf("getEmbeddedStore failed: %w", err)
	}

	return definition.GetAllSporks(sd.Storage()), nil
}
func (ms *momentumStore) IsSporkActive(implemented *types.ImplementedSpork) (bool, error) {
	frontier, err := ms.GetFrontierMomentum()
	if err != nil {
		return false, err
	}
	if frontier.Height == 1 {
		return false, nil
	}

	sporks, err := ms.GetAllDefinedSporks()
	if err != nil {
		return false, err
	}

	for _, spork := range sporks {
		if spork.Activated && spork.EnforcementHeight <= frontier.Height && spork.Id == implemented.SporkId {
			return true, nil
		}
	}

	return false, nil
}

func (ms *momentumStore) getEmbeddedStore(address types.Address) (store.Account, error) {
	return ms.GetAccountStore(address), nil
}
