package implementation

import (
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/wallet"
)

var (
	User1, _ = wallet.DeriveWithIndex(1, hexutil.MustDecode("0x01234567890123456789012345678902"))

	defaultPtlc = definition.CreatePtlcParam{
		ExpirationTime: 1000000000,
		PointType:      0,
		PointLock:      User1.Public,
	}
)

func TestPtlc_PointType(t *testing.T) {
	ptlc := defaultPtlc
	common.ExpectError(t, checkPtlc(ptlc), nil)
	ptlc.PointType = 1
	common.ExpectError(t, checkPtlc(ptlc), nil)
	ptlc.PointType = 2
	common.ExpectError(t, checkPtlc(ptlc), constants.ErrInvalidPointType)
}

func TestPtlc_LockLength(t *testing.T) {
	ptlc := defaultPtlc
	ptlc.PointLock = ptlc.PointLock[1:]
	common.ExpectError(t, checkPtlc(ptlc), constants.ErrInvalidPointLock)
	ptlc.PointType = 1
	common.ExpectError(t, checkPtlc(ptlc), constants.ErrInvalidPointLock)
}
