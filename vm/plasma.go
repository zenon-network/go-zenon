package vm

import (
	"math/big"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

// GetDifficultyForPlasma returns the proof-of-work difficulty that
// yields requiredPlasma (constants.PoWDifficultyPerPlasma — 1500 —
// per unit). Plasma beyond what PoW may cover for one block
// (constants.MaxPoWPlasmaForAccountBlock) is rejected with
// constants.ErrForbiddenParam.
func GetDifficultyForPlasma(requiredPlasma uint64) (uint64, error) {
	if requiredPlasma > constants.MaxPoWPlasmaForAccountBlock {
		return 0, constants.ErrForbiddenParam
	} else if requiredPlasma == 0 {
		return 0, nil
	}
	return requiredPlasma * constants.PoWDifficultyPerPlasma, nil
}

// DifficultyToPlasma is the inverse of GetDifficultyForPlasma: the
// plasma granted by a proof-of-work of the given difficulty
// (difficulty / 1500), capped at
// constants.MaxPoWPlasmaForAccountBlock.
func DifficultyToPlasma(difficulty uint64) uint64 {
	// Check for 0
	if difficulty == 0 {
		return 0
	}

	// Check for more than max plasma allowed
	if difficulty > constants.MaxDifficultyForAccountBlock {
		return constants.MaxPoWPlasmaForAccountBlock
	}

	return difficulty / constants.PoWDifficultyPerPlasma
}

// FussedAmountToPlasma converts an amount of QSR fused to a
// beneficiary into its plasma capacity: every whole fusion unit
// (constants.CostPerFusionUnit, 1 QSR) grants
// constants.PlasmaPerFusionUnit, capped at
// constants.MaxFusionPlasmaForAccount (5000 units' worth).
func FussedAmountToPlasma(amount *big.Int) uint64 {
	// Check for 0
	if amount == nil || amount.Sign() <= 0 {
		return 0
	}
	// Check for more than max plasma allowed
	if amount.Cmp(constants.MaxFussedAmountForAccountBig) >= 0 {
		return constants.MaxFusionPlasmaForAccount
	}

	numUnits := amount.Uint64() / constants.CostPerFusionUnit
	return numUnits * constants.PlasmaPerFusionUnit
}

// AvailablePlasma returns only the total amount of plasma available.
//
// Plasma equals to fusedPlasma - plasmaUsedByUnconfirmedBlocks.
//
// fusedPlasma is FussedAmountToPlasma of the QSR fused for the
// account, and the unconfirmed usage is the difference between the
// account store's cumulative chain-plasma counter (which includes
// unconfirmed blocks) and the momentum-confirmed one — plasma spent
// by a block stops counting once the block is confirmed.
func AvailablePlasma(momentum store.Momentum, account store.Account) (uint64, error) {
	address := *account.Address()
	committed, err := momentum.GetAccountStore(address).GetChainPlasma()
	if err != nil {
		return 0, err
	}
	fused, err := momentum.GetStakeBeneficialAmount(address)
	if err != nil {
		return 0, err
	}
	uncommitted, err := account.GetChainPlasma()
	if err != nil {
		return 0, err
	}
	fusedPlasma := big.NewInt(int64(FussedAmountToPlasma(fused)))

	answer := new(big.Int).Add(fusedPlasma, committed)
	answer = answer.Sub(answer, uncommitted)
	if answer.Sign() == -1 {
		return 0, errors.Errorf("got negative available plasma")
	}
	if answer.Cmp(constants.MaxFussedAmountForAccountBig) == +1 {
		return constants.MaxFussedAmountForAccount, nil
	} else {
		return answer.Uint64(), nil
	}
}

// GetBasePlasmaForAccountBlock calculates the smallest plasma required for an account block.
//
// Three tiers: receive blocks cost a flat
// constants.AccountBlockBasePlasma; sends to user addresses cost the
// flat base plus constants.ABByteDataPlasma per byte of Data (capped
// at constants.MaxDataLength bytes); sends to embedded contracts cost
// the called method's fixed price from constants.AlphanetPlasmaTable.
// Blocks issued by embedded addresses cost 0.
func GetBasePlasmaForAccountBlock(context vm_context.AccountVmContext, block *nom.AccountBlock) (uint64, error) {
	if types.IsEmbeddedAddress(block.Address) {
		return 0, nil
	}
	if block.IsReceiveBlock() {
		return constants.AccountBlockBasePlasma, nil
	} else {
		if method, err := embedded.GetEmbeddedMethod(context, block.ToAddress, block.Data); err == constants.ErrNotContractAddress {
			if len(block.Data) > constants.MaxDataLength {
				return 0, verifier.ErrABDataTooBig
			}
			return uint64(len(block.Data)*constants.ABByteDataPlasma + constants.AccountBlockBasePlasma), nil
		} else if err != nil {
			return 0, err
		} else {
			return method.GetPlasma(&constants.AlphanetPlasmaTable)
		}
	}
}
