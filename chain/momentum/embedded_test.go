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

func TestGetJailedPillars_ReturnsOnlyJailedActiveNames(t *testing.T) {
	ms := &momentumStore{DB: db.NewMemDB()}

	jailed, err := ms.GetJailedPillars()
	common.FailIfErr(t, err)
	if jailed == nil || len(jailed) != 0 {
		t.Fatalf("got %v, want a non-nil empty map", jailed)
	}

	storage := account.NewAccountStore(types.PillarContract, ms.DB.Subset(getAccountStorePrefix(types.PillarContract))).Storage()
	common.FailIfErr(t, (&definition.PillarJail{Name: "pillar-a", Jailed: true, Since: 10}).Save(storage))
	common.FailIfErr(t, (&definition.PillarJail{Name: "pillar-b", Jailed: false, Since: 20}).Save(storage))
	common.FailIfErr(t, (&definition.PillarJail{Name: "pillar-d", Jailed: true, Since: 30}).Save(storage))
	common.FailIfErr(t, (&definition.PillarJail{Name: "pillar-e", Jailed: true, Since: 40}).Save(storage))
	for _, name := range []string{"pillar-a", "pillar-b", "pillar-c"} {
		common.FailIfErr(t, (&definition.PillarInfo{Name: name, Amount: big.NewInt(1)}).Save(storage))
	}
	// pillar-d is revoked; pillar-e has no pillar record at all.
	common.FailIfErr(t, (&definition.PillarInfo{Name: "pillar-d", Amount: big.NewInt(1), RevokeTime: 50}).Save(storage))

	jailed, err = ms.GetJailedPillars()
	common.FailIfErr(t, err)
	if len(jailed) != 1 || !jailed["pillar-a"] {
		t.Fatalf("got %v, want map[pillar-a:true]", jailed)
	}
}
