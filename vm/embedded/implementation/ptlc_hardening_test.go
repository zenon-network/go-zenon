package implementation

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

func TestPtlc_UnlockWitnessSizeBound(t *testing.T) {
	id := types.NewHash([]byte("witness-size"))
	methods := map[string]struct {
		method validatePtlcSendBlockMethod
		data   func(witness []byte) []byte
	}{
		"unlock": {
			method: &UnlockPtlcMethod{definition.UnlockPtlcMethodName},
			data: func(witness []byte) []byte {
				return definition.ABIPtlc.PackMethodPanic(definition.UnlockPtlcMethodName, id, witness)
			},
		},
		"proxy unlock": {
			method: &ProxyUnlockPtlcMethod{definition.ProxyUnlockPtlcMethodName},
			data: func(witness []byte) []byte {
				return definition.ABIPtlc.PackMethodPanic(definition.ProxyUnlockPtlcMethodName, id, User1.Address, witness)
			},
		},
	}
	sizes := map[int]error{
		0:                           constants.ErrInvalidPointSignature,
		31:                          constants.ErrInvalidPointSignature,
		32:                          nil,
		33:                          constants.ErrInvalidPointSignature,
		63:                          constants.ErrInvalidPointSignature,
		64:                          nil,
		65:                          constants.ErrInvalidPointSignature,
		constants.MaxDataLength + 1: constants.ErrInvalidPointSignature,
		1 << 20:                     constants.ErrInvalidPointSignature,
	}

	for name, m := range methods {
		for size, want := range sizes {
			block := &nom.AccountBlock{Amount: big.NewInt(0), Data: m.data(bytes.Repeat([]byte{1}, size))}
			sent := len(block.Data)
			err := m.method.ValidateSendBlock(block)
			if err != want {
				t.Errorf("%s, %d-byte witness: got %v, want %v", name, size, err, want)
			}
			if want != nil && len(block.Data) != sent {
				t.Errorf("%s, %d-byte witness: the refused block was repacked", name, size)
			}
		}
	}
}
