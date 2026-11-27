package vm

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
)

type panicChainForAutoReceive struct {
	chain.Chain
}

func (c *panicChainForAutoReceive) GetFrontierMomentumStore() store.Momentum {
	return nil
}

func TestGenerateAutoReceive_RecoverPanic(t *testing.T) {
	s := NewSupervisor(&panicChainForAutoReceive{}, nil)

	sendBlock := &nom.AccountBlock{
		BlockType: nom.BlockTypeUserSend,
		Address:   types.ParseAddressPanic("z1qzuej4d8292sstg38qpnca9tfpfmglqjryffpv"),
		ToAddress: types.BridgeContract,
		Hash:      types.HexToHashPanic("02aab631b44daf47f68810a416bea4ed3695e89c0ab9668a0ab8acb834dfb901"),
		Height:    2,
	}

	execution, err := s.GenerateAutoReceive(sendBlock)
	common.ExpectError(t, err, constants.ErrVmRunPanic)
	if execution != nil {
		t.Fatalf("expected nil execution on panic recovery, got %+v", execution)
	}
}
