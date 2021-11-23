package constants

import "math/big"

// PlasmaTable is used to query plasma used by op code and transactions
type PlasmaTable struct {
	TxPlasma     uint64
	TxDataPlasma uint64

	// Embedded plasma costs
	EmbeddedSimple          uint64
	EmbeddedWWithdraw       uint64
	EmbeddedWDoubleWithdraw uint64
}

var (
	AlphanetPlasmaTable = PlasmaTable{
		TxPlasma:     AccountBlockBasePlasma,
		TxDataPlasma: ABByteDataPlasma,

		EmbeddedSimple:          EmbeddedSimplePlasma,
		EmbeddedWWithdraw:       EmbeddedWResponse,
		EmbeddedWDoubleWithdraw: EmbeddedWDoubleResponse,
	}
)

const (
	AccountBlockBasePlasma = 21000
	ABByteDataPlasma       = 68

	EmbeddedSimplePlasma    = 2.5 * AccountBlockBasePlasma
	EmbeddedWResponse       = 3.5 * AccountBlockBasePlasma
	EmbeddedWDoubleResponse = 4.5 * AccountBlockBasePlasma

	NumFusionUnitsForBasePlasma = 10
	PlasmaPerFusionUnit         = AccountBlockBasePlasma / NumFusionUnitsForBasePlasma
	CostPerFusionUnit           = 100000000

	PoWDifficultyPerPlasma = 1500

	// MaxDataLength defines limit of account-block data to 16Kb
	MaxDataLength = 1024 * 16

	// MaxPlasmaForAccountBlock defines max available plasma for an account block.
	MaxPlasmaForAccountBlock = MaxFusionPlasmaForAccount

	MaxPoWPlasmaForAccountBlock  = EmbeddedWDoubleResponse
	MaxDifficultyForAccountBlock = MaxPoWPlasmaForAccountBlock * PoWDifficultyPerPlasma

	// MaxFusionUnitsPerAccount limits each account to a maximum of 5000 fusion units.
	// All units above this will not increase the maximum plasma.
	MaxFusionUnitsPerAccount  = 5000
	MaxFusionPlasmaForAccount = MaxFusionUnitsPerAccount * PlasmaPerFusionUnit
	MaxFussedAmountForAccount = CostPerFusionUnit * MaxFusionUnitsPerAccount
)

var (
	MaxFussedAmountForAccountBig = big.NewInt(MaxFussedAmountForAccount)
)
