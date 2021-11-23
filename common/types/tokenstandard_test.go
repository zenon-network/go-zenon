package types

import (
	"encoding/hex"
	"testing"

	"github.com/zenon-network/go-zenon/common"
)

var (
	ppBytes, _ = hex.DecodeString("b520b446d56ef2c4a1cb")
	ppZTS      = BytesToZTSPanic(ppBytes)
)

func Test_ZTSBech32(t *testing.T) {
	str, err := formatBech32("zts", ZnnTokenStandard.Bytes())
	common.FailIfErr(t, err)
	common.Expect(t, str, "zts1znnxxxxxxxxxxxxx9z4ulx")
	common.Expect(t, ZeroTokenStandard, "zts1qqqqqqqqqqqqqqqqtq587y")
	common.Expect(t, ZnnTokenStandard, "zts1znnxxxxxxxxxxxxx9z4ulx")
	common.Expect(t, QsrTokenStandard, "zts1qsrxxxxxxxxxxxxxmrhjll")
	common.Expect(t, ppZTS, "zts1k5stg3k4dmevfgwtgmg36u")
}
