package vm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

// noSporkCache answers "not active" to every spork lookup, the state
// of a fresh chain for the contracts this test touches.
type noSporkCache struct {
	store.Cache
}

func (noSporkCache) IsSporkActive(*types.ImplementedSpork) (bool, error) { return false, nil }

// autoReceiveMismatchSetup serves a DepositQsr send to the pillar
// contract and a frontier momentum, the two lookups
// generateEmbeddedReceive makes, on a fresh context each call so a
// failed applyBlock cannot leak changes into the next one. DepositQsr
// is used because its ReceiveBlock cannot fail past validation on a
// fresh store, so the generated receive demonstrates agreement on a
// successful execution (executionErr == nil), not on a rollback.
func autoReceiveMismatchSetup() (vm_context.AccountVmContext, *nom.AccountBlock) {
	contract := types.PillarContract
	sender := types.ParseAddressPanic("z1qzuej4d8292sstg38qpnca9tfpfmglqjryffpv")
	sendBlock := &nom.AccountBlock{
		BlockType:     nom.BlockTypeUserSend,
		Address:       sender,
		ToAddress:     contract,
		Amount:        big.NewInt(10),
		TokenStandard: types.QsrTokenStandard,
		Data:          definition.ABIPillars.PackMethodPanic(definition.DepositQsrMethodName),
		Hash:          types.HexToHashPanic("03aab631b44daf47f68810a416bea4ed3695e89c0ab9668a0ab8acb834dfb902"),
	}
	momentumStore := &rollbackMomentumStore{
		frontier: &nom.Momentum{
			Hash:   types.HexToHashPanic("01aab631b44daf47f68810a416bea4ed3695e89c0ab9668a0ab8acb834dfb900"),
			Height: 10,
		},
		sendBlock: sendBlock,
	}
	ctx := vm_context.NewAccountContext(momentumStore, account.NewAccountStore(contract, db.NewMemDB()), noSporkCache{}, nil)
	ctx.AddBalance(&types.ZnnTokenStandard, big.NewInt(100))
	return ctx, sendBlock
}

// TestApplyBlock_AutoReceiveHashMismatch pins the issue-#299 sentinel for
// the branch where a served contract-receive matches the locally
// generated changes-hash but its hash does not: the refusal must wrap
// constants.ErrAutoReceiveHashMismatch so the protocol layer classifies
// it (remember the bytes, quarantine the peer). With the pre-#299 plain
// errors.Errorf the errors.Is assertion fails, so a revert of the
// production change is caught. The fixture uses a successful method
// call (DepositQsr), asserting executionErr == nil, so the tampered
// hash is judged against a real execution and the control applies the
// unmodified generated block on a fresh context.
func TestApplyBlock_AutoReceiveHashMismatch(t *testing.T) {
	generatedFor := func(t *testing.T) *nom.AccountBlock {
		t.Helper()
		ctx, sendBlock := autoReceiveMismatchSetup()
		generated, executionErr, processErr := NewVM(ctx, nil).generateEmbeddedReceive(sendBlock.Hash)
		common.FailIfErr(t, processErr)
		if executionErr != nil {
			t.Fatalf("DepositQsr generation must succeed: %v", executionErr)
		}
		if generated == nil {
			t.Fatal("no receive generated")
		}
		return generated
	}

	served := generatedFor(t)
	served.Hash = types.NewHash([]byte("tampered served hash"))

	ctx, _ := autoReceiveMismatchSetup()
	err := NewVM(ctx, nil).applyBlock(served)
	if !errors.Is(err, constants.ErrAutoReceiveHashMismatch) {
		t.Fatalf("served hash differing from the computed one: got %v, want %v", err, constants.ErrAutoReceiveHashMismatch)
	}

	ctx, _ = autoReceiveMismatchSetup()
	if err := NewVM(ctx, nil).applyBlock(generatedFor(t)); err != nil {
		t.Fatalf("the unmodified generated receive must apply clean: %v", err)
	}
}
