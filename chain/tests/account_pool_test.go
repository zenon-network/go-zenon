package tests

import (
	"bytes"
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

func TestAccountPool_CachedBlocksAreDefensiveCopies(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	inserted := z.InsertSendBlock(&nom.AccountBlock{
		Address:     g.User1.Address,
		FusedPlasma: 21000,
	}, nil, mock.SkipVmChanges)
	originalHash := inserted.Hash
	originalFusedPlasma := inserted.FusedPlasma

	inserted.Hash = types.ZeroHash
	inserted.FusedPlasma = 1

	uncommitted := z.Chain().GetAllUncommittedAccountBlocks()
	common.Expect(t, len(uncommitted), 1)
	common.ExpectString(t, uncommitted[0].Hash.String(), originalHash.String())
	common.ExpectUint64(t, uncommitted[0].FusedPlasma, originalFusedPlasma)

	uncommitted[0].Hash = types.ZeroHash
	uncommitted[0].FusedPlasma = 2

	uncommitted = z.Chain().GetAllUncommittedAccountBlocks()
	common.Expect(t, len(uncommitted), 1)
	common.ExpectString(t, uncommitted[0].Hash.String(), originalHash.String())
	common.ExpectUint64(t, uncommitted[0].FusedPlasma, originalFusedPlasma)

	byAddress := z.Chain().GetUncommittedAccountBlocksByAddress(g.User1.Address)
	common.Expect(t, len(byAddress), 1)
	common.ExpectString(t, byAddress[0].Hash.String(), originalHash.String())
	common.ExpectUint64(t, byAddress[0].FusedPlasma, originalFusedPlasma)

	byAddress[0].Hash = types.ZeroHash
	byAddress[0].FusedPlasma = 3

	byAddress = z.Chain().GetUncommittedAccountBlocksByAddress(g.User1.Address)
	common.Expect(t, len(byAddress), 1)
	common.ExpectString(t, byAddress[0].Hash.String(), originalHash.String())
	common.ExpectUint64(t, byAddress[0].FusedPlasma, originalFusedPlasma)
}

func TestAccountPool_CachedBlocksDeepCopySliceAndPointerFields(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	inserted := z.InsertSendBlock(&nom.AccountBlock{
		Address:       g.User1.Address,
		ToAddress:     g.User2.Address,
		TokenStandard: types.ZnnTokenStandard,
		Amount:        big.NewInt(100),
		Data:          []byte{0x01, 0x02, 0x03},
	}, nil, mock.SkipVmChanges)
	common.ExpectTrue(t, len(inserted.Signature) > 0)
	common.ExpectTrue(t, len(inserted.PublicKey) > 0)

	originalAmount := new(big.Int).Set(inserted.Amount)
	originalData := append([]byte(nil), inserted.Data...)
	originalSignature := append([]byte(nil), inserted.Signature...)
	originalPublicKey := append([]byte(nil), inserted.PublicKey...)

	// block.PublicKey aliases the global mock keypair (KeyPair.Signer returns
	// kp.Public), so restore it even if a Fatalf-based assertion exits the test
	// early; otherwise g.User1's key material stays corrupted for later tests
	defer copy(inserted.PublicKey, originalPublicKey)

	// mutate the backing arrays of the block the caller still holds
	inserted.Amount.SetInt64(1)
	inserted.Data[0] ^= 0xff
	inserted.Signature[0] ^= 0xff
	inserted.PublicKey[0] ^= 0xff

	uncommitted := z.Chain().GetUncommittedAccountBlocksByAddress(g.User1.Address)
	common.Expect(t, len(uncommitted), 1)
	cached := uncommitted[0]
	common.ExpectAmount(t, cached.Amount, originalAmount)
	common.ExpectTrue(t, bytes.Equal(cached.Data, originalData))
	common.ExpectTrue(t, bytes.Equal(cached.Signature, originalSignature))
	common.ExpectTrue(t, bytes.Equal(cached.PublicKey, originalPublicKey))

	// mutate the backing arrays of the returned copy
	cached.Amount.SetInt64(2)
	cached.Data[0] ^= 0xff
	cached.Signature[0] ^= 0xff
	cached.PublicKey[0] ^= 0xff

	uncommitted = z.Chain().GetUncommittedAccountBlocksByAddress(g.User1.Address)
	common.Expect(t, len(uncommitted), 1)
	common.ExpectAmount(t, uncommitted[0].Amount, originalAmount)
	common.ExpectTrue(t, bytes.Equal(uncommitted[0].Data, originalData))
	common.ExpectTrue(t, bytes.Equal(uncommitted[0].Signature, originalSignature))
	common.ExpectTrue(t, bytes.Equal(uncommitted[0].PublicKey, originalPublicKey))
}

func TestAccountPool_CachedDescendantBlocksAreDefensiveCopies(t *testing.T) {
	z := mock.NewMockZenon(t)
	defer z.StopPanic()

	z.CallContract(&nom.AccountBlock{
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
	})
	z.InsertNewMomentum() // generate contract receive and its descendant block

	byAddress := z.Chain().GetUncommittedAccountBlocksByAddress(types.TokenContract)
	var parent *nom.AccountBlock
	for _, block := range byAddress {
		if len(block.DescendantBlocks) > 0 {
			parent = block
		}
	}
	common.ExpectTrue(t, parent != nil)
	originalDescendantHash := parent.DescendantBlocks[0].Hash

	parent.DescendantBlocks[0].Hash = types.ZeroHash

	byAddress = z.Chain().GetUncommittedAccountBlocksByAddress(types.TokenContract)
	found := false
	for _, block := range byAddress {
		if block.Hash == parent.Hash {
			found = true
			common.ExpectString(t, block.DescendantBlocks[0].Hash.String(), originalDescendantHash.String())
		}
	}
	common.ExpectTrue(t, found)
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
