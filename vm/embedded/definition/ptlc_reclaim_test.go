package definition

import (
	"bytes"
	"encoding/hex"
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/abi"
	"github.com/zenon-network/go-zenon/vm/constants"
)

const jsonPtlcBeforeReclaimLock = `
	[
		{"type":"function","name":"Create", "inputs":[
			{"name":"expirationTime","type":"int64"},
			{"name":"pointType","type":"uint8"},
			{"name":"pointLock","type":"bytes"},
			{"name":"destination","type":"address"}
		]},
		{"type":"function","name":"Reclaim","inputs":[
			{"name":"id","type":"hash"}
		]},
		{"type":"function","name":"Unlock","inputs":[
			{"name":"id","type":"hash"},
			{"name":"signature","type":"bytes"}
		]},
		{"type":"function","name":"ProxyUnlock","inputs":[
			{"name":"id","type":"hash"},
			{"name":"destination","type":"address"},
			{"name":"signature","type":"bytes"}
		]},
		{"type":"variable","name":"ptlcInfo","inputs":[
			{"name":"timeLocked","type":"address"},
			{"name":"tokenStandard","type":"tokenStandard"},
			{"name":"amount","type":"uint256"},
			{"name":"expirationTime", "type":"int64"},
			{"name":"pointType","type":"uint8"},
			{"name":"pointLock","type":"bytes"},
			{"name":"destination","type":"address"}
		]}
	]`

func syntheticBytes(t *testing.T, encoded string, size int) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(encoded)
	if err != nil || len(decoded) != size {
		t.Fatalf("invalid synthetic field %s", encoded)
	}
	return decoded
}

func TestPtlcReclaimMessageVectors(t *testing.T) {
	var creator, otherCreator, destination types.Address
	copy(creator[:], syntheticBytes(t, "0022222222222222222222222222222222222222", types.AddressSize))
	copy(otherCreator[:], syntheticBytes(t, "0033333333333333333333333333333333333333", types.AddressSize))
	copy(destination[:], syntheticBytes(t, "0044444444444444444444444444444444444444", types.AddressSize))
	var token, otherToken types.ZenonTokenStandard
	copy(token[:], bytes.Repeat([]byte{0x55}, types.ZenonTokenStandardSize))
	copy(otherToken[:], bytes.Repeat([]byte{0x66}, types.ZenonTokenStandardSize))
	rampLock := syntheticBytes(t, "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f", 32)

	terms := func() *PtlcInfo {
		return &PtlcInfo{
			Id:             types.BytesToHashPanic(bytes.Repeat([]byte{0x11}, types.HashSize)),
			TimeLocked:     creator,
			TokenStandard:  token,
			Amount:         big.NewInt(0x3b9aca00),
			ExpirationTime: 1700000000,
			PointType:      PointTypeED25519,
			PointLock:      bytes.Repeat([]byte{0x11}, 32),
			Destination:    destination,
		}
	}

	tests := []struct {
		name        string
		chain       uint64
		reclaimType uint8
		edit        func(*PtlcInfo)
		want        string
	}{
		{"base", 69, PointTypeED25519, func(*PtlcInfo) {}, "b6fd4d7c9f732211766f9e04cbcfc7ed0d57771cddf601645b71ed11fa7e692b"},
		{"chain-zero", 0, PointTypeED25519, func(*PtlcInfo) {}, "056ef85081335a8a1507920693dbd6266adbe9ba354114a80326fb70501f5dd7"},
		{"chain-max-uint64", math.MaxUint64, PointTypeED25519, func(*PtlcInfo) {}, "b4fa3ba079ea07710c03e82ce84521ddeb1f01d05fc5228bbfbe3ed4f99617bd"},
		{"chain-byte-order", 0x0102030405060708, PointTypeED25519, func(*PtlcInfo) {}, "3f7b83457ddfabfb9ea4ddad347e18c7d91b8ffbd29c5d96ee8e0f9a7d979fd6"},
		{"bip340-reclaim", 69, PointTypeBIP340, func(*PtlcInfo) {}, "1cf43f99a8331e729eb7c76ed5370ca466ecb593635ce75e8d491cda7475cadf"},
		{"creator", 69, PointTypeED25519, func(e *PtlcInfo) { e.TimeLocked = otherCreator }, "70e4b33c933980d87ad8c6551f6c4033062bdb62817f3241b27e11db31b8da56"},
		{"open-destination", 69, PointTypeED25519, func(e *PtlcInfo) { e.Destination = types.ZeroAddress }, "09f9fe595a23a31c62a140993bcce9990f06578e1d7b996792520f5798f2b96b"},
		{"token", 69, PointTypeED25519, func(e *PtlcInfo) { e.TokenStandard = otherToken }, "78187a3e26b9373bb2bf72f4c22a5e2f26653d058983c767536d19326615b070"},
		{"amount-one", 69, PointTypeED25519, func(e *PtlcInfo) { e.Amount = big.NewInt(1) }, "b51053faab6fac9c5fea266d01c9dfc497ce14dbe5230632c672ca496a3c2698"},
		{"amount-max", 69, PointTypeED25519, func(e *PtlcInfo) { e.Amount = new(big.Int).SetBytes(bytes.Repeat([]byte{0xff}, 32)) }, "a05d1cda114918a1ffdce6f4e8294ab2156c1a16deebaae7e001548abf12f596"},
		{"expiration-byte-order", 69, PointTypeED25519, func(e *PtlcInfo) { e.ExpirationTime = 0x0102030405060708 }, "ef413d368802d2c5c130cfa81214934e612a197558d0a036ffd2838f68e1ded2"},
		{"secp256k1-point", 69, PointTypeED25519, func(e *PtlcInfo) {
			e.PointType = PointTypeSecp256k1Point
			e.PointLock = append([]byte{0x02}, bytes.Repeat([]byte{0x11}, 32)...)
		}, "e9f7486805038bdff0696b86b4a169399f2353fe09ed9c4c0262ab8b4a650ac5"},
		{"point-lock", 69, PointTypeED25519, func(e *PtlcInfo) { e.PointLock = rampLock }, "6ba40a6c48d53de71ae09c2b32638ec174c0d629c5f1c17eaa29e9b5eb0e84c7"},
	}

	seen := make(map[string]string)
	for _, test := range tests {
		if other, ok := seen[test.want]; ok {
			t.Fatalf("%s repeats the vector of %s", test.name, other)
		}
		seen[test.want] = test.name

		t.Run(test.name, func(t *testing.T) {
			entry := terms()
			test.edit(entry)

			message := GetPtlcReclaimMessage(test.chain, test.reclaimType, entry)
			if got := hex.EncodeToString(message); got != test.want {
				t.Fatalf("reclaim message = %s, want %s", got, test.want)
			}

			entry.Id = types.NewHash([]byte("another entry with the same terms"))
			if !bytes.Equal(GetPtlcReclaimMessage(test.chain, test.reclaimType, entry), message) {
				t.Fatal("reclaim message depends on the entry id")
			}
			if bytes.Equal(message, GetPtlcUnlockMessage(test.chain, test.reclaimType, entry.Id, entry.TimeLocked)) || bytes.Equal(message, GetPtlcUnlockMessage(test.chain, entry.PointType, entry.Id, entry.Destination)) {
				t.Fatal("reclaim message equals an unlock message")
			}
		})
	}
}

func TestPtlcAbiBeforeReclaimLockUnchanged(t *testing.T) {
	before := abi.JSONToABIContract(strings.NewReader(jsonPtlcBeforeReclaimLock))

	for _, name := range []string{CreatePtlcMethodName, ReclaimPtlcMethodName, UnlockPtlcMethodName, ProxyUnlockPtlcMethodName} {
		oldMethod, ok := before.Methods[name]
		if !ok {
			t.Fatalf("%s missing from the old ABI", name)
		}
		newMethod, ok := ABIPtlc.Methods[name]
		if !ok {
			t.Fatalf("%s missing from the ABI", name)
		}
		if !bytes.Equal(oldMethod.Id(), newMethod.Id()) {
			t.Errorf("%s selector changed: %x, was %x", name, newMethod.Id(), oldMethod.Id())
		}
	}

	for _, name := range []string{CreateWithReclaimLockPtlcMethodName, ReclaimWithSignaturePtlcMethodName} {
		newMethod, ok := ABIPtlc.Methods[name]
		if !ok {
			t.Fatalf("%s missing from the ABI", name)
		}
		for oldName, oldMethod := range before.Methods {
			if bytes.Equal(oldMethod.Id(), newMethod.Id()) {
				t.Errorf("%s selector collides with %s", name, oldName)
			}
		}
	}

	info := &PtlcInfo{
		Id:             types.NewHash([]byte("encoding")),
		TimeLocked:     types.PtlcContract,
		TokenStandard:  types.ZnnTokenStandard,
		Amount:         big.NewInt(123456789),
		ExpirationTime: 1000000300,
		PointType:      PointTypeED25519,
		PointLock:      bytes.Repeat([]byte{7}, 32),
		Destination:    types.PtlcContract,
	}
	oldData, err := before.PackVariable(variableNamePtlcInfo, info.TimeLocked, info.TokenStandard, info.Amount, info.ExpirationTime, info.PointType, info.PointLock, info.Destination)
	common.FailIfErr(t, err)

	store := db.NewMemDB()
	common.FailIfErr(t, info.Save(store))
	newData, err := store.Get(getPtlcInfoKey(info.Id))
	common.FailIfErr(t, err)
	if !bytes.Equal(oldData, newData) {
		t.Fatalf("stored entry encoding changed: %x, was %x", newData, oldData)
	}

	decoded := new(PtlcInfo)
	common.FailIfErr(t, ABIPtlc.UnpackVariable(decoded, variableNamePtlcInfo, oldData))
	decoded.Id = info.Id
	if decoded.String() != info.String() {
		t.Fatalf("old entry decodes as %s, want %s", decoded, info)
	}
}

func TestPtlcReclaimLockStorage(t *testing.T) {
	store := db.DisableNotFound(db.NewMemDB())
	id := types.NewHash([]byte("reclaim-lock"))
	info := &PtlcInfo{
		Id:             id,
		TimeLocked:     types.PtlcContract,
		TokenStandard:  types.ZnnTokenStandard,
		Amount:         big.NewInt(1),
		ExpirationTime: 1000000300,
		PointType:      PointTypeED25519,
		PointLock:      bytes.Repeat([]byte{7}, 32),
	}
	common.FailIfErr(t, info.Save(store))

	_, err := GetPtlcReclaimLock(store, id)
	common.ExpectError(t, err, constants.ErrDataNonExistent)

	lock := &PtlcReclaimLock{Id: id, PointType: PointTypeBIP340, PointLock: bytes.Repeat([]byte{9}, 32)}
	common.FailIfErr(t, lock.Save(store))

	got, err := GetPtlcReclaimLock(store, id)
	common.FailIfErr(t, err)
	if got.Id != id || got.PointType != lock.PointType || !bytes.Equal(got.PointLock, lock.PointLock) {
		t.Fatalf("reclaim lock = %s, want %s", got, lock)
	}

	stored, err := GetPtlcInfo(store, id)
	common.FailIfErr(t, err)
	if stored.String() != info.String() {
		t.Fatalf("entry = %s, want %s", stored, info)
	}

	_, err = GetPtlcReclaimLock(store, types.NewHash([]byte("other")))
	common.ExpectError(t, err, constants.ErrDataNonExistent)

	common.FailIfErr(t, lock.Delete(store))
	_, err = GetPtlcReclaimLock(store, id)
	common.ExpectError(t, err, constants.ErrDataNonExistent)
	_, err = GetPtlcInfo(store, id)
	common.FailIfErr(t, err)

	if bytes.Equal(getPtlcReclaimLockKey(id), getPtlcInfoKey(id)) || isPtlcInfoKey(getPtlcReclaimLockKey(id)) {
		t.Fatal("reclaim lock key overlaps the entry key space")
	}
}
