package genesis

import (
	"math/big"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
)

func checkAccountBalance(g *GenesisConfig, addr types.Address, required map[types.ZenonTokenStandard]*big.Int) error {
	found := false
	// Check account balance for enough qsr
	for _, block := range g.GenesisBlocks.Blocks {
		if block.Address != addr {
			continue
		}
		found = true

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

	if !found {
		for token, amount := range required {
			if amount.Cmp(common.Big0) != 0 {
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
	// The per-entry checks run before the aggregate ones so that the
	// sums below only ever see present, non-negative amounts.
	if err := CheckEntryAmounts(g); err != nil {
		return err
	}
	if err := CheckUniqueGenesisAddresses(g); err != nil {
		return err
	}
	if err := CheckLegacyEntries(g); err != nil {
		return err
	}
	if err := CheckPillarStakes(g); err != nil {
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

// CheckEntryAmounts verifies that no list in the config holds a nil
// entry and that every per-entry amount is present and non-negative:
// genesis balances, fusion amounts, swap ZNN and QSR, and token total
// supplies. Pillar amounts are checked by CheckPillarStakes. Balances
// and fused amounts are stored unsigned, so a negative entry would
// pass the signed aggregate checks and materialise as a different,
// positive value.
func CheckEntryAmounts(g *GenesisConfig) error {
	for i, block := range g.GenesisBlocks.Blocks {
		if block == nil {
			return errors.Wrapf(ErrGenesisNilEntry, "GenesisBlocks.Blocks[%d]", i)
		}
		for zts, amount := range block.BalanceList {
			if amount == nil {
				return errors.Wrapf(ErrGenesisNilAmount, "balance of %v for %v", zts, block.Address)
			}
			if amount.Sign() < 0 {
				return errors.Wrapf(ErrGenesisNegativeBalance, "%v %v for %v", amount, zts, block.Address)
			}
		}
	}
	for i, fusion := range g.PlasmaConfig.Fusions {
		if fusion == nil {
			return errors.Wrapf(ErrGenesisNilEntry, "PlasmaConfig.Fusions[%d]", i)
		}
		if fusion.Amount == nil {
			return errors.Wrapf(ErrGenesisNilAmount, "fusion %v", fusion.Id)
		}
		if fusion.Amount.Sign() < 0 {
			return errors.Wrapf(ErrGenesisNegativeFusion, "%v for fusion %v", fusion.Amount, fusion.Id)
		}
	}
	for i, entry := range g.SwapConfig.Entries {
		if entry == nil {
			return errors.Wrapf(ErrGenesisNilEntry, "SwapConfig.Entries[%d]", i)
		}
		if entry.Znn == nil || entry.Qsr == nil {
			return errors.Wrapf(ErrGenesisNilAmount, "swap entry %v", entry.KeyIdHash)
		}
		if entry.Znn.Sign() < 0 || entry.Qsr.Sign() < 0 {
			return errors.Wrapf(ErrGenesisNegativeSwapAmount, "znn %v qsr %v for swap entry %v", entry.Znn, entry.Qsr, entry.KeyIdHash)
		}
	}
	for i, token := range g.TokenConfig.Tokens {
		if token == nil {
			return errors.Wrapf(ErrGenesisNilEntry, "TokenConfig.Tokens[%d]", i)
		}
		if token.TotalSupply == nil {
			return errors.Wrapf(ErrGenesisNilAmount, "TotalSupply of %v", token.TokenStandard)
		}
	}
	return nil
}

// CheckUniqueGenesisAddresses verifies that every GenesisBlocks entry
// names a different address. The supply check counts every entry,
// while state construction keeps only the last entry per address and
// token, so a duplicate would silently drop balances that the supply
// check counted.
func CheckUniqueGenesisAddresses(g *GenesisConfig) error {
	seen := make(map[types.Address]struct{}, len(g.GenesisBlocks.Blocks))
	for _, block := range g.GenesisBlocks.Blocks {
		if _, ok := seen[block.Address]; ok {
			return errors.Wrapf(ErrGenesisDuplicateAddress, "%v", block.Address)
		}
		seen[block.Address] = struct{}{}
	}
	return nil
}

// CheckLegacyEntries verifies that every legacy pillar entry has at
// least one slot. Legacy registration only checks that the entry
// exists before decrementing its uint8 PillarCount, so a zero count
// would wrap to 255 free registrations.
func CheckLegacyEntries(g *GenesisConfig) error {
	for i, entry := range g.PillarConfig.LegacyEntries {
		if entry == nil {
			return errors.Wrapf(ErrGenesisNilEntry, "PillarConfig.LegacyEntries[%d]", i)
		}
		if entry.PillarCount == 0 {
			return errors.Wrapf(ErrGenesisZeroLegacySlots, "legacy entry %v", entry.KeyIdHash)
		}
	}
	return nil
}

// CheckPillarStakes verifies that every genesis pillar stakes exactly
// constants.PillarStakeAmount, the only amount registration accepts
// and the amount revocation refunds regardless of the stored Amount.
func CheckPillarStakes(g *GenesisConfig) error {
	for i, pillar := range g.PillarConfig.Pillars {
		if pillar == nil {
			return errors.Wrapf(ErrGenesisNilEntry, "PillarConfig.Pillars[%d]", i)
		}
		if pillar.Amount == nil || pillar.Amount.Cmp(constants.PillarStakeAmount) != 0 {
			return errors.Wrapf(ErrGenesisPillarStakeAmount, "pillar %q stakes %v, expected %v", pillar.Name, pillar.Amount, constants.PillarStakeAmount)
		}
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

// CheckGenesisCheckSum ensures that the hash of the account blocks don't change during the build.
func CheckGenesisCheckSum(g *GenesisConfig, expected types.Hash) error {
	genesis := NewGenesis(g)
	checkSum := genesis.GetGenesisMomentum().Hash
	if checkSum != expected {
		return errors.Errorf("invalid genesis-momentum hash. Expected %v but got %v", expected, checkSum)
	}
	return nil
}
