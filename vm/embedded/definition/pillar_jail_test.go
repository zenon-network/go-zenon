package definition

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
)

func TestPillarJail_SaveAndGetRoundTrip(t *testing.T) {
	store := db.DisableNotFound(db.NewMemDB())

	common.FailIfErr(t, (&PillarJail{Name: "pillar-a", Jailed: true, Since: 1700000000}).Save(store))
	got, err := GetPillarJail(store, "pillar-a")
	common.FailIfErr(t, err)
	if got.Name != "pillar-a" || !got.Jailed || got.Since != 1700000000 {
		t.Fatalf("unexpected record %+v", got)
	}

	common.FailIfErr(t, (&PillarJail{Name: "pillar-a", Jailed: false, Since: 1700000300}).Save(store))
	got, err = GetPillarJail(store, "pillar-a")
	common.FailIfErr(t, err)
	if got.Name != "pillar-a" || got.Jailed || got.Since != 1700000300 {
		t.Fatalf("unexpected record %+v", got)
	}
}

func TestPillarJail_StreakRoundTripAndDelete(t *testing.T) {
	store := db.DisableNotFound(db.NewMemDB())

	want := PillarJail{Name: "pillar-a", MissedSlots: 4, FirstMissTick: 1234}
	common.FailIfErr(t, want.Save(store))
	got, err := GetPillarJail(store, "pillar-a")
	common.FailIfErr(t, err)
	if *got != want {
		t.Fatalf("got record %+v, want %+v", got, want)
	}

	common.FailIfErr(t, got.Delete(store))
	if _, err := GetPillarJail(store, "pillar-a"); err != constants.ErrDataNonExistent {
		t.Fatalf("after delete got error %v, want %v", err, constants.ErrDataNonExistent)
	}
}

func TestPillarJailCursor_RoundTripAndAbsence(t *testing.T) {
	store := db.DisableNotFound(db.NewMemDB())

	if _, err := GetPillarJailCursor(store); err != constants.ErrDataNonExistent {
		t.Fatalf("got error %v, want %v", err, constants.ErrDataNonExistent)
	}
	common.FailIfErr(t, (&PillarJailCursor{NextTick: 288}).Save(store))
	got, err := GetPillarJailCursor(store)
	common.FailIfErr(t, err)
	if got.NextTick != 288 {
		t.Fatalf("got cursor %d, want 288", got.NextTick)
	}

	list, err := GetPillarJailList(store)
	common.FailIfErr(t, err)
	if len(list) != 0 {
		t.Fatalf("the cursor shows up as %d jail records", len(list))
	}
}

func TestPillarJail_GetUnknownNameIsNonExistent(t *testing.T) {
	store := db.DisableNotFound(db.NewMemDB())

	got, err := GetPillarJail(store, "nobody")
	if err != constants.ErrDataNonExistent {
		t.Fatalf("got error %v, want %v", err, constants.ErrDataNonExistent)
	}
	if got != nil {
		t.Fatalf("got record %+v, want nil", got)
	}
}

func TestPillarJail_ListReadsOnlyJailRecords(t *testing.T) {
	store := db.DisableNotFound(db.NewMemDB())

	list, err := GetPillarJailList(store)
	common.FailIfErr(t, err)
	if len(list) != 0 {
		t.Fatalf("empty store lists %d records", len(list))
	}

	common.FailIfErr(t, (&PillarInfo{Name: "pillar-a", Amount: big.NewInt(1)}).Save(store))
	common.FailIfErr(t, (&DelegationInfo{Name: "pillar-a", Backer: types.Address{1}}).Save(store))
	common.FailIfErr(t, (&PillarEpochTerms{Name: "pillar-a"}).Save(store))
	common.FailIfErr(t, (&PillarJail{Name: "pillar-a", Jailed: true, Since: 10}).Save(store))
	common.FailIfErr(t, (&PillarJail{Name: "pillar-b", Jailed: false, Since: 20}).Save(store))

	list, err = GetPillarJailList(store)
	common.FailIfErr(t, err)
	if len(list) != 2 {
		t.Fatalf("listed %d records, want 2", len(list))
	}
	byName := map[string]*PillarJail{}
	for _, jail := range list {
		byName[jail.Name] = jail
	}
	if a := byName["pillar-a"]; a == nil || !a.Jailed || a.Since != 10 {
		t.Fatalf("unexpected pillar-a record %+v", a)
	}
	if b := byName["pillar-b"]; b == nil || b.Jailed || b.Since != 20 {
		t.Fatalf("unexpected pillar-b record %+v", b)
	}

	pillars, err := GetPillarsList(store, false, AnyPillarType)
	common.FailIfErr(t, err)
	if len(pillars) != 1 {
		t.Fatalf("GetPillarsList returned %d pillars, want 1", len(pillars))
	}
}

func TestPillarJail_KeyIsPrefixSevenAndNameHash(t *testing.T) {
	want := common.JoinBytes([]byte{7}, types.NewHash([]byte("pillar-a")).Bytes())
	if got := getPillarJailKey("pillar-a"); !bytes.Equal(got, want) {
		t.Fatalf("got key %x, want %x", got, want)
	}
}
