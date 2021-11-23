package types

import (
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
