package tests

import (
	"bytes"
	"math/big"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

func TestPtlc_sendBlockRefusals(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()
	activatePtlc(t, z)

	unregisteredContract := types.Address{types.ContractAddrByte, 0xde, 0xad, 0xbe, 0xef}
	identityKey := append([]byte{1}, make([]byte, 31)...)

	create := func(pointLock []byte, destination types.Address) *nom.AccountBlock {
		return &nom.AccountBlock{
			Address:   g.User1.Address,
			ToAddress: types.PtlcContract,
			Data: definition.ABIPtlc.PackMethodPanic(definition.CreatePtlcMethodName,
				int64(genesisTimestamp+300),
				definition.PointTypeED25519,
				pointLock,
				destination,
			),
			TokenStandard: types.ZnnTokenStandard,
			Amount:        big.NewInt(10 * g.Zexp),
		}
	}

	z.InsertSendBlock(create(g.User2.Public, unregisteredContract), constants.ErrInvalidDestination, mock.NoVmChanges)
	z.InsertSendBlock(create(identityKey, types.ZeroAddress), constants.ErrInvalidPointLock, mock.NoVmChanges)

	ptlcId := z.InsertSendBlock(create(g.User2.Public, types.ZeroAddress), nil, mock.SkipVmChanges).Hash
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)

	unlock := func(witness []byte) *nom.AccountBlock {
		return &nom.AccountBlock{
			Address:   g.User2.Address,
			ToAddress: types.PtlcContract,
			Data:      definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName, ptlcId, witness),
		}
	}
	proxyUnlock := func(destination types.Address, witness []byte) *nom.AccountBlock {
		return &nom.AccountBlock{
			Address:   g.User3.Address,
			ToAddress: types.PtlcContract,
			Data:      definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName, ptlcId, destination, witness),
		}
	}

	for _, size := range []int{0, 63, 65, constants.MaxDataLength + 1, 1 << 20} {
		witness := bytes.Repeat([]byte{1}, size)
		z.InsertSendBlock(unlock(witness), constants.ErrInvalidPointSignature, mock.NoVmChanges)
		z.InsertSendBlock(proxyUnlock(g.User2.Address, witness), constants.ErrInvalidPointSignature, mock.NoVmChanges)
	}

	for _, destination := range []types.Address{types.ZeroAddress, types.PtlcContract, unregisteredContract} {
		signature := g.User2.Sign(ptlcUnlockMessage(z, definition.PointTypeED25519, ptlcId, destination))
		z.InsertSendBlock(proxyUnlock(destination, signature), constants.ErrInvalidDestination, mock.NoVmChanges)
	}
	z.InsertNewMomentum()
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 10*g.Zexp)

	signature := g.User2.Sign(ptlcUnlockMessage(z, definition.PointTypeED25519, ptlcId, g.User2.Address))
	defer z.CallContract(unlock(signature)).Error(t, nil)
	z.InsertNewMomentum()
	z.InsertNewMomentum()
	z.ExpectBalance(types.PtlcContract, types.ZnnTokenStandard, 0)
}
