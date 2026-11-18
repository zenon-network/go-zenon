package vm

import (
	"errors"
	"math/big"
	"strings"
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

// --- equal-patch local-generation failure (review on the sentinel PR) ---

// allSporksCache answers "active" to every spork lookup, the state this
// fixture needs for the liquidity unlock call.
type allSporksCache struct {
	store.Cache
}

func (allSporksCache) IsSporkActive(*types.ImplementedSpork) (bool, error) { return true, nil }

var errInjectedRead = errors.New("leveldb: injected read failure")

// failingStorage fails every read, standing in for a storage error inside
// the contract method during local generation.
type failingStorage struct{ db.DB }

func (failingStorage) Get([]byte) ([]byte, error) { return nil, errInjectedRead }

// failingStorageContext routes Storage() to the failing store; everything
// else delegates to the wrapped context.
type failingStorageContext struct{ vm_context.AccountVmContext }

func (c failingStorageContext) Storage() db.DB { return failingStorage{c.AccountVmContext.Storage()} }

// equalPatchSetup serves a zero-amount UnlockLiquidityStakeEntries send;
// its method succeeds without a write when no entry is unexpired, so the
// rollback patch of a failed local generation is byte-identical to the
// success patch.
func equalPatchSetup() (vm_context.AccountVmContext, *nom.AccountBlock) {
	contract := types.LiquidityContract
	sendBlock := &nom.AccountBlock{
		BlockType:     nom.BlockTypeUserSend,
		Address:       constants.InitialBridgeAdministrator,
		ToAddress:     contract,
		Amount:        big.NewInt(0),
		TokenStandard: types.ZnnTokenStandard,
		Data:          definition.ABILiquidity.PackMethodPanic(definition.UnlockLiquidityStakeEntriesMethodName),
		Hash:          types.HexToHashPanic("03aab631b44daf47f68810a416bea4ed3695e89c0ab9668a0ab8acb834dfb902"),
	}
	momentumStore := &rollbackMomentumStore{
		frontier: &nom.Momentum{
			Hash:   types.HexToHashPanic("01aab631b44daf47f68810a416bea4ed3695e89c0ab9668a0ab8acb834dfb900"),
			Height: 10,
		},
		sendBlock: sendBlock,
	}
	return vm_context.NewAccountContext(momentumStore, account.NewAccountStore(contract, db.NewMemDB()), allSporksCache{}, nil), sendBlock
}

// TestApplyBlock_AutoReceiveHashMismatchEqualPatch pins the review guard on
// the hash sentinel: when local generation failed but produced the
// byte-identical patch a successful execution would leave (zero-amount send,
// no write), the served block's hash differs from the computed one only
// because of the status byte in Data. That is not a verdict on the block:
// the refusal must carry no sentinel, or the bridge would remember an
// honest momentum's bytes and quarantine every peer that serves it.
func TestApplyBlock_AutoReceiveHashMismatchEqualPatch(t *testing.T) {
	ctx, sendBlock := equalPatchSetup()
	served, servedExecErr, err := NewVM(ctx, nil).generateEmbeddedReceive(sendBlock.Hash)
	if err != nil || servedExecErr != nil {
		t.Fatalf("setup: served generation must succeed, got execErr=%v err=%v", servedExecErr, err)
	}

	ctx, _ = equalPatchSetup()
	local, localExecErr, err := NewVM(failingStorageContext{ctx}, nil).generateEmbeddedReceive(sendBlock.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(localExecErr, errInjectedRead) {
		t.Fatalf("expected the injected read failure, got %v", localExecErr)
	}
	if local.ChangesHash != served.ChangesHash || local.Hash == served.Hash {
		t.Fatalf("premise does not hold: changes %v/%v hash %v/%v", local.ChangesHash, served.ChangesHash, local.Hash, served.Hash)
	}

	ctx, _ = equalPatchSetup()
	applyErr := NewVM(failingStorageContext{ctx}, nil).applyBlock(served)
	if applyErr == nil || !strings.Contains(applyErr.Error(), "auto-received block has different hash") {
		t.Fatalf("expected the hash-mismatch refusal, got: %v", applyErr)
	}
	if errors.Is(applyErr, constants.ErrAutoReceiveHashMismatch) {
		t.Fatal("a failed local generation must not be a verdict: the refusal must carry no sentinel")
	}

	ctx, _ = equalPatchSetup()
	if err := NewVM(ctx, nil).applyBlock(served); err != nil {
		t.Fatalf("control: %v", err)
	}
}
