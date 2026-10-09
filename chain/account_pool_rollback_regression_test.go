package chain

import (
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/chain/store"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
)

// These two tests are the permanent regression tests requested by edgepillar
// in review of PR #115.  They are deliberately written against the pool's
// observable state (frontier identifier and actual DB contents) rather than
// membership in manager.blocks: the earlier suffix tests built transactions
// with empty application patches, so they could not distinguish "the address
// was kept" from "the address was kept with its state intact".

const (
	testAppKeyK1     = "testapp/k1"
	testAppKeyK2     = "testapp/k2"
	testAppKeyShared = "testapp/shared"
)

// readValue returns the value at key in the manager's frontier DB, or "" when
// the key is absent.
func readValue(t *testing.T, manager *accountManager, key string) string {
	t.Helper()
	if manager == nil {
		return ""
	}
	value, err := manager.db.Frontier().Get([]byte(key))
	if err != nil {
		// A missing key is the interesting case for the stale-suffix
		// assertions, so it is not an error here.
		return ""
	}
	return string(value)
}

// applyState writes a non-empty application patch through the accountManager,
// so the frontier DB carries real state rather than an empty patch.
func applyState(t *testing.T, manager *accountManager, block *nom.AccountBlock, writes map[string]string) {
	t.Helper()
	patch := db.NewPatch()
	for key, value := range writes {
		patch.Put([]byte(key), []byte(value))
	}
	if err := manager.Add(&nom.AccountBlockTransaction{Block: block, Changes: patch}); err != nil {
		t.Fatalf("failed to add block %v: %v", block.Header(), err)
	}
}

// TestDeleteMomentumKeepsPrefixApplicationState is regression test 1 from
// review: after DeleteMomentum removes the stale suffix, the retained valid
// prefix must still be the frontier AND carry its actual application state,
// including the shared key reverting to the prefix's value.  It must hold both
// immediately after DeleteMomentum and after the subsequent rebuild.
//
// Against the pre-PR implementation this test fails because the whole address
// is dropped, taking the prefix with it.
func TestDeleteMomentumKeepsPrefixApplicationState(t *testing.T) {
	address := types.Address{0, 1}
	ap := newAccountPool(fakeStable{})
	manager := ap.getAccountManager(address)

	// b1 acknowledges momentum 5, which survives the rollback to below 10.
	b1 := &nom.AccountBlock{
		Address:              address,
		BlockType:            nom.BlockTypeUserSend,
		Height:               1,
		TotalPlasma:          100,
		BasePlasma:           100,
		MomentumAcknowledged: types.HashHeight{Hash: types.Hash{1}, Height: 5},
	}
	b1.Hash = b1.ComputeHash()
	applyState(t, manager, b1, map[string]string{
		testAppKeyK1:     "v1",
		testAppKeyShared: "b1",
	})

	// b2 acknowledges momentum 10, the momentum being rolled back.
	b2 := &nom.AccountBlock{
		Address:              address,
		BlockType:            nom.BlockTypeUserSend,
		Height:               2,
		PreviousHash:         b1.Hash,
		TotalPlasma:          100,
		BasePlasma:           100,
		MomentumAcknowledged: types.HashHeight{Hash: types.Hash{2}, Height: 10},
	}
	b2.Hash = b2.ComputeHash()
	applyState(t, manager, b2, map[string]string{
		testAppKeyK2:     "v2",
		testAppKeyShared: "b2",
	})

	ap.DeleteMomentum(&nom.DetailedMomentum{
		Momentum: &nom.Momentum{Height: 10, Hash: types.Hash{10}},
	})

	assertPrefixState := func(stage string) {
		t.Helper()
		m := ap.managers[address]
		if m == nil {
			t.Fatalf("%s: address manager was dropped entirely; the valid prefix (acknowledging momentum 5) was evicted with the stale suffix", stage)
		}
		if got := db.GetFrontierIdentifier(m.db.Frontier()); got != b1.Identifier() {
			t.Fatalf("%s: frontier = %v, want the retained prefix %v", stage, got, b1.Identifier())
		}
		if got := readValue(t, m, testAppKeyK1); got != "v1" {
			t.Errorf("%s: %s = %q, want %q — prefix application state was lost", stage, testAppKeyK1, got, "v1")
		}
		if got := readValue(t, m, testAppKeyShared); got != "b1" {
			t.Errorf("%s: %s = %q, want %q — stale-suffix effect survived truncation", stage, testAppKeyShared, got, "b1")
		}
		if got := readValue(t, m, testAppKeyK2); got != "" {
			t.Errorf("%s: %s = %q, want absent — evicted suffix left state behind", stage, testAppKeyK2, got)
		}
	}

	assertPrefixState("after DeleteMomentum")

	// The subsequent rebuild (InsertMomentum) must preserve the same state.
	if err := ap.rebuild(&nom.DetailedMomentum{
		Momentum: &nom.Momentum{Height: 9, Hash: types.Hash{9}},
	}); err != nil {
		t.Fatalf("rebuild after DeleteMomentum returned an error: %v", err)
	}

	assertPrefixState("after rebuild")
}

// mapStable hands out a caller-controlled stable DB per address, so a test can
// point one address at a frontier that makes its rebuild fail.
type mapStable struct {
	dbs map[types.Address]db.DB
}

func (s mapStable) GetStableAccountDB(address types.Address) db.DB {
	if existing, ok := s.dbs[address]; ok {
		return existing
	}
	fresh := db.NewMemDB()
	s.dbs[address] = fresh
	return fresh
}

func (s mapStable) GetFrontierMomentumStore() store.Momentum {
	return nil
}

// TestRebuildFailureIsolatesFailingAddress is regression test 2 from review: a
// rebuild failure for one address must discard only that address's manager,
// return the error, and still rebuild every other address regardless of map
// iteration order.
//
// The failure is induced by pointing the failing address's stable DB at a
// frontier block that differs from the one its pending chain starts after, so
// re-applying the chain cannot match its previous hash.
//
// Repeated runs are evidence over sampled iteration orders, not proof over
// all of them — the same caveat the reviewer attached.
func TestRebuildFailureIsolatesFailingAddress(t *testing.T) {
	const (
		addressCount   = 21
		failingIndex   = 7
		repeatRuns     = 50
		rollbackHeight = 10
	)

	for run := 0; run < repeatRuns; run++ {
		stable := mapStable{dbs: map[types.Address]db.DB{}}
		ap := newAccountPool(stable)

		failingAddress := types.Address{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
		addresses := make([]types.Address, 0, addressCount)
		for i := 0; i < addressCount; i++ {
			addresses = append(addresses, types.Address{byte(i + 1)})
		}
		addresses[failingIndex] = failingAddress

		// Record each address's pre-rebuild manager instance.  Block count and
		// frontier state alone cannot tell a rebuilt manager from a retained
		// pre-rebuild one — both hold the same two blocks — so the assertions
		// below compare instance identity as well.
		preRebuild := make(map[types.Address]*accountManager, len(addresses))

		for _, address := range addresses {
			manager := ap.getAccountManager(address)
			preRebuild[address] = manager
			first := &nom.AccountBlock{
				Address:     address,
				BlockType:   nom.BlockTypeUserSend,
				Height:      1,
				TotalPlasma: 100,
				BasePlasma:  100,
			}
			first.Hash = first.ComputeHash()
			applyState(t, manager, first, map[string]string{testAppKeyK1: "v1"})

			second := &nom.AccountBlock{
				Address:      address,
				BlockType:    nom.BlockTypeUserSend,
				Height:       2,
				PreviousHash: first.Hash,
				TotalPlasma:  100,
				BasePlasma:   100,
			}
			second.Hash = second.ComputeHash()
			applyState(t, manager, second, map[string]string{testAppKeyK2: "v2"})
		}

		// Corrupt the failing address's stable frontier: same height as its
		// first pending block, different hash.  The stored data must still be
		// a serialized account block — the account store deserializes the
		// frontier before rebuild compares hashes.
		foreignBlock := &nom.AccountBlock{
			Address:     failingAddress,
			BlockType:   nom.BlockTypeUserSend,
			Height:      1,
			TotalPlasma: 100,
			BasePlasma:  100,
			Data:        []byte("foreign"),
		}
		foreignBlock.Hash = foreignBlock.ComputeHash()
		foreignData, err := foreignBlock.Serialize()
		if err != nil {
			t.Fatalf("failed to serialize foreign frontier block: %v", err)
		}
		if err := db.SetFrontier(stable.GetStableAccountDB(failingAddress), foreignBlock.Identifier(), foreignData); err != nil {
			t.Fatalf("run %d: failed to seed foreign frontier: %v", run, err)
		}

		err = ap.rebuild(&nom.DetailedMomentum{
			Momentum: &nom.Momentum{Height: rollbackHeight, Hash: types.Hash{rollbackHeight}},
		})

		if err == nil {
			t.Fatalf("run %d: rebuild returned no error although address %v was seeded with an unmatchable frontier", run, failingAddress)
		}
		if m, ok := ap.managers[failingAddress]; ok && m != nil {
			t.Errorf("run %d: failing address kept a manager with %d blocks; it must be discarded", run, len(m.blocks))
		}

		for i, address := range addresses {
			if address == failingAddress {
				continue
			}
			m := ap.managers[address]
			if m == nil {
				t.Errorf("run %d: unaffected address #%d (%v) has no manager — rebuild abandoned the remaining addresses after a failure", run, i, address)
				continue
			}
			if m == preRebuild[address] {
				t.Errorf("run %d: unaffected address #%d (%v) still holds its pre-rebuild manager instance — rebuild abandoned this address after the failure at %v", run, i, address, failingAddress)
			}
			if len(m.blocks) != 2 {
				t.Errorf("run %d: unaffected address #%d (%v) has %d blocks, want 2", run, i, address, len(m.blocks))
			}
			if got := db.GetFrontierIdentifier(m.db.Frontier()); got.Height != 2 {
				t.Errorf("run %d: unaffected address #%d (%v) frontier height = %d, want 2", run, i, address, got.Height)
			}
			if got := readValue(t, m, testAppKeyK2); got != "v2" {
				t.Errorf("run %d: unaffected address #%d (%v) lost its re-applied patch: %s = %q, want %q", run, i, address, testAppKeyK2, got, "v2")
			}
		}
	}
}

// TestDeleteMomentumKeepsPrefixApplicationStateRejectsSiblingState guards the
// shared-key assertion above: it fails loudly if the helper stops reading the
// frontier DB rather than silently passing.
func TestDeleteMomentumSharedKeyAssertionIsMeaningful(t *testing.T) {
	address := types.Address{0, 9}
	ap := newAccountPool(fakeStable{})
	manager := ap.getAccountManager(address)

	block := &nom.AccountBlock{
		Address:     address,
		BlockType:   nom.BlockTypeUserSend,
		Height:      1,
		TotalPlasma: 100,
		BasePlasma:  100,
	}
	block.Hash = block.ComputeHash()
	applyState(t, manager, block, map[string]string{testAppKeyShared: "expected"})

	if got := readValue(t, manager, testAppKeyShared); got != "expected" {
		t.Fatalf("frontier DB read returned %q, want %q — the state assertions in the sibling test cannot be trusted", got, "expected")
	}
	if got := readValue(t, manager, "testapp/absent"); got != "" {
		t.Fatalf("absent key returned %q, want empty — the absence assertions in the sibling test cannot be trusted", got)
	}
}
