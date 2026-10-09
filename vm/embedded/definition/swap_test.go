package definition

import (
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// An empty-valued key must be skipped instead of aborting the whole listing
// with ErrDataNonExistent (GetLegacyPillarList already guards this way).
func TestGetSwapAssetsSkipsEmptyValues(t *testing.T) {
	context := db.NewMemDB()

	real := &SwapAssets{
		KeyIdHash: types.NewHash([]byte{1}),
		Znn:       big.NewInt(100),
		Qsr:       big.NewInt(200),
	}
	if err := real.Save(context); err != nil {
		t.Fatal(err)
	}
	empty := types.NewHash([]byte{2})
	if err := context.Put(getSwapAssetsKey(empty), []byte{}); err != nil {
		t.Fatal(err)
	}

	list, err := GetSwapAssets(context)
	if err != nil {
		t.Fatalf("GetSwapAssets returned %v", err)
	}
	if len(list) != 1 || list[0].KeyIdHash != real.KeyIdHash {
		t.Fatalf("expected only the real entry, got %v", list)
	}
}
