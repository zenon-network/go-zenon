package cache

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/zenon-network/go-zenon/chain/cache/storage"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

func getMockPatch() db.Patch {
	patch := db.NewPatch()
	fusedPlasmaKey, _ := hex.DecodeString("0301b3b6e5adcb4c1ff61be98c6318c6318c6318c60402aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	patch.Put(fusedPlasmaKey, []byte{1})
	sporkKey, _ := hex.DecodeString("0301b3b6e5adcb4d00bc76318c6318c6318c6318c60401aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	patch.Put(sporkKey, []byte{1})
	chainPlasmaKey, _ := hex.DecodeString("03aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa05")
	patch.Put(chainPlasmaKey, []byte{1})
	return patch
}

func getMockIdentifier(height uint64) types.HashHeight {
	return types.HashHeight{
		Hash:   types.NewHash([]byte(fmt.Sprint(height))),
		Height: height,
	}
}

func TestExtractor(t *testing.T) {
	dir := t.TempDir()
	m := storage.NewCacheDBManager(dir)
	defer m.Stop()

	identifier := types.ZeroHashHeight
	cs := NewCacheStore(identifier, m)

	changes := getMockPatch()
	extractor := &cacheExtractor{cache: cs, height: 1, patch: db.NewPatch()}
	if err := changes.Replay(extractor); err != nil {
		t.Fatal(err)
	}
	common.ExpectString(t, db.DebugPatch(extractor.patch), `
0300aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa0000000000000001 - 01
0400aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa0000000000000001 - 01
0301aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa0000000000000001 - 01
`)

	identifier = getMockIdentifier(1)
	m.Add(identifier, extractor.patch)

	common.ExpectString(t, db.DebugDB(m.DB()), `
00 - 0a220a2067b176705b46206614219f47a05aee7ae6a3edbe850bbbe214c536b989aea4d21001
0300aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa0000000000000001 - 01
0301aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa0000000000000001 - 01
0400aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa0000000000000001 - 01
`)

	cs = NewCacheStore(identifier, m)
	changes = getMockPatch()
	extractor = &cacheExtractor{cache: cs, height: 2, patch: db.NewPatch()}
	if err := changes.Replay(extractor); err != nil {
		t.Fatal(err)
	}
	common.ExpectString(t, db.DebugPatch(extractor.patch), `
0300aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa0000000000000002 - 01
0400aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa0000000000000002 - 01
`)

	identifier = getMockIdentifier(2)
	m.Add(identifier, extractor.patch)

	common.ExpectString(t, db.DebugDB(m.DB()), `
00 - 0a220a20b1b1bd1ed240b1496c81ccf19ceccf2af6fd24fac10ae42023628abbe26873101002
0300aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa0000000000000001 - 01
0300aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa0000000000000002 - 01
0301aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa0000000000000001 - 01
0400aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa0000000000000001 - 01
0400aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa0000000000000002 - 01
`)
}
