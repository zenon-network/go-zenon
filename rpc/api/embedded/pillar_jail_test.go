package embedded

import (
	"testing"
	"time"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
	"github.com/zenon-network/go-zenon/zenon/mock"
)

// GetAll reports isJailed from the pillar contract's jail records: true
// only for a record with Jailed set, false for a cleared record and for
// a pillar without a record.
func TestPillarGetAllReportsJailState(t *testing.T) {
	z := mock.NewMockZenonWithCustomEpochDuration(t, time.Hour)
	defer z.StopPanic()
	z.InsertMomentumsTo(10)

	storage := db.NewMemDB()
	owner := types.ParseAddressPanic("z1qzuej4d8292sstg38qpnca9tfpfmglqjryffpv")
	for _, name := range []string{"jailed-pillar", "cleared-pillar", "no-record-pillar"} {
		common.FailIfErr(t, (&definition.PillarInfo{
			Name:                  name,
			StakeAddress:          owner,
			BlockProducingAddress: owner,
			RewardWithdrawAddress: owner,
			Amount:                constants.PillarStakeAmount,
			PillarType:            definition.NormalPillarType,
		}).Save(storage))
	}
	common.FailIfErr(t, (&definition.PillarJail{Name: "jailed-pillar", Jailed: true, Since: 100}).Save(storage))
	common.FailIfErr(t, (&definition.PillarJail{Name: "cleared-pillar", Jailed: false, Since: 200}).Save(storage))

	cz := newCountingZenon(z)
	cz.chain.replaced[types.PillarContract] = storage
	p := NewPillarApi(cz, true)

	list, err := p.GetAll(0, 10)
	common.FailIfErr(t, err)
	want := map[string]bool{"jailed-pillar": true, "cleared-pillar": false, "no-record-pillar": false}
	if len(list.List) != len(want) {
		t.Fatalf("%d pillars listed, want %d", len(list.List), len(want))
	}
	for _, pillar := range list.List {
		if pillar.IsJailed != want[pillar.Name] {
			t.Fatalf("pillar %s isJailed = %v, want %v", pillar.Name, pillar.IsJailed, want[pillar.Name])
		}
	}

	jailed, err := p.GetByName("jailed-pillar")
	common.FailIfErr(t, err)
	if jailed == nil || !jailed.IsJailed {
		t.Fatalf("GetByName(jailed-pillar) = %+v, want isJailed true", jailed)
	}
}
