// Package verifier validates account-blocks and momentums against the
// consensus rules before they are inserted into the chain. It enforces
// version and chain-identifier checks, block-type and amount rules,
// plasma/pow, previous-block and momentum-acknowledged continuity, hash
// and Ed25519 signature correctness, and (for momentums) that the
// producer is the pillar elected for the slot.
package verifier

import (
	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/consensus"
)

var (
	log = common.VerifierLogger
)

// Verifier validates both account-blocks and momentums. It combines the
// AccountBlockVerifier and MomentumVerifier behaviors into a single type
// shared across the chain and consensus subsystems.
type Verifier interface {
	AccountBlockVerifier
	MomentumVerifier
}
type verifier struct {
	AccountBlockVerifier
	MomentumVerifier
}

// NewVerifier returns a Verifier backed by the given chain and consensus.
// The chain supplies the account and momentum stores used to resolve
// previous blocks and frontiers; the consensus is used to confirm that a
// momentum was produced by the pillar elected for its slot.
func NewVerifier(chain chain.Chain, consensus consensus.Consensus) Verifier {
	return &verifier{
		AccountBlockVerifier: NewAccountBlockVerifier(chain, consensus),
		MomentumVerifier:     NewMomentumVerifier(chain, consensus),
	}
}
