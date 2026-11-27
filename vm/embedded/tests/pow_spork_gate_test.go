package tests

import (
	"encoding/binary"
	"errors"
	"math"
	"math/big"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/pow"
	"github.com/zenon-network/go-zenon/verifier"
	"github.com/zenon-network/go-zenon/vm"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/wallet"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

func buildSignedPoWSend(
	t *testing.T,
	z mock.MockZenon,
	kp *wallet.KeyPair,
	ma types.HashHeight,
	difficulty uint64,
	fusedPlasma uint64,
	noncePredicate func(*nom.AccountBlock) bool,
) *nom.AccountBlock {
	t.Helper()

	frontier, err := z.Chain().GetFrontierAccountStore(kp.Address).Frontier()
	common.FailIfErr(t, err)

	var prevHash types.Hash
	height := uint64(1)
	if frontier != nil {
		prevHash = frontier.Hash
		height = frontier.Height + 1
	}

	block := &nom.AccountBlock{
		Version:              1,
		ChainIdentifier:      z.Chain().ChainIdentifier(),
		BlockType:            nom.BlockTypeUserSend,
		Address:              kp.Address,
		ToAddress:            g.User1.Address,
		Amount:               big.NewInt(0),
		TokenStandard:        types.ZeroTokenStandard,
		Height:               height,
		PreviousHash:         prevHash,
		MomentumAcknowledged: ma,
		Difficulty:           difficulty,
		FusedPlasma:          fusedPlasma,
	}

	if noncePredicate != nil {
		found := false
		for i := uint64(0); i < 200000; i++ {
			binary.LittleEndian.PutUint64(block.Nonce.Data[:], i)
			if noncePredicate(block) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("failed to find matching nonce for difficulty %d", difficulty)
		}
	}

	block.Hash = block.ComputeHash()
	sig, _, pub, err := kp.Signer(block.Hash.Bytes())
	common.FailIfErr(t, err)
	block.Signature = sig
	block.PublicKey = pub
	return block
}

// Before DynamicPlasmaSpork activates, strict admission paths (AccountBlock /
// Supervisor.ApplyBlock) must reject difficulties >= 2^63 even with a
// legacy-valid nonce, while in-momentum paths (AccountBlockInMomentum /
// Supervisor.VerifyBlockInMomentum / Supervisor.ApplyBlockInMomentum) preserve
// exact v0.0.10 legacy PoW verification so historical momentums replay identically.
func TestPoWSporkGate_PreDynamicPlasma_AdmissionVsInMomentum(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	saveSporkState(t)

	z.InsertMomentumsTo(5)
	frontier, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.FailIfErr(t, err)

	v := verifier.NewAccountBlockVerifier(z.Chain(), z.Consensus())
	sup := vm.NewSupervisor(z.Chain(), z.Consensus())

	for _, difficulty := range []uint64{1 << 63, math.MaxUint64 - 1, math.MaxUint64} {
		block := buildSignedPoWSend(t, z, g.User6, frontier.Identifier(), difficulty, 0, pow.CheckPoWNonceLegacy)

		// Strict admission paths reject unconditionally, even pre-DP.
		if err := v.AccountBlock(block.Copy()); !errors.Is(err, verifier.ErrABPoWInvalid) {
			t.Fatalf("pre-DP AccountBlock(difficulty=%d): got %v, want ErrABPoWInvalid", difficulty, err)
		}
		if _, err := sup.ApplyBlock(block.Copy()); !errors.Is(err, verifier.ErrABPoWInvalid) {
			t.Fatalf("pre-DP ApplyBlock(difficulty=%d): got %v, want ErrABPoWInvalid", difficulty, err)
		}

		// In-momentum paths accept pre-DP blocks with legacy-valid PoW.
		if err := v.AccountBlockInMomentum(block.Copy()); err != nil {
			t.Fatalf("pre-DP AccountBlockInMomentum(difficulty=%d): got %v, want nil", difficulty, err)
		}
		if err := sup.VerifyBlockInMomentum(block.Copy(), frontier.Height+1); err != nil {
			t.Fatalf("pre-DP VerifyBlockInMomentum(difficulty=%d): got %v, want nil", difficulty, err)
		}
		tx, err := sup.ApplyBlockInMomentum(block.Copy(), frontier.Height+1)
		if err != nil {
			t.Fatalf("pre-DP ApplyBlockInMomentum(difficulty=%d): got %v, want nil", difficulty, err)
		}
		if tx == nil || tx.Block.TotalPlasma != constants.MaxPoWPlasmaForAccountBlock {
			t.Fatalf("pre-DP ApplyBlockInMomentum(difficulty=%d): unexpected tx=%v", difficulty, tx)
		}
	}

	// Legacy-invalid nonce at 2^64-2 (digest < 2^63) must still fail in-momentum pre-DP.
	legacyInvalid := buildSignedPoWSend(t, z, g.User6, frontier.Identifier(), math.MaxUint64-1, 0, func(b *nom.AccountBlock) bool {
		return !pow.CheckPoWNonceLegacy(b)
	})
	if err := v.AccountBlockInMomentum(legacyInvalid.Copy()); !errors.Is(err, verifier.ErrABPoWInvalid) {
		t.Fatalf("pre-DP AccountBlockInMomentum(2^64-2, legacy-invalid): got %v, want ErrABPoWInvalid", err)
	}
	if err := sup.VerifyBlockInMomentum(legacyInvalid.Copy(), frontier.Height+1); !errors.Is(err, verifier.ErrABPoWInvalid) {
		t.Fatalf("pre-DP VerifyBlockInMomentum(2^64-2, legacy-invalid): got %v, want ErrABPoWInvalid", err)
	}
	if _, err := sup.ApplyBlockInMomentum(legacyInvalid.Copy(), frontier.Height+1); !errors.Is(err, verifier.ErrABPoWInvalid) {
		t.Fatalf("pre-DP ApplyBlockInMomentum(2^64-2, legacy-invalid): got %v, want ErrABPoWInvalid", err)
	}
}

// Once DynamicPlasmaSpork is enforced, both admission and in-momentum paths
// reject difficulties >= 2^63 (even with legacy-valid nonces), while valid
// in-range PoW blocks continue to pass on every path.
func TestPoWSporkGate_PostDynamicPlasma_RejectsLegacyHighDifficultyEverywhere(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	saveSporkState(t)

	activateDynamicPlasma(t, z)

	frontier, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.FailIfErr(t, err)
	active, err := z.Chain().GetFrontierMomentumStore().IsSporkActive(types.DynamicPlasmaSpork)
	common.FailIfErr(t, err)
	common.Expect(t, true, active)

	v := verifier.NewAccountBlockVerifier(z.Chain(), z.Consensus())
	sup := vm.NewSupervisor(z.Chain(), z.Consensus())

	for _, difficulty := range []uint64{1 << 63, math.MaxUint64 - 1, math.MaxUint64} {
		block := buildSignedPoWSend(t, z, g.User6, frontier.Identifier(), difficulty, 0, pow.CheckPoWNonceLegacy)

		if err := v.AccountBlock(block.Copy()); !errors.Is(err, verifier.ErrABPoWInvalid) {
			t.Fatalf("post-DP AccountBlock(difficulty=%d): got %v, want ErrABPoWInvalid", difficulty, err)
		}
		if _, err := sup.ApplyBlock(block.Copy()); !errors.Is(err, verifier.ErrABPoWInvalid) {
			t.Fatalf("post-DP ApplyBlock(difficulty=%d): got %v, want ErrABPoWInvalid", difficulty, err)
		}
		if err := v.AccountBlockInMomentum(block.Copy()); !errors.Is(err, verifier.ErrABPoWInvalid) {
			t.Fatalf("post-DP AccountBlockInMomentum(difficulty=%d): got %v, want ErrABPoWInvalid", difficulty, err)
		}
		if err := sup.VerifyBlockInMomentum(block.Copy(), frontier.Height+1); !errors.Is(err, verifier.ErrABPoWInvalid) {
			t.Fatalf("post-DP VerifyBlockInMomentum(difficulty=%d): got %v, want ErrABPoWInvalid", difficulty, err)
		}
		if _, err := sup.ApplyBlockInMomentum(block.Copy(), frontier.Height+1); !errors.Is(err, verifier.ErrABPoWInvalid) {
			t.Fatalf("post-DP ApplyBlockInMomentum(difficulty=%d): got %v, want ErrABPoWInvalid", difficulty, err)
		}
	}

	// Positive control: a user block with valid in-range PoW passes both
	// admission and in-momentum verification post-DP.
	validBlock := buildSignedPoWSend(t, z, g.User1, frontier.Identifier(), 1500, constants.AccountBlockBasePlasma, pow.CheckPoWNonce)
	if err := v.AccountBlock(validBlock.Copy()); err != nil {
		t.Fatalf("post-DP AccountBlock(valid PoW): got %v, want nil", err)
	}
	if err := v.AccountBlockInMomentum(validBlock.Copy()); err != nil {
		t.Fatalf("post-DP AccountBlockInMomentum(valid PoW): got %v, want nil", err)
	}
	if _, err := sup.ApplyBlock(validBlock.Copy()); err != nil {
		t.Fatalf("post-DP ApplyBlock(valid PoW): got %v, want nil", err)
	}
	if _, err := sup.ApplyBlockInMomentum(validBlock.Copy(), frontier.Height+1); err != nil {
		t.Fatalf("post-DP ApplyBlockInMomentum(valid PoW): got %v, want nil", err)
	}
}

// After DynamicPlasmaSpork is enforced at the frontier, a block cannot bypass
// the post-DP PoW rule by pointing MomentumAcknowledged to a pre-spork
// momentum: strict admission rejects with ErrABPoWInvalid, and in-momentum
// verification rejects with ErrABMAStale via acknowledgementRecency.
func TestPoWSporkGate_PostDynamicPlasma_PreSporkMABypassRejected(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	saveSporkState(t)

	z.InsertMomentumsTo(3)
	preSporkMomentum, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.FailIfErr(t, err)

	activateDynamicPlasma(t, z)
	frontier, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.FailIfErr(t, err)

	block := buildSignedPoWSend(t, z, g.User6, preSporkMomentum.Identifier(), math.MaxUint64, 0, pow.CheckPoWNonceLegacy)

	v := verifier.NewAccountBlockVerifier(z.Chain(), z.Consensus())
	sup := vm.NewSupervisor(z.Chain(), z.Consensus())

	if err := v.AccountBlock(block.Copy()); !errors.Is(err, verifier.ErrABPoWInvalid) {
		t.Fatalf("post-DP AccountBlock with pre-DP MA: got %v, want ErrABPoWInvalid", err)
	}
	if _, err := sup.ApplyBlock(block.Copy()); !errors.Is(err, verifier.ErrABPoWInvalid) {
		t.Fatalf("post-DP ApplyBlock with pre-DP MA: got %v, want ErrABPoWInvalid", err)
	}
	if err := v.AccountBlockInMomentum(block.Copy()); !errors.Is(err, verifier.ErrABMAStale) {
		t.Fatalf("post-DP AccountBlockInMomentum with pre-DP MA: got %v, want ErrABMAStale", err)
	}
	if err := sup.VerifyBlockInMomentum(block.Copy(), frontier.Height+1); !errors.Is(err, verifier.ErrABMAStale) {
		t.Fatalf("post-DP VerifyBlockInMomentum with pre-DP MA: got %v, want ErrABMAStale", err)
	}
	if _, err := sup.ApplyBlockInMomentum(block.Copy(), frontier.Height+1); !errors.Is(err, verifier.ErrABMAStale) {
		t.Fatalf("post-DP ApplyBlockInMomentum with pre-DP MA: got %v, want ErrABMAStale", err)
	}
}
