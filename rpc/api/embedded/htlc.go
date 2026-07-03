package embedded

import (
	"github.com/inconshreveable/log15"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/consensus"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/embedded/implementation"
	"github.com/zenon-network/go-zenon/zenon"
)

// HtlcApi implements the "embedded.htlc" JSON-RPC namespace, which
// reads hashed timelock contract entries from the htlc embedded
// contract as of the frontier momentum. An HTLC locks an amount of any
// ZTS token: the hash-locked counterparty claims it by revealing a
// preimage of the hash lock strictly before the expiration time, and
// the creator (the time-locked address) reclaims it once the expiration
// time has been reached. Every exported method is served as
// embedded.htlc.<lowerCamelMethodName>.
type HtlcApi struct {
	chain chain.Chain
	z     zenon.Zenon
	cs    consensus.Consensus
	log   log15.Logger
}

// NewHtlcApi returns an HtlcApi bound to the given node's chain. It is
// called by the RPC server when the "embedded" namespace is enabled; it
// is not itself an RPC method.
func NewHtlcApi(z zenon.Zenon) *HtlcApi {
	return &HtlcApi{
		chain: z.Chain(),
		z:     z,
		cs:    z.Consensus(),
		log:   common.RPCLogger.New("module", "embedded_htlc_api"),
	}
}

// GetById returns the active HTLC entry with the given id, which is the
// hash of the send block that created it. TimeLocked is the creator,
// HashLocked the counterparty who can unlock with the preimage, Amount
// the locked amount in smallest units of TokenStandard, ExpirationTime
// unix seconds, HashType the algorithm of the 32-byte HashLock digest
// (0 for SHA3-256, 1 for SHA-256) and KeyMaxSize the maximum accepted
// preimage length in bytes. Entries are deleted when unlocked or
// reclaimed, so spent or unknown ids produce an error rather than a
// nil result.
//
// JSON-RPC: embedded.htlc.getById
func (a *HtlcApi) GetById(id types.Hash) (*definition.HtlcInfo, error) {

	_, context, err := api.GetFrontierContext(a.chain, types.HtlcContract)
	if err != nil {
		return nil, err
	}

	htlcInfo, err := definition.GetHtlcInfo(context.Storage(), id)
	if err != nil {
		return nil, err
	}

	return htlcInfo, nil
}

// GetProxyUnlockStatus reports whether address allows proxy unlocking:
// when true, any address that knows the preimage may unlock an HTLC
// hash-locked to address (the funds always go to the hash-locked
// address); when false, only address itself may unlock. The flag is
// toggled per address through the contract's AllowProxyUnlock and
// DenyProxyUnlock methods and defaults to true for addresses that never
// called them.
//
// JSON-RPC: embedded.htlc.getProxyUnlockStatus
func (a *HtlcApi) GetProxyUnlockStatus(address types.Address) (bool, error) {
	_, context, err := api.GetFrontierContext(a.chain, types.HtlcContract)
	if err != nil {
		return false, err
	}
	return implementation.GetHtlcProxyUnlockStatus(context, address)
}
