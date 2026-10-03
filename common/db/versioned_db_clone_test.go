package db

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/zenon-network/go-zenon/common"
)

func TestMemDBManagerCloneDivergesIndependently(t *testing.T) {
	original := NewMemDBManager(NewMemDB())
	t1 := newMockTransaction(1, original.Frontier())
	common.FailIfErr(t, original.Add(t1))
	// Capture the expected bytes as an immutable string before anything else
	// touches the manager: Dump returns the patch's own buffer.
	t1PatchHex := "0x" + hex.EncodeToString(original.GetPatch(t1.commit.Identifier()).Dump())

	clone := original.(Cloneable).Clone()
	common.Expect(t, GetFrontierIdentifier(clone.Frontier()), t1.commit.Identifier())
	common.ExpectBytes(t, clone.GetPatch(t1.commit.Identifier()).Dump(), t1PatchHex)

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
	common.ExpectBytes(t, clone.GetPatch(t1.commit.Identifier()).Dump(), t1PatchHex)

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

// TestPatchDumpBaselineIsIndependentOfLaterMutation is the control for the
// baselines above. It takes both kinds of baseline the tests use, a copied
// slice and a hex string, plus the aliased slice they replaced, then alters
// one byte through the buffer Dump hands out: the aliased slice must follow
// the change (so a baseline built from it afterwards could never fail), while
// the copy and the string must stay put and the comparisons must now fail.
func TestPatchDumpBaselineIsIndependentOfLaterMutation(t *testing.T) {
	m := NewMemDBManager(NewMemDB())
	t1 := newMockTransaction(1, m.Frontier())
	common.FailIfErr(t, m.Add(t1))
	patch := m.GetPatch(t1.commit.Identifier())

	aliased := patch.Dump()
	copied := append([]byte(nil), patch.Dump()...)
	expectedHex := hex.EncodeToString(patch.Dump())
	if len(copied) == 0 {
		t.Fatal("patch has no bytes to compare")
	}

	// Alter one serialized byte through the buffer Dump hands out.
	patch.Dump()[0] ^= 0xff

	if patch.Dump()[0] != copied[0]^0xff {
		t.Fatal("Dump did not expose the patch's own buffer; the control proves nothing")
	}
	if hex.EncodeToString(aliased) != hex.EncodeToString(patch.Dump()) {
		t.Fatal("the aliased slice did not follow the mutation; the control proves nothing")
	}
	common.ExpectString(t, hex.EncodeToString(copied), expectedHex)
	if bytes.Equal(copied, patch.Dump()) {
		t.Fatal("the copied baseline did not detect the altered patch bytes")
	}
	if hex.EncodeToString(patch.Dump()) == expectedHex {
		t.Fatal("the string baseline did not detect the altered patch bytes")
	}
}
