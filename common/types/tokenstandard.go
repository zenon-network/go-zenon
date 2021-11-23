package types

import (
	"fmt"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/crypto"
)

const (
	ZTSPrefix              = "zts"
	ZenonTokenStandardSize = 10
)

type ZenonTokenStandard [ZenonTokenStandardSize]byte

var ZeroTokenStandard = ZenonTokenStandard{}
var ZnnTokenStandard = ParseZTSPanic("zts1znnxxxxxxxxxxxxx9z4ulx")
var QsrTokenStandard = ParseZTSPanic("zts1qsrxxxxxxxxxxxxxmrhjll")

func NewZenonTokenStandard(data ...[]byte) ZenonTokenStandard {
	zts, _ := BytesToZTS(crypto.Hash(data...)[0:ZenonTokenStandardSize])
	return zts
}

func (zts *ZenonTokenStandard) SetBytes(b []byte) error {
	if length := len(b); length != ZenonTokenStandardSize {
		return fmt.Errorf("invalid ZTS size error %v", length)
	}
	copy(zts[:], b)
	return nil
}
func (zts ZenonTokenStandard) Bytes() []byte { return zts[:] }
func (zts ZenonTokenStandard) String() string {
	s, err := formatBech32(ZTSPrefix, zts[:])
	if err != nil {
		panic(err)
	}
	return s
}

func BytesToZTS(b []byte) (ZenonTokenStandard, error) {
	var zts ZenonTokenStandard
	err := zts.SetBytes(b)
	return zts, err
}
func BytesToZTSPanic(b []byte) ZenonTokenStandard {
	var zts ZenonTokenStandard
	common.DealWithErr(zts.SetBytes(b))
	return zts
}
func ParseZTS(ztsString string) (ZenonTokenStandard, error) {
	hrp, data, err := parseBech32(ztsString)
	if err != nil {
		return ZeroTokenStandard, err
	}

	if hrp != ZTSPrefix {
		return ZeroTokenStandard, fmt.Errorf("invalid ZTS String prefix %v", hrp)
	}

	var zts ZenonTokenStandard
	err = zts.SetBytes(data)
	if err != nil {
		return ZeroTokenStandard, err
	}
	return zts, nil
}
func ParseZTSPanic(ztsString string) ZenonTokenStandard {
	zts, err := ParseZTS(ztsString)
	if err != nil {
		panic(errors.Errorf("failed to parse %v; reason %v", ztsString, err))
	}
	return zts
}

func (zts ZenonTokenStandard) MarshalText() ([]byte, error) {
	return []byte(zts.String()), nil
}
func (zts *ZenonTokenStandard) UnmarshalText(input []byte) error {
	raw, err := ParseZTS(string(input))
	if err != nil {
		return err
	}
	return zts.SetBytes(raw.Bytes())
}
