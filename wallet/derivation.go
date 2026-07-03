package wallet

import (
	"bytes"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
)

const (
	// ZenonAccountPathFormat is the BIP-44 derivation path for Zenon
	// accounts, with a single %d verb for the account index. Coin type
	// 73404 is registered to Zenon; every segment is hardened, as
	// required by SLIP-0010 ed25519 derivation.
	ZenonAccountPathFormat = "m/44'/73404'/%d'"
	// FirstHardenedIndex is the SLIP-0010 offset (2^31) added to a path
	// segment to form its hardened child index. ed25519 supports only
	// hardened derivation, so every derived index is at or above it.
	FirstHardenedIndex = uint32(0x80000000)
	// seedModifier is the SLIP-0010 HMAC-SHA512 key used to derive the
	// ed25519 master key from the seed.
	seedModifier = "ed25519 seed"
)

var (
	pathRegex = regexp.MustCompile("^m(\\/[0-9]+')+$")
)

type key struct {
	Key       []byte
	ChainCode []byte
}

func (k key) toKeyPair() (*KeyPair, error) {
	public, private, err := ed25519.GenerateKey(bytes.NewReader(k.Key))
	if err != nil {
		return nil, err
	}
	address := types.PubKeyToAddress(public)
	return &KeyPair{
		Public:  public,
		Private: private,
		Address: address,
	}, nil
}

// DeriveForPath derives key for chain path in BIP-44 format and chain seed.
// Ed25119 derivation operated on hardened keys only.
func DeriveForPath(path string, seed []byte) (*KeyPair, error) {
	if !isValidPath(path) {
		return nil, ErrInvalidPath
	}

	key, err := newMasterKey(seed)
	if err != nil {
		return nil, err
	}

	segments := strings.Split(path, "/")
	for _, segment := range segments[1:] {
		i64, err := strconv.ParseUint(strings.TrimRight(segment, "'"), 10, 32)
		if err != nil {
			return nil, err
		}

		i := uint32(i64) + FirstHardenedIndex
		key, err = key.derive(i)
		if err != nil {
			return nil, err
		}
	}

	return key.toKeyPair()
}

// DeriveWithIndex derives the key pair for account index i from seed,
// using the standard Zenon path m/44'/73404'/i'. It is shorthand for
// formatting ZenonAccountPathFormat and calling DeriveForPath.
func DeriveWithIndex(i uint32, seed []byte) (*KeyPair, error) {
	path := fmt.Sprintf(ZenonAccountPathFormat, i)
	return DeriveForPath(path, seed)
}

func newMasterKey(seed []byte) (*key, error) {
	newHmac := hmac.New(sha512.New, []byte(seedModifier))
	_, err := newHmac.Write(seed)
	if err != nil {
		return nil, err
	}
	sum := newHmac.Sum(nil)
	key := &key{
		Key:       sum[:32],
		ChainCode: sum[32:],
	}
	return key, nil
}
func (k *key) derive(i uint32) (*key, error) {
	// no public derivation for ed25519
	if i < FirstHardenedIndex {
		return nil, ErrNoPublicDerivation
	}

	iBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(iBytes, i)
	data := common.JoinBytes([]byte{0x0}, k.Key, iBytes)

	newHmac := hmac.New(sha512.New, k.ChainCode)
	_, err := newHmac.Write(data)
	if err != nil {
		return nil, err
	}
	sum := newHmac.Sum(nil)
	return &key{
		Key:       sum[:32],
		ChainCode: sum[32:],
	}, nil
}

func isValidPath(path string) bool {
	if !pathRegex.MatchString(path) {
		return false
	}

	// Check for overflows
	segments := strings.Split(path, "/")
	for _, segment := range segments[1:] {
		_, err := strconv.ParseUint(strings.TrimRight(segment, "'"), 10, 32)
		if err != nil {
			return false
		}
	}

	return true
}
