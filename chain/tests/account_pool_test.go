package tests

import (
	"math/big"
	"testing"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

func TestAccountPool_GetAllUncommittedAccountBlocks(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	blocks := []*nom.AccountBlock{
		{
			Address: g.User1.Address,
		},
		{
			Address: g.User2.Address,
		},
		{
			Address: g.User3.Address,
		},
		{
			Address:       g.User1.Address,
			ToAddress:     types.TokenContract,
			TokenStandard: types.ZnnTokenStandard,
			Amount:        constants.TokenIssueAmount,
			Data: definition.ABIToken.PackMethodPanic(definition.IssueMethodName,
				"test.tok3n_na-m3", //param.TokenName
				"TEST",             //param.TokenSymbol
				"",                 //param.TokenDomain
				big.NewInt(100),    //param.TotalSupply
				big.NewInt(1000),   //param.MaxSupply
				uint8(1),           //param.Decimals
				true,               //param.IsMintable
				true,               //param.IsBurnable
				false,              //param.IsUtility
			),
		},
	}

	for _, block := range blocks {
		if types.IsEmbeddedAddress(block.ToAddress) {
			z.CallContract(block)
		} else {
			z.InsertSendBlock(block, nil, mock.SkipVmChanges)
		}
	}

	uncommitted := z.Chain().GetAllUncommittedAccountBlocks()
	common.Expect(t, len(uncommitted), 4)

	z.InsertNewMomentum() // generate contract receive and its descendant block

	uncommitted = z.Chain().GetAllUncommittedAccountBlocks()
	common.Expect(t, len(uncommitted), 2)

	z.InsertNewMomentum()

	uncommitted = z.Chain().GetAllUncommittedAccountBlocks()
	common.Expect(t, len(uncommitted), 0)
}

func TestAccountPool_Rebuild(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	for i := 0; i < 100; i++ {
		z.InsertSendBlock(&nom.AccountBlock{
			Address: g.User1.Address,
		}, nil, mock.SkipVmChanges)
		z.InsertSendBlock(&nom.AccountBlock{
			Address: g.User2.Address,
		}, nil, mock.SkipVmChanges)
	}

	uncommitted := z.Chain().GetAllUncommittedAccountBlocks()
	common.Expect(t, len(uncommitted), 200)

	z.InsertNewMomentum() // trigger rebuild

	uncommitted = z.Chain().GetAllUncommittedAccountBlocks()
	common.Expect(t, len(uncommitted), 100)
}

func TestAccountPool_Priority(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	lowPriorityBlock := &nom.AccountBlock{
		Address:     g.User1.Address,
		FusedPlasma: 21000,
	}

	z.InsertSendBlock(lowPriorityBlock, nil, mock.SkipVmChanges)

	uncommitted := z.Chain().GetAllUncommittedAccountBlocks()
	common.Expect(t, len(uncommitted), 1)

	highPriorityBlock := uncommitted[0]
	highPriorityBlock.FusedPlasma = 22000

	z.InsertSendBlock(highPriorityBlock, nil, mock.SkipVmChanges)

	uncommitted = z.Chain().GetAllUncommittedAccountBlocks()
	common.Expect(t, len(uncommitted), 1)
	common.ExpectString(t, uncommitted[0].Hash.String(), highPriorityBlock.Hash.String())

	z.InsertSendBlock(lowPriorityBlock, nil, mock.SkipVmChanges)

	uncommitted = z.Chain().GetAllUncommittedAccountBlocks()
	common.Expect(t, len(uncommitted), 1)
	common.ExpectString(t, uncommitted[0].Hash.String(), highPriorityBlock.Hash.String())
}

func BenchmarkAccountPool_GetAllUncommittedAccountBlocks(b *testing.B) {
	z := mock.NewMockZenon(b)
	defer z.StopPanic()

	for i := 0; i < 500; i++ {
		z.InsertSendBlock(&nom.AccountBlock{
			Address: g.User1.Address,
		}, nil, mock.SkipVmChanges)
		z.InsertSendBlock(&nom.AccountBlock{
			Address: g.User2.Address,
		}, nil, mock.SkipVmChanges)
		z.InsertSendBlock(&nom.AccountBlock{
			Address: g.User3.Address,
		}, nil, mock.SkipVmChanges)
		z.InsertSendBlock(&nom.AccountBlock{
			Address: g.User4.Address,
		}, nil, mock.SkipVmChanges)
		z.InsertSendBlock(&nom.AccountBlock{
			Address: g.User5.Address,
		}, nil, mock.SkipVmChanges)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		z.Chain().GetAllUncommittedAccountBlocks()
	}
}
