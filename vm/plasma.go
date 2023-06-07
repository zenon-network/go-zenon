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

func GetDifficultyForPlasma(requiredPlasma uint64) (uint64, error) {
	if requiredPlasma > constants.MaxPoWPlasmaForAccountBlock {
		return 0, constants.ErrForbiddenParam
	} else if requiredPlasma == 0 {
		return 0, nil
	}
	return requiredPlasma * constants.PoWDifficultyPerPlasma, nil
}
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
// *Takes* into consideration used plasma by unconfirmed blocks.
//
// Plasma equals to fusedPlasma - plasmaUsedByUnconfirmedBlocks
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
