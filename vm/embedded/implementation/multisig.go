package implementation

import (
	"bytes"
	"crypto/ed25519"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

var (
	multisigLog = common.EmbeddedLogger.New("contract", "multisig")
)

// toPublicKeys re-types the raw ABI-unpacked byte slices as ed25519 public keys, with no copy.
// Bounds/length validation happens in definition.ValidPolicy / CanonicalizeSigners.
func toPublicKeys(raw [][]byte) []ed25519.PublicKey {
	out := make([]ed25519.PublicKey, len(raw))
	for i, r := range raw {
		out[i] = ed25519.PublicKey(r)
	}
	return out
}

func signersContain(signers []ed25519.PublicKey, pk ed25519.PublicKey) bool {
	for _, s := range signers {
		if bytes.Equal(s, pk) {
			return true
		}
	}
	return false
}

// CreateMultisigMethod creates a new mutable multisig account. The account address is derived
// deterministically from the sender's public key and the call's nonce
// (types.MultisigCreationToAddress) — it is never attacker-chosen. Creation is not subject to
// the policy-change maturity delay: there is no prior authority to protect. The caller must be a
// single-sig account: address derivation and the "creator must be one of the initial signers"
// check both key off the sender's public key, which a multisig account never has.
type CreateMultisigMethod struct {
	MethodName string
}

func (p *CreateMultisigMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	// include burn transaction
	return 2 * plasmaTable.EmbeddedSimple, nil
}

func (p *CreateMultisigMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	if types.IsMultisigAddress(block.Address) {
		return constants.ErrMultisigCreatorMustBeSingleSig
	}

	param := new(definition.CreateMultisigParam)
	if err := definition.ABIMultisig.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.TokenStandard != types.ZnnTokenStandard ||
		block.Amount.Cmp(constants.MultisigCreationBurnAmount) != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	policy := definition.MultisigPolicy{Threshold: param.Threshold, Signers: toPublicKeys(param.Signers)}
	if err := definition.ValidPolicy(&policy); err != nil {
		return constants.ErrMultisigInvalidPolicy
	}

	var err error
	block.Data, err = definition.ABIMultisig.PackMethod(p.MethodName, param.Nonce, param.Threshold, param.Signers)
	return err
}

func (p *CreateMultisigMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		multisigLog.Debug("invalid create - syntactic validation failed", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	param := new(definition.CreateMultisigParam)
	err := definition.ABIMultisig.UnpackMethod(param, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	derived := types.MultisigCreationToAddress(sendBlock.PublicKey, param.Nonce)

	existing, err := definition.GetMultisigRecord(context.Storage(), derived)
	common.DealWithErr(err)
	if existing != nil {
		return nil, constants.ErrMultisigAlreadyExists
	}

	policy := definition.MultisigPolicy{
		Threshold: param.Threshold,
		Signers:   toPublicKeys(param.Signers),
		Locked:    false,
	}
	if err := definition.ValidPolicy(&policy); err != nil {
		return nil, constants.ErrMultisigInvalidPolicy
	}
	if !signersContain(policy.Signers, sendBlock.PublicKey) {
		// the creator must be one of the initial signers
		return nil, constants.ErrMultisigInvalidPolicy
	}

	rec := &definition.MultisigRecord{
		Active:  policy,
		Pending: nil,
	}
	common.DealWithErr(definition.SaveMultisigRecord(context.Storage(), derived, rec))

	multisigLog.Debug("created", "address", derived, "threshold", policy.Threshold, "signers", len(policy.Signers))
	return []*nom.AccountBlock{
		{
			Address:       types.MultisigContract,
			ToAddress:     types.TokenContract,
			BlockType:     nom.BlockTypeContractSend,
			Amount:        constants.MultisigCreationBurnAmount,
			TokenStandard: types.ZnnTokenStandard,
			Data:          definition.ABIToken.PackMethodPanic(definition.BurnMethodName),
		},
	}, nil
}

// ChangePolicyMethod stages a policy change (signer rotation / threshold change / lock) on an
// existing multisig account. The change is authorised twice in defence-in-depth: once on the
// send side (this is itself a multisig block, threshold-signed under the policy effective at
// the send's height — enforced by the account verifier, not here) and once on the receive side
// (this method), which re-derives the effective policy at the receive frontier via the SAME
// definition.Promote() the verifier uses, materialises a matured pending change BEFORE staging
// the new one (never silently discarding it — see ReceiveBlock), and rejects if the
// (possibly just-materialised) active policy is locked.
//
// The receive path also re-verifies the send's threshold signatures against the post-maturity
// active policy: if a rotation matured between the send's MomentumAcknowledged height and this
// receive, the superseded signer set no longer retains authority to mutate (or lock) the account.
type ChangePolicyMethod struct {
	MethodName string
}

func (p *ChangePolicyMethod) GetPlasma(plasmaTable *constants.PlasmaTable) (uint64, error) {
	return plasmaTable.EmbeddedSimple, nil
}

func (p *ChangePolicyMethod) ValidateSendBlock(block *nom.AccountBlock) error {
	param := new(definition.ChangePolicyParam)
	if err := definition.ABIMultisig.UnpackMethod(param, p.MethodName, block.Data); err != nil {
		return constants.ErrUnpackError
	}

	if block.Amount.Sign() != 0 {
		return constants.ErrInvalidTokenOrAmount
	}

	policy := definition.MultisigPolicy{Threshold: param.Threshold, Signers: toPublicKeys(param.Signers)}
	if err := definition.ValidPolicy(&policy); err != nil {
		return constants.ErrMultisigInvalidPolicy
	}

	var err error
	block.Data, err = definition.ABIMultisig.PackMethod(p.MethodName, param.Threshold, param.Signers, param.Lock)
	return err
}

func (p *ChangePolicyMethod) ReceiveBlock(context vm_context.AccountVmContext, sendBlock *nom.AccountBlock) ([]*nom.AccountBlock, error) {
	if err := p.ValidateSendBlock(sendBlock); err != nil {
		multisigLog.Debug("invalid change-policy - syntactic validation failed", "address", sendBlock.Address, "reason", err)
		return nil, err
	}

	addr := sendBlock.Address

	frontierMomentum, err := context.GetFrontierMomentum()
	common.DealWithErr(err)
	H := frontierMomentum.Height

	rec, err := definition.GetMultisigRecord(context.Storage(), addr)
	common.DealWithErr(err)
	if rec == nil {
		return nil, constants.ErrMultisigNoPolicy
	}

	// (a) compute the maturity-aware effective record at THIS receive frontier, using the SAME
	//     Promote() the verifier read path uses (single source of truth).
	eff, matured := definition.Promote(rec, H)

	// (b) adopt a matured pending as the new Active before authorising/staging — never silently
	//     discard it. Persisted by the single save in step (e); no intermediate write.
	if matured {
		rec = &eff
	}

	// (b2) re-authorise the send against the CURRENT active policy (post-promotion). The verifier
	//      already authorised this send live against the policy active at its actual momentum
	//      inclusion height; this re-auth protects the DISTINCT gap between that inclusion height
	//      and this later receive height, during which another rotation could mature. If it has,
	//      the superseded signer set must not retain authority to mutate (or lock) the account here.
	if sendBlock.MultisigAuth == nil ||
		!definition.VerifyThresholdSignatures(&rec.Active, sendBlock.Hash.Bytes(), sendBlock.MultisigAuth.Signatures) {
		return nil, constants.ErrMultisigStaleAuthority
	}

	// (c) reject if the (possibly just-promoted) Active is locked. Lock is monotonic.
	if rec.Active.Locked {
		return nil, constants.ErrMultisigLocked
	}

	param := new(definition.ChangePolicyParam)
	err = definition.ABIMultisig.UnpackMethod(param, p.MethodName, sendBlock.Data)
	common.DealWithErr(err)

	// (d) validate the proposed next policy (bounds + canonical form). Lock is monotonic: once
	//     rec.Active.Locked is true, (c) above already rejected before we get here, so a locked
	//     policy can never be "unlocked" by a later ChangePolicy.
	next := definition.MultisigPolicy{
		Threshold: param.Threshold,
		Signers:   toPublicKeys(param.Signers),
		Locked:    param.Lock,
	}
	if err := definition.ValidPolicy(&next); err != nil {
		return nil, constants.ErrMultisigInvalidPolicy
	}

	// (e) stage the new pending on top of the (possibly just-promoted) Active. A second change
	//     while one is pending replaces the still-unmatured pending and resets the clock; a
	//     change after maturity stages on the freshly-promoted Active (no revert).
	rec.Pending = &next
	rec.PendingHeight = H
	common.DealWithErr(definition.SaveMultisigRecord(context.Storage(), addr, rec))

	multisigLog.Debug("policy change staged", "address", addr, "pendingHeight", H, "threshold", next.Threshold, "signers", len(next.Signers), "lock", next.Locked)
	return nil, nil
}
