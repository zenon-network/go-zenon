package definition

import (
	"bytes"
	"encoding/hex"
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

func TestPtlcReclaimMessageVectors(t *testing.T) {
	const (
		entry        = "1111111111111111111111111111111111111111111111111111111111111111"
		rampEntry    = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
		creator      = "0022222222222222222222222222222222222222"
		otherCreator = "0033333333333333333333333333333333333333"
	)

	tests := []struct {
		name      string
		chain     uint64
		pointType uint8
		entry     string
		creator   string
		want      string
	}{
		{"chain-zero", 0, PointTypeED25519, entry, creator, "e708fe2634b991394f18ab443c75dcb218bc97782e10cd057e4026385452084f"},
		{"chain-one", 1, PointTypeED25519, entry, creator, "905d7c8c513583767b8c5a519782deb330b03c071590b38d1ec484a5df239f05"},
		{"chain-69", 69, PointTypeED25519, entry, creator, "ea9d13d3024b9a1873de80cd3ea13b8226facb1a54163652f6003aae94a54e71"},
		{"chain-max-uint64", 18446744073709551615, PointTypeED25519, entry, creator, "731e459a4539b03642ad07488ce235f44a762693b6f5e3d85ae9ba7fc2014e47"},
		{"chain-byte-order", 0x0102030405060708, PointTypeED25519, entry, creator, "70019a2d7b6f930f8c23d56f838de854bb367ad31e33e46c6863274d7ee77147"},
		{"bip340", 69, PointTypeBIP340, entry, creator, "983daf2916e27c8010fd9fd4d25740f3bb12efdba4802e898c1ce7c17b4f373c"},
		{"entry-identifier", 69, PointTypeED25519, rampEntry, creator, "eaf6a230fc5df8aa5946a18b6aee0a82b8daabf01e8fc8913b48b11dd7b83be7"},
		{"creator", 69, PointTypeED25519, entry, otherCreator, "ea48557fcb9e91fa687c379dae86c168284c9768fb2be6fee5c9f1494ae74172"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			idBytes, err := hex.DecodeString(test.entry)
			if err != nil || len(idBytes) != types.HashSize {
				t.Fatal("invalid synthetic entry ID")
			}
			creatorBytes, err := hex.DecodeString(test.creator)
			if err != nil || len(creatorBytes) != types.AddressSize {
				t.Fatal("invalid synthetic creator")
			}
			var id types.Hash
			var timeLocked types.Address
			copy(id[:], idBytes)
			copy(timeLocked[:], creatorBytes)

			got := hex.EncodeToString(GetPtlcReclaimMessage(test.chain, test.pointType, id, timeLocked))
			if got != test.want {
				t.Fatalf("reclaim message = %s, want %s", got, test.want)
			}
			if bytes.Equal(GetPtlcReclaimMessage(test.chain, test.pointType, id, timeLocked), GetPtlcUnlockMessage(test.chain, test.pointType, id, timeLocked)) {
				t.Fatal("reclaim message equals the unlock message over the same fields")
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
