// Package types defines the foundational identity and wire types of the
// Network of Momentum that appear throughout the node and in every RPC
// payload: bech32-encoded account addresses (Address, human-readable
// part "z"), 32-byte SHA3-256 hashes (Hash), hash-height block
// identifiers (HashHeight, AccountHeader), token identifiers
// (ZenonTokenStandard, human-readable part "zts"), pillar delegation
// weights used by consensus (PillarDelegation), and the registry of
// protocol upgrades implemented by this build (ImplementedSpork).
//
// The identifier types carry both a protobuf codec (Proto/DeProto
// pairs, used for storage and the wire) and text-based JSON
// marshalling (bech32 strings for addresses and token standards, bare
// lowercase hex for hashes). The package also declares the well-known
// addresses of the embedded contracts and the well-known ZNN and QSR
// token standards.
package types

import (
	"bytes"
	"fmt"

	"github.com/btcsuite/btcd/btcutil/bech32"
	"golang.org/x/crypto/sha3"
)

const (
	// AddressPrefix is the bech32 human-readable part of every Zenon
	// address; rendered addresses therefore start with "z1" (the "1"
	// is the bech32 separator).
	AddressPrefix = "z"
	// AddressSize is the raw byte length of an Address: one class
	// byte followed by the 19-byte core.
	AddressSize = 1 + AddressCoreSize
	// AddressCoreSize is the byte length of the address core that
	// follows the class byte. For user addresses the core is the
	// first 19 bytes of the SHA3-256 digest of the public key.
	AddressCoreSize = 19
)

const (
	// UserAddrByte is the class byte of user (key-pair backed)
	// addresses.
	UserAddrByte = byte(0)
	// ContractAddrByte is the class byte of embedded-contract
	// addresses.
	ContractAddrByte = byte(1)
)

var (
	// PillarContract is the address of the embedded contract that
	// registers pillars and distributes their rewards.
	PillarContract = parseEmbedded("z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg")
	// PlasmaContract is the address of the embedded contract that
	// fuses QSR into plasma, the anti-spam resource required to issue
	// account blocks.
	PlasmaContract = parseEmbedded("z1qxemdeddedxplasmaxxxxxxxxxxxxxxxxsctrp")
	// StakeContract is the address of the embedded contract that
	// stakes ZNN for QSR rewards.
	StakeContract = parseEmbedded("z1qxemdeddedxstakexxxxxxxxxxxxxxxxjv8v62")
	// SporkContract is the address of the embedded contract that
	// creates and activates sporks, the protocol-upgrade mechanism.
	SporkContract = parseEmbedded("z1qxemdeddedxsp0rkxxxxxxxxxxxxxxxx956u48")
	// TokenContract is the address of the embedded contract that
	// issues, mints and burns ZTS tokens.
	TokenContract = parseEmbedded("z1qxemdeddedxt0kenxxxxxxxxxxxxxxxxh9amk0")
	// SentinelContract is the address of the embedded contract that
	// registers sentinels and distributes their rewards.
	SentinelContract = parseEmbedded("z1qxemdeddedxsentynelxxxxxxxxxxxxxwy0r2r")
	// SwapContract is the address of the embedded contract that
	// redeems legacy-network balances into ZNN and QSR.
	SwapContract = parseEmbedded("z1qxemdeddedxswapxxxxxxxxxxxxxxxxxxl4yww")
	// LiquidityContract is the address of the embedded contract that
	// distributes rewards to liquidity providers.
	LiquidityContract = parseEmbedded("z1qxemdeddedxlyquydytyxxxxxxxxxxxxflaaae")
	// AcceleratorContract is the address of the embedded contract that
	// funds Accelerator-Z projects through pillar voting.
	AcceleratorContract = parseEmbedded("z1qxemdeddedxaccelerat0rxxxxxxxxxxp4tk22")
	// HtlcContract is the address of the embedded contract that locks
	// funds in hashed time-locked contracts.
	HtlcContract = parseEmbedded("z1qxemdeddedxhtlcxxxxxxxxxxxxxxxxxygecvw")
	// BridgeContract is the address of the embedded contract that
	// wraps and unwraps assets across the multichain bridge.
	BridgeContract = parseEmbedded("z1qxemdeddedxdrydgexxxxxxxxxxxxxxxmqgr0d")

	// EmbeddedContracts lists the addresses of all embedded contracts.
	// The pillar worker walks this list to auto-generate the receive
	// blocks for transactions sent to the contracts.
	EmbeddedContracts = []Address{PlasmaContract, PillarContract, TokenContract, SentinelContract, SwapContract, StakeContract, SporkContract, LiquidityContract, AcceleratorContract, HtlcContract, BridgeContract}
	// EmbeddedWUpdate lists the embedded contracts that expose a
	// periodic Update method; pillars send Update transactions to
	// these addresses when enough time has passed since the last
	// update (see pillar/worker_updater.go).
	EmbeddedWUpdate = []Address{PillarContract, StakeContract, SentinelContract, LiquidityContract, AcceleratorContract}

	// SporkAddress is the address authorized to create and activate
	// sporks on the current network. It is nil until chain
	// initialization copies it from the genesis configuration (see
	// chain/chain.go).
	SporkAddress *Address

	// CommunitySporkAddress is a community-held address that is
	// additionally authorized to administer sporks within a fixed
	// momentum-height window, as a temporary measure until an embedded
	// governance contract is taken into use. The address belongs to
	// the Mariposa01 pillar.
	CommunitySporkAddress = ParseAddressPanic("z1qqvwzz2xq7q5gwk6uhcddgrpxlfcyzc8rsu82s")
)

// IsEmbeddedAddress reports whether addr belongs to the
// embedded-contract address class, i.e. its class byte is
// ContractAddrByte. It does not check that an embedded contract is
// actually deployed at the address.
func IsEmbeddedAddress(addr Address) bool {
	return addr[0] == ContractAddrByte
}

// Address is a 20-byte Network of Momentum account address: a class
// byte (UserAddrByte or ContractAddrByte) followed by a 19-byte core.
// User addresses are derived from an Ed25519 public key via
// PubKeyToAddress; embedded-contract addresses are fixed constants.
// The textual form is a bech32 string with human-readable part "z",
// for example "z1qxemdeddedxpyllarxxxxxxxxxxxxxxxsy3fmg".
type Address [AddressSize]byte

// ZeroAddress is the all-zero Address, used as the "no address"
// sentinel and as the error return of the parsing functions.
var ZeroAddress = Address{}

// SetBytes overwrites the address with b, which must be exactly
// AddressSize (20) bytes; otherwise the address is left unchanged and
// an error is returned.
func (addr *Address) SetBytes(b []byte) error {
	if length := len(b); length != AddressSize {
		return fmt.Errorf("error address size  %v", length)
	}
	copy(addr[:], b)
	return nil
}

// Bytes returns the 20 raw bytes of the address.
func (addr Address) Bytes() []byte { return addr[:] }

// IsZero reports whether the address equals ZeroAddress.
func (addr Address) IsZero() bool {
	return bytes.Equal(addr.Bytes(), ZeroAddress.Bytes())
}

// String renders the address as a bech32 string with human-readable
// part "z" ("z1..."). It is the inverse of ParseAddress.
func (addr Address) String() string {
	s, err := formatBech32(AddressPrefix, addr[:])
	if err != nil {
		panic(err)
	}
	return s
}

// BytesToAddress builds an Address from its 20 raw bytes. It returns
// ZeroAddress and an error if b is not exactly AddressSize bytes.
func BytesToAddress(b []byte) (Address, error) {
	var a Address
	err := a.SetBytes(b)
	return a, err
}

// ParseAddress decodes a bech32 address string. It returns ZeroAddress
// and an error if the string is not valid bech32 (including checksum
// failures), if the human-readable part is not "z", or if the decoded
// payload is not exactly 20 bytes.
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

// ParseAddressPanic is like ParseAddress but panics on invalid input.
// It is meant for hard-coded addresses known to be valid.
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

// PubKeyToAddress derives the user address that corresponds to an
// Ed25519 public key: the UserAddrByte class byte followed by the
// first 19 bytes of the SHA3-256 digest of the public key.
func PubKeyToAddress(pubKey []byte) Address {
	hash := sha3.Sum256(pubKey)
	var addr Address
	err := addr.SetBytes(append([]byte{UserAddrByte}, hash[:AddressCoreSize]...))
	if err != nil {
		panic(err)
	}
	return addr
}

// MarshalText implements encoding.TextMarshaler. The address is
// rendered as its bech32 string, so it appears in JSON as a quoted
// "z1..." string.
func (addr Address) MarshalText() ([]byte, error) {
	return []byte(addr.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler by parsing a
// bech32 address string with ParseAddress. On error the address is
// left unchanged.
func (addr *Address) UnmarshalText(input []byte) error {
	addresses, err := ParseAddress(string(input))
	if err != nil {
		return err
	}
	err = addr.SetBytes(addresses.Bytes())
	return err
}

// Proto wraps the raw address bytes in their protobuf message, used
// when embedding addresses in serialized chain structures.
func (addr *Address) Proto() *AddressProto {
	return &AddressProto{
		Address: addr[:],
	}
}

// DeProtoAddress is the inverse of Proto. It panics if the protobuf
// payload is not exactly AddressSize bytes.
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
