package implementation

import (
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

const unjailPillarName = "unjail-pillar"

var (
	unjailOwner    = termsTestAddress(11)
	unjailProducer = termsTestAddress(12)
)

func newUnjailContext(t *testing.T, revokeTime int64, jail *definition.PillarJail) *testEpochBoundContext {
	t.Helper()
	ctx := newTestEpochBoundContext(t, time.Unix(5000, 0))
	common.FailIfErr(t, (&definition.PillarInfo{
		Name:                  unjailPillarName,
		StakeAddress:          unjailOwner,
		BlockProducingAddress: unjailProducer,
		RewardWithdrawAddress: unjailOwner,
		Amount:                constants.PillarStakeAmount,
		PillarType:            definition.NormalPillarType,
		RevokeTime:            revokeTime,
	}).Save(ctx.Storage()))
	if jail != nil {
		common.FailIfErr(t, jail.Save(ctx.Storage()))
	}
	return ctx
}

func unjailSendBlock(sender types.Address, name string, token types.ZenonTokenStandard, amount *big.Int) *nom.AccountBlock {
	return &nom.AccountBlock{
		BlockType:     nom.BlockTypeUserSend,
		Address:       sender,
		ToAddress:     types.PillarContract,
		TokenStandard: token,
		Amount:        amount,
		Data:          definition.ABIPillars.PackMethodPanic(definition.UnjailMethodName, name),
	}
}

func expectJail(t *testing.T, ctx *testEpochBoundContext, want *definition.PillarJail) {
	t.Helper()
	got, err := definition.GetPillarJail(ctx.Storage(), unjailPillarName)
	if want == nil {
		if err != constants.ErrDataNonExistent {
			t.Fatalf("jail record err = %v, want %v", err, constants.ErrDataNonExistent)
		}
		return
	}
	common.FailIfErr(t, err)
	if got.Name != want.Name || got.Jailed != want.Jailed || got.Since != want.Since {
		t.Fatalf("jail record = %+v, want %+v", got, want)
	}
}

func TestUnjail_ValidateSendBlock(t *testing.T) {
	method := &UnjailMethod{MethodName: definition.UnjailMethodName}
	unit := constants.PillarUnjailAmount
	revokeData := definition.ABIPillars.PackMethodPanic(definition.RevokeMethodName, unjailPillarName)

	for _, tc := range []struct {
		name  string
		block *nom.AccountBlock
		data  []byte
		want  error
	}{
		{"valid", unjailSendBlock(unjailOwner, unjailPillarName, types.ZnnTokenStandard, unit), nil, nil},
		{"one below", unjailSendBlock(unjailOwner, unjailPillarName, types.ZnnTokenStandard, new(big.Int).Sub(unit, big.NewInt(1))), nil, constants.ErrInvalidTokenOrAmount},
		{"one above", unjailSendBlock(unjailOwner, unjailPillarName, types.ZnnTokenStandard, new(big.Int).Add(unit, big.NewInt(1))), nil, constants.ErrInvalidTokenOrAmount},
		{"zero", unjailSendBlock(unjailOwner, unjailPillarName, types.ZnnTokenStandard, big.NewInt(0)), nil, constants.ErrInvalidTokenOrAmount},
		{"qsr", unjailSendBlock(unjailOwner, unjailPillarName, types.QsrTokenStandard, big.NewInt(10*constants.Decimals)), nil, constants.ErrInvalidTokenOrAmount},
		{"empty name", unjailSendBlock(unjailOwner, "", types.ZnnTokenStandard, unit), nil, constants.ErrInvalidName},
		{"bad name", unjailSendBlock(unjailOwner, "bad name!", types.ZnnTokenStandard, unit), nil, constants.ErrInvalidName},
		{"other method data", unjailSendBlock(unjailOwner, unjailPillarName, types.ZnnTokenStandard, unit), revokeData, constants.ErrUnpackError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.data != nil {
				tc.block.Data = tc.data
			}
			want := definition.ABIPillars.PackMethodPanic(definition.UnjailMethodName, unjailPillarName)
			err := method.ValidateSendBlock(tc.block)
			if err != tc.want {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if tc.want == nil && !reflect.DeepEqual(tc.block.Data, want) {
				t.Fatalf("data = %x, want %x", tc.block.Data, want)
			}
		})
	}
}

func TestUnjail_ReceiveBlockRejectsAndKeepsRecord(t *testing.T) {
	method := &UnjailMethod{MethodName: definition.UnjailMethodName}
	unit := constants.PillarUnjailAmount
	jailed := &definition.PillarJail{Name: unjailPillarName, Jailed: true, Since: 100}
	cleared := &definition.PillarJail{Name: unjailPillarName, Jailed: false, Since: 100}

	for _, tc := range []struct {
		name       string
		revokeTime int64
		jail       *definition.PillarJail
		send       *nom.AccountBlock
		want       error
	}{
		{"unknown pillar", 0, jailed, unjailSendBlock(unjailOwner, "no-such-pillar", types.ZnnTokenStandard, unit), constants.ErrDataNonExistent},
		{"revoked pillar", 100, jailed, unjailSendBlock(unjailOwner, unjailPillarName, types.ZnnTokenStandard, unit), constants.ErrNotActive},
		{"producer sender", 0, jailed, unjailSendBlock(unjailProducer, unjailPillarName, types.ZnnTokenStandard, unit), constants.ErrPermissionDenied},
		{"no record", 0, nil, unjailSendBlock(unjailOwner, unjailPillarName, types.ZnnTokenStandard, unit), constants.ErrPillarNotJailed},
		{"not jailed", 0, cleared, unjailSendBlock(unjailOwner, unjailPillarName, types.ZnnTokenStandard, unit), constants.ErrPillarNotJailed},
		{"wrong amount", 0, jailed, unjailSendBlock(unjailOwner, unjailPillarName, types.ZnnTokenStandard, big.NewInt(99*constants.Decimals)), constants.ErrInvalidTokenOrAmount},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newUnjailContext(t, tc.revokeTime, tc.jail)
			blocks, err := method.ReceiveBlock(ctx, tc.send)
			if err != tc.want {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if blocks != nil {
				t.Fatalf("descendant blocks = %v, want none", blocks)
			}
			expectJail(t, ctx, tc.jail)
		})
	}
}

func TestUnjail_ReceiveBlockClearsJailAndBurnsFee(t *testing.T) {
	method := &UnjailMethod{MethodName: definition.UnjailMethodName}
	ctx := newUnjailContext(t, 0, &definition.PillarJail{Name: unjailPillarName, Jailed: true, Since: 100})
	send := unjailSendBlock(unjailOwner, unjailPillarName, types.ZnnTokenStandard, constants.PillarUnjailAmount)

	blocks, err := method.ReceiveBlock(ctx, send)
	common.FailIfErr(t, err)
	if len(blocks) != 1 {
		t.Fatalf("descendant blocks = %d, want 1", len(blocks))
	}
	burn := blocks[0]
	if burn.Address != types.PillarContract || burn.ToAddress != types.TokenContract ||
		burn.BlockType != nom.BlockTypeContractSend || burn.TokenStandard != types.ZnnTokenStandard ||
		burn.Amount.Cmp(constants.PillarUnjailAmount) != 0 ||
		!reflect.DeepEqual(burn.Data, definition.ABIToken.PackMethodPanic(definition.BurnMethodName)) {
		t.Fatalf("unexpected burn descendant %+v", burn)
	}
	expectJail(t, ctx, &definition.PillarJail{Name: unjailPillarName, Jailed: false, Since: 5000})

	blocks, err = method.ReceiveBlock(ctx, unjailSendBlock(unjailOwner, unjailPillarName, types.ZnnTokenStandard, constants.PillarUnjailAmount))
	if err != constants.ErrPillarNotJailed || blocks != nil {
		t.Fatalf("second unjail = %v, %v, want %v and no blocks", blocks, err, constants.ErrPillarNotJailed)
	}
	expectJail(t, ctx, &definition.PillarJail{Name: unjailPillarName, Jailed: false, Since: 5000})
}
