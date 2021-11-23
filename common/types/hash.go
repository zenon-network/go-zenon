package types

import (
	"encoding/hex"
	"fmt"

	"github.com/zenon-network/go-zenon/common/crypto"
)

const (
	HashSize = 32
)

type Hash [HashSize]byte

var ZeroHash = Hash{}

func NewHash(data []byte) Hash {
	h, _ := BytesToHash(crypto.Hash(data))
	return h
}

func (h *Hash) SetBytes(b []byte) error {
	if len(b) != HashSize {
		return fmt.Errorf("error hash size %v", len(b))
	}
	copy(h[:], b)
	return nil
}
func (h Hash) Bytes() []byte {
	return h[:]
}
func (h Hash) IsZero() bool {
	return h == ZeroHash
}
func (h Hash) String() string {
	return hex.EncodeToString(h[:])
}

func BytesToHash(b []byte) (Hash, error) {
	var h Hash
	err := h.SetBytes(b)
	return h, err
}
func BytesToHashPanic(b []byte) Hash {
	h, err := BytesToHash(b)
	if err != nil {
		panic(err)
	}
	return h
}
func HexToHash(hexStr string) (Hash, error) {
	if len(hexStr) != 2*HashSize {
		return Hash{}, fmt.Errorf("error hex hash size %v", len(hexStr))
	}
	b, err := hex.DecodeString(hexStr)
	if err != nil {
		return Hash{}, err
	}
	return BytesToHash(b)
}
func HexToHashPanic(hexStr string) Hash {
	h, err := HexToHash(hexStr)
	if err != nil {
		panic(err)
	}
	return h
}

func (h *Hash) UnmarshalText(input []byte) error {
	hash, e := HexToHash(string(input))
	if e != nil {
		return e
	}
	return h.SetBytes(hash.Bytes())
}
func (h Hash) MarshalText() ([]byte, error) {
	return []byte(h.String()), nil
}

func (h *Hash) Proto() *HashProto {
	return &HashProto{
		Hash: h[:],
	}
}
func DeProtoHash(pb *HashProto) *Hash {
	if len(pb.Hash) != HashSize {
		panic(fmt.Sprintf("invalid DeProto - wanted hash size %v but got %v", HashSize, len(pb.Hash)))
	}
	h := new(Hash)
	copy(h[:], pb.Hash)
	return h
}
