// Package constants gathers the protocol parameters of Alphanet in
// one place: the consensus cadence (ConsensusConfig), the plasma cost
// model (plasma.go), the economic parameters of every embedded
// contract (embedded.go) and the sentinel error values the VM and the
// embedded contracts reject blocks with (errors.go).
//
// Two unit conventions run through the package. Token amounts are
// expressed in smallest units — both ZNN and QSR carry 8 decimals, so
// 1 coin equals Decimals smallest units. Time is measured either in
// seconds (durations built from SecsInDay) or in momentums and epochs
// (counts built from MomentumsPerHour and MomentumsPerEpoch; one
// momentum every 10 seconds, one epoch per day).
package constants

import (
	"github.com/zenon-network/go-zenon/common/types"
	"math/big"

	"github.com/zenon-network/go-zenon/common"
)

const (
	// Decimals is the scale factor between one whole coin and its
	// smallest units. ZNN and QSR both carry 8 decimal places, so
	// amounts throughout the codebase are multiples of 1e-8 coins.
	Decimals = 100000000
	// SecsInDay is the number of seconds in a day, the base unit for
	// the lock, revoke and voting durations below.
	SecsInDay = 24 * 60 * 60
)

var (
	/// === Common ===

	// MomentumsPerHour is the number of momentums produced per hour,
	// derived from the 10-second momentum cadence. Should be used
	// instead of plain '3600' or similar.
	MomentumsPerHour int64 = 3600 / 10
	// MomentumsPerEpoch is the number of momentums in one epoch (one
	// day), the period rewards are accounted in.
	MomentumsPerEpoch = MomentumsPerHour * 24
	// RewardTimeLimit is how long (in seconds) after an epoch ends
	// before that epoch's rewards may be computed, giving the network
	// an hour of settling time.
	RewardTimeLimit int64 = 3600

	// UpdateMinNumMomentums is the number momentums between 2
	// UpdateEmbedded* calls which will execute, used for all
	// applicable contracts; calls arriving sooner fail with
	// ErrUpdateTooRecent.
	UpdateMinNumMomentums = uint64(MomentumsPerHour * 5 / 6)
	// MaxEpochsPerUpdate caps how many epochs' rewards a single
	// update call processes; older epochs wait for the next call.
	MaxEpochsPerUpdate = 20

	// === Accelerator ===

	// ProjectNameLengthMax is the maximum length of an Accelerator-Z
	// project name, in bytes.
	ProjectNameLengthMax = 30
	// ProjectDescriptionLengthMax is the maximum length of a project
	// description, in bytes.
	ProjectDescriptionLengthMax = 240
	// ProjectZnnMaximumFunds caps the ZNN a single project may
	// request (5,000 ZNN, in smallest units).
	ProjectZnnMaximumFunds = big.NewInt(5000 * Decimals)
	// ProjectQsrMaximumFunds caps the QSR a single project may
	// request (50,000 QSR, in smallest units).
	ProjectQsrMaximumFunds = big.NewInt(50000 * Decimals)
	// ProjectCreationAmount is the non-refundable fee, in ZNN
	// smallest units, that must accompany a CreateProject call.
	ProjectCreationAmount = big.NewInt(1 * Decimals)
	// PhaseTimeUnit is the accelerator's time unit, in seconds (one
	// day).
	PhaseTimeUnit int64 = 24 * 60 * 60
	// AcceleratorDuration is how long, in seconds after genesis, the
	// accelerator accepts new projects (20 years of 360 days); past
	// it, calls fail with ErrAcceleratorEnded.
	AcceleratorDuration = 20 * 12 * 30 * PhaseTimeUnit
	// VoteAcceptanceThreshold is the minimum voter turnout, in
	// percent of registered pillars, for a project or phase vote to
	// pass; on top of it, yes votes must outnumber no votes.
	VoteAcceptanceThreshold uint32 = 33
	// AcceleratorProjectVotingPeriod is how long, in seconds after
	// its creation, a project must wait while pillars vote before it
	// can be marked active.
	AcceleratorProjectVotingPeriod = 14 * PhaseTimeUnit
	// MaxBlocksPerUpdate caps the number of phase payouts a single
	// accelerator update call issues.
	MaxBlocksPerUpdate = 40

	/// ==== Pillar constants ===

	// PillarStakeAmount is the ZNN deposit (15,000 ZNN, in smallest
	// units) locked while a pillar is registered and returned when it
	// is revoked.
	PillarStakeAmount = big.NewInt(15e3 * Decimals)
	// PillarQsrStakeBaseAmount is the QSR cost (150,000 QSR, in
	// smallest units) used for legacy pillars and for the first
	// pillar in the network.
	PillarQsrStakeBaseAmount = big.NewInt(150e3 * Decimals)
	// PillarQsrStakeIncreaseAmount is the increase of the QSR cost
	// (10,000 QSR, in smallest units) for each active non-legacy
	// pillar already registered.
	PillarQsrStakeIncreaseAmount = big.NewInt(10e3 * Decimals)
	// PillarEpochLockTime is the lock cycle, in seconds (83 days): a
	// pillar can only be revoked once each lock window elapses.
	PillarEpochLockTime int64 = 83 * SecsInDay
	// PillarEpochRevokeTime is the window, in seconds (7 days), after
	// each lock cycle during which the pillar may be revoked.
	PillarEpochRevokeTime int64 = 7 * SecsInDay
	// PillarNameLengthMax is the maximum length of a pillar name, in
	// bytes.
	PillarNameLengthMax = 40

	/// === Sentinel constants ===

	// SentinelZnnRegisterAmount is the ZNN amount (5,000 ZNN, in
	// smallest units) required to register a sentinel.
	SentinelZnnRegisterAmount = big.NewInt(5e3 * Decimals)
	// SentinelQsrDepositAmount is the QSR amount (50,000 QSR, in
	// smallest units) that must be deposited before registering a
	// sentinel.
	SentinelQsrDepositAmount = big.NewInt(50e3 * Decimals)
	// SentinelLockTimeWindow is the lock cycle, in seconds (27 days):
	// a sentinel can only be revoked once each lock window elapses.
	SentinelLockTimeWindow int64 = 27 * SecsInDay
	// SentinelRevokeTimeWindow is the window, in seconds (3 days),
	// after each lock cycle during which the sentinel may be revoked.
	SentinelRevokeTimeWindow int64 = 3 * SecsInDay

	/// === Staking constants ===

	// StakeTimeUnitSec is the staking time unit, in seconds (30
	// days). Stake durations are whole multiples of it, and each
	// additional unit raises the stake's reward weight by 0.1x on top
	// of the 1x base — amount times (9 + units) / 10.
	StakeTimeUnitSec int64 = 30 * SecsInDay
	// StakeTimeMinSec is the shortest stake duration: 1 time unit.
	StakeTimeMinSec = StakeTimeUnitSec * 1
	// StakeTimeMaxSec is the longest stake duration: 12 time units
	// (360 days), where the reward weight peaks at 2.1x.
	StakeTimeMaxSec = StakeTimeUnitSec * 12
	// StakeMinAmount is the smallest amount of ZNN (in smallest
	// units) that can be staked in one entry.
	StakeMinAmount = big.NewInt(1 * Decimals)

	// === Plasma constants ===

	// FuseMinAmount is the smallest amount of QSR (in smallest units)
	// that can be fused in one entry.
	FuseMinAmount = big.NewInt(10 * Decimals)
	// FuseExpiration is the number of momentums (10 hours' worth) a
	// fusion entry must age before the QSR can be unfused.
	FuseExpiration = uint64(MomentumsPerHour * 10)

	/// === Token constants ===

	// TokenIssueAmount is the non-refundable fee, in ZNN smallest
	// units, that must accompany a token IssueToken call.
	TokenIssueAmount = big.NewInt(1 * Decimals)
	// TokenNameLengthMax is the maximum length of a token name, in
	// bytes.
	TokenNameLengthMax = 40
	// TokenSymbolLengthMax is the maximum length of a token symbol,
	// in bytes.
	TokenSymbolLengthMax = 10
	// TokenDomainLengthMax is the maximum length of a token domain,
	// in bytes.
	TokenDomainLengthMax = 128
	// TokenMaxSupplyBig is the largest total or max supply a token
	// may declare: 2^255-1, in the token's own smallest units.
	TokenMaxSupplyBig = common.BigP255m1
	// TokenMaxDecimals is the largest number of decimal places a
	// token may declare.
	TokenMaxDecimals = 18

	/// === Spork constants ===

	// SporkMinHeightDelay is the number of momentums between a
	// spork's activation and its enforcement: an activated spork
	// takes effect at the frontier height plus this delay.
	SporkMinHeightDelay = uint64(6)
	// SporkNameMinLength is the minimum length of a spork name, in
	// bytes.
	SporkNameMinLength = 5
	// SporkNameMaxLength is the maximum length of a spork name, in
	// bytes.
	SporkNameMaxLength = 40
	// SporkDescriptionMaxLength is the maximum length of a spork
	// description, in bytes.
	SporkDescriptionMaxLength = 400

	/// === Swap constants ===

	// SwapAssetDecayEpochsOffset is the number of epochs after
	// genesis (90) during which unclaimed swap balances keep their
	// full value before the decay kicks in.
	SwapAssetDecayEpochsOffset = 30 * 3
	// SwapAssetDecayTickEpochs is the number of epochs in each decay
	// tick (30) once the offset has passed.
	SwapAssetDecayTickEpochs = 30
	// SwapAssetDecayTickValuePercentage is the percentage of the
	// original unclaimed balance lost in each decay tick — 10% per
	// SwapAssetDecayTickEpochs after SwapAssetDecayEpochsOffset, so
	// nothing is left after 390 epochs.
	SwapAssetDecayTickValuePercentage = 10

	/// === Bridge constants ===

	// InitialBridgeAdministrator is the address installed as
	// administrator of the bridge and liquidity contracts when their
	// state is first initialized.
	InitialBridgeAdministrator = types.ParseAddressPanic("z1qr9vtwsfr2n0nsxl2nfh6l5esqjh2wfj85cfq9")
	// MaximumFee is the denominator of the bridge's fee arithmetic
	// (10,000 = 100%, i.e. fees are basis points) and therefore also
	// the highest fee percentage a network may set.
	MaximumFee = uint32(10000)
	// MinUnhaltDurationInMomentums is the fewest momentums (6 hours'
	// worth) a halted bridge must stay halted after an unhalt request
	// before it resumes processing.
	MinUnhaltDurationInMomentums = uint64(6 * MomentumsPerHour)
	// MinAdministratorDelay is the smallest time-challenge delay, in
	// momentums (2 epochs' worth), for administrator-level actions
	// such as changing the administrator or the guardians.
	MinAdministratorDelay = uint64(2 * MomentumsPerEpoch)
	// MinSoftDelay is the smallest time-challenge delay, in momentums
	// (1 epoch's worth), for soft actions such as changing the TSS
	// public key.
	MinSoftDelay = uint64(MomentumsPerEpoch)
	// MinGuardians is the minimum number of guardians that must be
	// nominated for the bridge and liquidity contracts.
	MinGuardians = 5

	// DecompressedECDSAPubKeyLength is the byte length of an
	// uncompressed secp256k1 public key (0x04 prefix plus X and Y).
	DecompressedECDSAPubKeyLength = 65
	// CompressedECDSAPubKeyLength is the byte length of a compressed
	// secp256k1 public key (parity prefix plus X).
	CompressedECDSAPubKeyLength = 33
	// ECDSASignatureLength is the byte length of a recoverable
	// secp256k1 signature (r, s and the recovery id).
	ECDSASignatureLength = 65

	/// === Reward constants ===

	// RewardTickDurationInEpochs is the duration, in epochs (30, one
	// month's worth), of each step of the emission schedules below.
	RewardTickDurationInEpochs uint64 = 30

	// NetworkZnnRewardConfig is the ZNN emission schedule: entry i is
	// the total ZNN minted per epoch (in smallest units) during
	// reward tick i. The final entry remains in force forever once
	// the schedule is exhausted; see NetworkZnnRewardPerEpoch.
	NetworkZnnRewardConfig = []int64{
		10 * MomentumsPerEpoch / 6 * Decimals,
		6 * MomentumsPerEpoch / 6 * Decimals,
		5 * MomentumsPerEpoch / 6 * Decimals,
		7 * MomentumsPerEpoch / 6 * Decimals,
		5 * MomentumsPerEpoch / 6 * Decimals,
		4 * MomentumsPerEpoch / 6 * Decimals,
		7 * MomentumsPerEpoch / 6 * Decimals,
		4 * MomentumsPerEpoch / 6 * Decimals,
		3 * MomentumsPerEpoch / 6 * Decimals,
		7 * MomentumsPerEpoch / 6 * Decimals,
		3 * MomentumsPerEpoch / 6 * Decimals,
	}

	// NetworkQsrRewardConfig is the QSR emission schedule: entry i is
	// the total QSR minted per epoch (in smallest units) during
	// reward tick i. The final entry remains in force forever once
	// the schedule is exhausted; see NetworkQsrRewardPerEpoch.
	NetworkQsrRewardConfig = []int64{
		20000 * Decimals,
		20000 * Decimals,
		20000 * Decimals,
		20000 * Decimals,
		15000 * Decimals,
		15000 * Decimals,
		15000 * Decimals,
		5000 * Decimals,
	}

	// DelegationZnnRewardPercentage is the share, in percent, of each
	// epoch's ZNN emission paid to pillars for their delegated
	// weight. The four ZNN percentages sum to 100.
	DelegationZnnRewardPercentage int64 = 24
	// MomentumProducingZnnRewardPercentage is the share, in percent,
	// of each epoch's ZNN emission paid to pillars for the momentums
	// they produced.
	MomentumProducingZnnRewardPercentage int64 = 50
	// SentinelZnnRewardPercentage is the share, in percent, of each
	// epoch's ZNN emission split among registered sentinels pro-rata
	// to their active time in the epoch.
	SentinelZnnRewardPercentage int64 = 13
	// LiquidityZnnRewardPercentage is the share, in percent, of each
	// epoch's ZNN emission directed to the liquidity contract.
	LiquidityZnnRewardPercentage int64 = 13
	// LiquidityZnnTotalPercentages is the denominator (10,000 =
	// 100%, i.e. basis points) that the per-token ZNN percentages
	// configured in the liquidity contract must sum to.
	LiquidityZnnTotalPercentages uint32 = 10000

	// StakingQsrRewardPercentage is the share, in percent, of each
	// epoch's QSR emission paid to ZNN stakers by weighted amount.
	// The three QSR percentages sum to 100.
	StakingQsrRewardPercentage int64 = 50
	// SentinelQsrRewardPercentage is the share, in percent, of each
	// epoch's QSR emission split among registered sentinels pro-rata
	// to their active time in the epoch.
	SentinelQsrRewardPercentage int64 = 25
	// LiquidityQsrRewardPercentage is the share, in percent, of each
	// epoch's QSR emission directed to the liquidity contract.
	LiquidityQsrRewardPercentage int64 = 25
	// LiquidityQsrTotalPercentages is the denominator (10,000 =
	// 100%, i.e. basis points) that the per-token QSR percentages
	// configured in the liquidity contract must sum to.
	LiquidityQsrTotalPercentages uint32 = 10000
	// LiquidityStakeWeights maps a liquidity stake's duration, in
	// stake time units, to its reward weight multiplier: a stake of n
	// units counts n times its amount.
	LiquidityStakeWeights = []int64{
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12,
	}
)

// NetworkZnnRewardPerEpoch returns the total ZNN minted in the given
// epoch, in smallest units: the NetworkZnnRewardConfig entry for the
// epoch's reward tick, or the final entry once the schedule is
// exhausted.
func NetworkZnnRewardPerEpoch(epoch uint64) int64 {
	tick := int(epoch / RewardTickDurationInEpochs)
	if tick >= len(NetworkZnnRewardConfig) {
		return NetworkZnnRewardConfig[len(NetworkZnnRewardConfig)-1]
	} else {
		return NetworkZnnRewardConfig[tick]
	}
}

// NetworkQsrRewardPerEpoch returns the total QSR minted in the given
// epoch, in smallest units: the NetworkQsrRewardConfig entry for the
// epoch's reward tick, or the final entry once the schedule is
// exhausted.
func NetworkQsrRewardPerEpoch(epoch uint64) int64 {
	tick := int(epoch / RewardTickDurationInEpochs)
	if tick >= len(NetworkQsrRewardConfig) {
		return NetworkQsrRewardConfig[len(NetworkQsrRewardConfig)-1]
	} else {
		return NetworkQsrRewardConfig[tick]
	}
}

// PillarRewardPerMomentum returns the delegation and producing ZNN
// rewards available per momentum in the given epoch, in smallest
// units: the epoch emission's respective percentage shares divided by
// the momentums in an epoch.
func PillarRewardPerMomentum(epoch uint64) (*big.Int, *big.Int) {
	delegation := (NetworkZnnRewardPerEpoch(epoch) * DelegationZnnRewardPercentage) / 100 / MomentumsPerEpoch
	producing := (NetworkZnnRewardPerEpoch(epoch) * MomentumProducingZnnRewardPercentage) / 100 / MomentumsPerEpoch
	return big.NewInt(delegation), big.NewInt(producing)
}

// SentinelRewardForEpoch returns the total ZNN and QSR rewards (in
// smallest units) set aside for sentinels in the given epoch: the
// sentinel percentage shares of that epoch's emissions.
func SentinelRewardForEpoch(epoch uint64) (*big.Int, *big.Int) {
	znn := (NetworkZnnRewardPerEpoch(epoch) * SentinelZnnRewardPercentage) / 100
	qsr := (NetworkQsrRewardPerEpoch(epoch) * SentinelQsrRewardPercentage) / 100
	return big.NewInt(znn), big.NewInt(qsr)
}

// LiquidityRewardForEpoch returns the total ZNN and QSR rewards (in
// smallest units) directed to the liquidity contract in the given
// epoch: the liquidity percentage shares of that epoch's emissions.
func LiquidityRewardForEpoch(epoch uint64) (*big.Int, *big.Int) {
	znn := (NetworkZnnRewardPerEpoch(epoch) * LiquidityZnnRewardPercentage) / 100
	qsr := (NetworkQsrRewardPerEpoch(epoch) * LiquidityQsrRewardPercentage) / 100
	return big.NewInt(znn), big.NewInt(qsr)
}

// StakeQsrRewardPerEpoch returns the total QSR reward (in smallest
// units) shared by ZNN stakers in the given epoch: the staking
// percentage share of that epoch's QSR emission.
func StakeQsrRewardPerEpoch(epoch uint64) *big.Int {
	qsr := (NetworkQsrRewardPerEpoch(epoch) * StakingQsrRewardPercentage) / 100
	return big.NewInt(qsr)
}
