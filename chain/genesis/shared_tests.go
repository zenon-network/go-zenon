package genesis

import (
	"math/big"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

// checkAccountBalance verifies that addr's GenesisBlocks balances
// match required exactly: no extra tokens, no amount mismatches and
// no missing tokens with a non-zero required amount. An address
// absent from GenesisBlocks passes vacuously.
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

// CheckGenesis runs all consistency checks on an untrusted genesis
// config and returns the first failure; ReadGenesisConfigFromFile
// rejects configs that do not pass. The individual Check* functions
// are exported so the embedded and mock genesis tests can exercise
// them one by one.
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

// CheckFieldsExist verifies that every mandatory section of the
// config is present; only SporkConfig and ExtraData are optional.
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

// CheckPlasmaInfo verifies that no fusion entry is nil and that the
// plasma contract's QSR balance in GenesisBlocks equals the total
// fused amount.
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

// CheckSwapAccount verifies that every swap entry declares both ZNN
// and QSR amounts and that the swap contract itself holds no genesis
// balance — swapped funds are minted at retrieval, not pre-funded.
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

// CheckPillarBalance verifies that the pillar contract's ZNN balance
// in GenesisBlocks equals the sum of all pillars' staked amounts.
func CheckPillarBalance(g *GenesisConfig) error {
	totalAmount := big.NewInt(0)

	for _, el := range g.PillarConfig.Pillars {
		totalAmount.Add(totalAmount, el.Amount)
	}

	return checkAccountBalance(g, types.PillarContract, map[types.ZenonTokenStandard]*big.Int{
		types.ZnnTokenStandard: totalAmount,
	})
}

// CheckTokenTotalSupply verifies that the declared tokens and the
// distributed balances agree: every token's TotalSupply must equal
// the sum given out via GenesisBlocks, and no balance may use a ZTS
// that is not declared in TokenConfig.
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

// CheckGenesisCheckSum ensures the genesis construction is
// reproducible across builds: it fabricates the full genesis state
// from g and compares the resulting momentum hash against expected,
// pinning the deterministic construction.
func CheckGenesisCheckSum(g *GenesisConfig, expected types.Hash) error {
	genesis := NewGenesis(g)
	checkSum := genesis.GetGenesisMomentum().Hash
	if checkSum != expected {
		return errors.Errorf("invalid genesis-momentum hash. Expected %v but got %v", expected, checkSum)
	}
	return nil
}
