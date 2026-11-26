//go:build e2e

package e2e

import (
	"math/big"
	"strings"
	"testing"

	"github.com/zenon-network/go-zenon/chain/nom"
	"github.com/zenon-network/go-zenon/common/types"
	"github.com/zenon-network/go-zenon/dp"
	"github.com/zenon-network/go-zenon/rpc/api"
	"github.com/zenon-network/go-zenon/vm/embedded/definition"
)

// TestVerifierAccountRules covers the account-block verifier rules that the
// B-series in ledger_test.go does not reach: send-side amount fields, the
// momentum-acknowledged gap check, the Dynamic-Plasma-gated receive-data
// rule, and the transaction-level plasma/descendant/public-key rules. Every
// case publishes a block a correct client would never build and expects the
// pool to refuse it with the verifier's own message; refusals leave no
// trace on the chain.
//
// The forged embedded-receive cases (B-13) are gone: since #327 the RPC
// boundary refuses any block addressed to an embedded contract
// (ErrEmbeddedAddressNotAllowed, pinned by TestLedger/B-5n), so the
// sequencer, changes-hash and acknowledgement rules behind it are not
// reachable over JSON-RPC. They stay covered by the verifier and
// vm/embedded/tests unit tests.
func TestVerifierAccountRules(t *testing.T) {
	group(t)
	a := newAccount(t, 130, 50, 800, 1000)
	b := newAccount(t, 131, 10, 500, 1000)

	// momentumAt returns the momentum hash at exactly the given height.
	momentumAt := func(t *testing.T, height uint64) types.HashHeight {
		t.Helper()
		var list struct {
			List []struct {
				Hash   types.Hash `json:"hash"`
				Height uint64     `json:"height"`
			} `json:"list"`
		}
		get(t, &list, "ledger.getMomentumsByHeight", height, 1)
		if len(list.List) != 1 || list.List[0].Height != height {
			t.Fatalf("momentum at height %d: got %+v, want exactly that height", height, list.List)
		}
		return types.HashHeight{Hash: list.List[0].Hash, Height: height}
	}

	t.Run("B-10_send_amount_fields_refused", func(t *testing.T) {
		cover(t, "E6", "error", "verifier.ErrABAmountNegative", "verifier.ErrABZtsMissing", "verifier.ErrABFromBlockHashMustBeZero")
		cover(t, "E6", "verifier-rule", "accountBlockVerifier.amounts")
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Fields: func(blk *nom.AccountBlock) {
			blk.Amount = new(big.Int).Neg(units(1))
		}}), "amount can't be negative")
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Fields: func(blk *nom.AccountBlock) {
			blk.TokenStandard = types.ZeroTokenStandard
		}}), "zts is missing")
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Fields: func(blk *nom.AccountBlock) {
			blk.FromBlockHash = types.NewHash([]byte("not zero " + runTag))
		}}), "from-block-hash must be zero")
	})

	t.Run("B-11_acknowledgement_gap_refused", func(t *testing.T) {
		cover(t, "E6", "error", "verifier.ErrABMAGap")
		cover(t, "E6", "verifier-rule", "accountBlockVerifier.momentumAcknowledged")
		// The acknowledged momentum must not be older than the one the
		// previous block of this account acknowledged — that is the previous
		// block's own MomentumAcknowledged, not the height of the momentum
		// that confirmed it (confirmation can lag acknowledgement).
		var frontier *api.AccountBlock
		get(t, &frontier, "ledger.getFrontierAccountBlock", a.Address)
		if frontier == nil || frontier.ConfirmationDetail == nil {
			t.Fatalf("account %s has no confirmed frontier block", a.Address)
		}
		prevAck := frontier.MomentumAcknowledged.Height
		if prevAck < 2 {
			t.Skipf("account frontier acknowledged momentum %d, too early for a gap case", prevAck)
		}
		older := momentumAt(t, prevAck-1)
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Fields: func(blk *nom.AccountBlock) {
			blk.MomentumAcknowledged = older
		}}), "older momentum than previous")
	})

	t.Run("B-12_receive_with_data_refused", func(t *testing.T) {
		blk := transfer(t, a, b.Address, znn, units(3), nil)
		if _, err := net.WaitConfirmed(blk.Hash, wait); err != nil {
			t.Fatal(err)
		}
		withData := &Mutator{Fields: func(blk *nom.AccountBlock) {
			blk.Data = []byte{1}
		}}
		if !live["dynamic-plasma"] {
			// The rule is gated on Dynamic Plasma: before it, a receive may
			// carry data (it gets flat plasma regardless of size, which is
			// what the rule closes). Assert the off state instead of
			// skipping, so the case says something on every variant.
			recv, err := net.Receive(b, blk.Hash, withData)
			if err != nil {
				t.Fatalf("receive with data before dynamic plasma: %v (the rule must be off)", err)
			}
			if _, err := net.WaitConfirmed(recv.Hash, wait); err != nil {
				t.Fatal(err)
			}
			return
		}
		cover(t, "E7", "error", "verifier.ErrABDataMustBeEmpty")
		cover(t, "E7", "verifier-rule", "accountBlockVerifier.receiveData", "accountBlockVerifier.isDynamicPlasmaActiveAtMA")
		_, err := net.Receive(b, blk.Hash, withData)
		mustReject(t, err, "data must be empty on receive blocks")
		receiveAll(t, b, 1)
	})

	t.Run("B-14_descendant_blocks_on_a_user_send_refused", func(t *testing.T) {
		cover(t, "E6", "error", "verifier.ErrABDescendantMustBeZero")
		cover(t, "E6", "verifier-rule", "accountBlockTransactionVerifier.descendantBlocks")
		// Reaching the descendant rule needs a well-formed batch:
		// block.Previous() reports the previous of the FIRST descendant,
		// so the child must chain onto the real account frontier
		// (child Height = frontier+1, child PreviousHash = frontier hash)
		// and the head must sit one above it with PreviousHash = child
		// hash. A malformed batch dies earlier, in getContext, with
		// "previous block is missing" — a probe with a debug build
		// confirmed the height-1 underflow that produces that refusal.
		mustReject(t, tryTransfer(a, b.Address, znn, units(1), &Mutator{Fields: func(blk *nom.AccountBlock) {
			fm, err := net.Frontier()
			if err != nil {
				t.Fatal(err)
			}
			var frontier *api.AccountBlock
			get(t, &frontier, "ledger.getFrontierAccountBlock", a.Address)
			if frontier == nil {
				t.Fatalf("account %s has no frontier block", a.Address)
			}
			ack := types.HashHeight{Hash: fm.Hash, Height: fm.Height}
			child := &nom.AccountBlock{
				Version:              1,
				ChainIdentifier:      net.ChainID,
				BlockType:            nom.BlockTypeUserSend,
				Address:              a.Address,
				ToAddress:            b.Address,
				Amount:               units(1),
				TokenStandard:        znn,
				Height:               frontier.Height + 1,
				PreviousHash:         frontier.Hash,
				MomentumAcknowledged: ack,
			}
			child.Hash = child.ComputeHash()
			blk.Height = frontier.Height + 2
			blk.PreviousHash = child.Hash
			blk.MomentumAcknowledged = ack
			blk.DescendantBlocks = []*nom.AccountBlock{child}
		}}), "descendant blocks must be empty")
	})
}

// TestVerifierMomentumRules covers the momentum-side verifier rules with
// positive assertions on the live chain: every momentum a node accepted
// passed these rules when it was inserted. The walk reads the last stretch
// of momentums and re-checks each rule's invariant from RPC data, and the
// dynamic-plasma boundary is checked one momentum below, at and above the
// enforcement height from history.
//
// What a live network cannot show without producer-side fault injection —
// a forged momentum reaching an honest node — is stated instead of covered:
// the negative arms of momentum.changesHash and momentum.contentByteBound,
// and the malformed-arms of the remaining rules.
func TestVerifierMomentumRules(t *testing.T) {
	group(t)

	dpEnf, dpKnown := sporkHeights["dynamic-plasma"]

	type momentum struct {
		Hash            types.Hash          `json:"hash"`
		Height          uint64              `json:"height"`
		Version         uint64              `json:"version"`
		ChainIdentifier uint64              `json:"chainIdentifier"`
		PreviousHash    types.Hash          `json:"previousHash"`
		Timestamp       int64               `json:"timestamp"`
		Data            []byte              `json:"data"`
		Content         nom.MomentumContent `json:"content"`
		NextFusionPrice uint64              `json:"nextFusionPrice"`
		NextWorkPrice   uint64              `json:"nextWorkPrice"`
		PublicKey       []byte              `json:"publicKey"`
	}
	read := func(t *testing.T, height uint64) momentum {
		t.Helper()
		var list struct {
			List []momentum `json:"list"`
		}
		get(t, &list, "ledger.getMomentumsByHeight", height, 1)
		if len(list.List) != 1 || list.List[0].Height != height {
			t.Fatalf("momentum at height %d: got %+v, want exactly that height", height, list.List)
		}
		return list.List[0]
	}

	t.Run("B-15_walk_the_recent_momentums", func(t *testing.T) {
		// momentumTransactionVerifier.producer is deliberately not credited:
		// the walk below checks the producer is a registered pillar, not
		// the pillar elected for that momentum's slot — no RPC answers the
		// per-slot election, so that arm is pinned by the verifier unit
		// tests instead.
		cover(t, "E1", "verifier-rule", "rawMomentumVerifier.all", "momentumVerifier.getContext", "rawMomentumVerifier.chainIdentifier",
			"rawMomentumVerifier.version", "rawMomentumVerifier.timestamp", "rawMomentumVerifier.previous", "rawMomentumVerifier.data",
			"rawMomentumVerifier.nextFusionPrice", "rawMomentumVerifier.nextWorkPrice", "rawMomentumVerifier.content")
		var pillars struct {
			List []struct {
				BlockProducingAddress types.Address `json:"producerAddress"`
			} `json:"list"`
		}
		get(t, &pillars, "embedded.pillar.getAll", 0, 200)
		producers := map[types.Address]bool{}
		for _, p := range pillars.List {
			producers[p.BlockProducingAddress] = true
		}
		if len(producers) == 0 {
			t.Fatal("no pillars on this chain")
		}
		frontier, err := net.Frontier()
		if err != nil {
			t.Fatal(err)
		}
		// Walk what the chain has: on a short chain (the driver can start
		// a few momentums after genesis) a shorter stretch still exercises
		// every rule, it just says less about history.
		stretch := uint64(30)
		if frontier.Height < stretch+1 {
			stretch = frontier.Height - 1
		}
		if stretch < 2 {
			t.Fatalf("chain too short to walk: height %d", frontier.Height)
		}
		var prev *momentum
		for h := frontier.Height - stretch + 1; h <= frontier.Height; h++ {
			m := read(t, h)
			if m.ChainIdentifier != net.ChainID {
				t.Errorf("momentum %d: chain identifier %d, want %d", h, m.ChainIdentifier, net.ChainID)
			}
			wantVersion := uint64(1)
			if dpKnown && h > dpEnf {
				wantVersion = 2
			}
			if m.Version != wantVersion {
				t.Errorf("momentum %d: version %d, want %d (dp enforced at %d, known %v)", h, m.Version, wantVersion, dpEnf, dpKnown)
			}
			if len(m.Data) != 0 {
				t.Errorf("momentum %d: data is %d bytes, want empty", h, len(m.Data))
			}
			if dpKnown && h > dpEnf {
				if m.NextFusionPrice < dp.MinResourcePrice || m.NextWorkPrice < dp.MinResourcePrice {
					t.Errorf("momentum %d: prices %d/%d, want both >= %d once dynamic plasma is in force", h, m.NextFusionPrice, m.NextWorkPrice, dp.MinResourcePrice)
				}
			} else {
				if m.NextFusionPrice != 0 || m.NextWorkPrice != 0 {
					t.Errorf("momentum %d: prices %d/%d, want both zero before dynamic plasma", h, m.NextFusionPrice, m.NextWorkPrice)
				}
			}
			if prev != nil {
				if m.PreviousHash != prev.Hash {
					t.Errorf("momentum %d: previous hash %s, want %s of momentum %d", h, m.PreviousHash, prev.Hash, prev.Height)
				}
				if m.Timestamp <= prev.Timestamp {
					t.Errorf("momentum %d: timestamp %d not increasing over %d", h, m.Timestamp, prev.Timestamp)
				}
			}
			if !producers[types.PubKeyToAddress(m.PublicKey)] && m.Height != 1 {
				t.Errorf("momentum %d: producer %s is not a registered pillar", h, types.PubKeyToAddress(m.PublicKey))
			}
			for _, entry := range m.Content {
				var blk *api.AccountBlock
				get(t, &blk, "ledger.getAccountBlockByHash", entry.Hash)
				if blk == nil || blk.ConfirmationDetail == nil || blk.ConfirmationDetail.MomentumHeight != m.Height {
					t.Errorf("momentum %d content entry %s does not resolve to a block confirmed by this momentum", h, entry.Hash)
				}
			}
			prev = &m
		}
	})

	t.Run("B-16_acknowledgement_recency_of_content_blocks", func(t *testing.T) {
		if !live["dynamic-plasma"] {
			// The rule is in force only once Dynamic Plasma is enforced at
			// the parent momentum. Assert the off state instead of skipping
			// or passing vacuously: a first block acknowledging a momentum
			// from before every spork is accepted. It must be a send — a
			// receive's acknowledgement is pinned to its send's confirmation
			// height by fromHash, and an established chain's by the gap rule,
			// so only a first send is free to acknowledge an old momentum.
			//
			// The send pays with proof of work, not fused plasma: the plasma
			// check reads the account's fusion at the acknowledged momentum,
			// and a fresh account has none at genesis — fusion confirmed
			// later does not exist there. Work is stateless, so it is valid
			// at any acknowledgement.
			c := net.Key(runBase + 140)
			funder := net.Key(IdxFunder)
			genesis := read(t, 1)
			sent, err := net.Send(c, &nom.AccountBlock{BlockType: nom.BlockTypeUserSend, ToAddress: funder.Address, TokenStandard: znn, Amount: units(0)}, &Mutator{Fields: func(blk *nom.AccountBlock) {
				blk.MomentumAcknowledged = types.HashHeight{Hash: genesis.Hash, Height: 1}
			}})
			if err != nil {
				t.Fatalf("first send acknowledging momentum 1 before dynamic plasma: %v (the recency rule must be off)", err)
			}
			// Pool acceptance alone is not the off state: the momentum-side
			// arm of the rule must also let the block confirm.
			if _, err := net.WaitConfirmed(sent.Hash, wait); err != nil {
				t.Fatalf("off-state send acknowledging momentum 1 not confirmed: %v (the recency rule must be off)", err)
			}
			return
		}
		cover(t, "E1", "verifier-rule", "rawMomentumVerifier.acknowledgementRecency")
		// Every user block confirmed by a momentum whose parent enforces
		// Dynamic Plasma must acknowledge a momentum at which every spork
		// enforced at that parent is already enforced — the momentum-side
		// arm of the acknowledgement-recency rule (#280). The requirement
		// is built from the parent (verifier/momentum.go passes
		// rmv.momentumStore, the store at m.Height-1), so a spork enforced
		// exactly at m.Height is not yet required of m's blocks.
		frontier, err := net.Frontier()
		if err != nil {
			t.Fatal(err)
		}
		checked := 0
		for depth := uint64(0); depth < 200 && frontier.Height-depth > dpEnf; depth++ {
			m := read(t, frontier.Height-depth)
			for _, entry := range m.Content {
				var blk *api.AccountBlock
				get(t, &blk, "ledger.getAccountBlockByHash", entry.Hash)
				if blk == nil || types.IsEmbeddedAddress(blk.Address) {
					continue
				}
				requirement := uint64(0)
				for _, h := range sporkHeights {
					if h <= m.Height-1 && h > requirement {
						requirement = h
					}
				}
				if blk.MomentumAcknowledged.Height < requirement {
					t.Errorf("block %s in momentum %d acknowledges momentum %d below the highest spork enforced at its parent %d",
						blk.Hash, m.Height, blk.MomentumAcknowledged.Height, requirement)
				}
				checked++
			}
			if checked > 0 {
				break // one momentum with user content is enough
			}
		}
		if checked == 0 {
			// A quiet devnet can have no user block in the window; make
			// one and check it the same way, so the case can neither pass
			// nor fail vacuously.
			funder := net.Key(IdxFunder)
			res := callOK(t, funder, types.PlasmaContract, definition.ABIPlasma.PackMethodPanic(definition.FuseMethodName, funder.Address), qsr, coins(10))
			m := read(t, res.Send.ConfirmationDetail.MomentumHeight)
			requirement := uint64(0)
			for _, h := range sporkHeights {
				if h <= m.Height-1 && h > requirement {
					requirement = h
				}
			}
			if res.Send.MomentumAcknowledged.Height < requirement {
				t.Errorf("fresh block %s in momentum %d acknowledges momentum %d below the highest spork enforced at its parent %d",
					res.Send.Hash, m.Height, res.Send.MomentumAcknowledged.Height, requirement)
			}
		}
	})

	t.Run("B-17_dynamic_plasma_boundary_from_history", func(t *testing.T) {
		if !dpKnown {
			// No Dynamic Plasma boundary on this chain: the pre-spork
			// invariant must hold at every momentum — version 1, zero
			// resource prices. Assert that instead of skipping, so the case
			// says something on every variant.
			frontier, err := net.Frontier()
			if err != nil {
				t.Fatal(err)
			}
			stretch := uint64(30)
			if frontier.Height < stretch+1 {
				stretch = frontier.Height - 1
			}
			if stretch < 1 {
				t.Fatalf("chain too short to walk: height %d", frontier.Height)
			}
			for h := frontier.Height - stretch + 1; h <= frontier.Height; h++ {
				m := read(t, h)
				if m.Version != 1 || m.NextFusionPrice != 0 || m.NextWorkPrice != 0 {
					t.Errorf("momentum %d: version %d prices %d/%d, want v1 0/0 with no dynamic plasma on this chain", h, m.Version, m.NextFusionPrice, m.NextWorkPrice)
				}
			}
			return
		}
		cover(t, "E7", "verifier-rule", "rawMomentumVerifier.version", "rawMomentumVerifier.nextFusionPrice", "rawMomentumVerifier.nextWorkPrice")
		// The version switch lands one momentum late: a producer builds
		// momentum N on its parent N-1's spork state, so at the enforcement
		// height the momentum is still Version 1 with zero prices. With the
		// spork enforced at height 0 or 1 there is no momentum below the
		// boundary to read (and dpEnf-1 would underflow); the heights above
		// it follow the B-15 rule — version 2 past the enforcement.
		heights := []uint64{}
		if dpEnf >= 2 {
			heights = append(heights, dpEnf-1)
		}
		if dpEnf >= 1 {
			heights = append(heights, dpEnf)
		}
		heights = append(heights, dpEnf+1)
		for _, h := range heights {
			m := read(t, h)
			wantVersion, wantPriced := uint64(1), false
			if h > dpEnf {
				wantVersion, wantPriced = 2, true
			}
			if m.Version != wantVersion {
				t.Errorf("momentum %d: version %d, want %d", h, m.Version, wantVersion)
			}
			if wantPriced {
				if m.NextFusionPrice < dp.MinResourcePrice || m.NextWorkPrice < dp.MinResourcePrice {
					t.Errorf("momentum %d: prices %d/%d, want both >= %d", h, m.NextFusionPrice, m.NextWorkPrice, dp.MinResourcePrice)
				}
			} else if m.NextFusionPrice != 0 || m.NextWorkPrice != 0 {
				t.Errorf("momentum %d: prices %d/%d, want both zero before the switch", h, m.NextFusionPrice, m.NextWorkPrice)
			}
		}
	})
}

func contains(err error, sub string) bool {
	return err != nil && strings.Contains(err.Error(), sub)
}
