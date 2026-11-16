package momentum

import (
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/chain/account"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

func TestGetJailedPillars_ReturnsOnlyJailedNames(t *testing.T) {
	ms := &momentumStore{DB: db.NewMemDB()}

	jailed, err := ms.GetJailedPillars()
	common.FailIfErr(t, err)
	if jailed == nil || len(jailed) != 0 {
		t.Fatalf("got %v, want a non-nil empty map", jailed)
	}

	storage := account.NewAccountStore(types.PillarContract, ms.DB.Subset(getAccountStorePrefix(types.PillarContract))).Storage()
	common.FailIfErr(t, (&definition.PillarJail{Name: "pillar-a", Jailed: true, Since: 10}).Save(storage))
	common.FailIfErr(t, (&definition.PillarJail{Name: "pillar-b", Jailed: false, Since: 20}).Save(storage))
	common.FailIfErr(t, (&definition.PillarInfo{Name: "pillar-c", Amount: big.NewInt(1)}).Save(storage))

	jailed, err = ms.GetJailedPillars()
	common.FailIfErr(t, err)
	if len(jailed) != 1 || !jailed["pillar-a"] {
		t.Fatalf("got %v, want map[pillar-a:true]", jailed)
	}
}
