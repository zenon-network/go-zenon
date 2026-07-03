package types

import (
	"fmt"

	"github.com/pkg/errors"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/crypto"
)

const (
	// ZTSPrefix is the bech32 human-readable part of every Zenon
	// token standard; rendered identifiers therefore start with
	// "zts1" (the "1" is the bech32 separator).
	ZTSPrefix = "zts"
	// ZenonTokenStandardSize is the raw byte length of a
	// ZenonTokenStandard.
	ZenonTokenStandardSize = 10
)

// ZenonTokenStandard is the 10-byte identifier of a ZTS token. New
// identifiers are derived by the token embedded contract from the hash
// of the issuing send block (see NewZenonTokenStandard); the textual
// form is a bech32 string with human-readable part "zts", for example
// "zts1znnxxxxxxxxxxxxx9z4ulx".
type ZenonTokenStandard [ZenonTokenStandardSize]byte

// ZeroTokenStandard is the all-zero token standard, used as the "no
// token" sentinel and as the error return of the parsing functions.
var ZeroTokenStandard = ZenonTokenStandard{}

// ZnnTokenStandard is the token standard of ZNN, the native coin of
// the network.
var ZnnTokenStandard = ParseZTSPanic("zts1znnxxxxxxxxxxxxx9z4ulx")

// QsrTokenStandard is the token standard of QSR, the native coin that
// is fused into plasma and consumed by pillar and sentinel
// registrations.
var QsrTokenStandard = ParseZTSPanic("zts1qsrxxxxxxxxxxxxxmrhjll")

// NewZenonTokenStandard derives a token standard from arbitrary data:
// the first 10 bytes of the SHA3-256 digest over the concatenated data
// slices. The token embedded contract calls it with the hash of the
// send block that issues the token.
func NewZenonTokenStandard(data ...[]byte) ZenonTokenStandard {
	zts, _ := BytesToZTS(crypto.Hash(data...)[0:ZenonTokenStandardSize])
	return zts
}

// SetBytes overwrites the token standard with b, which must be exactly
// ZenonTokenStandardSize (10) bytes; otherwise the token standard is
// left unchanged and an error is returned.
func (zts *ZenonTokenStandard) SetBytes(b []byte) error {
	if length := len(b); length != ZenonTokenStandardSize {
		return fmt.Errorf("invalid ZTS size error %v", length)
	}
	copy(zts[:], b)
	return nil
}

// Bytes returns the 10 raw bytes of the token standard.
func (zts ZenonTokenStandard) Bytes() []byte { return zts[:] }

// String renders the token standard as a bech32 string with
// human-readable part "zts" ("zts1..."). It is the inverse of
// ParseZTS.
func (zts ZenonTokenStandard) String() string {
	s, err := formatBech32(ZTSPrefix, zts[:])
	if err != nil {
		panic(err)
	}
	return s
}

// BytesToZTS builds a ZenonTokenStandard from its 10 raw bytes. It
// returns the zero token standard and an error if b is not exactly
// ZenonTokenStandardSize bytes.
func BytesToZTS(b []byte) (ZenonTokenStandard, error) {
	var zts ZenonTokenStandard
	err := zts.SetBytes(b)
	return zts, err
}

// BytesToZTSPanic is like BytesToZTS but panics on invalid input.
func BytesToZTSPanic(b []byte) ZenonTokenStandard {
	var zts ZenonTokenStandard
	common.DealWithErr(zts.SetBytes(b))
	return zts
}

// ParseZTS decodes a bech32 token standard string. It returns
// ZeroTokenStandard and an error if the string is not valid bech32
// (including checksum failures), if the human-readable part is not
// "zts", or if the decoded payload is not exactly 10 bytes.
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

// ParseZTSPanic is like ParseZTS but panics on invalid input. It is
// meant for hard-coded token standards known to be valid.
func ParseZTSPanic(ztsString string) ZenonTokenStandard {
	zts, err := ParseZTS(ztsString)
	if err != nil {
		panic(errors.Errorf("failed to parse %v; reason %v", ztsString, err))
	}
	return zts
}

// MarshalText implements encoding.TextMarshaler. The token standard is
// rendered as its bech32 string, so it appears in JSON as a quoted
// "zts1..." string.
func (zts ZenonTokenStandard) MarshalText() ([]byte, error) {
	return []byte(zts.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler by parsing a
// bech32 token standard string with ParseZTS. On error the token
// standard is left unchanged.
func (zts *ZenonTokenStandard) UnmarshalText(input []byte) error {
	raw, err := ParseZTS(string(input))
	if err != nil {
		return err
	}
	return zts.SetBytes(raw.Bytes())
}
