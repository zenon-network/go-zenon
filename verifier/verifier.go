package verifier

import (
	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/consensus"
)

var (
	log = common.VerifierLogger
)

type Verifier interface {
	AccountBlockVerifier
	MomentumVerifier
}
type verifier struct {
	AccountBlockVerifier
	MomentumVerifier
}

type CanonicalBasePlasmaFunc func(c chain.Chain, block *nom.AccountBlock) (uint64, error)

func NewVerifier(chain chain.Chain, consensus consensus.Consensus, canonicalBasePlasma CanonicalBasePlasmaFunc) Verifier {
	return &verifier{
		AccountBlockVerifier: NewAccountBlockVerifier(chain, consensus),
		MomentumVerifier:     NewMomentumVerifier(chain, consensus, canonicalBasePlasma),
	}
}
