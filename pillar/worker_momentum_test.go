package pillar

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common"
	"github.com/zenon-network/go-zenon/common/db"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/vm/constants"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// multisigAddr builds a deterministic, distinct multisig-class address (first byte
// types.MultisigAddrByte) for filter tests without needing a real creation event.
func multisigAddr(tag byte) types.Address {
	var addr types.Address
	addr[0] = types.MultisigAddrByte
	addr[1] = tag
	return addr
}

type policyKey struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

// genPolicy builds a valid, canonicalised n-signer/threshold policy and returns the private keys
// alongside it, for signing test blocks.
func genPolicy(t *testing.T, n int, threshold uint8) (definition.MultisigPolicy, []policyKey) {
	t.Helper()
	keys := make([]policyKey, n)
	pubs := make([]ed25519.PublicKey, n)
	for i := 0; i < n; i++ {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		keys[i] = policyKey{pub: pub, priv: priv}
		pubs[i] = pub
	}
	p := definition.MultisigPolicy{Threshold: threshold, Signers: pubs}
	if err := definition.ValidPolicy(&p); err != nil {
		t.Fatal(err)
	}
	return p, keys
}

// signBlock computes block.Hash and attaches a MultisigAuth with exactly policy.Threshold
// signatures over that hash, from the matching keys in the given pool.
func signBlock(t *testing.T, block *nom.AccountBlock, policy definition.MultisigPolicy, keys []policyKey) {
	t.Helper()
	block.Hash = block.ComputeHash()
	sigs := make([][]byte, 0, policy.Threshold)
	for i := 0; i < int(policy.Threshold); i++ {
		pub := policy.Signers[i]
		for _, k := range keys {
			if string(k.pub) == string(pub) {
				sigs = append(sigs, ed25519.Sign(k.priv, block.Hash.Bytes()))
				break
			}
		}
	}
	block.MultisigAuth = &nom.MultisigAuth{Signatures: sigs}
}

// TestFilterExpiredMultisigBlocks_Contiguity is the regression guard for the poison-chain halt: a
// stale multisig parent (MomentumAcknowledged below the recency floor) followed by a still-recent
// child on the SAME account. Filtering only the parent would orphan the child, and content()'s
// previous check would then reject the whole momentum every attempt — the exact permanent halt
// this filter exists to prevent. Dropping must therefore be contiguous per account: once a block
// is dropped, every higher block on that account is dropped too.
func TestFilterExpiredMultisigBlocks_Contiguity(t *testing.T) {
	// previousHeight=100 => momentumHeight=101. A multisig block is expired iff
	// MomentumAcknowledged.Height + MultisigMaxMaLag < 101.
	const previousHeight = uint64(100)
	staleMA := 101 - constants.MultisigMaxMaLag - 1 // expired: staleMA+lag == 100 < 101
	freshMA := uint64(100)                          // recent: freshMA+lag well above 101

	msA := multisigAddr(0xAA)
	msB := multisigAddr(0xBB)
	user := types.ParseAddressPanic("z1qzal6c5s9rjnnxd2z7dvdhjxpmmj4fmw56a0mz")

	ma := func(h uint64) types.HashHeight { return types.HashHeight{Height: h} }

	registry := db.DisableNotFound(db.NewMemDB())
	policyA, keysA := genPolicy(t, 3, 2)
	if err := definition.SaveMultisigRecord(registry, msA, &definition.MultisigRecord{Active: policyA}); err != nil {
		t.Fatal(err)
	}
	policyB, keysB := genPolicy(t, 3, 2)
	if err := definition.SaveMultisigRecord(registry, msB, &definition.MultisigRecord{Active: policyB}); err != nil {
		t.Fatal(err)
	}

	// Stale parent + fresh child on the same account: both must be dropped (the stale parent short
	// circuits the auth check for both, since dropping is cascading).
	poisonChain := []*nom.AccountBlock{
		{Address: msA, Height: 1, MomentumAcknowledged: ma(staleMA)},
		{Address: msA, Height: 2, MomentumAcknowledged: ma(freshMA)},
	}
	common.Expect(t, len(filterExpiredMultisigBlocks(poisonChain, previousHeight, registry)), 0)

	// A wholly-recent, validly-signed multisig account is untouched.
	recentChain := []*nom.AccountBlock{
		{Address: msA, Height: 1, MomentumAcknowledged: ma(freshMA)},
		{Address: msA, Height: 2, MomentumAcknowledged: ma(freshMA)},
	}
	for _, b := range recentChain {
		signBlock(t, b, policyA, keysA)
	}
	common.Expect(t, len(filterExpiredMultisigBlocks(recentChain, previousHeight, registry)), 2)

	// Non-multisig blocks are never dropped, even next to a poisoned multisig account.
	mixed := []*nom.AccountBlock{
		{Address: user, Height: 1, MomentumAcknowledged: ma(staleMA)},
		{Address: msA, Height: 1, MomentumAcknowledged: ma(staleMA)},
		{Address: msA, Height: 2, MomentumAcknowledged: ma(freshMA)},
		{Address: user, Height: 2, MomentumAcknowledged: ma(staleMA)},
	}
	gotMixed := filterExpiredMultisigBlocks(mixed, previousHeight, registry)
	common.Expect(t, len(gotMixed), 2)
	for _, b := range gotMixed {
		if b.Address != user {
			t.Fatalf("expected only the user account's blocks to survive, got a %v block", b.Address)
		}
	}

	// Poisoning is per account: a stale msA parent must not evict an unrelated recent, validly
	// signed msB block, even when the accounts' blocks are interleaved.
	msBBlock := &nom.AccountBlock{Address: msB, Height: 1, MomentumAcknowledged: ma(freshMA)}
	signBlock(t, msBBlock, policyB, keysB)
	interleaved := []*nom.AccountBlock{
		{Address: msA, Height: 1, MomentumAcknowledged: ma(staleMA)},
		msBBlock,
		{Address: msA, Height: 2, MomentumAcknowledged: ma(freshMA)},
	}
	gotInter := filterExpiredMultisigBlocks(interleaved, previousHeight, registry)
	common.Expect(t, len(gotInter), 1)
	common.Expect(t, gotInter[0].Address, msB)
}

// TestFilterExpiredMultisigBlocks_LiveAuth_Cascade covers the live-auth drop reason (#4): a
// recent-MA block whose signatures no longer satisfy the currently active (rotated) policy is
// dropped, and cascades to every later block on that account, while an unrelated account's validly
// signed block survives.
func TestFilterExpiredMultisigBlocks_LiveAuth_Cascade(t *testing.T) {
	const previousHeight = uint64(100)
	freshMA := uint64(100)

	msA := multisigAddr(0xAA)
	msB := multisigAddr(0xBB)
	ma := func(h uint64) types.HashHeight { return types.HashHeight{Height: h} }

	registry := db.DisableNotFound(db.NewMemDB())
	oldPolicy, oldKeys := genPolicy(t, 3, 2)
	rotatedPolicy, rotatedKeys := genPolicy(t, 3, 2)
	// msA's active policy has rotated away from oldPolicy.
	if err := definition.SaveMultisigRecord(registry, msA, &definition.MultisigRecord{Active: rotatedPolicy}); err != nil {
		t.Fatal(err)
	}
	policyB, keysB := genPolicy(t, 3, 2)
	if err := definition.SaveMultisigRecord(registry, msB, &definition.MultisigRecord{Active: policyB}); err != nil {
		t.Fatal(err)
	}

	// blockA1 is signed under the now-superseded oldPolicy: fails live auth against rotatedPolicy.
	blockA1 := &nom.AccountBlock{Address: msA, Height: 1, MomentumAcknowledged: ma(freshMA)}
	signBlock(t, blockA1, oldPolicy, oldKeys)
	// blockA2 is validly signed under the CURRENT active policy, but must still be dropped: it
	// descends from the already-dropped blockA1, so cascading contiguity applies.
	blockA2 := &nom.AccountBlock{Address: msA, Height: 2, MomentumAcknowledged: ma(freshMA)}
	signBlock(t, blockA2, rotatedPolicy, rotatedKeys)

	blockB := &nom.AccountBlock{Address: msB, Height: 1, MomentumAcknowledged: ma(freshMA)}
	signBlock(t, blockB, policyB, keysB)

	blocks := []*nom.AccountBlock{blockA1, blockB, blockA2}
	got := filterExpiredMultisigBlocks(blocks, previousHeight, registry)
	common.Expect(t, len(got), 1)
	common.Expect(t, got[0].Address, msB)
}
