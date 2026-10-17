package embedded

import (
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	api "github.com/zenon-network/go-zenon/consensus/api"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/vm_context"
)

// testTrailingBytesContext implements vm_context.AccountVmContext with
// configurable spork enforcement flags.
type testTrailingBytesContext struct {
	store.Account
	dpEnforced bool
}

func (ctx *testTrailingBytesContext) CacheStore() store.Cache       { return nil }
func (ctx *testTrailingBytesContext) MomentumStore() store.Momentum { return nil }
func (ctx *testTrailingBytesContext) GetFrontierMomentum() (*nom.Momentum, error) {
	return nil, nil
}
func (ctx *testTrailingBytesContext) GetGenesisMomentum() *nom.Momentum { return nil }
func (ctx *testTrailingBytesContext) Save()                             {}
func (ctx *testTrailingBytesContext) Reset()                            {}
func (ctx *testTrailingBytesContext) Done()                             {}
func (ctx *testTrailingBytesContext) AddBalance(*types.ZenonTokenStandard, *big.Int) {}
func (ctx *testTrailingBytesContext) SubBalance(*types.ZenonTokenStandard, *big.Int) {}
func (ctx *testTrailingBytesContext) IsAcceleratorSporkEnforced() bool       { return true }
func (ctx *testTrailingBytesContext) IsHtlcSporkEnforced() bool              { return true }
func (ctx *testTrailingBytesContext) IsBridgeAndLiquiditySporkEnforced() bool { return true }
func (ctx *testTrailingBytesContext) IsDynamicPlasmaSporkEnforced() bool      { return ctx.dpEnforced }
func (ctx *testTrailingBytesContext) IsPtlcSporkEnforced() bool               { return true }
func (ctx *testTrailingBytesContext) GetPillarWeights() (map[string]*big.Int, error) {
	return nil, nil
}
func (ctx *testTrailingBytesContext) EpochTicker() common.Ticker { return nil }
func (ctx *testTrailingBytesContext) EpochStats(uint64) (*api.EpochStats, error) {
	return nil, nil
}
func (ctx *testTrailingBytesContext) GetPillarDelegationsByEpoch(uint64) (map[string]*types.PillarDelegationDetail, error) {
	return nil, nil
}

// Ensure testTrailingBytesContext satisfies the interface.
var _ vm_context.AccountVmContext = (*testTrailingBytesContext)(nil)

func TestCheckNoTrailingBytes_CleanData(t *testing.T) {
	ctx := &testTrailingBytesContext{dpEnforced: true}

	// Plasma.Fuse(address) — canonical encoding.
	beneficiary := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	data := definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, beneficiary)

	if err := CheckNoTrailingBytes(ctx, types.PlasmaContract, data); err != nil {
		t.Errorf("clean Fuse call: got %v, want nil", err)
	}
}

func TestCheckNoTrailingBytes_TrailingBytes(t *testing.T) {
	ctx := &testTrailingBytesContext{dpEnforced: true}

	beneficiary := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	clean := definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, beneficiary)

	// Append one trailing byte.
	withTrailing := append(append([]byte{}, clean...), 0x00)
	err := CheckNoTrailingBytes(ctx, types.PlasmaContract, withTrailing)
	if err != constants.ErrABDataTrailingBytes {
		t.Errorf("Fuse call with 1 trailing byte: got %v, want %v", err, constants.ErrABDataTrailingBytes)
	}

	// Append 1 MiB of trailing bytes.
	withLotsOfTrailing := append(append([]byte{}, clean...), make([]byte, 1<<20)...)
	err = CheckNoTrailingBytes(ctx, types.PlasmaContract, withLotsOfTrailing)
	if err != constants.ErrABDataTrailingBytes {
		t.Errorf("Fuse call with 1 MiB trailing: got %v, want %v", err, constants.ErrABDataTrailingBytes)
	}
}

func TestCheckNoTrailingBytes_NonEmbeddedAddress(t *testing.T) {
	ctx := &testTrailingBytesContext{dpEnforced: true}

	userAddr := types.ParseAddressPanic("z1qph8dkja68pg3g6j4spwk9re0kjdkul0amwqnt")
	// Non-embedded address: always nil (the check only applies to embedded contracts).
	if err := CheckNoTrailingBytes(ctx, userAddr, []byte("any data")); err != nil {
		t.Errorf("non-embedded address: got %v, want nil", err)
	}
}

func TestCheckNoTrailingBytes_ShortData(t *testing.T) {
	ctx := &testTrailingBytesContext{dpEnforced: true}

	// Data ≤ 4 bytes: can't contain a method selector + arguments.
	if err := CheckNoTrailingBytes(ctx, types.PlasmaContract, []byte{0x01, 0x02}); err != nil {
		t.Errorf("short data: got %v, want nil", err)
	}
}

func TestCheckNoTrailingBytes_UnknownMethod(t *testing.T) {
	ctx := &testTrailingBytesContext{dpEnforced: true}

	// Unknown method selector: CheckNoTrailingBytes returns nil (lets
	// ValidateSendBlock report the actual error).
	badSelector := append([]byte{0xde, 0xad, 0xbe, 0xef}, make([]byte, 32)...)
	if err := CheckNoTrailingBytes(ctx, types.PlasmaContract, badSelector); err != nil {
		t.Errorf("unknown method: got %v, want nil", err)
	}
}
