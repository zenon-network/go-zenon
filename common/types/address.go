package types

import (
	"bytes"
	"fmt"

	"github.com/btcsuite/btcd/btcutil/bech32"
	"golang.org/x/crypto/sha3"
)

const (
	AddressPrefix   = "z"
	AddressSize     = 1 + AddressCoreSize
	AddressCoreSize = 19
)

const (
	UserAddrByte     = byte(0)
	ContractAddrByte = byte(1)
)

var (
	PillarContract      = parseEmbedded("z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg")
	PlasmaContract      = parseEmbedded("z1qxemdeddedxplasmaxxxxxxxxxxxxxxxxsctrp")
	StakeContract       = parseEmbedded("z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62")
	SporkContract       = parseEmbedded("z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48")
	TokenContract       = parseEmbedded("z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0")
	SentinelContract    = parseEmbedded("z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r")
	SwapContract        = parseEmbedded("z1qxemdeddedxswapxxxxxxxxxxxxxxxxxxl4yww")
	LiquidityContract   = parseEmbedded("z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae")
	AcceleratorContract = parseEmbedded("z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22")
	HtlcContract        = parseEmbedded("z1qxemdeddedxhtlcxxxxxxxxxxxxxxxxxygecvw")
	BridgeContract      = parseEmbedded("z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d")

	EmbeddedContracts = []Address{PlasmaContract, PillarContract, TokenContract, SentinelContract, SwapContract, StakeContract, SporkContract, LiquidityContract, AcceleratorContract, HtlcContract, BridgeContract}
	EmbeddedWUpdate   = []Address{PillarContract, StakeContract, SentinelContract, LiquidityContract, AcceleratorContract}

	SporkAddress *Address
)

func IsEmbeddedAddress(addr Address) bool {
	return addr[0] == ContractAddrByte
}

type Address [AddressSize]byte

var ZeroAddress = Address{}

func (addr *Address) SetBytes(b []byte) error {
	if length := len(b); length != AddressSize {
		return fmt.Errorf("error address size  %v", length)
	}
	copy(addr[:], b)
	return nil
}
func (addr Address) Bytes() []byte { return addr[:] }
func (addr Address) IsZero() bool {
	return bytes.Equal(addr.Bytes(), ZeroAddress.Bytes())
}
func (addr Address) String() string {
	s, err := formatBech32(AddressPrefix, addr[:])
	if err != nil {
		panic(err)
	}
	return s
}

func BytesToAddress(b []byte) (Address, error) {
	var a Address
	err := a.SetBytes(b)
	return a, err
}
func ParseAddress(addrStr string) (Address, error) {
	hrp, b, err := parseBech32(addrStr)
	if err != nil {
		return ZeroAddress, err
	}

	if hrp != AddressPrefix {
		return ZeroAddress, fmt.Errorf("invalid address prefix %v", hrp)
	}

	var addr Address
	err = addr.SetBytes(b)
	return addr, err
}
func ParseAddressPanic(addrStr string) Address {
	addr, err := ParseAddress(addrStr)
	if err != nil {
		panic(err)
	}
	return addr
}
func parseEmbedded(addrStr string) Address {
	a, err := ParseAddress(addrStr)
	if err != nil {
		panic(fmt.Sprintf("Address %v err %v", addrStr, err))
	}
	if !IsEmbeddedAddress(a) {
		panic(fmt.Sprintf("Address %v is not a contract address", addrStr))
	}
	return a
}

func PubKeyToAddress(pubKey []byte) Address {
	hash := sha3.Sum256(pubKey)
	var addr Address
	err := addr.SetBytes(append([]byte{UserAddrByte}, hash[:AddressCoreSize]...))
	if err != nil {
		panic(err)
	}
	return addr
}

func (addr Address) MarshalText() ([]byte, error) {
	return []byte(addr.String()), nil
}
func (addr *Address) UnmarshalText(input []byte) error {
	addresses, err := ParseAddress(string(input))
	if err != nil {
		return err
	}
	err = addr.SetBytes(addresses.Bytes())
	return err
}

func (addr *Address) Proto() *AddressProto {
	return &AddressProto{
		Address: addr[:],
	}
}
func DeProtoAddress(pb *AddressProto) *Address {
	if len(pb.Address) != AddressSize {
		panic(fmt.Sprintf("invalid DeProto - wanted hash size %v but got %v", HashSize, len(pb.Address)))
	}
	addr := new(Address)
	copy(addr[:], pb.Address)
	return addr
}

// parseBech32 takes a bech32 address as input and returns the HRP and data
// section of a bech32 address
func parseBech32(addrStr string) (string, []byte, error) {
	rawHRP, decoded, err := bech32.Decode(addrStr)
	if err != nil {
		return "", nil, err
	}
	addrBytes, err := bech32.ConvertBits(decoded, 5, 8, true)
	if err != nil {
		return "", nil, fmt.Errorf("unable to convert address from 5-bit to 8-bit formatting")
	}
	return rawHRP, addrBytes, nil
}

// formatBech32 takes an address's bytes as input and returns a bech32 address
func formatBech32(hrp string, payload []byte) (string, error) {
	fiveBits, err := bech32.ConvertBits(payload, 8, 5, true)
	if err != nil {
		return "", fmt.Errorf("unable to convert address from 8-bit to 5-bit formatting")
	}
	addr, err := bech32.Encode(hrp, fiveBits)
	if err != nil {
		return "", err
	}
	return addr, nil
}
