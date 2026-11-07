package genesis

import "github.com/pkg/errors"

// The Err* sentinels report why a genesis config could not be
// loaded; ReadGenesisConfigFromFile returns all of them except
// ErrNoEmbeddedGenesis, which only MakeEmbeddedGenesisConfig returns.
// The node treats any of them as fatal at startup. A panic during
// state construction is recovered and reported as
// ErrInvalidGenesisConfig (see ReadGenesisConfigFromFile).
var (
	// ErrInvalidGenesisPath signals that the genesis file could not
	// be opened.
	ErrInvalidGenesisPath = errors.New("can't open genesis file")
	// ErrInvalidGenesisJson signals that the genesis file is not
	// valid JSON for GenesisConfig.
	ErrInvalidGenesisJson = errors.New("malformed genesis json structure")
	// ErrIncompleteGenesisJson signals that the genesis file ended
	// mid-document (decoder reported EOF).
	ErrIncompleteGenesisJson = errors.New("incomplete genesis json")
	// ErrInvalidGenesisConfig signals that the decoded config failed
	// the CheckGenesis consistency checks.
	ErrInvalidGenesisConfig = errors.New("invalid genesis config. Failed to pass tests")

	// ErrNoEmbeddedGenesis signals that this build carries no
	// embedded genesis, so a genesis file must be provided.
	ErrNoEmbeddedGenesis = errors.New("the codebase has no embedded genesis")
)

// The ErrGenesis* sentinels name the CheckGenesis failures for
// per-entry values that the running contracts could never have
// produced. CheckGenesis wraps them with the offending entry, so
// match them with errors.Is; ReadGenesisConfigFromFile still reports
// every CheckGenesis failure as ErrInvalidGenesisConfig.
var (
	// ErrGenesisNilEntry signals a null element in one of the
	// config's lists (genesis blocks, pillars, legacy entries, swap
	// entries, tokens or fusions).
	ErrGenesisNilEntry = errors.New("genesis config has a nil entry")
	// ErrGenesisNilAmount signals a missing amount where one is
	// required: a genesis balance, a fusion, a swap entry's ZNN or
	// QSR, or a token's TotalSupply.
	ErrGenesisNilAmount = errors.New("genesis config has a nil amount")
	// ErrGenesisNegativeBalance signals a negative genesis balance.
	// Balances are stored unsigned, so a negative entry would pass
	// the signed supply sum and materialise as its absolute value.
	ErrGenesisNegativeBalance = errors.New("genesis balance is negative")
	// ErrGenesisNegativeFusion signals a negative fusion amount,
	// which the plasma contract would store as a huge unsigned fused
	// amount.
	ErrGenesisNegativeFusion = errors.New("genesis fusion amount is negative")
	// ErrGenesisNegativeSwapAmount signals a negative ZNN or QSR
	// amount in a swap entry.
	ErrGenesisNegativeSwapAmount = errors.New("genesis swap amount is negative")
	// ErrGenesisDuplicateAddress signals two GenesisBlocks entries for
	// one address. The supply check counts both, but only the last
	// one would be kept in state.
	ErrGenesisDuplicateAddress = errors.New("genesis blocks list an address more than once")
	// ErrGenesisZeroLegacySlots signals a legacy pillar entry with a
	// PillarCount of zero; legacy registration would decrement it to
	// 255.
	ErrGenesisZeroLegacySlots = errors.New("genesis legacy pillar entry has zero slots")
	// ErrGenesisPillarStakeAmount signals a genesis pillar whose
	// staked Amount is not exactly constants.PillarStakeAmount;
	// revocation always refunds PillarStakeAmount.
	ErrGenesisPillarStakeAmount = errors.New("genesis pillar stake is not the pillar stake amount")
)
