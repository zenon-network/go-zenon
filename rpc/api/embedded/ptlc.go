package embedded

import (
	"github.com/inconshreveable/log15"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon"
)

// PtlcApi implements the "embedded.ptlc" JSON-RPC namespace, which
// reads point time-locked contract entries from the ptlc embedded
// contract at the account-pool frontier. A PTLC locks an amount of any
// ZTS token behind a public point: whoever presents a valid witness
// for that point — a signature over the unlock message for Ed25519 and
// BIP-340 points, the 32-byte scalar for a secp256k1 point, which must
// then pay a destination fixed at creation — claims it before the
// expiration time, and the creator reclaims it afterwards. Every
// exported method is served as embedded.ptlc.<lowerCamelMethodName>.
type PtlcApi struct {
	chain chain.Chain
	z     zenon.Zenon
	cs    consensus.Consensus
	log   log15.Logger
}

// NewPtlcApi returns a PtlcApi bound to the given node's chain and
// consensus. It is called by the RPC server when the "embedded"
// namespace is enabled; it is not itself an RPC method.
func NewPtlcApi(z zenon.Zenon) *PtlcApi {
	return &PtlcApi{
		chain: z.Chain(),
		z:     z,
		cs:    z.Consensus(),
		log:   common.RPCLogger.New("module", "embedded_ptlc_api"),
	}
}

// GetById returns the PTLC entry with the given id (the hash of the
// Create send block) at the account-pool frontier, or an error if no such
// entry exists.
//
// JSON-RPC: embedded.ptlc.getById
func (a *PtlcApi) GetById(id types.Hash) (*definition.PtlcInfo, error) {

	_, context, err := api.GetFrontierContext(a.chain, types.PtlcContract)
	if err != nil {
		return nil, err
	}

	ptlcInfo, err := definition.GetPtlcInfo(context.Storage(), id)
	if err != nil {
		return nil, err
	}

	return ptlcInfo, nil
}

func (a *PtlcApi) GetReclaimLockById(id types.Hash) (*definition.PtlcReclaimLock, error) {

	_, context, err := api.GetFrontierContext(a.chain, types.PtlcContract)
	if err != nil {
		return nil, err
	}

	reclaimLock, err := definition.GetPtlcReclaimLock(context.Storage(), id)
	if err != nil {
		return nil, err
	}

	return reclaimLock, nil
}
