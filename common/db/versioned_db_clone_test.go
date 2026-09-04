package db

import (
	"encoding/hex"
	"testing"

	"github.com/zenon-network/go-zenon/common"
)

func TestMemDBManagerCloneDivergesIndependently(t *testing.T) {
	original := NewMemDBManager(NewMemDB())
	t1 := newMockTransaction(1, original.Frontier())
	common.FailIfErr(t, original.Add(t1))
	t1Patch := original.GetPatch(t1.commit.Identifier()).Dump()

	clone := original.(Cloneable).Clone()
	common.Expect(t, GetFrontierIdentifier(clone.Frontier()), t1.commit.Identifier())
	common.ExpectBytes(t, clone.GetPatch(t1.commit.Identifier()).Dump(), "0x"+hex.EncodeToString(t1Patch))

	// Mutating the original after cloning must not show up in the clone.
	t2 := newMockTransaction(2, original.Frontier())
	common.FailIfErr(t, original.Add(t2))
	common.Expect(t, GetFrontierIdentifier(original.Frontier()), t2.commit.Identifier())
	common.Expect(t, GetFrontierIdentifier(clone.Frontier()), t1.commit.Identifier())
	if clone.GetPatch(t2.commit.Identifier()) != nil {
		t.Fatal("clone sees a transaction added to the original")
	}
	if clone.Get(t2.commit.Identifier()) != nil {
		t.Fatal("clone sees a version added to the original")
	}

	// Popping the original back past the shared transaction must not remove
	// it from the clone, and the clone's copy must be unchanged.
	common.FailIfErr(t, original.Pop())
	common.FailIfErr(t, original.Pop())
	common.Expect(t, GetFrontierIdentifier(original.Frontier()), GetFrontierIdentifier(NewMemDB()))
	common.Expect(t, GetFrontierIdentifier(clone.Frontier()), t1.commit.Identifier())
	common.ExpectBytes(t, clone.GetPatch(t1.commit.Identifier()).Dump(), "0x"+hex.EncodeToString(t1Patch))

	// Mutating the clone must not show up in the original.
	t3 := newMockTransaction(3, clone.Frontier())
	common.FailIfErr(t, clone.Add(t3))
	common.Expect(t, GetFrontierIdentifier(clone.Frontier()), t3.commit.Identifier())
	if original.GetPatch(t3.commit.Identifier()) != nil {
		t.Fatal("original sees a transaction added to the clone")
	}
	if original.Get(t1.commit.Identifier()) != nil {
		t.Fatal("original still holds a popped version")
	}
}
