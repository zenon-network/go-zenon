package definition

import (
	"encoding/binary"
	"math/big"
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/vm/abi"
)

func aliasWord(n int) []byte {
	b := make([]byte, abi.WordSize)
	binary.BigEndian.PutUint64(b[abi.WordSize-8:], uint64(n))
	return b
}

// aliasedSetTokenTuple encodes a SetTokenTuple call whose tokenStandards
// array has n offsets naming one payload, with the three numeric arrays
// empty: the four head offsets, the string array (count, n offsets all
// equal to n*32, the payload's length word and right-padded bytes) and
// three zero counts.
func aliasedSetTokenTuple(n int, payload string) []byte {
	padded := make([]byte, (len(payload)+abi.WordSize-1)/abi.WordSize*abi.WordSize)
	copy(padded, payload)
	stringsLen := abi.WordSize + n*abi.WordSize + abi.WordSize + len(padded)

	var enc []byte
	enc = append(enc, ABILiquidity.Methods[SetTokenTupleMethodName].Id()...)
	head := 4 * abi.WordSize
	enc = append(enc, aliasWord(head)...)
	enc = append(enc, aliasWord(head+stringsLen)...)
	enc = append(enc, aliasWord(head+stringsLen+abi.WordSize)...)
	enc = append(enc, aliasWord(head+stringsLen+2*abi.WordSize)...)
	enc = append(enc, aliasWord(n)...)
	for i := 0; i < n; i++ {
		enc = append(enc, aliasWord(n*abi.WordSize)...)
	}
	enc = append(enc, aliasWord(len(payload))...)
	enc = append(enc, padded...)
	enc = append(enc, aliasWord(0)...)
	enc = append(enc, aliasWord(0)...)
	enc = append(enc, aliasWord(0)...)
	return enc
}

// The one method input with a dynamic-element array is SetTokenTuple's
// tokenStandards. An encoding whose offsets alias one payload is refused
// by the decoder (issue #269) instead of decoding into n copies of it;
// the canonical encoding of the same call still decodes.
func TestSetTokenTuple_AliasedTokenStandardsAreRejected(t *testing.T) {
	payload := strings.Repeat("zts1q", 400)

	param := new(TokenTuplesParam)
	err := ABILiquidity.UnpackMethod(param, SetTokenTupleMethodName, aliasedSetTokenTuple(1000, payload))
	if err == nil {
		t.Fatalf("decoded %d token standards from an aliased message; want an error", len(param.TokenStandards))
	}
	if !strings.Contains(err.Error(), "alias") {
		t.Fatalf("unexpected error: %v", err)
	}

	// With one element the fixture is the canonical encoding.
	param = new(TokenTuplesParam)
	common.FailIfErr(t, ABILiquidity.UnpackMethod(param, SetTokenTupleMethodName, aliasedSetTokenTuple(1, payload)))
	if len(param.TokenStandards) != 1 || param.TokenStandards[0] != payload {
		t.Fatalf("decoded %q", param.TokenStandards)
	}

	canonical, err := ABILiquidity.PackMethod(SetTokenTupleMethodName,
		[]string{"zts1a", "zts1b"}, []uint32{5000, 5000}, []uint32{5000, 5000}, []*big.Int{big.NewInt(1), big.NewInt(2)})
	common.FailIfErr(t, err)
	param = new(TokenTuplesParam)
	common.FailIfErr(t, ABILiquidity.UnpackMethod(param, SetTokenTupleMethodName, canonical))
	if len(param.TokenStandards) != 2 || param.TokenStandards[1] != "zts1b" || len(param.MinAmounts) != 2 {
		t.Fatalf("decoded %+v", param)
	}
}
