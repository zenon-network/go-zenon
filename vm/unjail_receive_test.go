package vm

import (
	"math/big"
	"testing"
	"time"

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

// Unjail through generateEmbeddedReceive with the real account context
// over an in-memory pillar contract store: the receive runs the method
// lookup, the jail write, the balance credit and the burn send of the
// fee, and the failure path runs the rollback and the refund.

const unjailVMPillar = "vm-unjail-pillar"

var (
	unjailVMOwner    = termsVMAddress(21)
	unjailVMStranger = termsVMAddress(22)
)

// unjailSporkCache reports only types.PillarJailSpork as active.
type unjailSporkCache struct {
	store.Cache
}

func (unjailSporkCache) IsSporkActive(spork *types.ImplementedSpork) (bool, error) {
	return spork.SporkId == types.PillarJailSpork.SporkId, nil
}

// unjailVMBalance is the ZNN the pillar contract holds before the
// receive.
var unjailVMBalance = big.NewInt(500 * constants.Decimals)

// newUnjailVMHarness seeds the pillar contract with an active pillar
// owned by unjailVMOwner, a jail record {Jailed: true, Since: 100} and
// unjailVMBalance ZNN.
func newUnjailVMHarness(t *testing.T) (*VM, vm_context.AccountVmContext, *rollbackMomentumStore) {
	t.Helper()
	acc := account.NewAccountStore(types.PillarContract, db.NewMemDB())
	storage := acc.Storage()
	common.FailIfErr(t, (&definition.PillarInfo{
		Name:                  unjailVMPillar,
		BlockProducingAddress: unjailVMOwner,
		RewardWithdrawAddress: unjailVMOwner,
		StakeAddress:          unjailVMOwner,
		Amount:                constants.PillarStakeAmount,
		PillarType:            definition.NormalPillarType,
	}).Save(storage))
	common.FailIfErr(t, (&definition.PillarJail{Name: unjailVMPillar, Jailed: true, Since: 100}).Save(storage))

	ts := time.Unix(5000, 0)
	ms := &rollbackMomentumStore{
		frontier: &nom.Momentum{
			Hash:      types.HexToHashPanic("01aab631b44daf47f68810a416bea4ed3695e89c0ab9668a0ab8acb834dfb900"),
			Height:    constants.UpdateMinNumMomentums + 10,
			Timestamp: &ts,
		},
	}
	ctx := vm_context.NewAccountContext(ms, acc, unjailSporkCache{}, nil)
	ctx.AddBalance(&types.ZnnTokenStandard, unjailVMBalance)
	return NewVM(ctx, ms), ctx, ms
}

func unjailVMReceive(t *testing.T, vm *VM, ms *rollbackMomentumStore, sender types.Address) *nom.AccountBlock {
	t.Helper()
	send := &nom.AccountBlock{
		BlockType:     nom.BlockTypeUserSend,
		Address:       sender,
		ToAddress:     types.PillarContract,
		Amount:        constants.PillarUnjailAmount,
		TokenStandard: types.ZnnTokenStandard,
		Data:          definition.ABIPillars.PackMethodPanic(definition.UnjailMethodName, unjailVMPillar),
	}
	send.Hash = send.ComputeHash()
	ms.sendBlock = send
	block, _, err := vm.generateEmbeddedReceive(send.Hash)
	common.FailIfErr(t, err)
	if block == nil {
		t.Fatal("no receive block")
	}
	return block
}

func expectUnjailVMState(t *testing.T, ctx vm_context.AccountVmContext, jailed bool, since int64) {
	t.Helper()
	jail, err := definition.GetPillarJail(ctx.Storage(), unjailVMPillar)
	common.FailIfErr(t, err)
	if jail.Jailed != jailed || jail.Since != since {
		t.Fatalf("jail record {Jailed: %v, Since: %d}, want {Jailed: %v, Since: %d}", jail.Jailed, jail.Since, jailed, since)
	}
	balance, err := ctx.GetBalance(types.ZnnTokenStandard)
	common.FailIfErr(t, err)
	if balance.Cmp(unjailVMBalance) != 0 {
		t.Fatalf("pillar contract ZNN balance %v, want %v", balance, unjailVMBalance)
	}
}

func TestGenerateEmbeddedReceive_UnjailBurnsFeeAndClearsJail(t *testing.T) {
	vm, ctx, ms := newUnjailVMHarness(t)

	block := unjailVMReceive(t, vm, ms, unjailVMOwner)

	if got := common.BytesToUint64(block.Data); got != resultSuccess {
		t.Fatalf("receive status %d, want %d", got, resultSuccess)
	}
	if len(block.DescendantBlocks) != 1 {
		t.Fatalf("descendant blocks %d, want 1", len(block.DescendantBlocks))
	}
	burn := block.DescendantBlocks[0]
	if burn.ToAddress != types.TokenContract || burn.TokenStandard != types.ZnnTokenStandard || burn.Amount.Cmp(constants.PillarUnjailAmount) != 0 {
		t.Fatalf("burn descendant to %v of %v %v, want %v of %v %v", burn.ToAddress, burn.Amount, burn.TokenStandard,
			types.TokenContract, constants.PillarUnjailAmount, types.ZnnTokenStandard)
	}
	expectUnjailVMState(t, ctx, false, 5000)
}

func TestGenerateEmbeddedReceive_UnjailByNonOwnerRefundsSender(t *testing.T) {
	vm, ctx, ms := newUnjailVMHarness(t)

	block := unjailVMReceive(t, vm, ms, unjailVMStranger)

	if got := common.BytesToUint64(block.Data); got != resultFail {
		t.Fatalf("receive status %d, want %d", got, resultFail)
	}
	if len(block.DescendantBlocks) != 1 {
		t.Fatalf("descendant blocks %d, want 1", len(block.DescendantBlocks))
	}
	refund := block.DescendantBlocks[0]
	if refund.ToAddress != unjailVMStranger || refund.TokenStandard != types.ZnnTokenStandard || refund.Amount.Cmp(constants.PillarUnjailAmount) != 0 {
		t.Fatalf("refund descendant to %v of %v %v, want %v of %v %v", refund.ToAddress, refund.Amount, refund.TokenStandard,
			unjailVMStranger, constants.PillarUnjailAmount, types.ZnnTokenStandard)
	}
	expectUnjailVMState(t, ctx, true, 100)
}
