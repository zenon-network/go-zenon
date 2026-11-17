package wallet

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
)

func TestDeriveMultisigAddress_MatchesCanonicalDerivation(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	common.FailIfErr(t, err)

	got := DeriveMultisigAddress(pub, 7)
	want := types.MultisigCreationToAddress(pub, 7)
	if got != want {
		t.Fatalf("DeriveMultisigAddress = %v, want %v", got, want)
	}

	// distinct nonce -> distinct address
	if other := DeriveMultisigAddress(pub, 8); other == got {
		t.Fatalf("expected distinct addresses for distinct nonces, got the same: %v", other)
	}
}

func TestNewCreateMultisigTemplate_BuildsExpectedBlock(t *testing.T) {
	creator := types.PubKeyToAddress(make([]byte, ed25519.PublicKeySize))
	s1, _, err := ed25519.GenerateKey(rand.Reader)
	common.FailIfErr(t, err)
	s2, _, err := ed25519.GenerateKey(rand.Reader)
	common.FailIfErr(t, err)

	block := NewCreateMultisigTemplate(creator, 1, 2, []ed25519.PublicKey{s1, s2})
	if block.Address != creator {
		t.Fatalf("unexpected Address: %v", block.Address)
	}
	if block.ToAddress != types.MultisigContract {
		t.Fatalf("expected ToAddress to be MultisigContract, got %v", block.ToAddress)
	}
	if block.Amount.Cmp(constants.MultisigCreationBurnAmount) != 0 {
		t.Fatalf("expected amount == MultisigCreationBurnAmount, got %v", block.Amount)
	}
	if block.TokenStandard != types.ZnnTokenStandard {
		t.Fatalf("expected TokenStandard == ZNN, got %v", block.TokenStandard)
	}
}

func TestNewChangePolicyTemplate_AddressedFromMultisigAccount(t *testing.T) {
	multisigAddr := DeriveMultisigAddress(make([]byte, ed25519.PublicKeySize), 1)
	s1, _, err := ed25519.GenerateKey(rand.Reader)
	common.FailIfErr(t, err)
	s2, _, err := ed25519.GenerateKey(rand.Reader)
	common.FailIfErr(t, err)

	block := NewChangePolicyTemplate(multisigAddr, 2, []ed25519.PublicKey{s1, s2}, false)
	if block.Address != multisigAddr {
		t.Fatalf("expected Address to be the multisig account itself, got %v", block.Address)
	}
	if block.ToAddress != types.MultisigContract {
		t.Fatalf("expected ToAddress to be MultisigContract, got %v", block.ToAddress)
	}
}

func TestSignMultisigBlock_And_AssembleMultisigAuth(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	common.FailIfErr(t, err)
	kp := &KeyPair{Public: pub, Private: priv}

	block := NewCreateMultisigTemplate(types.ZeroAddress, 1, 2, []ed25519.PublicKey{pub})
	block.Hash = block.ComputeHash()

	sig := SignMultisigBlock(block, kp)
	if !ed25519.Verify(pub, block.Hash.Bytes(), sig) {
		t.Fatalf("expected signature to verify against block.Hash")
	}

	AssembleMultisigAuth(block, [][]byte{sig})
	if len(block.PublicKey) != 0 {
		t.Fatalf("expected PublicKey to be cleared")
	}
	if len(block.Signature) != 0 {
		t.Fatalf("expected Signature to be cleared")
	}
	if block.MultisigAuth == nil || len(block.MultisigAuth.Signatures) != 1 {
		t.Fatalf("expected MultisigAuth with one signature, got %+v", block.MultisigAuth)
	}
}
