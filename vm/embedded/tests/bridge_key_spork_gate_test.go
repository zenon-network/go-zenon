package tests

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"testing"
	"time"

	ecommon "github.com/ethereum/go-ethereum/common"
	ecrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/secp256k1"

	g "github.com/zenon-network/go-zenon/chain/genesis/mock"
	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	zcrypto "github.com/zenon-network/go-zenon/common/crypto"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/rpc/api/embedded"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/vm/embedded/implementation"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

type testShortCoordKeyPair struct {
	privB64       string
	compressedB64 string
	unpadded      []byte
	padded        []byte
}

// deriveShortCoordKeyPair deterministically scans small non-zero secp256k1
// private scalars until it finds a valid keypair matching the requested
// leading-zero-byte shape on X and/or Y.
func deriveShortCoordKeyPair(t *testing.T, shortX, shortY bool) testShortCoordKeyPair {
	t.Helper()
	var seed [32]byte
	for i := uint32(1); i <= 100000; i++ {
		binary.BigEndian.PutUint32(seed[28:], i)
		priv, err := ecrypto.ToECDSA(seed[:])
		if err != nil {
			continue
		}
		compressed := ecrypto.CompressPubkey(&priv.PublicKey)
		X, Y := secp256k1.DecompressPubkey(compressed)
		if X == nil || Y == nil {
			continue
		}
		xBytes := X.Bytes()
		yBytes := Y.Bytes()
		if (len(xBytes) < 32) == shortX && (len(yBytes) < 32) == shortY {
			unpadded := make([]byte, 1, 65)
			unpadded[0] = 4
			unpadded = append(unpadded, xBytes...)
			unpadded = append(unpadded, yBytes...)

			padded := make([]byte, 1, 65)
			padded[0] = 4
			padded = append(padded, ecommon.LeftPadBytes(xBytes, 32)...)
			padded = append(padded, ecommon.LeftPadBytes(yBytes, 32)...)

			return testShortCoordKeyPair{
				privB64:       base64.StdEncoding.EncodeToString(seed[:]),
				compressedB64: base64.StdEncoding.EncodeToString(compressed),
				unpadded:      unpadded,
				padded:        padded,
			}
		}
	}
	t.Fatalf("failed to derive secp256k1 key pair (shortX=%v, shortY=%v)", shortX, shortY)
	return testShortCoordKeyPair{}
}

func makeOffCurveKeys() (zeroesB64, maxB64 string) {
	zeroes := make([]byte, constants.CompressedECDSAPubKeyLength)
	zeroesB64 = base64.StdEncoding.EncodeToString(zeroes)

	maxBytes := make([]byte, constants.CompressedECDSAPubKeyLength)
	maxBytes[0] = 0x02
	for i := 1; i < len(maxBytes); i++ {
		maxBytes[i] = 0xff
	}
	maxB64 = base64.StdEncoding.EncodeToString(maxBytes)
	return zeroesB64, maxB64
}

func findTimeChallenge(t *testing.T, bridgeAPI *embedded.BridgeApi, methodName string) *definition.TimeChallengeInfo {
	t.Helper()
	challenges, err := bridgeAPI.GetTimeChallengesInfo()
	common.FailIfErr(t, err)
	for _, tc := range challenges.List {
		if tc.MethodName == methodName {
			return tc
		}
	}
	t.Fatalf("time challenge for %q not found", methodName)
	return nil
}

// TestBridgeKeySporkGate_OffCurvePreDP verifies that before DynamicPlasmaSpork
// is active, an off-curve 33-byte ChangeTssECDSAPubKey send is still accepted
// on admission (preserving pre-spork send compatibility) and cleanly rolled
// back with ErrInvalidCompressedECDSAPubKey in ReceiveBlock without panicking.
func TestBridgeKeySporkGate_OffCurvePreDP(t *testing.T) {
	offCurveZeroes, offCurveMax := makeOffCurveKeys()
	cases := []struct {
		name   string
		pubKey string
	}{
		{"zeroes", offCurveZeroes},
		{"max_x", offCurveMax},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
			defer z.StopPanic()
			saveSporkState(t)

			activateBridgeStep3(t, z)

			active, err := z.Chain().GetFrontierMomentumStore().IsSporkActive(types.DynamicPlasmaSpork)
			common.FailIfErr(t, err)
			common.Expect(t, false, active)

			bridgeAPI := embedded.NewBridgeApi(z)
			infoBefore, err := bridgeAPI.GetBridgeInfo()
			common.FailIfErr(t, err)

			// CallContract inserts the send block expecting nil error at applySend,
			// proving pre-spork send admission accepts the 33-byte payload.
			expecter := z.CallContract(changeTssWithAdministratorStep(g.User5.Address, tc.pubKey))

			// Grab the newly inserted send block hash from the sender's frontier
			// so we can inspect the generated ContractReceive after momentum production.
			sendFrontier, err := z.Chain().GetFrontierAccountStore(g.User5.Address).Frontier()
			common.FailIfErr(t, err)

			insertMomentums(z, 2)

			// ReceiveBlock must unconditionally reject the off-curve key without panicking.
			expecter.Error(t, constants.ErrInvalidCompressedECDSAPubKey)

			// Verify a rolled-back ContractReceive (status = resultFail = 2) was cemented.
			recvBlock, err := z.Chain().GetFrontierMomentumStore().GetBlockWhichReceives(sendFrontier.Hash)
			common.FailIfErr(t, err)
			if recvBlock == nil {
				t.Fatal("expected ContractReceive block for off-curve send, got nil")
			}
			common.Expect(t, nom.BlockTypeContractReceive, recvBlock.BlockType)
			common.Expect(t, sendFrontier.Hash, recvBlock.FromBlockHash)
			if !bytes.Equal(recvBlock.Data, common.Uint64ToBytes(2)) {
				t.Fatalf("expected rolled-back ContractReceive status 2, got %x", recvBlock.Data)
			}

			// Bridge key state must remain unchanged.
			infoAfter, err := bridgeAPI.GetBridgeInfo()
			common.FailIfErr(t, err)
			common.Expect(t, infoBefore.CompressedTssECDSAPubKey, infoAfter.CompressedTssECDSAPubKey)
			common.Expect(t, infoBefore.DecompressedTssECDSAPubKey, infoAfter.DecompressedTssECDSAPubKey)
		})
	}
}

// TestBridgeKeySporkGate_OffCurvePostDP verifies that once DynamicPlasmaSpork
// is active, off-curve 33-byte ChangeTssECDSAPubKey sends are rejected at
// applySend with ErrInvalidCompressedECDSAPubKey for both administrator and
// non-administrator callers.
func TestBridgeKeySporkGate_OffCurvePostDP(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	saveSporkState(t)

	activateBridgeStep3(t, z)
	activateDynamicPlasmaPastHeight(t, z)

	offCurveZeroes, offCurveMax := makeOffCurveKeys()

	// Admin off-curve sends rejected at applySend.
	z.InsertSendBlock(
		changeTssWithAdministratorStep(g.User5.Address, offCurveZeroes),
		constants.ErrInvalidCompressedECDSAPubKey,
		mock.NoVmChanges,
	)
	z.InsertSendBlock(
		changeTssWithAdministratorStep(g.User5.Address, offCurveMax),
		constants.ErrInvalidCompressedECDSAPubKey,
		mock.NoVmChanges,
	)

	// Non-admin off-curve send rejected at applySend.
	z.InsertSendBlock(
		changeTssWithSignature(offCurveZeroes, "", ""),
		constants.ErrInvalidCompressedECDSAPubKey,
		mock.NoVmChanges,
	)
}

// TestBridgeKeySporkGate_OffCurvePreDPSendReceivedPostDP verifies that an
// off-curve ChangeTssECDSAPubKey send admitted on the last pre-spork momentum
// (enforcementHeight - 1) and auto-received once DynamicPlasmaSpork is active
// is still rejected in ReceiveBlock with ErrInvalidCompressedECDSAPubKey and
// leaves bridge state unchanged.
func TestBridgeKeySporkGate_OffCurvePreDPSendReceivedPostDP(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	saveSporkState(t)

	activateBridgeStep3(t, z)

	bridgeAPI := embedded.NewBridgeApi(z)
	infoBefore, err := bridgeAPI.GetBridgeInfo()
	common.FailIfErr(t, err)

	// Schedule DynamicPlasmaSpork and stop at enforcementHeight - 1.
	createSend := z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data: definition.ABISpork.PackMethodPanic(definition.SporkCreateMethodName,
			"dynamic-plasma",
			"Activates Dynamic Plasma",
		),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	id := createSend.Hash

	z.InsertSendBlock(&nom.AccountBlock{
		Address:   g.Spork.Address,
		ToAddress: types.SporkContract,
		Data:      definition.ABISpork.PackMethodPanic(definition.SporkActivateMethodName, id),
	}, nil, mock.SkipVmChanges)
	z.InsertNewMomentum()
	types.DynamicPlasmaSpork.SporkId = id
	types.ImplementedSporksMap[id] = true

	sporkAPI := embedded.NewSporkApi(z)
	sporkList, err := sporkAPI.GetAll(0, 10)
	common.FailIfErr(t, err)
	var enforcementHeight uint64
	for _, s := range sporkList.List {
		if s.Id == id {
			enforcementHeight = s.EnforcementHeight
			break
		}
	}
	if enforcementHeight == 0 {
		t.Fatal("dynamic plasma spork not found in spork list")
	}

	z.InsertMomentumsTo(enforcementHeight - 1)
	activeBefore, err := z.Chain().GetFrontierMomentumStore().IsSporkActive(types.DynamicPlasmaSpork)
	common.FailIfErr(t, err)
	common.Expect(t, false, activeBefore)

	offCurveZeroes, _ := makeOffCurveKeys()
	expecter := z.CallContract(changeTssWithAdministratorStep(g.User5.Address, offCurveZeroes))
	sendFrontier, err := z.Chain().GetFrontierAccountStore(g.User5.Address).Frontier()
	common.FailIfErr(t, err)

	// Advance past enforcementHeight so the auto-receive executes post-spork.
	z.InsertMomentumsTo(enforcementHeight + 1)
	activeAfter, err := z.Chain().GetFrontierMomentumStore().IsSporkActive(types.DynamicPlasmaSpork)
	common.FailIfErr(t, err)
	common.Expect(t, true, activeAfter)

	expecter.Error(t, constants.ErrInvalidCompressedECDSAPubKey)

	recvBlock, err := z.Chain().GetFrontierMomentumStore().GetBlockWhichReceives(sendFrontier.Hash)
	common.FailIfErr(t, err)
	if recvBlock == nil {
		t.Fatal("expected ContractReceive block for cross-boundary off-curve send, got nil")
	}
	common.Expect(t, nom.BlockTypeContractReceive, recvBlock.BlockType)
	if !bytes.Equal(recvBlock.Data, common.Uint64ToBytes(2)) {
		t.Fatalf("expected rolled-back ContractReceive status 2, got %x", recvBlock.Data)
	}

	infoAfter, err := bridgeAPI.GetBridgeInfo()
	common.FailIfErr(t, err)
	common.Expect(t, infoBefore.CompressedTssECDSAPubKey, infoAfter.CompressedTssECDSAPubKey)
	common.Expect(t, infoBefore.DecompressedTssECDSAPubKey, infoAfter.DecompressedTssECDSAPubKey)
}

// TestBridgeKeySporkGate_ShortCoordinatePreDP verifies that before
// DynamicPlasmaSpork is active, ChangeTssECDSAPubKey preserves minimal-width
// X.Bytes()/Y.Bytes() encoding for both the administrator TimeChallenge hash
// and the stored DecompressedTssECDSAPubKey (producing < 65 bytes when a
// coordinate has a leading 0x00 byte).
func TestBridgeKeySporkGate_ShortCoordinatePreDP(t *testing.T) {
	cases := []struct {
		name   string
		shortX bool
		shortY bool
	}{
		{"leading_zero_X", true, false},
		{"leading_zero_Y", false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kp := deriveShortCoordKeyPair(t, tc.shortX, tc.shortY)
			if len(kp.unpadded) >= constants.DecompressedECDSAPubKeyLength {
				t.Fatalf("expected unpadded decompressed key < %d bytes, got %d", constants.DecompressedECDSAPubKeyLength, len(kp.unpadded))
			}

			z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
			defer z.StopPanic()
			saveSporkState(t)

			activateBridgeStep3(t, z)

			active, err := z.Chain().GetFrontierMomentumStore().IsSporkActive(types.DynamicPlasmaSpork)
			common.FailIfErr(t, err)
			common.Expect(t, false, active)

			bridgeAPI := embedded.NewBridgeApi(z)
			securityInfo, err := bridgeAPI.GetSecurityInfo()
			common.FailIfErr(t, err)

			// Step 1: initiate admin TimeChallenge and verify its ParamsHash uses
			// the unpadded pre-spork byte representation.
			step1 := z.CallContract(changeTssWithAdministratorStep(g.User5.Address, kp.compressedB64))
			insertMomentums(z, 2)
			step1.Error(t, nil)

			challenge := findTimeChallenge(t, bridgeAPI, definition.ChangeTssECDSAPubKeyMethodName)
			expectedHash := types.BytesToHashPanic(zcrypto.Hash(kp.unpadded))
			paddedHash := types.BytesToHashPanic(zcrypto.Hash(kp.padded))
			common.Expect(t, expectedHash, challenge.ParamsHash)
			if challenge.ParamsHash == paddedHash {
				t.Fatal("pre-spork TimeChallenge hash unexpectedly matched padded key hash")
			}

			// Step 2: advance past SoftDelay and complete the admin rotation.
			frMom, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
			common.FailIfErr(t, err)
			z.InsertMomentumsTo(frMom.Height + securityInfo.SoftDelay + 2)

			step2 := z.CallContract(changeTssWithAdministratorStep(g.User5.Address, kp.compressedB64))
			insertMomentums(z, 2)
			step2.Error(t, nil)

			info, err := bridgeAPI.GetBridgeInfo()
			common.FailIfErr(t, err)
			common.Expect(t, kp.compressedB64, info.CompressedTssECDSAPubKey)

			stored, err := base64.StdEncoding.DecodeString(info.DecompressedTssECDSAPubKey)
			common.FailIfErr(t, err)
			if len(stored) >= constants.DecompressedECDSAPubKeyLength {
				t.Fatalf("pre-spork stored decompressed key should be < %d bytes, got %d", constants.DecompressedECDSAPubKeyLength, len(stored))
			}
			if !bytes.Equal(stored, kp.unpadded) {
				t.Fatalf("pre-spork stored decompressed key mismatch: got %x, want %x", stored, kp.unpadded)
			}
		})
	}
}

// TestBridgeKeySporkGate_ShortCoordinatePostDP verifies that once
// DynamicPlasmaSpork is active:
//   - the administrator ChangeTssECDSAPubKey path hashes and stores a fixed
//     65-byte left-padded decompressed key (for a leading-zero-X key) that
//     passes CheckECDSASignature; and
//   - the non-administrator AllowKeyGen rotation path rotates from that key to
//     a leading-zero-Y key, verifying both old and new signatures and storing
//     a fixed 65-byte left-padded decompressed key that passes
//     CheckECDSASignature.
func TestBridgeKeySporkGate_ShortCoordinatePostDP(t *testing.T) {
	shortXKP := deriveShortCoordKeyPair(t, true, false)
	shortYKP := deriveShortCoordKeyPair(t, false, true)
	if len(shortXKP.unpadded) >= constants.DecompressedECDSAPubKeyLength {
		t.Fatalf("expected short-X unpadded length < 65, got %d", len(shortXKP.unpadded))
	}
	if len(shortYKP.unpadded) >= constants.DecompressedECDSAPubKeyLength {
		t.Fatalf("expected short-Y unpadded length < 65, got %d", len(shortYKP.unpadded))
	}

	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	saveSporkState(t)

	activateBridgeStep3(t, z)
	activateDynamicPlasmaPastHeight(t, z)

	bridgeAPI := embedded.NewBridgeApi(z)
	securityInfo, err := bridgeAPI.GetSecurityInfo()
	common.FailIfErr(t, err)

	// 1) Admin path with leading-zero-X key: TimeChallenge hash and stored
	// decompressed key must use fixed 65-byte padded coordinates.
	step1 := z.CallContract(changeTssWithAdministratorStep(g.User5.Address, shortXKP.compressedB64))
	insertMomentums(z, 2)
	step1.Error(t, nil)

	challenge := findTimeChallenge(t, bridgeAPI, definition.ChangeTssECDSAPubKeyMethodName)
	expectedPaddedHash := types.BytesToHashPanic(zcrypto.Hash(shortXKP.padded))
	unpaddedHash := types.BytesToHashPanic(zcrypto.Hash(shortXKP.unpadded))
	common.Expect(t, expectedPaddedHash, challenge.ParamsHash)
	if challenge.ParamsHash == unpaddedHash {
		t.Fatal("post-spork TimeChallenge hash unexpectedly matched unpadded key hash")
	}

	frMom, err := z.Chain().GetFrontierMomentumStore().GetFrontierMomentum()
	common.FailIfErr(t, err)
	z.InsertMomentumsTo(frMom.Height + securityInfo.SoftDelay + 2)

	step2 := z.CallContract(changeTssWithAdministratorStep(g.User5.Address, shortXKP.compressedB64))
	insertMomentums(z, 2)
	step2.Error(t, nil)

	infoAfterAdmin, err := bridgeAPI.GetBridgeInfo()
	common.FailIfErr(t, err)
	common.Expect(t, shortXKP.compressedB64, infoAfterAdmin.CompressedTssECDSAPubKey)

	storedAdmin, err := base64.StdEncoding.DecodeString(infoAfterAdmin.DecompressedTssECDSAPubKey)
	common.FailIfErr(t, err)
	common.Expect(t, constants.DecompressedECDSAPubKeyLength, len(storedAdmin))
	if !bytes.Equal(storedAdmin, shortXKP.padded) {
		t.Fatalf("post-spork admin stored key mismatch: got %x, want %x", storedAdmin, shortXKP.padded)
	}

	// Verify CheckECDSASignature succeeds with the stored short-X key.
	digest := zcrypto.Hash([]byte("post-dp-short-x-check"))
	sigX, err := sign(digest, shortXKP.privB64)
	common.FailIfErr(t, err)
	ok, err := implementation.CheckECDSASignature(digest, infoAfterAdmin.DecompressedTssECDSAPubKey, sigX)
	common.FailIfErr(t, err)
	common.Expect(t, true, ok)

	// 2) AllowKeyGen non-admin TSS rotation path: rotate from short-X key to
	// leading-zero-Y key.
	allowStep := z.CallContract(setAllowKeyGen(g.User5.Address, true))
	insertMomentums(z, 2)
	allowStep.Error(t, nil)

	infoBeforeRotate, err := bridgeAPI.GetBridgeInfo()
	common.FailIfErr(t, err)
	common.Expect(t, true, infoBeforeRotate.AllowKeyGen)

	msg, err := implementation.GetChangePubKeyMessage(
		definition.ChangeTssECDSAPubKeyMethodName,
		definition.NoMClass,
		z.Chain().ChainIdentifier(),
		infoBeforeRotate.TssNonce,
		shortYKP.compressedB64,
	)
	common.FailIfErr(t, err)

	oldSig, err := sign(msg, shortXKP.privB64)
	common.FailIfErr(t, err)
	newSig, err := sign(msg, shortYKP.privB64)
	common.FailIfErr(t, err)

	rotateStep := z.CallContract(changeTssWithSignature(shortYKP.compressedB64, oldSig, newSig))
	insertMomentums(z, 2)
	rotateStep.Error(t, nil)

	infoAfterRotate, err := bridgeAPI.GetBridgeInfo()
	common.FailIfErr(t, err)
	common.Expect(t, shortYKP.compressedB64, infoAfterRotate.CompressedTssECDSAPubKey)
	common.Expect(t, false, infoAfterRotate.AllowKeyGen)
	common.Expect(t, infoBeforeRotate.TssNonce+1, infoAfterRotate.TssNonce)

	storedRotate, err := base64.StdEncoding.DecodeString(infoAfterRotate.DecompressedTssECDSAPubKey)
	common.FailIfErr(t, err)
	common.Expect(t, constants.DecompressedECDSAPubKeyLength, len(storedRotate))
	if !bytes.Equal(storedRotate, shortYKP.padded) {
		t.Fatalf("post-spork AllowKeyGen stored key mismatch: got %x, want %x", storedRotate, shortYKP.padded)
	}

	// Verify CheckECDSASignature succeeds with the stored short-Y key.
	digestY := zcrypto.Hash([]byte("post-dp-short-y-check"))
	sigY, err := sign(digestY, shortYKP.privB64)
	common.FailIfErr(t, err)
	ok, err = implementation.CheckECDSASignature(digestY, infoAfterRotate.DecompressedTssECDSAPubKey, sigY)
	common.FailIfErr(t, err)
	common.Expect(t, true, ok)
}
