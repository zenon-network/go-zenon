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
	// AlphanetPlasmaTable is the PlasmaTable in force on Alphanet,
	// assembled from the constants below.
	AlphanetPlasmaTable = PlasmaTable{
		TxPlasma:     AccountBlockBasePlasma,
		TxDataPlasma: ABByteDataPlasma,

		EmbeddedSimple:          EmbeddedSimplePlasma,
		EmbeddedWWithdraw:       EmbeddedWResponse,
		EmbeddedWDoubleWithdraw: EmbeddedWDoubleResponse,
	}
)

const (
	// AccountBlockBasePlasma (21000) is the flat plasma cost of any
	// plain account block: every receive block, and every send to a
	// user address before the data surcharge.
	AccountBlockBasePlasma = 21000
	// ABByteDataPlasma (68) is the additional plasma charged per byte
	// of Data on a send block addressed to a user address.
	ABByteDataPlasma = 68

	// EmbeddedSimplePlasma is the flat cost of calling an embedded
	// contract method that sends no response block (PlasmaTable
	// field EmbeddedSimple).
	EmbeddedSimplePlasma = 2.5 * AccountBlockBasePlasma
	// EmbeddedWResponse is the flat cost of calling an embedded
	// contract method that sends one response block, such as a
	// withdrawal (PlasmaTable field EmbeddedWWithdraw).
	EmbeddedWResponse = 3.5 * AccountBlockBasePlasma
	// EmbeddedWDoubleResponse is the flat cost of calling an embedded
	// contract method that sends two response blocks, such as
	// revoking a sentinel and recovering both ZNN and QSR
	// (PlasmaTable field EmbeddedWDoubleWithdraw).
	EmbeddedWDoubleResponse = 4.5 * AccountBlockBasePlasma

	// NumFusionUnitsForBasePlasma is the number of fusion units
	// needed to cover one base account block.
	NumFusionUnitsForBasePlasma = 10
	// PlasmaPerFusionUnit (2100) is the plasma capacity each fusion
	// unit grants the beneficiary per account block.
	PlasmaPerFusionUnit = AccountBlockBasePlasma / NumFusionUnitsForBasePlasma
	// CostPerFusionUnit is the QSR price of one fusion unit, in
	// smallest units: 1 QSR.
	CostPerFusionUnit = 100000000

	// PoWDifficultyPerPlasma (1500) is the proof-of-work difficulty
	// that yields one unit of plasma.
	PoWDifficultyPerPlasma = 1500

	// MaxDataLength defines limit of account-block data to 16Kb
	MaxDataLength = 1024 * 16

	// MaxPlasmaForAccountBlock defines max available plasma for an
	// account block before the Dynamic Plasma spork; afterwards the
	// larger bounds in package dp apply.
	MaxPlasmaForAccountBlock = MaxFusionPlasmaForAccount

	// MaxPoWPlasmaForAccountBlock caps the plasma a single block may
	// obtain from proof of work at the double-response embedded tier
	// (EmbeddedWDoubleResponse).
	MaxPoWPlasmaForAccountBlock = EmbeddedWDoubleResponse
	// MaxDifficultyForAccountBlock is MaxPoWPlasmaForAccountBlock
	// expressed as proof-of-work difficulty.
	MaxDifficultyForAccountBlock = MaxPoWPlasmaForAccountBlock * PoWDifficultyPerPlasma

	// MaxFusionUnitsPerAccount limits each account to a maximum of 5000 fusion units
	// before the Dynamic Plasma spork (package dp lifts the cap afterwards).
	// All units above this will not increase the maximum plasma.
	MaxFusionUnitsPerAccount = 5000
	// MaxFusionPlasmaForAccount is the plasma capacity of an account
	// at the fusion-unit cap.
	MaxFusionPlasmaForAccount = MaxFusionUnitsPerAccount * PlasmaPerFusionUnit
	// MaxFussedAmountForAccount is the QSR amount, in smallest units,
	// that buys the fusion-unit cap; fusing more for one beneficiary
	// grants no extra plasma.
	MaxFussedAmountForAccount = CostPerFusionUnit * MaxFusionUnitsPerAccount
)

var (
	// MaxFussedAmountForAccountBig is MaxFussedAmountForAccount as a
	// big.Int, for balance comparisons.
	MaxFussedAmountForAccountBig = big.NewInt(MaxFussedAmountForAccount)
	// MaxFusionPlasmaForAccountBig is MaxFusionPlasmaForAccount as a
	// big.Int.
	MaxFusionPlasmaForAccountBig = big.NewInt(MaxFusionPlasmaForAccount)
)
