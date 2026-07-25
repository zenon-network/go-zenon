package types

import (
	"crypto/ed25519"
	"testing"
)

func TestValidAddress(t *testing.T) {
	testAddress := "qwertyiou"
	if _, err := ParseAddress(testAddress); err == nil {
		t.Errorf("test 1")
	}

	// changed some letters => wrong checksum
	testAddress = "z1qqeurma4yh0d5wd3ysluzc30gxp63cwvuqz076"
	if _, err := ParseAddress(testAddress); err == nil {
		t.Errorf("changed some letters => wrong checksum")
	}

	// changed checksum
	testAddress = "z1qqeu4amryh0d5wd3ysluzc30gxp63cwvuqz055"
	if _, err := ParseAddress(testAddress); err == nil {
		t.Errorf("changed checksum")
	}

	//changed length
	testAddress = "z1qqeu4amryh0d5wd33yysysluzc30gxp63cwvuqz076"
	if _, err := ParseAddress(testAddress); err == nil {
		t.Errorf("changed length")
	}

	// no hrp
	testAddress = "1qqzheltgher090k5ums7avs20uugsqa66e8zkhx"
	if _, err := ParseAddress(testAddress); err == nil {
		t.Errorf("no hrp")
	}

	// no delimiter
	testAddress = "zzqzheltgher090k5ums7avs20uugsqa66e8zkhx"
	if _, err := ParseAddress(testAddress); err == nil {
		t.Errorf("no delimiter")
	}

	testAddress = "z1qprpnyv6xmc4mu5d405jgxjqfd79ggf8fjdewr"
	if addr, err := ParseAddress(testAddress); err != nil || IsEmbeddedAddress(addr) {
		t.Errorf("good account address")
	}

	// pillar contract address
	testAddress = PillarContract.String()
	if addr, err := ParseAddress(testAddress); err != nil || !IsEmbeddedAddress(addr) {
		t.Errorf("pillar contract address")
	}
}

func TestPubKeyToAddress(t *testing.T) {
	publicKeyBytes := []byte{233, 58, 2, 195, 155, 8, 2, 46, 13, 5, 226, 101, 53, 104, 117, 74, 104, 122, 37, 184, 121, 65, 147, 88, 241, 163, 160, 40, 41, 165, 29, 62}
	testAddress := PubKeyToAddress(publicKeyBytes)
	testAddressString := testAddress.String()
	if testAddressString != "z1qry3r6n4adzwlyqrm6e2s8hz4kff9uzmkqjnqy" {
		t.Errorf("good address")
	}
	publicKeyBytes = publicKeyBytes[1:]
	testAddress = PubKeyToAddress(publicKeyBytes)
	testAddressString = testAddress.String()
	if testAddressString != "z1qpg720fhchs4rud5v6zk4w6ch35yjvmyr7hee8" {
		t.Errorf("one byte less")
	}

	publicKeyBytes = []byte{}
	testAddress = PubKeyToAddress(publicKeyBytes)
	testAddressString = testAddress.String()
	if testAddressString != "z1qznll3hchu0dwej3c9r4dgrp6e30tq8l7qv2em" {
		t.Errorf("0 byte array")
	}
}

func TestMultisigCreationToAddress(t *testing.T) {
	pubKey1, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	pubKey2, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	// deterministic: same (pubKey, nonce) -> same address every call
	addrA := MultisigCreationToAddress(pubKey1, 0)
	addrB := MultisigCreationToAddress(pubKey1, 0)
	if addrA != addrB {
		t.Errorf("expected deterministic derivation, got %v != %v", addrA, addrB)
	}

	// distinct nonces (same pubkey) -> distinct addresses
	addrNonce1 := MultisigCreationToAddress(pubKey1, 1)
	if addrA == addrNonce1 {
		t.Errorf("expected distinct addresses for distinct nonces, got the same address %v", addrA)
	}

	// distinct pubkeys (same nonce) -> distinct addresses
	addrOtherKey := MultisigCreationToAddress(pubKey2, 0)
	if addrA == addrOtherKey {
		t.Errorf("expected distinct addresses for distinct pubkeys, got the same address %v", addrA)
	}

	// leading byte is MultisigAddrByte
	if addrA[0] != MultisigAddrByte {
		t.Errorf("expected leading byte %v, got %v", MultisigAddrByte, addrA[0])
	}
}

func TestIsMultisigAddress(t *testing.T) {
	pubKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	multisigAddr := MultisigCreationToAddress(pubKey, 0)
	if !IsMultisigAddress(multisigAddr) {
		t.Errorf("expected derived multisig address to be recognised as a multisig address")
	}

	userAddr := PubKeyToAddress(pubKey)
	if IsMultisigAddress(userAddr) {
		t.Errorf("expected a user address to not be recognised as a multisig address")
	}

	if IsMultisigAddress(PillarContract) {
		t.Errorf("expected an embedded contract address to not be recognised as a multisig address")
	}

	if IsMultisigAddress(MultisigContract) {
		t.Errorf("expected the multisig registry contract address itself to not be recognised as a multisig account address")
	}
}

func TestMultisigContractAddress(t *testing.T) {
	if !IsEmbeddedAddress(MultisigContract) {
		t.Errorf("expected MultisigContract to be a valid embedded address")
	}

	for _, addr := range EmbeddedContracts {
		if addr == MultisigContract {
			continue
		}
		if addr == ZeroAddress {
			t.Errorf("unexpected zero address in EmbeddedContracts")
		}
	}

	seen := map[Address]bool{}
	for _, addr := range EmbeddedContracts {
		if seen[addr] {
			t.Errorf("duplicate address %v in EmbeddedContracts", addr)
		}
		seen[addr] = true
	}

	found := false
	for _, addr := range EmbeddedContracts {
		if addr == MultisigContract {
			found = true
		}
	}
	if !found {
		t.Errorf("expected MultisigContract to be present in EmbeddedContracts")
	}
}
