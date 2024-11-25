package constants

import (
	"github.com/zenon-network/go-zenon/common/types"
	"math/big"

	"github.com/zenon-network/go-zenon/common"
)

const (
	Decimals  = 100000000
	SecsInDay = 24 * 60 * 60
)

var (
	/// === Common ===

	MomentumsPerHour  int64 = 3600 / 10 // Number of momentums per hour. Should be used instead of plain '3600' or similar.
	MomentumsPerEpoch       = MomentumsPerHour * 24
	RewardTimeLimit   int64 = 3600

	// UpdateMinNumMomentums is the number momentums between 2 UpdateEmbedded* calls which will execute, used for all applicable contracts
	UpdateMinNumMomentums = uint64(MomentumsPerHour * 5 / 6)
	MaxEpochsPerUpdate    = 20

	// === Accelerator ===

	ProjectNameLengthMax                  = 30
	ProjectDescriptionLengthMax           = 240
	ProjectZnnMaximumFunds                = big.NewInt(5000 * Decimals)
	ProjectQsrMaximumFunds                = big.NewInt(50000 * Decimals)
	ProjectCreationAmount                 = big.NewInt(1 * Decimals)
	PhaseTimeUnit                  int64  = 24 * 60 * 60
	AcceleratorDuration                   = 20 * 12 * 30 * PhaseTimeUnit
	VoteAcceptanceThreshold        uint32 = 33
	AcceleratorProjectVotingPeriod        = 14 * PhaseTimeUnit
	MaxBlocksPerUpdate                    = 40

	// === Governance ===

	Type1Action                           = uint8(1)
	Type2Action                           = uint8(2)
	Type1ActionAcceptanceThreshold uint32 = 66
	Type2ActionAcceptanceThreshold uint32 = 50
	Type1ActionVotingPeriod               = 45 * PhaseTimeUnit
	Type2ActionVotingPeriod               = 30 * PhaseTimeUnit

	/// ==== Pillar constants ===

	PillarStakeAmount = big.NewInt(15e3 * Decimals)
	// PillarQsrStakeBaseAmount is the amount of QSR used for legacy pillars and for the first pillar in the network
	PillarQsrStakeBaseAmount           = big.NewInt(150e3 * Decimals)
	PillarQsrStakeIncreaseAmount       = big.NewInt(10e3 * Decimals) // Increase of cost for each pillar after PillarsQsrStakeNumWithInitial
	PillarEpochLockTime          int64 = 83 * SecsInDay
	PillarEpochRevokeTime        int64 = 7 * SecsInDay
	PillarNameLengthMax                = 40

	/// === Sentinel constants ===

	SentinelZnnRegisterAmount       = big.NewInt(5e3 * Decimals)  // sentinel Znn amount required for registration
	SentinelQsrDepositAmount        = big.NewInt(50e3 * Decimals) // sentinel Qsr amount required for registration
	SentinelLockTimeWindow    int64 = 27 * SecsInDay
	SentinelRevokeTimeWindow  int64 = 3 * SecsInDay

	/// === Staking constants ===

	// Testnet value
	StakeTimeUnitSec int64 = 30 * SecsInDay
	StakeTimeMinSec        = StakeTimeUnitSec * 1
	StakeTimeMaxSec        = StakeTimeUnitSec * 12
	StakeMinAmount         = big.NewInt(1 * Decimals)

	// === Plasma constants ===

	FuseMinAmount  = big.NewInt(10 * Decimals)
	FuseExpiration = uint64(MomentumsPerHour * 10) // for testnet, 10 hours

	/// === Token constants ===

	TokenIssueAmount     = big.NewInt(1 * Decimals)
	TokenNameLengthMax   = 40  // Maximum length of a token name
	TokenSymbolLengthMax = 10  // Maximum length of a token symbol
	TokenDomainLengthMax = 128 // Maximum length of a token domain
	TokenMaxSupplyBig    = common.BigP255m1
	TokenMaxDecimals     = 18

	/// === Spork constants ===

	SporkMinHeightDelay       = uint64(6)
	SporkNameMinLength        = 5
	SporkNameMaxLength        = 40
	SporkDescriptionMaxLength = 400

	/// === Swap constants ===

	// SwapAssetDecayEpochsOffset is the number of epochs before the decay kicks in
	SwapAssetDecayEpochsOffset = 30 * 3
	// SwapAssetDecayTickEpochs is the number of epochs for each decay tick
	SwapAssetDecayTickEpochs = 30
	// SwapAssetDecayTickValuePercentage is the percentage that is lost after in each tick, equal to 10% per SwapAssetDecayTickEpochs, after SwapAssetDecayEpochsOffset
	SwapAssetDecayTickValuePercentage = 10

	/// === Bridge constants ===

	InitialBridgeAdministrator   = types.ParseAddressPanic("z1qr9vtwsfr2n0nsxl2nfh6l5esqjh2wfj85cfq9")
	MaximumFee                   = uint32(10000)
	MinUnhaltDurationInMomentums = uint64(6 * MomentumsPerHour)  //main net
	MinAdministratorDelay        = uint64(2 * MomentumsPerEpoch) // main net
	MinSoftDelay                 = uint64(MomentumsPerEpoch)     // main net
	MinGuardians                 = 5                             // main net

	DecompressedECDSAPubKeyLength = 65
	CompressedECDSAPubKeyLength   = 33
	ECDSASignatureLength          = 65

	/// === Reward constants ===

	// RewardTickDurationInEpochs represents the duration (in epochs) for each reward tick
	RewardTickDurationInEpochs uint64 = 30

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

	DelegationZnnRewardPercentage        int64  = 24
	MomentumProducingZnnRewardPercentage int64  = 50
	SentinelZnnRewardPercentage          int64  = 13
	LiquidityZnnRewardPercentage         int64  = 13
	LiquidityZnnTotalPercentages         uint32 = 10000

	StakingQsrRewardPercentage   int64  = 50
	SentinelQsrRewardPercentage  int64  = 25
	LiquidityQsrRewardPercentage int64  = 25
	LiquidityQsrTotalPercentages uint32 = 10000
	LiquidityStakeWeights               = []int64{
		0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12,
	}
)

func NetworkZnnRewardPerEpoch(epoch uint64) int64 {
	tick := int(epoch / RewardTickDurationInEpochs)
	if tick >= len(NetworkZnnRewardConfig) {
		return NetworkZnnRewardConfig[len(NetworkZnnRewardConfig)-1]
	} else {
		return NetworkZnnRewardConfig[tick]
	}
}

func NetworkQsrRewardPerEpoch(epoch uint64) int64 {
	tick := int(epoch / RewardTickDurationInEpochs)
	if tick >= len(NetworkQsrRewardConfig) {
		return NetworkQsrRewardConfig[len(NetworkQsrRewardConfig)-1]
	} else {
		return NetworkQsrRewardConfig[tick]
	}
}

// PillarRewardPerMomentum returns delegation & producing reward per momentum.
func PillarRewardPerMomentum(epoch uint64) (*big.Int, *big.Int) {
	delegation := (NetworkZnnRewardPerEpoch(epoch) * DelegationZnnRewardPercentage) / 100 / MomentumsPerEpoch
	producing := (NetworkZnnRewardPerEpoch(epoch) * MomentumProducingZnnRewardPercentage) / 100 / MomentumsPerEpoch
	return big.NewInt(delegation), big.NewInt(producing)
}

// SentinelRewardForEpoch returns sentinel Znn and Qsr reward for a specific epoch.
func SentinelRewardForEpoch(epoch uint64) (*big.Int, *big.Int) {
	znn := (NetworkZnnRewardPerEpoch(epoch) * SentinelZnnRewardPercentage) / 100
	qsr := (NetworkQsrRewardPerEpoch(epoch) * SentinelQsrRewardPercentage) / 100
	return big.NewInt(znn), big.NewInt(qsr)
}

// LiquidityRewardForEpoch returns liquidity Znn and Qsr reward for a specific epoch.
func LiquidityRewardForEpoch(epoch uint64) (*big.Int, *big.Int) {
	znn := (NetworkZnnRewardPerEpoch(epoch) * LiquidityZnnRewardPercentage) / 100
	qsr := (NetworkQsrRewardPerEpoch(epoch) * LiquidityQsrRewardPercentage) / 100
	return big.NewInt(znn), big.NewInt(qsr)
}

// StakeQsrRewardPerEpoch returns staking Qsr reward for a specific epoch
func StakeQsrRewardPerEpoch(epoch uint64) *big.Int {
	qsr := (NetworkQsrRewardPerEpoch(epoch) * StakingQsrRewardPercentage) / 100
	return big.NewInt(qsr)
}
