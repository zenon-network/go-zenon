package genesis

import "github.com/pkg/errors"

var (
	ErrInvalidGenesisPath    = errors.New("can't open genesis file")
	ErrInvalidGenesisJson    = errors.New("malformed genesis json structure")
	ErrIncompleteGenesisJson = errors.New("incomplete genesis json")
	ErrInvalidGenesisConfig  = errors.New("invalid genesis config. Failed to pass tests")

	ErrNoEmbeddedGenesis = errors.New("the codebase has no embedded genesis")
)
