package genesis

import (
	"math/big"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

func checkAccountBalance(g *GenesisConfig, addr types.Address, required map[types.ZenonTokenStandard]*big.Int) error {
	// Check account balance for enough qsr
	for _, block := range g.GenesisBlocks.Blocks {
		if block.Address != addr {
			continue
		}

		for zts, amount := range block.BalanceList {
			requiredAmount, ok := required[zts]
			if !ok {
				return errors.Errorf("invalid balance for %v Extra token %v", addr, zts)
			} else {
				if requiredAmount.Cmp(amount) != 0 {
					return errors.Errorf("invalid balance for %v Expected %v %v but got %v", addr, requiredAmount, zts, amount)
				}
			}
		}

		for token := range required {
			_, ok := block.BalanceList[token]
			if !ok && required[token].Cmp(common.Big0) != 0 {
				return errors.Errorf("invalid balance for %v Expected token %v to be present", addr, token)
			}
		}
	}

	return nil
}

func CheckGenesis(g *GenesisConfig) error {
	if err := CheckFieldsExist(g); err != nil {
		return err
	}
	if err := CheckPlasmaInfo(g); err != nil {
		return err
	}
	if err := CheckSwapAccount(g); err != nil {
		return err
	}
	if err := CheckPillarBalance(g); err != nil {
		return err
	}
	if err := CheckTokenTotalSupply(g); err != nil {
		return err
	}
	return nil
}

func CheckFieldsExist(g *GenesisConfig) error {
	if g.GenesisBlocks == nil {
		return errors.Errorf("GenesisBlocks is nil")
	}
	if g.TokenConfig == nil {
		return errors.Errorf("TokenConfig is nil")
	}
	if g.PillarConfig == nil {
		return errors.Errorf("PillarConfig is nil")
	}
	if g.SporkAddress == nil {
		return errors.Errorf("SporkAddress is nil")
	}
	if g.PlasmaConfig == nil {
		return errors.Errorf("PlasmaConfig is nil")
	}
	if g.SwapConfig == nil {
		return errors.Errorf("SwapConfig is nil")
	}
	return nil
}
func CheckPlasmaInfo(g *GenesisConfig) error {
	totalAmount := big.NewInt(0)

	for addr, fusion := range g.PlasmaConfig.Fusions {
		if fusion == nil {
			return errors.Errorf("nil FusionInfo for %v", addr)
		}
		totalAmount.Add(totalAmount, fusion.Amount)
	}

	return checkAccountBalance(g, types.PlasmaContract, map[types.ZenonTokenStandard]*big.Int{
		types.QsrTokenStandard: totalAmount,
	})
}
func CheckSwapAccount(g *GenesisConfig) error {
	given := map[types.ZenonTokenStandard]*big.Int{
		types.ZnnTokenStandard: big.NewInt(0),
		types.QsrTokenStandard: big.NewInt(0),
	}

	for _, entry := range g.SwapConfig.Entries {
		if entry.Qsr == nil || entry.Znn == nil {
			return errors.Errorf("invalid swap balance for KeyIdHash %v", entry.KeyIdHash)
		}
	}

	return checkAccountBalance(g, types.SwapContract, given)
}
func CheckPillarBalance(g *GenesisConfig) error {
	totalAmount := big.NewInt(0)

	for _, el := range g.PillarConfig.Pillars {
		totalAmount.Add(totalAmount, el.Amount)
	}

	return checkAccountBalance(g, types.PillarContract, map[types.ZenonTokenStandard]*big.Int{
		types.ZnnTokenStandard: totalAmount,
	})
}
func CheckTokenTotalSupply(g *GenesisConfig) error {
	given := make(map[types.ZenonTokenStandard]*big.Int)
	for _, block := range g.GenesisBlocks.Blocks {
		for zts, amount := range block.BalanceList {
			total, ok := given[zts]
			if !ok {
				given[zts] = new(big.Int).Set(amount)
			} else {
				total.Add(total, amount)
			}
		}
	}

	for _, token := range g.TokenConfig.Tokens {
		total, ok := given[token.TokenStandard]
		if !ok {
			return errors.Errorf("token %v declared but not given at all", token)
		} else if token.TotalSupply.Cmp(total) != 0 {
			return errors.Errorf("invalid token total balance for %v Expected %v but got %v", token, total, token.TotalSupply)
		}
	}

	for zts := range given {
		found := false
		for _, token := range g.TokenConfig.Tokens {
			if token.TokenStandard == zts {
				found = true
				break
			}
		}

		if !found {
			return errors.Errorf("invalid token %v given but not declared", zts)
		}
	}
	return nil
}

// CheckGenesisCheckSum ensures that the hash of the account blocks don't change during the build.
func CheckGenesisCheckSum(g *GenesisConfig, expected types.Hash) error {
	genesis := NewGenesis(g)
	checkSum := genesis.GetGenesisMomentum().Hash
	if checkSum != expected {
		return errors.Errorf("invalid genesis-momentum hash. Expected %v but got %v", expected, checkSum)
	}
	return nil
}
