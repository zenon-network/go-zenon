package embedded

import (
	"github.com/inconshreveable/log15"
	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon"
)

// MultisigApi exposes read-only inspection of mutable multisig account policies. It is
// off-consensus tooling: it never writes ledger state, only reads the registry contract's state
// via the single canonical codec (vm/embedded/definition).
type MultisigApi struct {
	chain chain.Chain
	log   log15.Logger
}

func NewMultisigApi(z zenon.Zenon) *MultisigApi {
	return &MultisigApi{
		chain: z.Chain(),
		log:   common.RPCLogger.New("module", "embedded_multisig_api"),
	}
}

// MultisigPolicyInfo is the JSON-friendly shape of definition.MultisigPolicy. Signers marshal as
// base64, the same encoding/json default already used for AccountBlock.PublicKey.
type MultisigPolicyInfo struct {
	Threshold uint8    `json:"threshold"`
	Signers   [][]byte `json:"signers"`
	Locked    bool     `json:"locked"`
}

func toMultisigPolicyInfo(p *definition.MultisigPolicy) *MultisigPolicyInfo {
	if p == nil {
		return nil
	}
	signers := make([][]byte, len(p.Signers))
	for i, s := range p.Signers {
		signers[i] = s
	}
	return &MultisigPolicyInfo{
		Threshold: p.Threshold,
		Signers:   signers,
		Locked:    p.Locked,
	}
}

// MultisigRecordInfo is the JSON-friendly, maturity-aware view of a multisig account's registry
// record at a given height: Active is the effective policy at that height (a matured Pending is
// already promoted into it, via Promote()); Pending/PendingHeight describe a still-unmatured
// staged change, nil/0 if none.
type MultisigRecordInfo struct {
	Active        *MultisigPolicyInfo `json:"active"`
	Pending       *MultisigPolicyInfo `json:"pending"`
	PendingHeight uint64              `json:"pendingHeight"`
}

// GetPolicy reads the multisig registry record for address at height (or the current frontier
// momentum height if height is nil), returning the maturity-aware effective view via Promote(),
// the SAME single source of truth the verifier and ChangePolicy.ReceiveBlock use. Returns nil if
// no record exists for address at that height.
func (a *MultisigApi) GetPolicy(address types.Address, height *uint64) (*MultisigRecordInfo, error) {
	frontierStore := a.chain.GetFrontierMomentumStore()

	H := uint64(0)
	if height != nil {
		H = *height
	} else {
		frontier, err := frontierStore.GetFrontierMomentum()
		if err != nil {
			return nil, err
		}
		H = frontier.Height
	}

	momentum, err := frontierStore.GetMomentumByHeight(H)
	if err != nil {
		return nil, err
	}
	if momentum == nil {
		return nil, errors.Errorf("momentum at height %v not found", H)
	}

	momentumStore := a.chain.GetMomentumStore(momentum.Identifier())
	if momentumStore == nil {
		return nil, errors.Errorf("momentum store at height %v not found", H)
	}

	rec, err := definition.GetMultisigRecord(momentumStore.GetAccountStore(types.MultisigContract).Storage(), address)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, nil
	}

	eff, _ := definition.Promote(rec, H)
	info := &MultisigRecordInfo{
		Active: toMultisigPolicyInfo(&eff.Active),
	}
	if eff.Pending != nil {
		info.Pending = toMultisigPolicyInfo(eff.Pending)
		info.PendingHeight = eff.PendingHeight
	}
	return info, nil
}
