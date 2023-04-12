package implementation

import (
	"encoding/hex"
	"testing"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

var (
	hashlock, _ = hex.DecodeString("b7845adcd41eec4e4fa1cc75a868014811b575942c6e4a72551bc01f63705634")
	defaultHtlc = definition.CreateHtlcParam{
		HashLocked:     types.HtlcContract,
		ExpirationTime: 1000000000,
		HashType:       0,
		KeyMaxSize:     32,
		HashLock:       hashlock,
	}
)

func TestHtlc_HashType(t *testing.T) {
	htlc := defaultHtlc
	common.ExpectError(t, checkHtlc(htlc), nil)
	htlc.HashType = 1
	common.ExpectError(t, checkHtlc(htlc), nil)
	htlc.HashType = 2
	common.ExpectError(t, checkHtlc(htlc), constants.ErrInvalidHashType)
}

func TestHtlc_LockLength(t *testing.T) {
	htlc := defaultHtlc
	htlc.HashLock = htlc.HashLock[1:]
	common.ExpectError(t, checkHtlc(htlc), constants.ErrInvalidHashDigest)
	htlc.HashType = 1
	common.ExpectError(t, checkHtlc(htlc), constants.ErrInvalidHashDigest)
}
