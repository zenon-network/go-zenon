package momentum

import (
	"fmt"
	"math/big"
	"sort"

	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// GetActivePillars reads the list of non-revoked pillars of any type
// from the pillar embedded contract's storage.
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

// ComputePillarDelegations weighs every active pillar by the summed
// ZNN balances of the accounts delegating to it (read from the cached
// per-address ZNN balance index), returning the details sorted
// descending by weight with ties broken by name. The consensus
// election uses this to pick producers.
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

// GetStakeBeneficialAmount returns the QSR amount fused for the
// benefit of addr, read from the plasma embedded contract's storage;
// it determines the account's fusion-backed plasma.
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

// GetTokenInfoByTs reads a token's metadata from the token embedded
// contract's storage; it returns nil if the token does not exist.
func (ms *momentumStore) GetTokenInfoByTs(ts types.ZenonTokenStandard) (*definition.TokenInfo, error) {
	sd, err := ms.getEmbeddedStore(types.TokenContract)
	if err != nil {
		return nil, fmt.Errorf("getEmbeddedStore failed: %w", err)
	}

	return definition.GetTokenInfo(sd.Storage(), ts)
}

// GetAllDefinedSporks reads every spork ever created from the spork
// embedded contract's storage, activated or not.
func (ms *momentumStore) GetAllDefinedSporks() ([]*definition.Spork, error) {
	sd, err := ms.getEmbeddedStore(types.SporkContract)
	if err != nil {
		return nil, fmt.Errorf("getEmbeddedStore failed: %w", err)
	}

	return definition.GetAllSporks(sd.Storage()), nil
}

// IsSporkActive reports whether the given implemented spork is
// activated with an enforcement height at or below the frontier
// momentum; it gates spork-dependent features in the VM and verifier.
// It is always false at height 1 (the genesis momentum).
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
