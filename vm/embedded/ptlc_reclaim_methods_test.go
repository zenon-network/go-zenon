package embedded

import (
	"bytes"
	"testing"

	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/embedded/implementation"
)

type ptlcSporkContext struct {
	testTrailingBytesContext
	ptlcEnforced bool
}

func (ctx *ptlcSporkContext) IsPtlcSporkEnforced() bool { return ctx.ptlcEnforced }

func TestPtlcReclaimLockMethodsBehindPtlcSpork(t *testing.T) {
	calls := map[string][]byte{
		definition.CreateWithReclaimLockPtlcMethodName: definition.ABIPtlc.PackMethodPanic(definition.CreateWithReclaimLockPtlcMethodName,
			int64(1000), definition.PointTypeED25519, bytes.Repeat([]byte{1}, 32), types.ZeroAddress, definition.PointTypeED25519, bytes.Repeat([]byte{2}, 32)),
		definition.ReclaimWithSignaturePtlcMethodName: definition.ABIPtlc.PackMethodPanic(definition.ReclaimWithSignaturePtlcMethodName,
			types.NewHash([]byte("id")), bytes.Repeat([]byte{3}, 64)),
	}

	for name, data := range calls {
		_, err := GetEmbeddedMethod(&ptlcSporkContext{ptlcEnforced: false}, types.PtlcContract, data)
		if err != constants.ErrContractDoesntExist {
			t.Errorf("%s before the PTLC spork: got %v, want %v", name, err, constants.ErrContractDoesntExist)
		}

		enforced := &ptlcSporkContext{ptlcEnforced: true}
		method, err := GetEmbeddedMethod(enforced, types.PtlcContract, data)
		if err != nil {
			t.Fatalf("%s after the PTLC spork: %v", name, err)
		}
		switch name {
		case definition.CreateWithReclaimLockPtlcMethodName:
			if _, ok := method.(*implementation.CreateWithReclaimLockPtlcMethod); !ok {
				t.Errorf("%s resolves to %T", name, method)
			}
		case definition.ReclaimWithSignaturePtlcMethodName:
			if _, ok := method.(*implementation.ReclaimWithSignaturePtlcMethod); !ok {
				t.Errorf("%s resolves to %T", name, method)
			}
		}

		if err := CheckNoTrailingBytes(enforced, types.PtlcContract, data); err != nil {
			t.Errorf("%s, clean: got %v, want nil", name, err)
		}
		if err := CheckNoTrailingBytes(enforced, types.PtlcContract, append(append([]byte{}, data...), 0)); err != constants.ErrABDataTrailingBytes {
			t.Errorf("%s, one trailing byte: got %v, want %v", name, err, constants.ErrABDataTrailingBytes)
		}
	}
}
