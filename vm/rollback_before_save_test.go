package vm

import (
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

// rollbackMomentumStore serves the send block and a frontier momentum,
// the two lookups rollbackEmbedded and finalizeEmbedded make.
type rollbackMomentumStore struct {
	store.Momentum
	frontier  *nom.Momentum
	sendBlock *nom.AccountBlock
}

func (s *rollbackMomentumStore) GetFrontierMomentum() (*nom.Momentum, error) {
	return s.frontier, nil
}
func (s *rollbackMomentumStore) GetAccountBlockByHash(hash types.Hash) (*nom.AccountBlock, error) {
	if hash == s.sendBlock.Hash {
		return s.sendBlock, nil
	}
	return nil, nil
}
func (s *rollbackMomentumStore) ChainIdentifier() uint64 { return 1 }

// rollbackBeforeSave drives the branch generateEmbeddedReceive takes when
// the send's method no longer resolves: rollbackEmbedded runs before
// Save, so Reset has no snapshot to restore. The method lookup itself is
// not exercised (no current spork variant removes a method); the test
// calls the rollback directly, with the embedded contract holding
// `held` ZNN before the receive.
func rollbackBeforeSave(t *testing.T, amount *big.Int, held int64) (vm_context.AccountVmContext, *nom.AccountBlock, *nom.AccountBlock) {
	t.Helper()
	contract := types.TokenContract
	sender := types.ParseAddressPanic("z1qzuej4d8292sstg38qpnca9tfpfmglqjryffpv")
	sendBlock := &nom.AccountBlock{
		BlockType:     nom.BlockTypeUserSend,
		Address:       sender,
		ToAddress:     contract,
		Amount:        amount,
		TokenStandard: types.ZnnTokenStandard,
		Hash:          types.HexToHashPanic("03aab631b44daf47f68810a416bea4ed3695e89c0ab9668a0ab8acb834dfb902"),
	}
	momentumStore := &rollbackMomentumStore{
		frontier: &nom.Momentum{
			Hash:   types.HexToHashPanic("01aab631b44daf47f68810a416bea4ed3695e89c0ab9668a0ab8acb834dfb900"),
			Height: 10,
		},
		sendBlock: sendBlock,
	}
	ctx := vm_context.NewAccountContext(momentumStore, account.NewAccountStore(contract, db.NewMemDB()), nil, nil)
	ctx.AddBalance(&types.ZnnTokenStandard, big.NewInt(held))

	var (
		block      *nom.AccountBlock
		methodErr  error
		processErr error
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("rollback before Save panicked: %v", r)
			}
		}()
		block, methodErr, processErr = NewVM(ctx, nil).rollbackEmbedded(sendBlock.Hash, constants.ErrContractMethodNotFound)
	}()
	common.FailIfErr(t, processErr)
	common.ExpectError(t, methodErr, constants.ErrContractMethodNotFound)
	if block == nil {
		t.Fatal("no receive block")
	}
	common.Expect(t, block.BlockType, uint64(nom.BlockTypeContractReceive))
	common.Expect(t, block.Address, contract)
	common.Expect(t, block.FromBlockHash, sendBlock.Hash)
	common.Expect(t, common.BytesToUint64(block.Data), resultFail)
	return ctx, sendBlock, block
}

func expectContractBalance(t *testing.T, ctx vm_context.AccountVmContext, want int64) {
	t.Helper()
	b, err := ctx.GetBalance(types.ZnnTokenStandard)
	common.FailIfErr(t, err)
	if b.Cmp(big.NewInt(want)) != 0 {
		t.Fatalf("contract balance %v, want %d", b, want)
	}
}

// A send carrying funds is refunded to its sender in full, and the
// contract keeps exactly the balance it held before the receive.
func TestRollbackBeforeSave_RefundsTheSender(t *testing.T) {
	ctx, sendBlock, block := rollbackBeforeSave(t, big.NewInt(7), 100)

	if len(block.DescendantBlocks) != 1 {
		t.Fatalf("%d descendant blocks, want 1 refund", len(block.DescendantBlocks))
	}
	refund := block.DescendantBlocks[0]
	common.Expect(t, refund.BlockType, uint64(nom.BlockTypeContractSend))
	common.Expect(t, refund.Address, sendBlock.ToAddress)
	common.Expect(t, refund.ToAddress, sendBlock.Address)
	common.Expect(t, refund.TokenStandard, sendBlock.TokenStandard)
	if refund.Amount.Cmp(sendBlock.Amount) != 0 {
		t.Fatalf("refund amount %v, want %v", refund.Amount, sendBlock.Amount)
	}
	if refund.Amount == sendBlock.Amount {
		t.Fatal("refund shares the send block's amount pointer")
	}
	expectContractBalance(t, ctx, 100)

	// The context is still usable for a normal Save/Reset cycle.
	ctx.Save()
	ctx.AddBalance(&types.ZnnTokenStandard, big.NewInt(5))
	expectContractBalance(t, ctx, 105)
	ctx.Reset()
	expectContractBalance(t, ctx, 100)
}

// A send without funds takes the other branch: no refund block, and the
// contract balance is unchanged.
func TestRollbackBeforeSave_ZeroAmountHasNoRefund(t *testing.T) {
	ctx, _, block := rollbackBeforeSave(t, big.NewInt(0), 100)

	if len(block.DescendantBlocks) != 0 {
		t.Fatalf("%d descendant blocks, want none", len(block.DescendantBlocks))
	}
	expectContractBalance(t, ctx, 100)

	ctx.Save()
	ctx.AddBalance(&types.ZnnTokenStandard, big.NewInt(5))
	ctx.Done()
	expectContractBalance(t, ctx, 105)
}
